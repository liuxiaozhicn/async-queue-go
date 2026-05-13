package queue

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/liuxiaozhicn/async-queue-go/pkg/core"
	"github.com/liuxiaozhicn/async-queue-go/pkg/logger"
)

// Handler defines business processing logic for one message.
type Handler interface {
	Handle(context.Context, *core.Message) (core.Result, error)
}

// HandlerFunc adapts plain function to Handler interface.
type HandlerFunc func(context.Context, *core.Message) (core.Result, error)

// Handle adapts HandlerFunc to Handler interface.
func (f HandlerFunc) Handle(ctx context.Context, m *core.Message) (core.Result, error) {
	return f(ctx, m)
}

type ConsumerHooks struct {
	OnAck     func(context.Context, *core.Message)
	OnRetry   func(context.Context, *core.Message)
	OnRequeue func(context.Context, *core.Message)
	OnFail    func(context.Context, *core.Message)
	OnDrop    func(context.Context, *core.Message)
}

// ConsumerStats exposes cumulative consumer counters.
type ConsumerStats struct {
	Processed int64
	Acked     int64
	Retried   int64
	Requeued  int64
	Failed    int64
	Dropped   int64
	Errors    int64
}

// ConsumerRunError aggregates concurrent run errors.
type ConsumerRunError struct {
	First error
	Count int64
}

// Error formats aggregated consumer run errors.
func (e *ConsumerRunError) Error() string {
	if e == nil || e.First == nil {
		return ""
	}
	if e.Count <= 1 {
		return e.First.Error()
	}
	return fmt.Sprintf("%s (and %d more errors)", e.First.Error(), e.Count-1)
}

// Consumer executes message polling, handling and commit transitions.
type Consumer struct {
	driver              Driver
	channel             string
	handler             Handler
	concurrentLimit     int
	autoRestart         bool
	maxMessages         int
	PopTimeout          time.Duration
	handleTimeout       time.Duration
	retrySeconds        []int
	messageTTL          int
	handleTimeoutAction string

	hooks  ConsumerHooks
	logger logger.Interface

	name      string
	processID int
	processed int64
	acked     int64
	retried   int64
	requeued  int64
	failed    int64
	dropped   int64
	errors    int64

	errMu         sync.Mutex
	concurrentErr error
}

// NewConsumer constructs a message consumer bound to one channel and handler.
func NewConsumer(driver Driver, channel string, handler Handler, opts ...ConsumerOption) *Consumer {
	consumerOptions := defaultConsumerOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(&consumerOptions)
		}
	}

	return &Consumer{
		driver:          driver,
		channel:         channel,
		handler:         handler,
		concurrentLimit: consumerOptions.concurrentLimit,
		autoRestart:     consumerOptions.autoRestart,
		maxMessages:     consumerOptions.maxMessages,
		PopTimeout:      consumerOptions.PopTimeout,
		handleTimeout:   consumerOptions.handleTimeout,
		retrySeconds:    append([]int(nil), consumerOptions.retrySeconds...),
		messageTTL:      consumerOptions.messageTTL,
		hooks:           consumerOptions.hooks,
		logger:          consumerOptions.logger,
		name:            consumerOptions.name,
		processID:       consumerOptions.processID,
	}
}

// Stats returns current consumer counters.
func (c *Consumer) Stats() ConsumerStats {
	return ConsumerStats{
		Processed: atomic.LoadInt64(&c.processed),
		Acked:     atomic.LoadInt64(&c.acked),
		Retried:   atomic.LoadInt64(&c.retried),
		Requeued:  atomic.LoadInt64(&c.requeued),
		Failed:    atomic.LoadInt64(&c.failed),
		Dropped:   atomic.LoadInt64(&c.dropped),
		Errors:    atomic.LoadInt64(&c.errors),
	}
}

