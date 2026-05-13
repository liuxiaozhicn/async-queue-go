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
)

// Manager manages queue runtime lifecycle for multiple configured queues.
//
// It owns worker startup/shutdown orchestration, forwarder processes, driver registry
// and aggregated error reporting across queue/process dimensions.
type Manager struct {
	config     *Config
	serveMux   *ServeMux
	queues     map[string]*Queue
	workers    map[string][]*iworker.Worker
	forwarders map[string]*iworker.Worker
	logger     logger.Interface
	drivers    map[string]iqueue.Driver

	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	errMu   sync.Mutex
	errors  map[string]error
	started bool
}

// NewManager creates a manager with validated dependencies.
//
// config and serveMux are required; logger falls back to default when nil.
func NewManager(config *Config, serveMux *ServeMux, l logger.Interface) (*Manager, error) {
	return newManager(config, serveMux, l)
}

// newManager initializes internal state and cancellation context.
func newManager(config *Config, serveMux *ServeMux, l logger.Interface) (*Manager, error) {
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
		config:     config,
		serveMux:   serveMux,
		queues:     make(map[string]*Queue),
		workers:    make(map[string][]*iworker.Worker),
		forwarders: make(map[string]*iworker.Worker),
		logger:     l,
		drivers:    make(map[string]iqueue.Driver),
		ctx:        ctx,
		cancel:     cancel,
	}, nil
}

// RegisterDriver registers a prepared driver instance by driver name.
//
// queue configs reference this name via QueueConfig.Driver.
// Register should be called before StartWorker.
func (m *Manager) RegisterDriver(driverName string, d iqueue.Driver) {
	if m == nil || driverName == "" || d == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.drivers[driverName] = d
}

// RegisterQueueDriver is kept for backward compatibility.
//
// It is an alias of RegisterDriver.
func (m *Manager) RegisterQueueDriver(queueName string, d iqueue.Driver) {
	m.RegisterDriver(queueName, d)
}

// checkStartWorkerOptions validates runtime prerequisites before worker startup.
//
// For each enabled queue:
// - a handler must be registered in ServeMux
// - QueueConfig.Driver must be non-empty
// - the named driver must already be registered
func (m *Manager) checkStartWorkerOptions() error {
	for queueName, queueCfg := range m.config.Queues {
		if !queueCfg.Enabled {
			continue
		}
		if _, ok := m.serveMux.Get(queueName); !ok {
			return fmt.Errorf("[Async-Queue-Manager] handler not registered for queue: %s", queueName)
		}
		if queueCfg.Driver == "" {
			return fmt.Errorf("[Async-Queue-Manager] driver is empty for queue: %s", queueName)
		}
		if _, ok := m.drivers[queueCfg.Driver]; !ok {
			return fmt.Errorf("[Async-Queue-Manager] driver %q not registered for queue: %s", queueCfg.Driver, queueName)
		}
		if m.drivers[queueCfg.Driver] == nil {
			return fmt.Errorf("[Async-Queue-Manager] driver %q not registered for queue: %s", queueCfg.Driver, queueName)
		}
	}

	return nil
}

