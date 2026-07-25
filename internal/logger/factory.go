package logger

// New creates the Logger used in production.
func New(config LoggingConfig) Logger {
	return NewSlogLogger(config)
}

// NewDefault creates a Logger with the default configuration: info level, text format.
func NewDefault() Logger {
	return New(LoggingConfig{Level: "info", Format: "text"})
}
