package asyncqueue

import "github.com/liuxiaozhicn/async-queue-go/pkg/queue"

// WithDriver registers a prepared driver instance by driver name.
//
// driverName should match QueueConfig.Driver in config.
// This option is applied during NewServer construction.
func WithDriver(driverName string, driver queue.Driver) Option {
	return func(s *Server) {
		if s == nil || driverName == "" || driver == nil {
			return
		}
		if s.drivers == nil {
			s.drivers = make(map[string]queue.Driver)
		}
		s.drivers[driverName] = driver
	}
}
