package logger

import (
	"context"
	"time"
)

// LogLevel represents the logging verbosity.
type LogLevel int

const (
	Silent LogLevel = iota
	Error
	Warn
	Info
)

// Interface is the logger contract, modeled after GORM's logger.Interface.
type Interface interface {
	LogMode(LogLevel) Interface
	Info(context.Context, string, ...interface{})
	Warn(context.Context, string, ...interface{})
	Error(context.Context, string, ...interface{})
	// Trace logs a single message processing round-trip.
	// fc is called lazily — not invoked when level is Silent.
	// Returns (queue name, disposition).
	Trace(ctx context.Context, begin time.Time, fc func() (queue, disposition string), err error)
}
