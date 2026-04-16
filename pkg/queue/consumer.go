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

type Handler interface {
	Handle(context.Context, *core.Message) (core.Result, error)
}

type HandlerFunc func(context.Context, *core.Message) (core.Result, error)

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

type ConsumerStats struct {
	Processed int64
	Acked     int64
	Retried   int64
	Requeued  int64
	Failed    int64
	Dropped   int64
	Errors    int64
}

type ConsumerRunError struct {
	First error
	Count int64
}

func (e *ConsumerRunError) Error() string {
	if e == nil || e.First == nil {
		return ""
	}
	if e.Count <= 1 {
		return e.First.Error()
	}
	return fmt.Sprintf("%s (and %d more errors)", e.First.Error(), e.Count-1)
}

type Consumer struct {
	driver              Driver
	handler             Handler
	concurrentLimit     int
	autoRestart         bool
	maxMessages         int
	handleTimeout       int
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

func NewConsumer(driver Driver, handler Handler, concurrentLimit int, autoRestart bool, maxMessages int, name string, processID int, handleTimeout int, l logger.Interface) *Consumer {
	return NewConsumerWithHooks(driver, handler, concurrentLimit, autoRestart, maxMessages, ConsumerHooks{}, name, processID, handleTimeout, l)
}

func NewConsumerWithHooks(driver Driver, handler Handler, concurrentLimit int, autoRestart bool, maxMessages int, hooks ConsumerHooks, name string, processID int, handleTimeout int, l logger.Interface) *Consumer {
	if concurrentLimit <= 0 {
		concurrentLimit = 1
	}
	if l == nil {
		l = logger.Default
	}
	return &Consumer{driver: driver, handler: handler, concurrentLimit: concurrentLimit, autoRestart: autoRestart, maxMessages: maxMessages, hooks: hooks, name: name, processID: processID, handleTimeout: handleTimeout, logger: l}
}

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
			c.logger.Info(ctx, "[Consumer:%s-%d] context cancelled, waiting for running jobs to finish...", c.name, c.processID)
			start := time.Now()
			wg.Wait()
			c.logger.Info(ctx, "[Consumer:%s-%d] all running jobs finished, waited %v", c.name, c.processID, time.Since(start))
			return c.runErr()
		default:
		}

		messageID, message, err := c.driver.Pop(ctx)
		if err != nil {
			if ctx.Err() != nil {
				c.logger.Info(ctx, "[Consumer:%s-%d] context cancelled, waiting for running jobs to finish...", c.name, c.processID)
				wg.Wait()
				c.logger.Info(ctx, "[Consumer:%s-%d] running jobs done, consumer shutdown complete ", c.name, c.processID)
				return c.runErr()
			}
			c.logger.Error(ctx, "[Consumer:%s-%d] Pop error: %v", c.name, c.processID, err)
			c.recordErr(err)
			wg.Wait()
			return c.runErr()
		}
		if messageID == "" || message == nil {
			continue
		}
		count++
		atomic.AddInt64(&c.processed, 1)
		c.logger.Info(ctx, "[Consumer:%s-%d] REC|ID:%s payload:%s attempts:%d/%d status:%s",
			c.name, c.processID, messageID, message.Payload, message.Attempts, message.MaxAttempts, message.Status)
		sem <- struct{}{}
		wg.Add(1)
		go func(messageID string, msg *core.Message) {
			defer wg.Done()
			defer func() { <-sem }()
			err := c.handleOne(ctx, messageID, msg)
			if err != nil {
					c.logger.Error(ctx, "[Consumer:%s-%d:handleOneError] ERR|ID:%s payload:%s attempts:%d/%d error:%v",
					c.name, c.processID, messageID, msg.Payload, msg.Attempts, msg.MaxAttempts, err)
				c.incrErrors(err)
			}
		}(messageID, message)
	}

	wg.Wait()
	return c.runErr()
}

func (c *Consumer) incrErrors(err error) {
	if err == nil {
		return
	}
	atomic.AddInt64(&c.errors, 1)
}

func (c *Consumer) recordErr(err error) {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	c.concurrentErr = err
}

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

