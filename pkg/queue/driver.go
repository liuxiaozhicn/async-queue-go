package queue

import (
	"context"
	"time"

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
	Ping(ctx context.Context) error
	Push(ctx context.Context, channel string, m *core.Message, delaySeconds int, messageTTL int) error
	Delete(ctx context.Context, channel string, m *core.Message) error
	Pop(ctx context.Context, channel string, popTimeout time.Duration, handleTimeout time.Duration) (messageID string, message *core.Message, err error)
	Remove(ctx context.Context, channel string, messageID string) error // Remove from reserved queue
	Ack(ctx context.Context, channel string, messageID string) error    // Acknowledge = remove from reserved
	Fail(ctx context.Context, channel string, messageID string) error   // Remove from reserved + push to failed
	Requeue(ctx context.Context, channel string, messageID string) error
	Retry(ctx context.Context, channel string, m *core.Message, retrySeconds []int) error
	Reload(ctx context.Context, channel string, queue string) (int, error)
	Flush(ctx context.Context, channel string, queue string) error
	Info(ctx context.Context, channel string) (Info, error)
}

// MessageReader is an optional capability for reading message entities by id.
type MessageReader interface {
	GetMessage(ctx context.Context, channel string, id string) (*core.Message, error)
}

// MessageWriter is an optional capability for mutating message entities by id.
type MessageWriter interface {
	DeleteMessage(ctx context.Context, channel string, id string) (bool, error)
	RetryMessage(ctx context.Context, channel string, id string, delaySeconds int) (bool, error)
}

// MessageForwarder is an optional capability for forwarding due messages.
type MessageForwarder interface {
	ForwardMessages(ctx context.Context, channel string) (forwardedDelayed int64, forwardedTimeout int64, err error)
}
