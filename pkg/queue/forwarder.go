package queue

import (
	"context"
	"time"

	"github.com/liuxiaozhicn/async-queue-go/pkg/logger"
)

// forwarder moves delayed/expired messages between internal buckets.
type forwarder struct {
	driver               Driver
	queueName            string
	channel              string
	logger               logger.Interface
	idlePollInterval     time.Duration
	busyPollInterval     time.Duration
	errorRetryBackoff    time.Duration
	errorRetryMaxBackoff time.Duration
}

// NewForwarder creates a background forwarder for scheduled and timeout-recovery flows.
func NewForwarder(driver Driver, queueName string, channel string, l logger.Interface) *forwarder {
	if driver == nil {
		return nil
	}
	if l == nil {
		l = logger.Default
	}
	f := &forwarder{
		driver:               driver,
		queueName:            queueName,
		channel:              channel,
		logger:               l,
		idlePollInterval:     time.Second,
		busyPollInterval:     time.Second,
		errorRetryBackoff:    2 * time.Second,
		errorRetryMaxBackoff: 30 * time.Second,
	}
	f.normalizeIntervals()
	return f
}

// Run executes forwarding periodically until ctx is canceled.
// Transient errors are retried with exponential backoff; the forwarder
// only stops when ctx is canceled.
func (f *forwarder) Run(ctx context.Context) error {
	if f == nil || f.driver == nil {
		return nil
	}
	f.normalizeIntervals()

	errorBackoff := newBackoff(f.errorRetryBackoff, f.errorRetryMaxBackoff)
	timer := time.NewTimer(0)
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
				f.logger.Error(ctx, "[Forwarder:%s] forward failed, retrying in %s | error=%v", f.queueName, errorBackoff.Current(), err)
				timer.Reset(errorBackoff.Current())
				errorBackoff.Advance()
				continue
			}

			errorBackoff.Reset()

			totalMoved := forwardedDelayed + forwardedTimeout
			nextPollInterval := f.idlePollInterval
			if totalMoved > 0 {
				nextPollInterval = f.busyPollInterval
				f.logger.Info(
					ctx,
					"[Forwarder:%s] FORWARD|delayed:%d timeout:%d total:%d nextPoll:%s",
					f.queueName, forwardedDelayed, forwardedTimeout, totalMoved, nextPollInterval,
				)
			}
			timer.Reset(nextPollInterval)
		}
	}
}

func (f *forwarder) normalizeIntervals() {
	if f.idlePollInterval <= 0 {
		f.idlePollInterval = time.Second
	}
	if f.busyPollInterval <= 0 {
		f.busyPollInterval = time.Second
	}
	if f.errorRetryBackoff <= 0 {
		f.errorRetryBackoff = 2 * time.Second
	}
	if f.errorRetryMaxBackoff <= 0 || f.errorRetryMaxBackoff < f.errorRetryBackoff {
		f.errorRetryMaxBackoff = f.errorRetryBackoff
	}
}
