// Package logger provides the logging abstraction used across the application.
//
// The interface exists so that tests can inject a silent implementation
// (NoOpLogger) without capturing stdout.
package logger

import "context"

// LoggingConfig holds the configuration for the logger.
type LoggingConfig struct {
	Level  string `koanf:"level"`
	Format string `koanf:"format"`
}

// Logger is the interface that wraps basic logging methods.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)

	// With returns a Logger that includes args in every subsequent record.
	With(args ...any) Logger

	// Context-aware logging methods.
	DebugContext(ctx context.Context, msg string, args ...any)
	InfoContext(ctx context.Context, msg string, args ...any)
	WarnContext(ctx context.Context, msg string, args ...any)
	ErrorContext(ctx context.Context, msg string, args ...any)
}
