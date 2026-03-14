package asyncqueue

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	iqueue "github.com/liuxiaozhicn/async-queue-go/pkg/queue"
	iworker "github.com/liuxiaozhicn/async-queue-go/pkg/worker"
	"github.com/redis/go-redis/v9"
)

// Manager manages workers for multiple queues.
type Manager struct {
	config      *Config
	serveMux    *ServeMux
	queues      map[string]*Queue
	workers     map[string][]*iworker.Worker
	redisClient redis.UniversalClient // Optional external Redis client

	mu      sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	errors  []error
	started bool

	// Worker restart tracking
	restartWg sync.WaitGroup // Tracks restart goroutines
}

// NewManager creates a queue manager.
func NewManager(config *Config, serveMux *ServeMux) (*Manager, error) {
	if config == nil {
		return nil, errors.New("config is nil")
	}
	if serveMux == nil {
		return nil, errors.New("serveMux is nil")
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		config:   config,
		serveMux: serveMux,
		queues:   make(map[string]*Queue),
		workers:  make(map[string][]*iworker.Worker),
		ctx:      ctx,
		cancel:   cancel,
	}, nil
}

// NewManagerWithRedis creates a queue manager with external Redis client.
func NewManagerWithRedis(config *Config, serveMux *ServeMux, redisClient redis.UniversalClient) (*Manager, error) {
	if config == nil {
		return nil, errors.New("config is nil")
	}
	if serveMux == nil {
		return nil, errors.New("serveMux is nil")
	}
	if redisClient == nil {
		return nil, errors.New("redis client is nil")
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		config:      config,
		serveMux:    serveMux,
		queues:      make(map[string]*Queue),
		workers:     make(map[string][]*iworker.Worker),
		redisClient: redisClient,
		ctx:         ctx,
		cancel:      cancel,
	}, nil
}

// StartWorker starts workers for all enabled queues.
func (m *Manager) StartWorker() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return errors.New("manager already started")
	}

	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.errors = nil
	m.workers = make(map[string][]*iworker.Worker)

	for name, queueCfg := range m.config.Queues {
		if !queueCfg.Enabled {
			continue
		}

		handler, ok := m.serveMux.Get(name)
		if !ok {
			m.rollbackStartLocked()
			return fmt.Errorf("handler not registered for queue: %s", name)
		}

		var queue *Queue
		var err error

		// Use external Redis client
		queue, err = NewAsyncQueue(m.redisClient, queueCfg.Channel, queueCfg.TimeoutSeconds, queueCfg.HandleTimeout, queueCfg.RetrySeconds, queueCfg.MaxAttempts)

		if err != nil {
			m.rollbackStartLocked()
			return fmt.Errorf("create queue %s: %w", name, err)
		}
		m.queues[name] = queue

		workers := make([]*iworker.Worker, 0, queueCfg.Processes)
		for i := 0; i < queueCfg.Processes; i++ {
			consumer := iqueue.NewConsumer(queue.driver, handler, queueCfg.Concurrent, queueCfg.MaxMessages, time.Duration(queueCfg.HandleTimeout)*time.Second, name)
			workerInstance := iworker.NewWorker(consumer)

			workers = append(workers, workerInstance)
			m.wg.Add(1)

			// Check if auto-restart is enabled for this queue
			if queueCfg.AutoRestart && queueCfg.MaxMessages > 0 {
				// Auto-restart mode: restart worker when it exits normally
				go m.runWorkerWithAutoRestart(name, i, workerInstance, queue, handler, queueCfg)
			} else {
				// Normal mode: worker exits after max_messages or on error
				go func(queueName string, processID int, worker *iworker.Worker) {
					defer m.wg.Done()
					if err := worker.Start(m.ctx); err != nil {
						m.recordError(fmt.Errorf("queue %s process %d start: %w", queueName, processID, err))
						return
					}
					if err := worker.Wait(); err != nil {
						m.recordError(fmt.Errorf("queue %s process %d: %w", queueName, processID, err))
					}
				}(name, i, workerInstance)
			}
		}

		m.workers[name] = workers
	}

	m.started = true
	return nil
}

