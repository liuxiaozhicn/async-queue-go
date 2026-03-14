package queue

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/liuxiaozhicn/async-queue-go/pkg/core"
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
	driver          Driver
	handler         Handler
	concurrentLimit int
	maxMessages     int
	handleTimeout   int
	hooks           ConsumerHooks

	name string

	processed int64
	acked     int64
	retried   int64
	requeued  int64
	failed    int64
	dropped   int64
	errors    int64

	errMu    sync.Mutex
	firstErr error
}

func NewConsumer(driver Driver, handler Handler, concurrentLimit int, maxMessages int, name string, handleTimeout int) *Consumer {
	return NewConsumerWithHooks(driver, handler, concurrentLimit, maxMessages, ConsumerHooks{}, name, handleTimeout)
}

func NewConsumerWithHooks(driver Driver, handler Handler, concurrentLimit int, maxMessages int, hooks ConsumerHooks, name string, handleTimeout int) *Consumer {
	if concurrentLimit <= 0 {
		concurrentLimit = 1
	}
	return &Consumer{driver: driver, handler: handler, concurrentLimit: concurrentLimit, maxMessages: maxMessages, hooks: hooks, name: name, handleTimeout: handleTimeout}
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
		if c.maxMessages > 0 && count >= c.maxMessages {
			break
		}
		select {
		case <-ctx.Done():
			log.Printf("[Consumer] context cancelled, waiting for in-flight jobs to finish...")
			start := time.Now()
			wg.Wait()
			log.Printf("[Consumer] all in-flight jobs done, waited %v", time.Since(start))
			return c.runErr()
		default:
		}

		data, message, err := c.driver.Pop(ctx)
		if err != nil {
			if ctx.Err() != nil {
				wg.Wait()
				return c.runErr()
			}
			wg.Wait()
			return err
		}
		if data == "" || message == nil {
			continue
		}
		count++
		atomic.AddInt64(&c.processed, 1)

		sem <- struct{}{}
		wg.Add(1)
		go func(data string, msg *core.Message) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := c.handleOne(ctx, data, msg); err != nil {
				c.recordErr(err)
			}
		}(data, message)
	}

	wg.Wait()
	return c.runErr()
}

func (c *Consumer) recordErr(err error) {
	if err == nil {
		return
	}
	atomic.AddInt64(&c.errors, 1)
	c.errMu.Lock()
	if c.firstErr == nil {
		c.firstErr = err
	}
	c.errMu.Unlock()
}

func (c *Consumer) runErr() error {
	count := atomic.LoadInt64(&c.errors)
	if count == 0 {
		return nil
	}
	c.errMu.Lock()
	first := c.firstErr
	c.errMu.Unlock()
	return &ConsumerRunError{First: first, Count: count}
}

func (c *Consumer) handleOne(ctx context.Context, data string, message *core.Message) error {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Consumer] handler panic: payload=%s attempts=%d/%d panic=%v", message.Payload, message.Attempts, message.MaxAttempts, r)
			c.handleError(context.Background(), data, message)
		}
	}()

	handleCtx, cancel := context.WithTimeout(ctx, time.Duration(c.handleTimeout)*time.Second)
	defer cancel()
	result, err := c.handler.Handle(handleCtx, message)
	// The handler has finished; now we must commit the message disposition
	// (ack/retry/fail/requeue). This context is detached from cancellation
	// to guarantee delivery semantics — a completed handler whose result
	// is not committed leads to duplicate processing.
	atomicCtx := context.WithoutCancel(ctx)
	if err != nil {
		log.Printf("[Consumer] handler error: payload=%s attempts=%d/%d error=%v", message.Payload, message.Attempts, message.MaxAttempts, err)
		return c.handleError(atomicCtx, data, message)
	}
	switch result {
	case core.REQUEUE:
		if err := c.driver.Remove(atomicCtx, data); err != nil {
			return err
		}
		if err := c.driver.Requeue(atomicCtx, data); err != nil {
			return err
		}
		atomic.AddInt64(&c.requeued, 1)
		if c.hooks.OnRequeue != nil {
			c.hooks.OnRequeue(atomicCtx, message)
		}
		return nil

	case core.RETRY:
		if err := c.driver.Remove(atomicCtx, data); err != nil {
			return err
		}
		if message.AttemptsAllowed() {
			if err := c.driver.Retry(atomicCtx, message); err != nil {
				return err
			}
			atomic.AddInt64(&c.retried, 1)
			if c.hooks.OnRetry != nil {
				c.hooks.OnRetry(atomicCtx, message)
			}
		} else {
			// Attempts exhausted — move to failed queue (matches PHP error path)
			if err := c.driver.Fail(atomicCtx, data); err != nil {
				return err
			}
			atomic.AddInt64(&c.failed, 1)
			if c.hooks.OnFail != nil {
				c.hooks.OnFail(atomicCtx, message)
			}
		}
		return nil

	case core.DROP:
		if err := c.driver.Remove(atomicCtx, data); err != nil {
			return err
		}
		atomic.AddInt64(&c.dropped, 1)
		if c.hooks.OnDrop != nil {
			c.hooks.OnDrop(atomicCtx, message)
		}
		return nil

	case core.ACK:
		fallthrough
	default:
		return c.ackAndHook(atomicCtx, data, message)
	}
}

// handleError processes a handler that returned an error: retry if attempts remain, otherwise fail.
// Mirrors PHP: attempts() ? (remove + retry) : fail
func (c *Consumer) handleError(ctx context.Context, data string, message *core.Message) error {
	if message.AttemptsAllowed() {
		if err := c.driver.Remove(ctx, data); err != nil {
			return err
		}
		if err := c.driver.Retry(ctx, message); err != nil {
			return err
		}
		atomic.AddInt64(&c.retried, 1)
		if c.hooks.OnRetry != nil {
			c.hooks.OnRetry(ctx, message)
		}
		return nil
	}

	if err := c.driver.Fail(ctx, data); err != nil {
		return err
	}
	atomic.AddInt64(&c.failed, 1)
	if c.hooks.OnFail != nil {
		c.hooks.OnFail(ctx, message)
	}
	return nil
}

// ackAndHook acknowledges successful processing: removes from reserved,
// increments acked counter, and fires OnAck hook. Only used for ACK result.
func (c *Consumer) ackAndHook(ctx context.Context, data string, message *core.Message) error {
	if err := c.driver.Ack(ctx, data); err != nil {
		return err
	}
	atomic.AddInt64(&c.acked, 1)
	if c.hooks.OnAck != nil {
		c.hooks.OnAck(ctx, message)
	}
	return nil
}
