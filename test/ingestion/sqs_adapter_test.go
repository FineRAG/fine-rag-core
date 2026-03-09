package ingestion_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"enterprise-go-rag/internal/contracts"
	"enterprise-go-rag/internal/services/ingestion"
)

type stubSQSClient struct {
	sent      []ingestion.SQSSendInput
	received  []ingestion.SQSMessage
	deleted   []string
	sendErr   error
	recvErr   error
	deleteErr error
}

func (s *stubSQSClient) SendMessage(_ context.Context, in ingestion.SQSSendInput) (string, error) {
	if s.sendErr != nil {
		return "", s.sendErr
	}
	s.sent = append(s.sent, in)
	return "msg-1", nil
}

func (s *stubSQSClient) ReceiveMessages(_ context.Context, _ ingestion.SQSReceiveInput) ([]ingestion.SQSMessage, error) {
	if s.recvErr != nil {
		return nil, s.recvErr
	}
	out := s.received
	s.received = nil
	return out, nil
}

func (s *stubSQSClient) DeleteMessage(_ context.Context, _ string, receiptHandle string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.deleted = append(s.deleted, receiptHandle)
	return nil
}

func TestQueueProviderSQSValidationAndFallback(t *testing.T) {
	cfg := ingestion.QueueProviderConfig{Provider: "sqs", LocalFallbackMode: true}
	fallback := ingestion.NewInMemoryQueueAdapter()
	producer, consumer, dlq, mode, err := ingestion.BuildQueueAdapters(cfg, nil, fallback)
	if err != nil {
		t.Fatalf("build queue adapters with fallback: %v", err)
	}
	if mode != "memory-fallback" {
		t.Fatalf("expected memory-fallback mode, got %q", mode)
	}
	if producer == nil || consumer == nil || dlq == nil {
		t.Fatal("expected fallback adapters to be wired")
	}
}

func TestSQSAdapterQueueAndDLQFlow(t *testing.T) {
	client := &stubSQSClient{}
	cfg := ingestion.QueueProviderConfig{
		Provider:       "sqs",
		AWSRegion:      "ap-south-1",
		QueueURL:       "https://sqs.ap-south-1.amazonaws.com/123/ingestion.fifo",
		DLQURL:         "https://sqs.ap-south-1.amazonaws.com/123/ingestion-dlq.fifo",
		MessageGroupID: "ingestion-jobs",
	}
	adapter, err := ingestion.NewSQSQueueAdapter(client, cfg)
	if err != nil {
		t.Fatalf("new sqs adapter: %v", err)
	}

	message := contracts.QueueMessage{
		MessageID:      "msg-1",
		Job:            approvedJob(),
		IdempotencyKey: "idem-1",
		Attempt:        0,
		EnqueuedAt:     time.Now().UTC().Round(0),
	}
	if err := adapter.Enqueue(t.Context(), message); err != nil {
		t.Fatalf("enqueue sqs message: %v", err)
	}
	if len(client.sent) != 1 || client.sent[0].QueueURL != cfg.QueueURL {
		t.Fatalf("expected queue send to primary URL, got %+v", client.sent)
	}

	client.received = []ingestion.SQSMessage{{MessageID: "aws-id-1", Body: `{"MessageID":"msg-1","Job":{"JobID":"job-e2t3-1","TenantID":"tenant-a","SourceURI":"s3://tenant-a-ap-south-1/docs/manual.txt","Checksum":"sum-1","PolicyDecision":"approved","CreatedAt":"2026-03-09T11:12:13Z"},"IdempotencyKey":"idem-1","Attempt":0,"EnqueuedAt":"2026-03-09T11:12:13Z"}`, ReceiptHandle: "receipt-1"}}
	decoded, err := adapter.Dequeue(t.Context())
	if err != nil {
		t.Fatalf("dequeue sqs message: %v", err)
	}
	if decoded.MessageID != "msg-1" || decoded.Job.TenantID != "tenant-a" {
		t.Fatalf("unexpected decoded message: %+v", decoded)
	}
	if len(client.deleted) != 1 || client.deleted[0] != "receipt-1" {
		t.Fatalf("expected receipt handle deletion, got %+v", client.deleted)
	}

	if err := adapter.Publish(t.Context(), message, "retry_exhausted"); err != nil {
		t.Fatalf("publish dlq message: %v", err)
	}
	if len(client.sent) != 2 || client.sent[1].QueueURL != cfg.DLQURL {
		t.Fatalf("expected DLQ send to dlq URL, got %+v", client.sent)
	}
}

func TestQueueProviderRejectsSQSWithoutFallback(t *testing.T) {
	cfg := ingestion.QueueProviderConfig{Provider: "sqs", LocalFallbackMode: false}
	_, _, _, _, err := ingestion.BuildQueueAdapters(cfg, nil, nil)
	if err == nil {
		t.Fatal("expected misconfigured sqs provider to fail without fallback")
	}
}

func TestSQSAdapterPropagatesClientError(t *testing.T) {
	client := &stubSQSClient{sendErr: errors.New("access denied")}
	cfg := ingestion.QueueProviderConfig{
		Provider:       "sqs",
		AWSRegion:      "ap-south-1",
		QueueURL:       "https://sqs.ap-south-1.amazonaws.com/123/ingestion.fifo",
		DLQURL:         "https://sqs.ap-south-1.amazonaws.com/123/ingestion-dlq.fifo",
		MessageGroupID: "ingestion-jobs",
	}
	adapter, err := ingestion.NewSQSQueueAdapter(client, cfg)
	if err != nil {
		t.Fatalf("new sqs adapter: %v", err)
	}

	err = adapter.Enqueue(t.Context(), contracts.QueueMessage{
		MessageID:      "msg-err-1",
		Job:            approvedJob(),
		IdempotencyKey: "idem-err-1",
		Attempt:        0,
		EnqueuedAt:     time.Now().UTC().Round(0),
	})
	if err == nil {
		t.Fatal("expected client access denied error")
	}
	if err.Error() != "access denied" {
		t.Fatalf("expected client error to be propagated, got: %v", err)
	}
}
