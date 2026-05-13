package asyncqueue

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/liuxiaozhicn/async-queue-go/pkg/logger"
	"github.com/liuxiaozhicn/async-queue-go/pkg/queue"
)

type Option func(*Server)

// Server provides a high-level entry point for loading config,
// registering handlers, and running workers.
type Server struct {
	config   *Config
	serveMux *ServeMux
	manager  *Manager
	logger   logger.Interface
	drivers  map[string]queue.Driver
}

// NewServer creates a server from configuration and registered options.
//
// Drivers are not auto-built from config: callers register prepared driver instances via WithDriver.
// The created server is set as global default server.
func NewServer(cfg *Config, opts ...Option) (*Server, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}

	serveMux := NewServeMux()

	s := &Server{
		config:   cfg,
		serveMux: serveMux,
		logger:   logger.Default,
		drivers:  make(map[string]queue.Driver),
	}

	// Apply functional options first so WithLogger takes effect before Manager is created.
	for _, opt := range opts {
		opt(s)
	}

	manager, err := newManager(cfg, serveMux, s.logger)
	if err != nil {
		return nil, err
	}
	for driverName, driver := range s.drivers {
		manager.RegisterDriver(driverName, driver)
	}
	s.manager = manager

	setDefaultWithWarn(s)
	return s, nil
}

// Handle registers one handler for a queue name.
//
// queueName should match the key in Config.Queues for runtime dispatch.
func (s *Server) Handle(queueName string, handler queue.Handler) {
	if s == nil || s.serveMux == nil {
		return
	}
	s.serveMux.Handle(queueName, handler)
}

// Bind registers one handler for a queue name.
//
// It is currently equivalent to Handle and kept for API readability/compatibility.
func (s *Server) Bind(queueName string, handler queue.Handler) {
	if s == nil || s.serveMux == nil {
		return
	}
	s.serveMux.Handle(queueName, handler)
}

// Run merges handlers from serveMux and starts manager lifecycle.
//
// When serveMux is non-nil, handlers are copied into server registry before startup.
// Run blocks until manager exits or ctx is canceled.
func (s *Server) Run(ctx context.Context, serveMux *ServeMux) error {
	if s == nil || s.manager == nil {
		return errors.New("server is nil")
	}
	if serveMux != nil {
		serveMux.mu.RLock()
		for name, h := range serveMux.handlers {
			s.serveMux.Handle(name, h)
		}
		serveMux.mu.RUnlock()
	}
	s.logger.Info(ctx, "[async-queue-server] starting | queues=%d", len(s.config.Queues))

	err := s.manager.Run(ctx, s.shutdownTimeout())
	if err != nil {
		s.logger.Error(ctx, "[async-queue-server] exited with error | %v", err)
	} else {
		s.logger.Info(ctx, "[async-queue-server] shutdown complete gracefully")
	}
	return err
}

// Stop gracefully stops all workers through manager.Stop.
//
// timeout <= 0 means wait indefinitely.
func (s *Server) Stop(timeout time.Duration) error {
	if s == nil || s.manager == nil {
		return errors.New("server is nil")
	}
	err := s.manager.Stop(timeout)
	defaultServer.CompareAndSwap(s, nil)
	return err
}

// Queue returns a queue facade by configured queue name.
//
// Returned queue is intended for producer-side operations (Push/Get/Cancel/Info).
func (s *Server) Queue(name string) (*Queue, error) {
	if s == nil || s.manager == nil {
		return nil, errors.New("server is nil")
	}
	return s.manager.GetQueue(name)
}

// Config returns the server configuration pointer.
//
// The returned pointer is owned by server; callers should treat it as read-only.
func (s *Server) Config() *Config {
	if s == nil {
		return nil
	}
	return s.config
}

// shutdownTimeout returns the largest enabled queue shutdown timeout.
//
// If no queue provides a larger value, default is 30 seconds.
func (s *Server) shutdownTimeout() time.Duration {
	if s == nil || s.config == nil {
		return 30 * time.Second
	}

	maxTimeout := 30
	for _, cfg := range s.config.Queues {
		if cfg.Enabled && cfg.ShutdownTimeout > maxTimeout {
			maxTimeout = cfg.ShutdownTimeout
		}
	}
	return time.Duration(maxTimeout) * time.Second
}

const logo = `

   ╭======================================================================╮
   │    ___                             _____                             │
   │   / _ \                           |  _  |                            │
   │  / /_\ \ ___  _   _  _ __    ___  | | | | _   _   ___  _   _   ___   │
   │  |  _  |/ __|| | | || '_ \  / __| | | | || | | | / _ \| | | | / _ \  │
   │  | | | |\__ \| |_| || | | || (__  \ \/' /| |_| ||  __/| |_| ||  __/  │
   │  \_| |_/|___/ \__, ||_| |_| \___|  \_/\_\ \__,_| \___| \__,_| \___|  │
   │                __/ |                                                 │
   │               |___/                                                  │
   ╰======================================================================╯

`

// Banner prints a startup banner to stdout and logs structured startup info.
func Banner(l logger.Interface) {
	fmt.Print(logo)
	l.Info(context.Background(), "async-queue server started")
}
