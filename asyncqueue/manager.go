package asyncqueue

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/liuxiaozhicn/async-queue-go/pkg/logger"
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
	logger      logger.Interface

	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	errMu   sync.Mutex
	errors  map[string]error
	started bool
}

// NewManager creates a queue manager.
func NewManager(config *Config, serveMux *ServeMux, l logger.Interface) (*Manager, error) {
	if config == nil {
		return nil, errors.New("config is nil")
	}
	if serveMux == nil {
		return nil, errors.New("serveMux is nil")
	}
	if l == nil {
		l = logger.Default
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		config:   config,
		serveMux: serveMux,
		queues:   make(map[string]*Queue),
		workers:  make(map[string][]*iworker.Worker),
		logger:   l,
		ctx:      ctx,
		cancel:   cancel,
	}, nil
}

// NewManagerWithRedis creates a queue manager with external Redis client.
func NewManagerWithRedis(config *Config, serveMux *ServeMux, redisClient redis.UniversalClient, l logger.Interface) (*Manager, error) {
	if config == nil {
		return nil, errors.New("config is nil")
	}
	if serveMux == nil {
		return nil, errors.New("serveMux is nil")
	}
	if redisClient == nil {
		return nil, errors.New("redis client is nil")
	}
	if l == nil {
		l = logger.Default
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		config:      config,
		serveMux:    serveMux,
		queues:      make(map[string]*Queue),
		workers:     make(map[string][]*iworker.Worker),
		redisClient: redisClient,
		logger:      l,
		ctx:         ctx,
		cancel:      cancel,
	}, nil
}

func (m *Manager) checkStartWorkerOptions() error {
	for name, qCfg := range m.config.Queues {
		if !qCfg.Enabled {
			continue
		}
		if _, ok := m.serveMux.Get(name); !ok {
			return fmt.Errorf("[Manager] handler not registered for queue: %s", name)
		}
	}

	if m.redisClient != nil {
		if err := m.redisClient.Ping(context.Background()).Err(); err != nil {
			return fmt.Errorf("[Manager] redis not reachable: %w", err)
		}
	}

	return nil
}

// StartWorker starts workers for all enabled queues.
func (m *Manager) StartWorker() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return errors.New("[Manager] already started")
	}

	if err := m.checkStartWorkerOptions(); err != nil {
		return err
	}

	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.errors = make(map[string]error, 0)
	m.workers = make(map[string][]*iworker.Worker)

	for name, queueCfg := range m.config.Queues {
		if !queueCfg.Enabled {
			continue
		}

		handler, _ := m.serveMux.Get(name)

		var queue *Queue
		var err error

		// Use external Redis client
		queue, err = NewAsyncQueue(m.redisClient, queueCfg.Channel, queueCfg.PopTimeout, queueCfg.HandleTimeout, queueCfg.RetrySeconds, queueCfg.MaxAttempts, name, m.logger)
		if err != nil {
			m.rollbackStartLocked()
			return fmt.Errorf("[Manager] create queue %s: %w", name, err)
		}
		if rd, ok := queue.driver.(*iqueue.RedisDriver); ok {
			rd.SetMessageTTL(queueCfg.MessageTTL)
		}
		m.queues[name] = queue

		workers := make([]*iworker.Worker, 0, queueCfg.Processes)
		for i := 0; i < queueCfg.Processes; i++ {
			consumer := iqueue.NewConsumer(queue.driver, handler, queueCfg.Concurrent, queueCfg.AutoRestart, queueCfg.MaxMessages, name, i, queueCfg.HandleTimeout, m.logger)
			workerInstance := iworker.NewWorker(consumer)

			workers = append(workers, workerInstance)
			m.wg.Add(1)

			// Check if auto-restart is enabled for this queue
			if queueCfg.AutoRestart && queueCfg.MaxMessages > 0 {
				// Auto-restart mode: restart worker when it exits normally
				go m.runWorkerWithAutoRestart(name, i, workerInstance, queue, handler, queueCfg)
			} else {
				// Normal mode: worker runs continuously until context cancel or error.
				go func(queueName string, processID int, worker *iworker.Worker) {
					defer m.wg.Done()
					if err := worker.Start(m.ctx); err != nil {
						m.recordError(queueName, processID, fmt.Errorf("start: %w", err))
						return
					}
					if err := worker.Wait(); err != nil {
						m.recordError(queueName, processID, fmt.Errorf("wait: %w", err))
					}
				}(name, i, workerInstance)
			}
		}

		m.workers[name] = workers
	}

	m.started = true
	//Banner(m.logger)
	return nil
}

