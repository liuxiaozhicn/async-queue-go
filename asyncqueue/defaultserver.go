package asyncqueue

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/liuxiaozhicn/async-queue-go/pkg/core"
)

var defaultServer atomic.Pointer[Server]

// DefaultServer returns the global Server, or nil if none is set.
func DefaultServer() *Server {
	return defaultServer.Load()
}

// SetDefaultServer explicitly sets (or clears) the global Server.
// Pass nil to clear it. Useful for testing.
func SetDefaultServer(s *Server) {
	defaultServer.Store(s)
}

// GetQueue returns the named Queue from the global Server.
func GetQueue(name string) (*Queue, error) {
	s := defaultServer.Load()
	if s == nil {
		return nil, errors.New("no default server: call NewServer first")
	}
	return s.Queue(name)
}

// Push marshals job and enqueues it on the named queue via the global Server.
func Push(ctx context.Context, queueName string, job Job, delaySeconds int) (string, error) {
	q, err := GetQueue(queueName)
	if err != nil {
		return "", err
	}
	return q.PushJob(ctx, job, delaySeconds)
}

// PushMessage enqueues a raw Message on the named queue via the global Server.
func PushMessage(ctx context.Context, queueName string, msg *core.Message, delaySeconds int) (string, error) {
	q, err := GetQueue(queueName)
	if err != nil {
		return "", err
	}
	return q.PushMessage(ctx, msg, delaySeconds)
}

func setDefaultWithWarn(s *Server) {
	old := defaultServer.Swap(s)
	if old != nil {
		s.logger.Warn(context.Background(), "overwriting existing default server")
	}
}
