package logging

import (
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Logger = zap.NewNop()
var bufferedWS *zapcore.BufferedWriteSyncer

func Init() error {
	cfg := zap.NewProductionEncoderConfig()
	cfg.TimeKey = "ts"
	cfg.EncodeTime = zapcore.ISO8601TimeEncoder

	bufferedWS = &zapcore.BufferedWriteSyncer{
		WS:            zapcore.AddSync(os.Stdout),
		Size:          256 * 1024,
		FlushInterval: 1 * time.Second,
	}

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(cfg),
		bufferedWS,
		zap.InfoLevel,
	)

	logger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zap.ErrorLevel))
	Logger = logger
	return nil
}

func Sync() {
	if Logger != nil {
		_ = Logger.Sync()
	}
	if bufferedWS != nil {
		_ = bufferedWS.Stop()
	}
}