// Run starts the consumption loop until context canceled or fatal error occurs.
func (c *Consumer) Run(ctx context.Context) error {
	sem := make(chan struct{}, c.concurrentLimit)
	var wg sync.WaitGroup

	count := 0
	for {
		if c.autoRestart && c.maxMessages > 0 && count >= c.maxMessages {
			break
		}
		select {
		case <-ctx.Done():
			c.logger.Info(ctx, "[Consumer:%s:%d] context cancelled, waiting for running jobs to finish...", c.name, c.processID)
			start := time.Now()
			wg.Wait()
			c.logger.Info(ctx, "[Consumer:%s:%d] all running jobs finished, waited %v", c.name, c.processID, time.Since(start))
			return c.runErr()
		default:
		}

		messageID, message, err := c.driver.Pop(ctx, c.channel, c.PopTimeout, c.handleTimeout)
		if err != nil {
			if ctx.Err() != nil {
				c.logger.Info(ctx, "[Consumer:%s:%d] context cancelled, waiting for running jobs to finish...", c.name, c.processID)
				wg.Wait()
				c.logger.Info(ctx, "[Consumer:%s:%d] running jobs done, consumer shutdown complete ", c.name, c.processID)
				return c.runErr()
			}
			c.logger.Error(ctx, "[Consumer:%s:%d] Pop error: %v", c.name, c.processID, err)
			c.recordErr(err)
			wg.Wait()
			return c.runErr()
		}
		if messageID == "" || message == nil {
			continue
		}
		count++
		atomic.AddInt64(&c.processed, 1)
		c.logger.Info(ctx, "[Consumer:%s:%d] REC|ID:%s payload:%s attempts:%d/%d status:%s",
			c.name, c.processID, messageID, message.Payload, message.Attempts, message.MaxAttempts, message.Status)
		sem <- struct{}{}
		wg.Add(1)
		go func(messageID string, msg *core.Message) {
			defer wg.Done()
			defer func() { <-sem }()
			err := c.handleOne(ctx, messageID, msg)
			if err != nil {
				c.logger.Error(ctx, "[Consumer:%s:%d:handleOneError] ERR|ID:%s payload:%s attempts:%d/%d error:%v",
					c.name, c.processID, messageID, msg.Payload, msg.Attempts, msg.MaxAttempts, err)
				c.incrErrors(err)
			}
		}(messageID, message)
	}

	wg.Wait()
	return c.runErr()
}

// incrErrors increments error counter when err is non-nil.
func (c *Consumer) incrErrors(err error) {
	if err == nil {
		return
	}
	atomic.AddInt64(&c.errors, 1)
}

// recordErr stores first concurrent fatal error.
func (c *Consumer) recordErr(err error) {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	c.concurrentErr = err
}

// runErr builds aggregated run error from internal counters and first error.
func (c *Consumer) runErr() error {
	count := atomic.LoadInt64(&c.errors)
	if count == 0 {
		return nil
	}
	c.errMu.Lock()
	concurrentErr := c.concurrentErr
	c.errMu.Unlock()
	return &ConsumerRunError{First: concurrentErr, Count: count}
}

