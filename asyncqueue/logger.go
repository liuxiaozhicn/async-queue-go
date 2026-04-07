package asyncqueue

import "github.com/liuxiaozhicn/async-queue-go/pkg/logger"

// Logger is the interface for configuring async-queue logging.
// Implement this interface to plug in your own logger (zap, logrus, slog, etc.).
type Logger = logger.Interface

// LogLevel controls verbosity. Use with WithLogger and LogMode.
// Use the constants from pkg/logger directly: logger.Silent, logger.Error, logger.Warn, logger.Info.
type LogLevel = logger.LogLevel

// WithLogger configures a custom logger for the server.
// If not provided, logger.Default (Info level, standard log) is used.
//
// Example:
//
//	asyncqueue.NewServer(cfg, client, asyncqueue.WithLogger(
//	    logger.Default.LogMode(asyncqueue.Warn),
//	))
func WithLogger(l logger.Interface) Option {
	return func(s *Server) {
		s.logger = l
	}
}
