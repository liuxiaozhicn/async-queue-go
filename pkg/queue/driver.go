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
	Pop(ctx context.Context) (messageID string, message *core.Message, err error)
	Remove(ctx context.Context, messageID string) error // Remove from reserved queue
	Ack(ctx context.Context, messageID string) error    // Acknowledge = remove from reserved
	Fail(ctx context.Context, messageID string) error   // Remove from reserved + push to failed
	Requeue(ctx context.Context, messageID string) error
	Retry(ctx context.Context, m *core.Message) error
	Reload(ctx context.Context, queue string) (int, error)
	Flush(ctx context.Context, queue string) error
	Info(ctx context.Context) (Info, error)
}

// MessageReader is an optional capability for reading message entities by id.
type MessageReader interface {
	GetMessage(ctx context.Context, id string) (*core.Message, error)
}

// MessageWriter is an optional capability for mutating message entities by id.
type MessageWriter interface {
	DeleteMessage(ctx context.Context, id string) (bool, error)
	RetryMessage(ctx context.Context, id string, delaySeconds int) (bool, error)
}

// MessageForwarder is an optional capability for forwarding due messages.
type MessageForwarder interface {
	ForwardMessages(ctx context.Context) (forwardedDelayed int64, forwardedTimeout int64, err error)
}
