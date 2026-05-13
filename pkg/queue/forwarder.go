package queue

import (
	"context"
	"time"

	"github.com/liuxiaozhicn/async-queue-go/pkg/logger"
)

// forwarder moves delayed/expired messages between internal buckets.
type forwarder struct {
	driver           Driver
	queueName        string
	channel          string
	logger           logger.Interface
	idleInterval     time.Duration
	busyInterval     time.Duration
	errorInterval    time.Duration
	errorMaxInterval time.Duration
}

// NewForwarder creates a background forwarder for scheduled and timeout-recovery flows.
func NewForwarder(driver Driver, queueName string, channel string, l logger.Interface) *forwarder {
	if driver == nil {
		return nil
	}
	if l == nil {
		l = logger.Default
	}
	return &forwarder{
		driver:           driver,
		queueName:        queueName,
		channel:          channel,
		logger:           l,
		idleInterval:     time.Second,
		busyInterval:     time.Second,
		errorInterval:    2 * time.Second,
		errorMaxInterval: 30 * time.Second,
	}
}

// Run executes forwarding periodically until ctx is canceled.
// Transient errors are retried with exponential backoff; the forwarder
// only stops when ctx is canceled.
func (f *forwarder) Run(ctx context.Context) error {
	if f == nil || f.driver == nil {
		return nil
	}

	idleInterval := f.idleInterval
	if idleInterval <= 0 {
		idleInterval = time.Second
	}
	if f.busyInterval <= 0 {
		f.busyInterval = time.Second
	}

	errorInterval := f.errorInterval
	if errorInterval <= 0 {
		errorInterval = 2 * time.Second
	}
	errorMaxInterval := f.errorMaxInterval
	if errorMaxInterval <= 0 {
		errorMaxInterval = 30 * time.Second
	}

	nextInterval := time.Duration(0)
	currentErrorInterval := errorInterval
	timer := time.NewTimer(nextInterval)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			f.logger.Info(ctx, "[Forwarder:%s] shutdown complete", f.queueName)
			return nil
		case <-timer.C:
			forwardedDelayed, forwardedTimeout, err := f.driver.ForwardMessages(ctx, f.channel)
			if err != nil {
				if ctx.Err() != nil {
					f.logger.Info(ctx, "[Forwarder:%s] shutdown complete", f.queueName)
					return nil
				}
				f.logger.Error(ctx, "[Forwarder:%s] forward failed, retrying in %s | error=%v", f.queueName, currentErrorInterval, err)
				timer.Reset(currentErrorInterval)
				if currentErrorInterval < errorMaxInterval {
					currentErrorInterval *= 2
					if currentErrorInterval > errorMaxInterval {
						currentErrorInterval = errorMaxInterval
					}
				}
				continue
			}

			currentErrorInterval = errorInterval

			totalMoved := forwardedDelayed + forwardedTimeout
			if totalMoved > 0 {
				nextInterval = f.busyInterval
				f.logger.Info(
					ctx,
					"[Forwarder:%s] FORWARD|delayed:%d timeout:%d total:%d nextPoll:%s",
					f.queueName, forwardedDelayed, forwardedTimeout, totalMoved, nextInterval,
				)
			} else {
				nextInterval = idleInterval
			}
			timer.Reset(nextInterval)
		}
	}
}
