package logging

import "context"

// Logger is the narrow structured logging boundary used by scaffold services.
type Logger interface {
	Info(ctx context.Context, event string, fields map[string]string)
	Error(ctx context.Context, event string, err error, fields map[string]string)
}

// NopLogger is useful for tests and placeholder wiring.
type NopLogger struct{}

func (NopLogger) Info(context.Context, string, map[string]string) {}

func (NopLogger) Error(context.Context, string, error, map[string]string) {}
