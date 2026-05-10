package queue

import "github.com/liuxiaozhicn/async-queue-go/pkg/clock"

type RedisDriverOption func(*redisDriverOptions)

type redisDriverOptions struct {
	clock clock.Clock
}

func defaultRedisDriverOptions() redisDriverOptions {
	return redisDriverOptions{
		clock: clock.RealClock{},
	}
}

func WithRedisDriverClock(c clock.Clock) RedisDriverOption {
	return func(o *redisDriverOptions) {
		if c != nil {
			o.clock = c
		}
	}
}