// Stop stops all queue workers.
func (m *Manager) Stop(timeout time.Duration) error {
	m.mu.RLock()
	started := m.started
	cancel := m.cancel
	m.mu.RUnlock()

	if !started || cancel == nil {
		return errors.New("[Manager] not started")
	}

	cancel()
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	if timeout <= 0 {
		<-done
	} else {
		select {
		case <-done:
		case <-time.After(timeout):
			return errors.New("[Manager] stop timeout")
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeQueuesLocked()
	m.started = false
	return m.runErrors()
}

// Wait waits for all workers to finish.
func (m *Manager) Wait() error {
	m.wg.Wait()
	return m.runErrors()
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
		m.logger.Warn(ctx, "async-queue server shutting down...")
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

func (m *Manager) recordError(queueName string, processID int, err error) {
	m.errMu.Lock()
	defer m.errMu.Unlock()
	key := fmt.Sprintf("%s:%d", queueName, processID)
	m.errors[key] = err
}

func (m *Manager) runErrors() error {
	m.errMu.Lock()
	defer m.errMu.Unlock()
	if len(m.errors) == 0 {
		return nil
	}
	errs := make([]error, 0, len(m.errors))
	for _, e := range m.errors {
		errs = append(errs, e)
	}
	return fmt.Errorf("[Manager] %d error(s): %w", len(errs), errors.Join(errs...))
}
func (m *Manager) rollbackStartLocked() {
	if m.cancel != nil {
		m.cancel()
	}

	// 释放 mu，让 goroutine 能正常退出（recordError 只抢 errMu，不会死锁，goroutine 退出路径上可能间接读 mu，保险起见还是释放）
	m.mu.Unlock()
	m.wg.Wait()
	m.mu.Lock()

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
			m.recordError(queueName, processID, fmt.Errorf("wait: %w", err))
			return
		}

		if restartCount == 0 {
			m.logger.Info(m.ctx, "[Manager] runWorkerWithAutoRestart queue %s process %d started", queueName, processID)
		} else {
			m.logger.Info(m.ctx, "[Manager] runWorkerWithAutoRestart queue %s process %d restarted (count=%d)", queueName, processID, restartCount)
		}

		// Wait for worker to finish
		err := w.Wait()

		// Check if context was cancelled (shutdown signal)
		select {
		case <-m.ctx.Done():
			// Manager is shutting down, don't restart
			if err != nil {
				m.recordError(queueName, processID, fmt.Errorf("[Manager] runWorkerWithAutoRestart ctx.Done error : %w", err))
			}
			return
		default:
		}

		// Worker exited (likely reached max_messages), restart it
		if err != nil {
			m.recordError(queueName, processID, fmt.Errorf("[Manager] runWorkerWithAutoRestart error : %w", err))
		}

		// Create a new worker instance for restart
		consumer := iqueue.NewConsumer(q.driver, handler, cfg.Concurrent, cfg.AutoRestart, cfg.MaxMessages, queueName, processID, cfg.HandleTimeout, m.logger)
		w = iworker.NewWorker(consumer)

		restartCount++
		m.logger.Info(m.ctx, "[Manager] runWorkerWithAutoRestart queue %s process %d reached max messages (%d), restarting...",
			queueName, processID, cfg.MaxMessages)

		// Optional: add a small delay before restart to avoid tight loops
		time.Sleep(100 * time.Millisecond)
	}
}
