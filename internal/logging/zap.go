package logging

import (
	"go.uber.org/zap"
)

var Logger *zap.Logger

func Init() error {
	cfg := zap.NewProductionConfig()
	// Enable async logging (buffered writes)
	cfg.Async = true
	logger, err := cfg.Build()
	if err != nil {
		return err
	}
	Logger = logger
	return nil
}

func Sync() {
	if Logger != nil {
		_ = Logger.Sync()
	}
}
