package queue

import "time"

type ConsumerOption func(*consumerOptions)

type consumerOptions struct {
	PopTimeout   time.Duration
	retrySeconds []int
	messageTTL   int
}

func defaultConsumerOptions() consumerOptions {
	return consumerOptions{
		PopTimeout:   time.Second,
		retrySeconds: []int{5},
	}
}

func WithConsumerPopTimeout(timeout time.Duration) ConsumerOption {
	return func(o *consumerOptions) {
		if timeout > 0 {
			o.PopTimeout = timeout
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