// StartWorker starts forwarders and consumers for all enabled queues.
//
// This method is idempotency-protected by m.started and returns error when called twice.
// On partial startup failure it rolls back created workers/queues and returns the first startup error.
func (m *Manager) StartWorker() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return errors.New("[Async-Queue-Manager] already started")
	}

	if err := m.checkStartWorkerOptions(); err != nil {
		return err
	}

	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.errors = make(map[string]error, 0)
	m.workers = make(map[string][]*iworker.Worker)
	m.forwarders = make(map[string]*iworker.Worker)

	for queueName, queueCfg := range m.config.Queues {
		if !queueCfg.Enabled {
			continue
		}

		handler, _ := m.serveMux.Get(queueName)

		var queue *Queue
		var err error

		queueOpts := []QueueOption{
			WithQueuePopTimeout(queueCfg.PopTimeout),
			WithQueueHandleTimeout(queueCfg.HandleTimeout),
			WithQueueRetrySeconds(queueCfg.RetrySeconds),
			WithQueueMessageTTL(queueCfg.MessageTTL),
			WithQueueMaxAttempts(queueCfg.MaxAttempts),
			WithQueueName(queueName),
			WithQueueLogger(m.logger),
		}
		driver := m.drivers[queueCfg.Driver]
		queue, err = NewAsyncQueue(driver, queueCfg.Channel, queueOpts...)
		if err != nil {
			m.rollbackStartLocked()
			return fmt.Errorf("[Async-Queue-Manager] create queue %s: %w", queueName, err)
		}
		m.queues[queueName] = queue

		if forwarder := iqueue.NewForwarder(driver, queueName, queue.channel, m.logger); forwarder != nil {
			forwarderWorker := iworker.NewWorker(forwarder)
			m.forwarders[queueName] = forwarderWorker
			m.wg.Add(1)
			go func(queueName string, worker *iworker.Worker) {
				defer m.wg.Done()
				if err := worker.Start(m.ctx); err != nil {
					m.recordError(queueName, forwarderProcessID, fmt.Errorf("forwarder-start: %w", err))
					return
				}
				if err := worker.Wait(); err != nil {
					m.recordError(queueName, forwarderProcessID, fmt.Errorf("forwarder-wait: %w", err))
				}
			}(queueName, forwarderWorker)
		}

		workers := make([]*iworker.Worker, 0, queueCfg.Processes)
		for i := 0; i < queueCfg.Processes; i++ {
			consumer := iqueue.NewConsumer(
				driver,
				queue.channel,
				handler,
				iqueue.WithConsumerConcurrentLimit(queueCfg.Concurrent),
				iqueue.WithConsumerAutoRestart(queueCfg.AutoRestart),
				iqueue.WithConsumerMaxMessages(queueCfg.MaxMessages),
				iqueue.WithConsumerPopTimeout(queue.PopTimeout),
				iqueue.WithConsumerHandleTimeout(queue.handleTimeout),
				iqueue.WithConsumerRetrySeconds(queue.retrySeconds),
				iqueue.WithConsumerMessageTTL(queue.messageTTL),
				iqueue.WithConsumerName(queueName),
				iqueue.WithConsumerProcessID(i),
				iqueue.WithConsumerLogger(m.logger),
			)
			consumerWorker := iworker.NewWorker(consumer)

			workers = append(workers, consumerWorker)
			m.wg.Add(1)

			// Check if auto-restart is enabled for this queue
			if queueCfg.AutoRestart && queueCfg.MaxMessages > 0 {
				// Auto-restart mode: restart worker when it exits normally
				go m.runWorkerWithAutoRestart(queueName, i, consumerWorker, queue, handler, queueCfg)
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
				}(queueName, i, consumerWorker)
			}
		}

		m.workers[queueName] = workers
	}

	m.started = true
	//Banner(m.logger)
	return nil
}

// Stop requests shutdown and waits until all goroutines exit or timeout is reached.
//
// timeout <= 0 means wait indefinitely.
// It also closes queue facades and returns aggregated worker errors, if any.
func (m *Manager) Stop(timeout time.Duration) error {
	m.mu.RLock()
	started := m.started
	cancel := m.cancel
	m.mu.RUnlock()

	if !started || cancel == nil {
		return errors.New("[Async-Queue-Manager] not started")
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
			return errors.New("[Async-Queue-Manager] stop timeout")
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeQueuesLocked()
	m.started = false
	m.workers = make(map[string][]*iworker.Worker)
	m.forwarders = make(map[string]*iworker.Worker)
	return m.runErrors()
}

// Wait blocks until all manager-managed goroutines exit.
//
// It does not initiate cancellation by itself; caller should coordinate lifecycle via Stop/Run context.
func (m *Manager) Wait() error {
	m.wg.Wait()
	return m.runErrors()
}

// Run starts workers and blocks until completion or context cancellation.
//
// When ctx is canceled, Run invokes Stop(shutdownTimeout) to perform graceful shutdown.
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
		m.logger.Warn(ctx, "[async-queue server] shutting down...")
		return m.Stop(shutdownTimeout)
	}
}

