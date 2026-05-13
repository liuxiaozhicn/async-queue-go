package queue

import "github.com/liuxiaozhicn/async-queue-go/pkg/clock"

type RedisDriverOption func(*redisDriverOptions)

type redisDriverOptions struct {
	clock clock.Clock
}

// defaultRedisDriverOptions returns baseline Redis driver options.
func defaultRedisDriverOptions() redisDriverOptions {
	return redisDriverOptions{
		clock: clock.RealClock{},
	}
}

// WithRedisDriverClock injects custom clock for deterministic tests.
func WithRedisDriverClock(c clock.Clock) RedisDriverOption {
	return func(o *redisDriverOptions) {
		if c != nil {
			o.clock = c
		}
	}
}