func (c *Consumer) handleOne(ctx context.Context, messageID string, message *core.Message) error {
	// The handler has finished; now we must commit the message disposition
	// (ack/retry/fail/requeue). This context is detached from cancellation
	// to guarantee delivery semantics — a completed handler whose result
	// is not committed leads to duplicate processing.
	atomicCtx := context.WithoutCancel(ctx)

	defer func() {
		if r := recover(); r != nil {
			c.logger.Error(ctx, "[Consumer:%s-%d:handleOne] PANIC|ID:%s payload:%s attempts:%d/%d panic:%v",
				c.name, c.processID, messageID, message.Payload, message.Attempts, message.MaxAttempts, r)
			err := c.handleError(atomicCtx, messageID, message)
			if err != nil {
				c.logger.Error(ctx, "[Consumer:%s-%d:handleError] FAILOVER|ID:%s payload:%s attempts:%d/%d error:%#v nextAttempt:%d",
					c.name, c.processID, messageID, message.Payload, message.Attempts, message.MaxAttempts, err, message.Attempts+1)
			}
		}
	}()

	c.logger.Info(ctx, "[Consumer:%s-%d] PROC|ID:%s payload:%s attempts:%d/%d status:%s",
		c.name, c.processID, messageID, message.Payload, message.Attempts, message.MaxAttempts, message.Status)
	handleTimeoutCtx, cancel := context.WithTimeout(atomicCtx, time.Duration(c.handleTimeout)*time.Second)
	defer cancel()
	result, err := c.handler.Handle(handleTimeoutCtx, message)
	if err != nil {
		c.logger.Error(ctx, "[Consumer:%s-%d:handleOne] FAIL|ID:%s payload:%s attempts:%d/%d error:%v",
			c.name, c.processID, messageID, message.Payload, message.Attempts, message.MaxAttempts, err)
		return c.handleError(atomicCtx, messageID, message)
	}

	switch result {
	case core.REQUEUE:
		if err := c.driver.Remove(atomicCtx, messageID); err != nil {
			return err
		}
		if err := c.driver.Requeue(atomicCtx, messageID); err != nil {
			return err
		}
		atomic.AddInt64(&c.requeued, 1)
			c.logger.Info(ctx, "[Consumer:%s-%d] REQ|ID:%s payload:%s attempts:%d/%d status:%s nextAttempt:%d",
			c.name, c.processID, messageID, message.Payload, message.Attempts, message.MaxAttempts, message.Status, message.Attempts+1)
		if c.hooks.OnRequeue != nil {
			c.hooks.OnRequeue(atomicCtx, message)
		}
		return nil

	case core.RETRY:
		if err := c.driver.Remove(atomicCtx, messageID); err != nil {
			return err
		}
		if message.AttemptsAllowed() {
			if err := c.driver.Retry(atomicCtx, message); err != nil {
				return err
			}
			atomic.AddInt64(&c.retried, 1)
				c.logger.Info(ctx, "[Consumer:%s-%d] RETRY|ID:%s payload:%s attempts:%d/%d status:%s nextAttempt:%d",
				c.name, c.processID, messageID, message.Payload, message.Attempts, message.MaxAttempts, message.Status, message.Attempts+1)
			if c.hooks.OnRetry != nil {
				c.hooks.OnRetry(atomicCtx, message)
			}
		} else {
			// Attempts exhausted — move to failed queue
			if err := c.driver.Fail(atomicCtx, messageID); err != nil {
				return err
			}
			atomic.AddInt64(&c.failed, 1)
				c.logger.Info(ctx, "[Consumer:%s-%d] RETRY|ID:%s payload:%s attempts:%d/%d status:%s moveTo=failed",
				c.name, c.processID, messageID, message.Payload, message.Attempts, message.MaxAttempts, message.Status)
			if c.hooks.OnFail != nil {
				c.hooks.OnFail(atomicCtx, message)
			}
		}
		return nil

	case core.DROP:
		if err := c.driver.Remove(atomicCtx, messageID); err != nil {
			return err
		}
		atomic.AddInt64(&c.dropped, 1)
			c.logger.Info(ctx, "[Consumer:%s-%d] DROP|ID:%s payload:%s attempts:%d/%d status:%s",
			c.name, c.processID, messageID, message.Payload, message.Attempts, message.MaxAttempts, message.Status)
		if c.hooks.OnDrop != nil {
			c.hooks.OnDrop(atomicCtx, message)
		}
		return nil

	case core.ACK:
		fallthrough
	default:
		return c.ackAndHook(atomicCtx, messageID, message)
	}
}

// handleError processes a handler that returned an error: retry if attempts to remain, otherwise fail.
func (c *Consumer) handleError(ctx context.Context, messageID string, message *core.Message) error {
	if message.AttemptsAllowed() {
		if err := c.driver.Remove(ctx, messageID); err != nil {
			return err
		}
		if err := c.driver.Retry(ctx, message); err != nil {
			return err
		}
		atomic.AddInt64(&c.retried, 1)
			c.logger.Info(ctx, "[Consumer:%s-%d:handleError] RETRY|ID:%s payload:%s attempts:%d/%d status:%s nextAttempt:%d",
			c.name, c.processID, messageID, message.Payload, message.Attempts, message.MaxAttempts, message.Status, message.Attempts+1)
		if c.hooks.OnRetry != nil {
			c.hooks.OnRetry(ctx, message)
		}
		return nil
	}

	if err := c.driver.Fail(ctx, messageID); err != nil {
		return err
	}
	atomic.AddInt64(&c.failed, 1)
	c.logger.Info(ctx, "[Consumer:%s-%d:handleError] FAILED|ID:%s payload:%s attempts:%d/%d status:%s",
		c.name, c.processID, messageID, message.Payload, message.Attempts, message.MaxAttempts, message.Status)
	if c.hooks.OnFail != nil {
		c.hooks.OnFail(ctx, message)
	}
	return nil
}

// ackAndHook acknowledges successful processing: removes from reserved,
// increments acked counter, and fires OnAck hook. Only used for ACK result.
func (c *Consumer) ackAndHook(ctx context.Context, messageID string, message *core.Message) error {
	if err := c.driver.Ack(ctx, messageID); err != nil {
		return err
	}

	logMessage := message
	if reader, ok := c.driver.(MessageReader); ok {
		latestMessage, err := reader.GetMessage(ctx, messageID)
		if err == nil && latestMessage != nil {
			*message = *latestMessage
			logMessage = message
		}
	}

	atomic.AddInt64(&c.acked, 1)
	c.logger.Info(ctx, "[Consumer:%s-%d] ACK|ID:%s payload:%s attempts:%d/%d status:%s",
		c.name, c.processID, messageID, logMessage.Payload, logMessage.Attempts, logMessage.MaxAttempts, logMessage.Status)
	if c.hooks.OnAck != nil {
		c.hooks.OnAck(ctx, logMessage)
	}
	return nil
}