// Stop stops all queue workers.
func (m *Manager) Stop(timeout time.Duration) error {
	m.mu.RLock()
	started := m.started
	cancel := m.cancel
	m.mu.RUnlock()

	if !started || cancel == nil {
		return errors.New("manager not started")
	}

	cancel()
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	var err error
	if timeout <= 0 {
		<-done
	} else {
		select {
		case <-done:
		case <-time.After(timeout):
			return errors.New("manager stop timeout")
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeQueuesLocked()
	m.started = false
	if len(m.errors) > 0 {
		err = fmt.Errorf("manager stopped with %d errors: %v", len(m.errors), m.errors[0])
	}
	return err
}

// Wait waits for all workers to finish.
func (m *Manager) Wait() error {
	m.wg.Wait()

	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.errors) > 0 {
		return fmt.Errorf("manager finished with %d errors: %v", len(m.errors), m.errors[0])
	}
	return nil
}

// Run starts the manager and stops it when the context is canceled.
func (m *Manager) Run(ctx context.Context, shutdownTimeout time.Duration) error {
	if err := m.StartWorker(); err != nil {
		return err
	}

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- m.Wait()
	}()

	select {
	case err := <-waitDone:
		return err
	case <-ctx.Done():
		return m.Stop(shutdownTimeout)
	}
}

// GetQueue returns the queue instance for publishing messages.
func (m *Manager) GetQueue(name string) (*Queue, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	q, ok := m.queues[name]
	if !ok {
		return nil, fmt.Errorf("queue not found: %s", name)
	}
	return q, nil
}

func (m *Manager) recordError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors = append(m.errors, err)
}

func (m *Manager) rollbackStartLocked() {
	if m.cancel != nil {
		m.cancel()
	}
	m.closeQueuesLocked()
	m.workers = make(map[string][]*iworker.Worker)
	m.errors = nil
	m.started = false
}

func (m *Manager) closeQueuesLocked() {
	for name, q := range m.queues {
		_ = q.Close()
		delete(m.queues, name)
	}
}

// runWorkerWithAutoRestart runs a worker and automatically restarts it when it exits normally.
func (m *Manager) runWorkerWithAutoRestart(queueName string, processID int, w *iworker.Worker, q *Queue, handler iqueue.Handler, cfg QueueConfig) {
	defer m.wg.Done()
	restartCount := 0
	for {
		// Check if manager is still running
		select {
		case <-m.ctx.Done():
			return
		default:
		}

		// Start the worker
		if err := w.Start(m.ctx); err != nil {
			m.recordError(fmt.Errorf("queue %s process %d start: %w", queueName, processID, err))
			return
		}

		if restartCount == 0 {
			log.Printf("[Manager] queue %s process %d started", queueName, processID)
		} else {
			log.Printf("[Manager] queue %s process %d restarted (count=%d)", queueName, processID, restartCount)
		}

		// Wait for worker to finish
		err := w.Wait()

		// Check if context was cancelled (shutdown signal)
		select {
		case <-m.ctx.Done():
			// Manager is shutting down, don't restart
			if err != nil {
				m.recordError(fmt.Errorf("queue %s process %d: %w", queueName, processID, err))
			}
			return
		default:
		}

		// Worker exited (likely reached max_messages), restart it
		if err != nil {
			m.recordError(fmt.Errorf("queue %s process %d: %w", queueName, processID, err))
		}

		// Create a new worker instance for restart
		consumer := iqueue.NewConsumer(q.driver, handler, cfg.Concurrent, cfg.MaxMessages, time.Duration(cfg.HandleTimeout)*time.Second, queueName)
		w = iworker.NewWorker(consumer)

		restartCount++
		log.Printf("[Manager] queue %s process %d reached max messages (%d), restarting...",
			queueName, processID, cfg.MaxMessages)

		// Optional: add a small delay before restart to avoid tight loops
		time.Sleep(100 * time.Millisecond)
	}
}
