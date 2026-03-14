package queue

import (
	"context"

	"github.com/liuxiaozhicn/async-queue-go/pkg/core"
)

type Info struct {
	Waiting  int64
	Reserved int64
	Delayed  int64
	Timeout  int64
	Failed   int64
}

type Driver interface {
	Push(ctx context.Context, m *core.Message, delaySeconds int) error
	Delete(ctx context.Context, m *core.Message) error
	Pop(ctx context.Context) (string, *core.Message, error)
	Remove(ctx context.Context, data string) error // Remove from reserved queue
	Ack(ctx context.Context, data string) error    // Acknowledge = remove from reserved
	Fail(ctx context.Context, data string) error   // Remove from reserved + push to failed
	Requeue(ctx context.Context, data string) error
	Retry(ctx context.Context, m *core.Message) error
	Reload(ctx context.Context, queue string) (int, error)
	Flush(ctx context.Context, queue string) error
	Info(ctx context.Context) (Info, error)
}