// GetQueue returns a started queue facade by logical queue name.
//
// The returned queue can be used to publish/cancel/query messages.
// Returns error when queue is not enabled or not started.
func (m *Manager) GetQueue(name string) (*Queue, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	q, ok := m.queues[name]
	if !ok {
		return nil, fmt.Errorf("queue not found: %s", name)
	}
	return q, nil
}

// recordError stores one worker error by queue and process ID.
//
// New errors overwrite previous error for the same key.
func (m *Manager) recordError(queueName string, processID int, err error) {
	m.errMu.Lock()
	defer m.errMu.Unlock()
	key := fmt.Sprintf("%s:%d", queueName, processID)
	m.errors[key] = err
}

// runErrors joins recorded worker errors into one error value.
//
// Returns nil when no worker errors were recorded.
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
	return fmt.Errorf("[Async-Queue-Manager] %d error(s): %w", len(errs), errors.Join(errs...))
}

// rollbackStartLocked undoes partial startup while m.mu is already held.
//
// It cancels manager context, waits background goroutines, closes queues, and resets maps/flags.
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
	m.forwarders = make(map[string]*iworker.Worker)
	m.errors = nil
	m.started = false
}

// closeQueuesLocked closes and removes all queue instances.
//
// Caller must hold m.mu.
func (m *Manager) closeQueuesLocked() {
	for name, q := range m.queues {
		_ = q.Close()
		delete(m.queues, name)
	}
}

// runWorkerWithAutoRestart runs one consumer worker in auto-restart mode.
//
// It recreates worker instances after normal exit (for max_messages style rolling restart),
// and terminates when manager context is canceled.
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
			m.logger.Info(m.ctx, "[Async-Queue-Manager] runWorkerWithAutoRestart queue %s process %d started", queueName, processID)
		} else {
			m.logger.Info(m.ctx, "[Async-Queue-Manager] runWorkerWithAutoRestart queue %s process %d restarted (count=%d)", queueName, processID, restartCount)
		}

		// Wait for worker to finish
		err := w.Wait()

		// Check if context was cancelled (shutdown signal)
		select {
		case <-m.ctx.Done():
			// Manager is shutting down, don't restart
			if err != nil {
				m.recordError(queueName, processID, fmt.Errorf("[Async-Queue-Manager] runWorkerWithAutoRestart ctx.Done error : %w", err))
			}
			return
		default:
		}

		// Worker exited (likely reached max_messages), restart it
		if err != nil {
			m.recordError(queueName, processID, fmt.Errorf("[Async-Queue-Manager] runWorkerWithAutoRestart error : %w", err))
		}

		// Create a new worker instance for restart
		consumer := iqueue.NewConsumer(
			q.driver,
			q.channel,
			handler,
			iqueue.WithConsumerConcurrentLimit(cfg.Concurrent),
			iqueue.WithConsumerAutoRestart(cfg.AutoRestart),
			iqueue.WithConsumerMaxMessages(cfg.MaxMessages),
			iqueue.WithConsumerPopTimeout(q.PopTimeout),
			iqueue.WithConsumerHandleTimeout(q.handleTimeout),
			iqueue.WithConsumerRetrySeconds(q.retrySeconds),
			iqueue.WithConsumerMessageTTL(q.messageTTL),
			iqueue.WithConsumerName(queueName),
			iqueue.WithConsumerProcessID(processID),
			iqueue.WithConsumerLogger(m.logger),
		)
		w = iworker.NewWorker(consumer)

		restartCount++
		m.logger.Info(m.ctx, "[Async-Queue-Manager] runWorkerWithAutoRestart queue %s process %d reached max messages (%d), restarting...",
			queueName, processID, cfg.MaxMessages)

		// Optional: add a small delay before restart to avoid tight loops
		time.Sleep(100 * time.Millisecond)
	}
}

const forwarderProcessID = -1
