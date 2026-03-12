package asyncqueue

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
)

type Option func(*Server)

// Server provides a high-level entry point for loading config,
// registering handlers, and running workers.
type Server struct {
	config      *Config
	serveMux    *ServeMux
	manager     *Manager
	redisClient redis.UniversalClient // Optional external Redis client
}

// NewServer creates a Server with an explicit Redis client.
//
// redisClient is required — passing nil returns an error immediately.
// Use opts to provide optional configuration (logger, tracer, etc.).
func NewServer(cfg *Config, redisClient redis.UniversalClient, opts ...Option) (*Server, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	if redisClient == nil {
		return nil, errors.New("redisClient is required — create a *redis.Client or *redis.ClusterClient and pass it in")
	}

	serveMux := NewServeMux()
	manager, err := NewManagerWithRedis(cfg, serveMux, redisClient)
	if err != nil {
		return nil, err
	}

	s := &Server{
		config:      cfg,
		serveMux:    serveMux,
		manager:     manager,
		redisClient: redisClient,
	}

	// Apply functional options
	for _, opt := range opts {
		opt(s)
	}

	setDefaultWithWarn(s)
	return s, nil
}

// LoadServer loads configuration from a file and creates a Server.
// Redis must be provided explicitly.
func LoadServer(path string, redisClient redis.UniversalClient, opts ...Option) (*Server, error) {
	cfg, err := LoadConfig(path)
	if err != nil {
		return nil, err
	}
	return NewServer(cfg, redisClient, opts...)
}

// NewServerFromConfig is an alias for LoadServer.
func NewServerFromConfig(path string, redisClient redis.UniversalClient, opts ...Option) (*Server, error) {
	return LoadServer(path, redisClient, opts...)
}

// StartWorker starts all queue workers without blocking.
func (s *Server) StartWorker() error {
	if s == nil || s.manager == nil {
		return errors.New("server is nil")
	}
	return s.manager.StartWorker()
}

// Handle registers a handler for a queue.
func (s *Server) Handle(queueName string, handler Handler) {
	if s == nil || s.serveMux == nil {
		return
	}
	s.serveMux.Register(queueName, handler)
}

// Bind registers one or more Jobs as handlers for their own queues.
// The queue name for each job is taken from j.GetType().
func (s *Server) Bind(jobs ...Job) {
	if s == nil || s.serveMux == nil {
		return
	}
	for _, j := range jobs {
		if j == nil {
			continue
		}
		s.serveMux.Register(j.GetType(), wrapJob(j))
	}
}

// Run merges all handlers from registry into the server, then starts
// processing, handles OS signals, and blocks until exit.
//
// Build the registry with NewHandlerRegistry + WrapJob, or via queueHandle:
func (s *Server) Run(serveMux *ServeMux) error {
	if s == nil || s.manager == nil {
		return errors.New("server is nil")
	}
	if serveMux != nil {
		serveMux.mu.RLock()
		for name, h := range serveMux.handlers {
			s.serveMux.Register(name, h)
		}
		serveMux.mu.RUnlock()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return s.manager.Run(ctx, s.shutdownTimeout())
}

// Stop gracefully stops the server.
func (s *Server) Stop(timeout time.Duration) error {
	if s == nil || s.manager == nil {
		return errors.New("server is nil")
	}
	err := s.manager.Stop(timeout)
	defaultServer.CompareAndSwap(s, nil)
	return err
}

// Queue returns the started queue instance by name.
func (s *Server) Queue(name string) (*Queue, error) {
	if s == nil || s.manager == nil {
		return nil, errors.New("server is nil")
	}
	return s.manager.GetQueue(name)
}

// Config returns the server configuration.
func (s *Server) Config() *Config {
	if s == nil {
		return nil
	}
	return s.config
}

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
