package queue

import (
	"context"
	"errors"
	"github.com/liuxiaozhicn/async-queue-go/pkg/core"
	"time"
)

type Info struct {
	Waiting  int64
	Reserved int64
	Delayed  int64
	Timeout  int64
	Failed   int64
}

var (
	ErrMessageAlreadyReadyForDispatch = errors.New("message already ready for dispatch and cannot be canceled")
	ErrMessageAlreadyInExecution      = errors.New("message is already in execution and cannot be canceled")
)

type Driver interface {
	Ping(ctx context.Context) error
	Info(ctx context.Context, channel string) (Info, error)
	Push(ctx context.Context, channel string, m *core.Message, delaySeconds int, messageTTL int) error
	Pop(ctx context.Context, channel string, popTimeout time.Duration, handleTimeout time.Duration) (messageID string, message *core.Message, err error)
	Get(ctx context.Context, channel string, id string) (*core.Message, error)
	Cancel(ctx context.Context, channel string, id string) (bool, error)
	Ack(ctx context.Context, channel string, messageID string) error // Acknowledge = remove from reserved
	Requeue(ctx context.Context, channel string, messageID string) error
	Retry(ctx context.Context, channel string, id string, delaySeconds int) (bool, error)
	Drop(ctx context.Context, channel string, messageID string) error // Remove from reserved + mark as dropped
	Fail(ctx context.Context, channel string, messageID string) error // Remove from reserved + push to failed
	ForwardMessages(ctx context.Context, channel string) (forwardedDelayed int64, forwardedTimeout int64, err error)
	Reload(ctx context.Context, channel string, queue string) (int, error)
	Flush(ctx context.Context, channel string, queue string) error
}
