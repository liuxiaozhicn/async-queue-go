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

func defaultQueueOptions(channel string) asyncQueueOptions {
	return asyncQueueOptions{
		popTimeout:    1,
		handleTimeout: 10,
		retrySeconds:  []int{5},
		messageTTL:    86400 * 30,
		maxAttempts:   3,
		name:          channel,
		logger:        logger.Default,
	}
}

func WithQueuePopTimeout(seconds int) QueueOption {
	return func(o *asyncQueueOptions) {
		if seconds > 0 {
			o.popTimeout = seconds
		}
	}
}

func WithQueueHandleTimeout(seconds int) QueueOption {
	return func(o *asyncQueueOptions) {
		if seconds > 0 {
			o.handleTimeout = seconds
		}
	}
}

func WithQueueRetrySeconds(retry []int) QueueOption {
	return func(o *asyncQueueOptions) {
		if len(retry) > 0 {
			o.retrySeconds = append([]int(nil), retry...)
		}
	}
}

func WithQueueMessageTTL(seconds int) QueueOption {
	return func(o *asyncQueueOptions) {
		if seconds >= 0 {
			o.messageTTL = seconds
		}
	}
}

func WithQueueMaxAttempts(max int) QueueOption {
	return func(o *asyncQueueOptions) {
		if max > 0 {
			o.maxAttempts = max
		}
	}
}

func WithQueueName(name string) QueueOption {
	return func(o *asyncQueueOptions) {
		if name != "" {
			o.name = name
		}
	}
}

func WithQueueLogger(l logger.Interface) QueueOption {
	return func(o *asyncQueueOptions) {
		if l != nil {
			o.logger = l
		}
	}
}