// handleOne processes a single message and commits handler result atomically.
func (c *Consumer) handleOne(ctx context.Context, messageID string, message *core.Message) error {
	// The handler has finished; now we must commit the message disposition
	// (ack/retry/fail/requeue). This context is detached from cancellation
	// to guarantee delivery semantics — a completed handler whose result
	// is not committed leads to duplicate processing.
	atomicCtx := context.WithoutCancel(ctx)

	defer func() {
		if r := recover(); r != nil {
			c.logger.Warn(ctx, "[Consumer:%s:%d] PANIC|ID:%s payload:%s attempts:%d/%d panic:%v",
				c.name, c.processID, messageID, message.Payload, message.Attempts, message.MaxAttempts, r)
			err := c.handleError(atomicCtx, messageID, message)
			if err != nil {
				c.logger.Warn(ctx, "[Consumer:%s:%d] PANIC||handleError|Err|ID:%s payload:%s attempts:%d/%d error:%#v nextAttempt:%d",
					c.name, c.processID, messageID, message.Payload, message.Attempts-1, message.MaxAttempts, err, message.Attempts)
			}
		}
	}()

	c.logger.Info(ctx, "[Consumer:%s:%d] PROC|ID:%s payload:%s attempts:%d/%d status:%s",
		c.name, c.processID, messageID, message.Payload, message.Attempts, message.MaxAttempts, message.Status)
	handleTimeoutCtx, cancel := context.WithTimeout(atomicCtx, c.handleTimeout)
	defer cancel()
	result, err := c.handler.Handle(handleTimeoutCtx, message)
	if err != nil {
		c.logger.Error(ctx, "[Consumer:%s:%d] FAIL|ID:%s payload:%s attempts:%d/%d error:%v",
			c.name, c.processID, messageID, message.Payload, message.Attempts, message.MaxAttempts, err)
		return c.handleError(atomicCtx, messageID, message)
	}

	switch result {
	case core.REQUEUE:
		if err := c.driver.Requeue(atomicCtx, c.channel, messageID); err != nil {
			return err
		}
		message.SetStatus(core.StatusWaiting)
		atomic.AddInt64(&c.requeued, 1)
		c.logger.Info(ctx, "[Consumer:%s:%d] REQUEUE|ID:%s payload:%s attempts:%d/%d status:%s nextAttempt:%d",
			c.name, c.processID, messageID, message.Payload, message.Attempts, message.MaxAttempts, message.Status, message.Attempts+1)
		if c.hooks.OnRequeue != nil {
			c.hooks.OnRequeue(atomicCtx, message)
		}
		return nil

	case core.RETRY:
		if message.AttemptsAllowed() {
			delaySeconds := core.RetrySeconds(c.retrySeconds, message.Attempts)
			ok, err := c.driver.Retry(atomicCtx, c.channel, messageID, delaySeconds)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("retry target message not found: %s", messageID)
			}
			if delaySeconds <= 0 {
				message.SetStatus(core.StatusWaiting)
			} else {
				message.SetStatus(core.StatusDelayed)
			}
			atomic.AddInt64(&c.retried, 1)
			c.logger.Info(ctx, "[Consumer:%s:%d] RETRY|ID:%s payload:%s attempts:%d/%d status:%s",
				c.name, c.processID, messageID, message.Payload, message.Attempts, message.MaxAttempts, message.Status)
			if c.hooks.OnRetry != nil {
				c.hooks.OnRetry(atomicCtx, message)
			}
		} else {
			// Attempts exhausted — move to failed queue
			if err := c.driver.Fail(atomicCtx, c.channel, messageID); err != nil {
				return err
			}
			message.SetStatus(core.StatusFailed)
			atomic.AddInt64(&c.failed, 1)
			c.logger.Warn(ctx, "[Consumer:%s:%d] RETRY|ID:%s payload:%s attempts:%d/%d status:%s moveTo=failed",
				c.name, c.processID, messageID, message.Payload, message.Attempts, message.MaxAttempts, message.Status)
			if c.hooks.OnFail != nil {
				c.hooks.OnFail(atomicCtx, message)
			}
		}
		return nil

	case core.DROP:
		if err := c.driver.Drop(atomicCtx, c.channel, messageID); err != nil {
			return err
		}
		message.SetStatus(core.StatusDropped)
		atomic.AddInt64(&c.dropped, 1)
		c.logger.Info(ctx, "[Consumer:%s:%d] DROP|ID:%s payload:%s attempts:%d/%d status:%s",
			c.name, c.processID, messageID, message.Payload, message.Attempts, message.MaxAttempts, message.Status)
		if c.hooks.OnDrop != nil {
			c.hooks.OnDrop(atomicCtx, message)
		}
		return nil

	case core.ACK:
		fallthrough
	default:
		if err := c.driver.Ack(atomicCtx, c.channel, messageID); err != nil {
			return err
		}
		message.SetStatus(core.StatusDone)
		atomic.AddInt64(&c.acked, 1)
		c.logger.Info(ctx, "[Consumer:%s:%d] ACK|ID:%s payload:%s attempts:%d/%d status:%s",
			c.name, c.processID, messageID, message.Payload, message.Attempts, message.MaxAttempts, message.Status)
		if c.hooks.OnAck != nil {
			c.hooks.OnAck(atomicCtx, message)
		}
		return nil
	}
}

// handleError processes a handler that returned an error: retry if attempts to remain, otherwise fail.
func (c *Consumer) handleError(ctx context.Context, messageID string, message *core.Message) error {
	if message.AttemptsAllowed() {
		delaySeconds := core.RetrySeconds(c.retrySeconds, message.Attempts)
		ok, err := c.driver.Retry(ctx, c.channel, messageID, delaySeconds)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("retry target message not found: %s", messageID)
		}
		if delaySeconds <= 0 {
			message.SetStatus(core.StatusWaiting)
		} else {
			message.SetStatus(core.StatusDelayed)
		}
		atomic.AddInt64(&c.retried, 1)
		c.logger.Info(ctx, "[Consumer:%s:%d:handleError] RETRY|ID:%s payload:%s attempts:%d/%d status:%s ",
			c.name, c.processID, messageID, message.Payload, message.Attempts, message.MaxAttempts, message.Status)
		if c.hooks.OnRetry != nil {
			c.hooks.OnRetry(ctx, message)
		}
		return nil
	}

	if err := c.driver.Fail(ctx, c.channel, messageID); err != nil {
		return err
	}
	message.SetStatus(core.StatusFailed)
	atomic.AddInt64(&c.failed, 1)
	c.logger.Info(ctx, "[Consumer:%s:%d:handleError] FAIL|ID:%s payload:%s attempts:%d/%d status:%s",
		c.name, c.processID, messageID, message.Payload, message.Attempts, message.MaxAttempts, message.Status)
	if c.hooks.OnFail != nil {
		c.hooks.OnFail(ctx, message)
	}
	return nil
}
