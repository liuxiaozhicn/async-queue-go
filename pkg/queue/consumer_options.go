package queue

import (
	"time"

	"github.com/liuxiaozhicn/async-queue-go/pkg/logger"
)

const defaultConsumerMessageTTLSeconds = 10 * 24 * 60 * 60

type ConsumerOption func(*consumerOptions)

type consumerOptions struct {
	concurrentLimit int
	autoRestart     bool
	maxMessages     int
	PopTimeout      time.Duration
	handleTimeout   time.Duration
	retrySeconds    []int
	messageTTL      int
	hooks           ConsumerHooks
	name            string
	processID       int
	logger          logger.Interface
}

// defaultConsumerOptions returns baseline runtime options for consumers.
func defaultConsumerOptions() consumerOptions {
	return consumerOptions{
		concurrentLimit: 10,
		PopTimeout:      1 * time.Second,
		handleTimeout:   180 * time.Second,
		retrySeconds:    []int{30, 90, 180, 300},
		messageTTL:      defaultConsumerMessageTTLSeconds,
		logger:          logger.Default,
	}
}

// WithConsumerConcurrentLimit sets max in-flight handler goroutines.
func WithConsumerConcurrentLimit(limit int) ConsumerOption {
	return func(o *consumerOptions) {
		if limit > 0 {
			o.concurrentLimit = limit
		}
	}
}

// WithConsumerAutoRestart enables/disables bounded auto-restart mode.
func WithConsumerAutoRestart(enabled bool) ConsumerOption {
	return func(o *consumerOptions) {
		o.autoRestart = enabled
	}
}

// WithConsumerMaxMessages sets per-run message cap; 0 means unlimited.
func WithConsumerMaxMessages(maxMessages int) ConsumerOption {
	return func(o *consumerOptions) {
		if maxMessages >= 0 {
			o.maxMessages = maxMessages
		}
	}
}

// WithConsumerPopTimeout sets polling wait timeout for Pop.
func WithConsumerPopTimeout(timeout time.Duration) ConsumerOption {
	return func(o *consumerOptions) {
		if timeout > 0 {
			o.PopTimeout = timeout
		}
	}
}

// WithConsumerHandleTimeout sets per-message handle timeout.
func WithConsumerHandleTimeout(timeout time.Duration) ConsumerOption {
	return func(o *consumerOptions) {
		if timeout > 0 {
			o.handleTimeout = timeout
		}
	}
}

// WithConsumerRetrySeconds sets retry delay sequence by attempt index.
func WithConsumerRetrySeconds(retry []int) ConsumerOption {
	return func(o *consumerOptions) {
		if len(retry) > 0 {
			o.retrySeconds = append([]int(nil), retry...)
		}
	}
}

// WithConsumerMessageTTL sets message TTL metadata in seconds.
func WithConsumerMessageTTL(seconds int) ConsumerOption {
	return func(o *consumerOptions) {
		if seconds >= 0 {
			o.messageTTL = seconds
		}
	}
}

// WithConsumerHooks sets optional lifecycle callbacks.
func WithConsumerHooks(hooks ConsumerHooks) ConsumerOption {
	return func(o *consumerOptions) {
		o.hooks = hooks
	}
}

// WithConsumerName sets consumer logical name for logs and metrics.
func WithConsumerName(name string) ConsumerOption {
	return func(o *consumerOptions) {
		o.name = name
	}
}

// WithConsumerProcessID sets process index used in logs and hooks.
func WithConsumerProcessID(processID int) ConsumerOption {
	return func(o *consumerOptions) {
		o.processID = processID
	}
}

// WithConsumerLogger sets consumer logger implementation.
func WithConsumerLogger(l logger.Interface) ConsumerOption {
	return func(o *consumerOptions) {
		if l != nil {
			o.logger = l
		}
	}
}
