package ingestion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"enterprise-go-rag/internal/contracts"
)

type QueueProviderConfig struct {
	Provider          string
	AWSRegion         string
	QueueURL          string
	DLQURL            string
	MessageGroupID    string
	LocalFallbackMode bool
}

func LoadQueueProviderConfigFromEnv(lookupEnv func(string) (string, bool)) QueueProviderConfig {
	provider := "memory"
	if v, ok := lookupEnv("FINE_RAG_QUEUE_PROVIDER"); ok && strings.TrimSpace(v) != "" {
		provider = strings.ToLower(strings.TrimSpace(v))
	}
	fallback := true
	if v, ok := lookupEnv("FINE_RAG_QUEUE_LOCAL_FALLBACK"); ok && strings.EqualFold(strings.TrimSpace(v), "false") {
		fallback = false
	}
	messageGroupID := strings.TrimSpace(getEnv(lookupEnv, "FINE_RAG_SQS_MESSAGE_GROUP_ID"))
	if messageGroupID == "" {
		messageGroupID = "ingestion-jobs"
	}
	return QueueProviderConfig{
		Provider:          provider,
		AWSRegion:         strings.TrimSpace(getEnv(lookupEnv, "AWS_REGION")),
		QueueURL:          strings.TrimSpace(getEnv(lookupEnv, "FINE_RAG_SQS_QUEUE_URL")),
		DLQURL:            strings.TrimSpace(getEnv(lookupEnv, "FINE_RAG_SQS_DLQ_URL")),
		MessageGroupID:    messageGroupID,
		LocalFallbackMode: fallback,
	}
}

func (c QueueProviderConfig) Validate() error {
	switch c.Provider {
	case "", "memory":
		return nil
	case "sqs":
		if c.AWSRegion == "" {
			return errors.New("AWS_REGION is required when FINE_RAG_QUEUE_PROVIDER=sqs")
		}
		if c.QueueURL == "" {
			return errors.New("FINE_RAG_SQS_QUEUE_URL is required when FINE_RAG_QUEUE_PROVIDER=sqs")
		}
		if c.DLQURL == "" {
			return errors.New("FINE_RAG_SQS_DLQ_URL is required when FINE_RAG_QUEUE_PROVIDER=sqs")
		}
		return nil
	default:
		return fmt.Errorf("unsupported FINE_RAG_QUEUE_PROVIDER %q", c.Provider)
	}
}

type InMemoryQueueAdapter struct {
	mu       sync.Mutex
	messages []contracts.QueueMessage
	dlq      []contracts.QueueMessage
}

func NewInMemoryQueueAdapter() *InMemoryQueueAdapter {
	return &InMemoryQueueAdapter{}
}

func (q *InMemoryQueueAdapter) Enqueue(_ context.Context, message contracts.QueueMessage) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.messages = append(q.messages, message)
	return nil
}

func (q *InMemoryQueueAdapter) Dequeue(_ context.Context) (contracts.QueueMessage, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.messages) == 0 {
		return contracts.QueueMessage{}, ErrQueueEmpty
	}
	msg := q.messages[0]
	q.messages = q.messages[1:]
	return msg, nil
}

func (q *InMemoryQueueAdapter) Publish(_ context.Context, message contracts.QueueMessage, _ string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.dlq = append(q.dlq, message)
	return nil
}

type SQSSendInput struct {
	QueueURL               string
	Body                   string
	MessageGroupID         string
	MessageDeduplicationID string
}

type SQSReceiveInput struct {
	QueueURL            string
	MaxNumberOfMessages int
	WaitTimeSeconds     int
}

type SQSMessage struct {
	MessageID     string
	Body          string
	ReceiptHandle string
}

type SQSClient interface {
	SendMessage(ctx context.Context, in SQSSendInput) (string, error)
	ReceiveMessages(ctx context.Context, in SQSReceiveInput) ([]SQSMessage, error)
	DeleteMessage(ctx context.Context, queueURL string, receiptHandle string) error
}

type SQSQueueAdapter struct {
	client SQSClient
	cfg    QueueProviderConfig
}

