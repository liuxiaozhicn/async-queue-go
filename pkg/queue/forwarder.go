package queue

import (
	"context"
	"time"

	"github.com/liuxiaozhicn/async-queue-go/pkg/logger"
)

type forwarder struct {
	driver       MessageForwarder
	queueName    string
	logger       logger.Interface
	idleInterval time.Duration
	busyInterval time.Duration
}

// NewForwarder creates a background forwarder for scheduled and timeout-recovery flows.
// Returns nil when the provided driver does not support forwarding capability.
func NewForwarder(driver Driver, queueName string, l logger.Interface) *forwarder {
	if driver == nil {
		return nil
	}
	forwarderDriver, ok := driver.(MessageForwarder)
	if !ok {
		return nil
	}
	if l == nil {
		l = logger.Default
	}
	return &forwarder{
		driver:       forwarderDriver,
		queueName:    queueName,
		logger:       l,
		idleInterval: time.Second,
		busyInterval: time.Second,
	}
}

// Run executes forwarding periodically until ctx is canceled.
func (f *forwarder) Run(ctx context.Context) error {
	if f == nil || f.driver == nil {
		return nil
	}

	nextInterval := f.idleInterval
	if nextInterval <= 0 {
		nextInterval = time.Second
	}
	if f.busyInterval <= 0 {
		f.busyInterval = time.Second
	}

	forwardedDelayed, forwardedTimeout, err := f.driver.ForwardMessages(ctx)
	if err != nil {
		if ctx.Err() != nil {
			f.logger.Info(ctx, "[Forwarder:%s] FORWARD|shutdown complete", f.queueName)
			return nil
		}
		f.logger.Error(ctx, "[Forwarder:%s] FORWARD|error:%v", f.queueName, err)
		return err
	}
	moved := forwardedDelayed + forwardedTimeout
	if moved > 0 {
		nextInterval = f.busyInterval
	}
	f.logger.Info(
		ctx,
		"[Forwarder:%s] FORWARD|delayed_forwarded:%d timeout_forwarded:%d moved:%d next:%s",
		f.queueName, forwardedDelayed, forwardedTimeout, moved, nextInterval,
	)

	timer := time.NewTimer(nextInterval)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			f.logger.Info(ctx, "[Forwarder:%s] FORWARD|shutdown complete", f.queueName)
			return nil
		case <-timer.C:
			forwardedDelayed, forwardedTimeout, err := f.driver.ForwardMessages(ctx)
			if err != nil {
				if ctx.Err() != nil {
					f.logger.Info(ctx, "[Forwarder:%s] FORWARD|shutdown complete", f.queueName)
					return nil
				}
				f.logger.Error(ctx, "[Forwarder:%s] FORWARD|error:%v", f.queueName, err)
				return err
			}

			moved := forwardedDelayed + forwardedTimeout
			if moved > 0 {
				nextInterval = f.busyInterval
			} else {
				nextInterval = f.idleInterval
			}
			f.logger.Info(
				ctx,
				"[Forwarder:%s] FORWARD|delayed_forwarded:%d timeout_forwarded:%d moved:%d next:%s",
				f.queueName, forwardedDelayed, forwardedTimeout, moved, nextInterval,
			)
			timer.Reset(nextInterval)
		}
	}
}
