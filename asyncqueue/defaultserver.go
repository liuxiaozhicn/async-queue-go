package asyncqueue

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/liuxiaozhicn/async-queue-go/pkg/core"
)

var defaultServer atomic.Pointer[Server]

// DefaultServer returns the current global server instance.
//
// It returns nil when no server has been created or after explicit reset.
func DefaultServer() *Server {
	return defaultServer.Load()
}

// SetDefaultServer explicitly sets (or clears) the global Server.
// Pass nil to clear it. Useful for testing.
func SetDefaultServer(s *Server) {
	defaultServer.Store(s)
}

// GetQueue returns a queue facade from the global server.
//
// Returns error when global server is not initialized or queue is missing.
func GetQueue(name string) (*Queue, error) {
	s := defaultServer.Load()
	if s == nil {
		return nil, errors.New("no default server: call NewServer first")
	}
	return s.Queue(name)
}

// Push enqueues a job through the global server queue.
//
// It is a convenience wrapper for GetQueue(queueName).PushJob(...).
func Push(ctx context.Context, queueName string, job Job, delaySeconds int) (string, error) {
	q, err := GetQueue(queueName)
	if err != nil {
		return "", err
	}
	return q.PushJob(ctx, job, delaySeconds)
}

// PushMessage enqueues a raw message through the global server queue.
//
// It is a convenience wrapper for GetQueue(queueName).PushMessage(...).
func PushMessage(ctx context.Context, queueName string, msg *core.Message, delaySeconds int) (string, error) {
	q, err := GetQueue(queueName)
	if err != nil {
		return "", err
	}
	return q.PushMessage(ctx, msg, delaySeconds)
}

// setDefaultWithWarn swaps the global server and warns on overwrite.
func setDefaultWithWarn(s *Server) {
	old := defaultServer.Swap(s)
	if old != nil {
		s.logger.Warn(context.Background(), "overwriting existing default server")
	}
}