func NewSQSQueueAdapter(client SQSClient, cfg QueueProviderConfig) (*SQSQueueAdapter, error) {
	if client == nil {
		return nil, errors.New("sqs client is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &SQSQueueAdapter{client: client, cfg: cfg}, nil
}

func (a *SQSQueueAdapter) Enqueue(ctx context.Context, message contracts.QueueMessage) error {
	if err := message.Validate(); err != nil {
		return contracts.WrapValidationErr("queue_message", err)
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	dedupID := message.IdempotencyKey
	if dedupID == "" {
		dedupID = message.MessageID
	}
	_, err = a.client.SendMessage(ctx, SQSSendInput{
		QueueURL:               a.cfg.QueueURL,
		Body:                   string(payload),
		MessageGroupID:         a.cfg.MessageGroupID,
		MessageDeduplicationID: dedupID,
	})
	return err
}

func (a *SQSQueueAdapter) Dequeue(ctx context.Context) (contracts.QueueMessage, error) {
	messages, err := a.client.ReceiveMessages(ctx, SQSReceiveInput{
		QueueURL:            a.cfg.QueueURL,
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     5,
	})
	if err != nil {
		return contracts.QueueMessage{}, err
	}
	if len(messages) == 0 {
		return contracts.QueueMessage{}, ErrQueueEmpty
	}

	raw := messages[0]
	var decoded contracts.QueueMessage
	if err := json.Unmarshal([]byte(raw.Body), &decoded); err != nil {
		return contracts.QueueMessage{}, fmt.Errorf("decode sqs message %s: %w", raw.MessageID, err)
	}
	if decoded.EnqueuedAt.IsZero() {
		decoded.EnqueuedAt = time.Now().UTC().Round(0)
	}
	if decoded.MessageID == "" {
		decoded.MessageID = raw.MessageID
	}
	if err := a.client.DeleteMessage(ctx, a.cfg.QueueURL, raw.ReceiptHandle); err != nil {
		return contracts.QueueMessage{}, err
	}
	return decoded, nil
}

func (a *SQSQueueAdapter) Publish(ctx context.Context, message contracts.QueueMessage, reason string) error {
	if err := message.Validate(); err != nil {
		return contracts.WrapValidationErr("queue_message", err)
	}
	wrapped := map[string]any{
		"message": message,
		"reason":  reason,
	}
	payload, err := json.Marshal(wrapped)
	if err != nil {
		return err
	}
	dedupID := message.IdempotencyKey
	if dedupID == "" {
		dedupID = message.MessageID
	}
	_, err = a.client.SendMessage(ctx, SQSSendInput{
		QueueURL:               a.cfg.DLQURL,
		Body:                   string(payload),
		MessageGroupID:         a.cfg.MessageGroupID,
		MessageDeduplicationID: dedupID,
	})
	return err
}

func BuildQueueAdapters(cfg QueueProviderConfig, sqsClient SQSClient, fallback *InMemoryQueueAdapter) (contracts.IngestionQueueProducer, contracts.IngestionQueueConsumer, contracts.DeadLetterQueue, string, error) {
	if fallback == nil {
		fallback = NewInMemoryQueueAdapter()
	}
	if cfg.Provider == "" || cfg.Provider == "memory" {
		return fallback, fallback, fallback, "memory", nil
	}
	if cfg.Provider != "sqs" {
		return nil, nil, nil, "", fmt.Errorf("unsupported queue provider %q", cfg.Provider)
	}
	if err := cfg.Validate(); err != nil {
		if cfg.LocalFallbackMode {
			return fallback, fallback, fallback, "memory-fallback", nil
		}
		return nil, nil, nil, "", err
	}
	adapter, err := NewSQSQueueAdapter(sqsClient, cfg)
	if err != nil {
		if cfg.LocalFallbackMode {
			return fallback, fallback, fallback, "memory-fallback", nil
		}
		return nil, nil, nil, "", err
	}
	return adapter, adapter, adapter, "sqs", nil
}

func getEnv(lookupEnv func(string) (string, bool), key string) string {
	if lookupEnv == nil {
		return ""
	}
	v, _ := lookupEnv(key)
	return v
}
