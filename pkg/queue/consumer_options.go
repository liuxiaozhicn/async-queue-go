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

func defaultConsumerOptions() consumerOptions {
	return consumerOptions{
		concurrentLimit: 10,
		PopTimeout:      3 * time.Second,
		handleTimeout:   180 * time.Second,
		retrySeconds:    []int{30, 90, 180, 300},
		messageTTL:      defaultConsumerMessageTTLSeconds,
		logger:          logger.Default,
	}
}

func WithConsumerConcurrentLimit(limit int) ConsumerOption {
	return func(o *consumerOptions) {
		if limit > 0 {
			o.concurrentLimit = limit
		}
	}
}

func WithConsumerAutoRestart(enabled bool) ConsumerOption {
	return func(o *consumerOptions) {
		o.autoRestart = enabled
	}
}

func WithConsumerMaxMessages(maxMessages int) ConsumerOption {
	return func(o *consumerOptions) {
		if maxMessages >= 0 {
			o.maxMessages = maxMessages
		}
	}
}

func WithConsumerPopTimeout(timeout time.Duration) ConsumerOption {
	return func(o *consumerOptions) {
		if timeout > 0 {
			o.PopTimeout = timeout
		}
	}
}

func WithConsumerHandleTimeout(timeout time.Duration) ConsumerOption {
	return func(o *consumerOptions) {
		if timeout > 0 {
			o.handleTimeout = timeout
		}
	}
}

func WithConsumerRetrySeconds(retry []int) ConsumerOption {
	return func(o *consumerOptions) {
		if len(retry) > 0 {
			o.retrySeconds = append([]int(nil), retry...)
		}
	}
}

func WithConsumerMessageTTL(seconds int) ConsumerOption {
	return func(o *consumerOptions) {
		if seconds >= 0 {
			o.messageTTL = seconds
		}
	}
}

func WithConsumerHooks(hooks ConsumerHooks) ConsumerOption {
	return func(o *consumerOptions) {
		o.hooks = hooks
	}
}

func WithConsumerName(name string) ConsumerOption {
	return func(o *consumerOptions) {
		o.name = name
	}
}

func WithConsumerProcessID(processID int) ConsumerOption {
	return func(o *consumerOptions) {
		o.processID = processID
	}
}

func WithConsumerLogger(l logger.Interface) ConsumerOption {
	return func(o *consumerOptions) {
		if l != nil {
			o.logger = l
		}
	}
}
