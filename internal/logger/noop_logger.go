package logger

import "context"

// NoOpLogger is a silent Logger, used by tests so that assertions are not
// buried under application output.
type NoOpLogger struct{}

// NewNoOpLogger creates a new NoOpLogger.
func NewNoOpLogger() *NoOpLogger { return &NoOpLogger{} }

func (l *NoOpLogger) Debug(string, ...any) {}
func (l *NoOpLogger) Info(string, ...any)  {}
func (l *NoOpLogger) Warn(string, ...any)  {}
func (l *NoOpLogger) Error(string, ...any) {}

func (l *NoOpLogger) With(...any) Logger { return l }

func (l *NoOpLogger) DebugContext(context.Context, string, ...any) {}
func (l *NoOpLogger) InfoContext(context.Context, string, ...any)  {}
func (l *NoOpLogger) WarnContext(context.Context, string, ...any)  {}
func (l *NoOpLogger) ErrorContext(context.Context, string, ...any) {}
