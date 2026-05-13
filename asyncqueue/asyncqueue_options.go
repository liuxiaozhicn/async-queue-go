package asyncqueue

import (
	"github.com/liuxiaozhicn/async-queue-go/pkg/logger"
)

type QueueOption func(*asyncQueueOptions)

type asyncQueueOptions struct {
	popTimeout    int
	handleTimeout int
	retrySeconds  []int
	messageTTL    int
	maxAttempts   int
	name          string
	logger        logger.Interface
}

// defaultQueueOptions returns production-oriented defaults for queue runtime knobs.
//
// These defaults are used when caller does not provide explicit QueueOption overrides.
func defaultQueueOptions(channel string) asyncQueueOptions {
	return asyncQueueOptions{
		popTimeout:    1,
		handleTimeout: 180,
		retrySeconds:  []int{30, 90, 300},
		messageTTL:    defaultMessageTTLSeconds,
		maxAttempts:   3,
		name:          channel,
		logger:        logger.Default,
	}
}

// WithQueuePopTimeout sets blocking pop timeout in seconds.
//
// Non-positive values are ignored.
func WithQueuePopTimeout(seconds int) QueueOption {
	return func(o *asyncQueueOptions) {
		if seconds > 0 {
			o.popTimeout = seconds
		}
	}
}

// WithQueueHandleTimeout sets per-message handler timeout in seconds.
//
// Non-positive values are ignored.
func WithQueueHandleTimeout(seconds int) QueueOption {
	return func(o *asyncQueueOptions) {
		if seconds > 0 {
			o.handleTimeout = seconds
		}
	}
}

// WithQueueRetrySeconds sets retry backoff (seconds) by attempt index.
//
// Empty slices are ignored.
func WithQueueRetrySeconds(retry []int) QueueOption {
	return func(o *asyncQueueOptions) {
		if len(retry) > 0 {
			o.retrySeconds = append([]int(nil), retry...)
		}
	}
}

// WithQueueMessageTTL sets message key TTL in seconds.
//
// Use 0 to disable TTL when supported by driver behavior.
func WithQueueMessageTTL(seconds int) QueueOption {
	return func(o *asyncQueueOptions) {
		if seconds >= 0 {
			o.messageTTL = seconds
		}
	}
}

// WithQueueMaxAttempts sets maximum delivery attempts per message.
//
// Non-positive values are ignored.
func WithQueueMaxAttempts(max int) QueueOption {
	return func(o *asyncQueueOptions) {
		if max > 0 {
			o.maxAttempts = max
		}
	}
}

// WithQueueName sets queue display name for logs and metadata.
//
// Empty names are ignored.
func WithQueueName(name string) QueueOption {
	return func(o *asyncQueueOptions) {
		if name != "" {
			o.name = name
		}
	}
}

// WithQueueLogger sets queue logger implementation.
//
// Nil logger is ignored.
func WithQueueLogger(l logger.Interface) QueueOption {
	return func(o *asyncQueueOptions) {
		if l != nil {
			o.logger = l
		}
	}
}
