package logger

import (
	"context"
	"fmt"
	"os"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"go.uber.org/zap"
)

var logger *zap.Logger

func init() {
	var err error

	sn := os.Getenv("SERVICE_NAME")
	if sn == "" {
		config := NewProductionConfig()
		logger, err = config.Build(zap.AddCallerSkip(1))
	} else {
		logger, err = NewProductionWithCore(WrapCore(
			ReportAllErrors(true),
			ServiceName(sn),
		), zap.AddCallerSkip(1))
		logger.Info("SERVICE_NAME exists. Enabling Error Reporting")
	}

	if err != nil {
		panic(err)
	}
}

// NewProductionWithCore is same as NewProduction but accepts a custom configured core
func NewProductionWithCore(core zap.Option, options ...zap.Option) (*zap.Logger, error) {
	options = append(options, core)

	return NewProductionConfig().Build(options...)
}

func Debug(message string, fields ...zap.Field) {
	logger.Debug(message, fields...)
}

func Info(message string, fields ...zap.Field) {
	logger.Info(message, fields...)
}

func Warn(message string, fields ...zap.Field) {
	logger.Warn(message, fields...)
}

func Error(message string, fields ...zap.Field) {
	logger.Error(message, fields...)
}

func Fatal(message string, fields ...zap.Field) {
	logger.Fatal(message, fields...)
}

func Sync() error {
	err := logger.Sync()
	if err != nil {
		return err
	}
	return nil
}

func InterceptorLogger(l *zap.Logger) logging.Logger {
	return logging.LoggerFunc(func(ctx context.Context, lvl logging.Level, msg string, fields ...any) {
		f := make([]zap.Field, 0, len(fields)/2)

		for i := 0; i < len(fields); i += 2 {
			key := fields[i]
			value := fields[i+1]

			switch v := value.(type) {
			case string:
				f = append(f, zap.String(key.(string), v))
			case int:
				f = append(f, zap.Int(key.(string), v))
			case bool:
				f = append(f, zap.Bool(key.(string), v))
			default:
				f = append(f, zap.Any(key.(string), v))
			}
		}

		switch lvl {
		case logging.LevelDebug:
			Debug(msg, f...)
		case logging.LevelInfo:
			Info(msg, f...)
		case logging.LevelWarn:
			Warn(msg, f...)
		case logging.LevelError:
			Error(msg, f...)
		default:
			Fatal(fmt.Sprintf("unknown level %v", lvl))
		}
	})
}

func GetGRPCLogger() logging.Logger {
	return InterceptorLogger(logger)
}
