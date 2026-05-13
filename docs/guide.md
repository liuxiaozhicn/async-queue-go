# Detailed Guide

[简体中文](zh-CN/guide.md)

## Overview

`async-queue-go` separates business routing from backend wiring:

- `queue name`: business queue key such as `order`
- `driver name`: backend registration key such as `redis`
- `channel`: backend storage namespace such as `queue:order`

The current repository ships with a Redis implementation and keeps the runtime behind `pkg/queue.Driver`.

## Configuration Example

```json
{
  "queues": {
    "order": {
      "driver": "redis",
      "channel": "queue:order",
      "enabled": true,
      "pop_timeout": 1,
      "handle_timeout": 30,
      "retry_seconds": [5, 10, 30],
      "message_ttl": 86400,
      "max_attempts": 3,
      "processes": 2,
      "concurrent": 20,
      "max_messages": 0,
      "auto_restart": false,
      "shutdown_timeout": 30
    }
  }
}
```

Primary usage (recommended): build `Config` in code and use `NewServer` directly.

Complete runnable example:

```go
package main

import (
	"context"
	"log"

	"github.com/redis/go-redis/v9"

	asyncqueue "github.com/liuxiaozhicn/async-queue-go/asyncqueue"
	"github.com/liuxiaozhicn/async-queue-go/pkg/core"
	"github.com/liuxiaozhicn/async-queue-go/pkg/queue"
)

type OrderJob struct {
	OrderNo string `json:"order_no"`
}

func (j *OrderJob) GetType() string { return "order.create" }

type OrderJobHandler struct{}

func (h *OrderJobHandler) Handle(ctx context.Context, m *core.Message) (core.Result, error) {
	return core.ACK, nil
}

func main() {
	ctx := context.Background()
	redisClient := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	driver := queue.NewRedisDriver(redisClient)

	cfg := &asyncqueue.Config{
		Queues: map[string]asyncqueue.QueueConfig{
			"order": {
				Driver:        "redis",
				Channel:       "queue:order",
				Enabled:       true,
				PopTimeout:    3,
				HandleTimeout: 180,
				RetrySeconds:  []int{30, 90, 180, 300},
				MessageTTL:    864000,
				MaxAttempts:   5,
				Processes:     2,
				Concurrent:    50,
			},
		},
	}

	server, err := asyncqueue.NewServer(cfg, asyncqueue.WithDriver("redis", driver))
	if err != nil {
		log.Fatal(err)
	}

	serveMux := asyncqueue.NewServeMux()
	serveMux.Handle((&OrderJob{}).GetType(), &OrderJobHandler{})

	go func() {
		if err := server.Run(ctx, serveMux); err != nil {
			log.Fatal(err)
		}
	}()

	q, err := server.Queue("order")
	if err != nil {
		log.Fatal(err)
	}
	messageID, err := q.PushJob(ctx, &OrderJob{OrderNo: "demo-1001"}, 0)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("published message id=%s", messageID)
}
```

If an enabled queue type is not bound in `ServeMux`, worker startup fails.
You can use this `messageID` later for management operations such as `GetMessage`, `Cancel`, or `RetryByID`.


Use file-loading only when you intentionally manage queue settings from external files.

## Configuration Reference

### Complete Example

```json
{
  "queues": {
    "order": {
      "driver": "redis",
      "channel": "queue:order",
      "enabled": true,
      "pop_timeout": 3,
      "handle_timeout": 180,
      "retry_seconds": [10, 30, 60, 120, 300],
      "message_ttl": 864000,
      "max_attempts": 5,
      "processes": 2,
      "concurrent": 50,
      "max_messages": 0,
      "auto_restart": false,
      "shutdown_timeout": 240
    }
  }
}
```

### Parameter Details

| Field | Default | Meaning | Recommended range / notes |
| --- | --- | --- | --- |
| `driver` | `redis` (auto-filled on file load) | Driver key resolved from `WithDriver(name, driver)` | Keep `redis` unless you register a custom driver |
| `channel` | none (required) | Physical queue namespace in backend | Use stable, business-scoped names, e.g. `queue:order` |
| `enabled` | `false` | Whether this queue starts workers/forwarder | `true` for active queues |
| `pop_timeout` | `1` (config load fallback) | Empty poll wait in seconds | `1~5`, higher reduces empty-poll pressure |
| `handle_timeout` | `10` (config load fallback) | Single message processing timeout in seconds | Usually `60~300`, set from handler p99 latency |
| `retry_seconds` | `[5]` (config load fallback) | Retry backoff schedule in seconds | Increasing sequence, e.g. `[10,30,60,120,300]` |
| `message_ttl` | `864000` | TTL for `message:<id>` entity in seconds | `1~30` days based on audit/debug needs; `0` means no expiration |
| `max_attempts` | `3` | Maximum delivery attempts | Usually `3~8`; tune with business idempotency and SLA |
| `processes` | `1` | Number of consumer processes in this runtime | Start with CPU core count or lower, then scale by throughput |
| `concurrent` | `10` | Goroutine concurrency per process | Tune by downstream capacity (DB/RPC), avoid overload |
| `max_messages` | `0` | Messages processed before worker exits (`0` unlimited) | Keep `0` for long-running workers |
| `auto_restart` | `false` | Restart worker after `max_messages` reached | Enable only when intentionally using bounded workers |
| `shutdown_timeout` | `30` | Graceful shutdown wait in seconds | Usually `60~300` for production |

### Quick Tuning Guide

| Symptom | Tune first |
| --- | --- |
| Messages frequently move to `timeout` | Increase `handle_timeout` |
| Retry storms under failures | Increase `retry_seconds` backoff and/or reduce `max_attempts` |
| High Redis CPU on idle queues | Increase `pop_timeout` |
| Downstream DB/RPC saturation | Reduce `concurrent` or `processes` |
| Slow shutdown on deploy/restart | Increase `shutdown_timeout` |

## Architecture

### Layered View

```mermaid
flowchart TD
    App[Application]
    Server[asyncqueue.Server]
    Manager[asyncqueue.Manager]
    Queue[asyncqueue.Queue]
    Consumer[pkg/queue.Consumer]
    Forwarder[pkg/queue.forwarder]
    Driver[pkg/queue.Driver]
    RedisDriver[pkg/queue.RedisDriver]
    Redis[(Redis)]

    App --> Server
    App --> Queue
    Server --> Manager
    Manager --> Queue
    Manager --> Consumer
    Manager --> Forwarder
    Queue --> Driver
    Consumer --> Driver
    Forwarder --> Driver
    Driver --> RedisDriver
    RedisDriver --> Redis
```

### Startup Flow

```mermaid
flowchart LR
    A[Load Config] --> B[Register driver with WithDriver]
    B --> C[NewServer]
    C --> D[Create Manager]
    D --> E[Run / StartWorker]
    E --> F[Validate handlers and drivers]
    F --> G[Create Queue per enabled config]
    G --> H[Start Forwarder]
    G --> I[Start Consumer x Processes]
```

### Runtime Responsibilities

| Component | Responsibility |
| --- | --- |
| `Server` | High-level entry point for config, driver registration, handlers, and lifecycle |
| `Manager` | Creates queues, consumers, and forwarders from config and manages startup/shutdown |
| `Queue` | Producer-facing API for publish, query, delete, retry, reload, and stats |
| `Consumer` | Consumption loop that calls handlers and commits ACK / RETRY / REQUEUE / DROP |
| `Forwarder` | Background mover for delayed jobs and expired reservations |
| `Driver` | Backend abstraction for queue operations and state transitions |
| `RedisDriver` | Built-in backend implementation |

## Message Lifecycle

Delivery result branch:

```mermaid
flowchart LR
    P[Producer] -->|delay = 0| W[waiting]
    P -->|delay > 0| D[delayed]
    D -->|Forwarder moves due message| W
    W -->|Consumer Pop| R[reserved]
```

Consumer result paths:

```mermaid
flowchart LR
    R[reserved]
    R -->|Handler returns ACK| ACK[done]
    R -->|Handler returns DROP| DROP[dropped]
    R -->|Handler returns REQUEUE| W[waiting]
    R -->|RETRY or error and attempts remain| D[delayed]
    R -->|RETRY or error and attempts exhausted| F[failed]
    R -->|handleTimeout reached| T[timeout]
```

Failure or timeout recovery branch:

```mermaid
flowchart LR
    T[timeout] -->|Manual reload timeout queue| W[waiting]
    F -->|Manual reload failed queue| W
```

Notes:

- `waiting` is the main consumable queue
- `reserved` means a consumer has claimed the message but not committed the result yet
- `delayed` is used for both explicit delay and retry backoff
- `timeout` and `failed` do not go back to `waiting` automatically
- manual reload is an explicit operation, so it is shown separately from the main processing path

### State Transition Matrix

| Stage | Trigger | Queue transition | Persisted status |
| --- | --- | --- | --- |
| Producer | `Push(delay=0)` | `-> waiting` | `waiting` |
| Producer | `Push(delay>0)` | `-> delayed` | `delayed` |
| Forwarder | delayed due | `delayed -> waiting` | `waiting` |
| Consumer acquire | `Pop` | `waiting -> reserved` | `reserved` |
| Consumer commit | `ACK` | `reserved -> (removed)` | `done` |
| Consumer commit | `DROP` | `reserved -> (removed)` | `dropped` |
| Consumer commit | `REQUEUE` | `reserved -> waiting` | `waiting` |
| Consumer commit | `RETRY` with `delay>0` | `reserved -> delayed` | `delayed` |
| Consumer commit | `RETRY` with `delay<=0` | `reserved -> waiting` | `waiting` |
| Consumer error | error and attempts remain, `delay>0` | `reserved -> delayed` | `delayed` |
| Consumer error | error and attempts remain, `delay<=0` | `reserved -> waiting` | `waiting` |
| Consumer terminal | error/RETRY and attempts exhausted | `reserved -> failed` | `failed` |
| Forwarder | reservation expired | `reserved -> timeout` | `timeout` |
| Manual operation | `Reload("timeout")` | `timeout -> waiting` | `waiting` |
| Manual operation | `Reload("failed")` | `failed -> waiting` | `waiting` |
| Manual operation | `Cancel` (only in `delayed`) | `delayed -> (removed)` | `canceled` |

### Concurrency & Consistency Rules

| Rule | Why |
| --- | --- |
| Queue position is the source of truth for dispatch state | `waiting/reserved/delayed` directly determine whether a message can be consumed, retried, or canceled |
| Message `status` is a persisted view, not the only decision source | In high-throughput scenarios, decision logic should rely on queue operations in Lua first, then update status |
| One commit action per consumed message | After a message is popped into `reserved`, only one of `ACK/RETRY/REQUEUE/DROP/FAIL` should succeed semantically |
| Retry path is `reserved -> waiting/delayed` | `delay<=0` means immediate next scheduling round (`waiting`), `delay>0` means backoff (`delayed`) |
| Cancel is allowed only in `delayed` | Once a message reaches `waiting` or `reserved`, cancellation is rejected by design |

### Troubleshooting Checklist

| Symptom | First checks |
| --- | --- |
| Message stuck in `reserved` | Check handler timeout and whether commit actions (`ACK/RETRY/...`) are returning errors |
| Message unexpectedly in `timeout` | Check `handleTimeout` against real handler p99 latency |
| Retry did not delay | Verify retry delay value; `delay<=0` intentionally goes to `waiting` |
| Cancel returns false | Confirm message is still in `delayed` (not `waiting`/`reserved`) |
| Status looks stale | Verify queue membership first, then inspect `message:<id>` payload |

## Handler Result Semantics

| Result | Meaning |
| --- | --- |
| `core.ACK` | Success; remove the message from the reserved queue |
| `core.RETRY` | Send the message to the delayed queue with retry policy |
| `core.REQUEUE` | Move the message back to the waiting queue immediately |
| `core.DROP` | Mark the message as `dropped` and stop retrying |

If the handler returns `error`, the framework follows the error path instead of the explicit `Result`.

`DROP` is a business-level discard decision, not a physical delete. The message leaves the active processing queues, but its entity can still remain until TTL expires.

## Redis Storage Model

The Redis driver generates a key set per `channel`:

```text
{queue:order}:waiting
{queue:order}:reserved
{queue:order}:delayed
{queue:order}:timeout
{queue:order}:failed
{queue:order}:message:<id>
{queue:order}:msg_seq
{queue:order}:msg_seq_epoch
```

Meaning:

- `waiting`: ready-to-consume queue
- `reserved`: claimed but not committed
- `delayed`: delayed and retry messages
- `timeout`: expired reservations
- `failed`: messages that exhausted retries
- `message:<id>`: message entity payload

The `{...}` hash tag keeps keys for one business queue in the same Redis Cluster slot.

## Queue Management APIs

| Method                                | Purpose |
|---------------------------------------| --- |
| `PushJob(ctx, job, delaySeconds)`     | Publish a structured job |
| `PushMessage(ctx, msg, delaySeconds)` | Publish a raw message |
| `Info(ctx)`                           | Read waiting / reserved / delayed / timeout / failed counts |
| `GetMessage(ctx, id)`                 | Fetch message details |
| `Cancel(ctx, id)`                     | Cancel a delayed message before it enters dispatch, and mark it as `canceled` |
| `RetryByID(ctx, id, delaySeconds)`    | Retry a message with a new delay |
| `Reload(ctx, "timeout" or "failed")`  | Move timeout or failed messages back to waiting |
| `Flush(ctx, queueName)`               | Clear one internal queue |

## Low-Level Consumer

If you do not want `Server` / `Manager`, you can compose the runtime yourself:

- `queue.NewRedisDriver(...)`
- `queue.NewConsumer(...)`
- `worker.NewWorker(...)`

Default `NewConsumer(...)` options (when not overridden):

| Option | Default |
| --- | --- |
| `concurrentLimit` | `10` |
| `popTimeout` | `3s` |
| `handleTimeout` | `180s` |
| `retrySeconds` | `[10,30,60,120,300]` |
| `messageTTL` | `864000` (10 days) |

Reference:

- [`../examples/worker/main.go`](../examples/worker/main.go)

## Custom Driver Extension

Implement:

```go
type Driver interface {
    Ping(ctx context.Context) error
    Push(ctx context.Context, channel string, m *core.Message, delaySeconds int, messageTTL int) error
    Get(ctx context.Context, channel string, id string) (*core.Message, error)
    Cancel(ctx context.Context, channel string, id string) (bool, error)
    Pop(ctx context.Context, channel string, popTimeout time.Duration, handleTimeout time.Duration) (string, *core.Message, error)
    Ack(ctx context.Context, channel string, messageID string) error
    Fail(ctx context.Context, channel string, messageID string) error
    Drop(ctx context.Context, channel string, messageID string) error
    Requeue(ctx context.Context, channel string, messageID string) error
    Retry(ctx context.Context, channel string, id string, delaySeconds int) (bool, error)
    Reload(ctx context.Context, channel string, queue string) (int, error)
    Flush(ctx context.Context, channel string, queue string) error
    Info(ctx context.Context, channel string) (Info, error)
    ForwardMessages(ctx context.Context, channel string) (int64, int64, error)
}
```

Registration:

```go
server, err := asyncqueue.NewServer(
    cfg,
    asyncqueue.WithDriver("custom", customDriver),
)
```

## FAQ

### Why is `driver` in config not the queue name?

Because `driver` identifies the backend implementation, not the business queue.

### Why can one `RedisDriver` serve multiple queues?

Because the current `Driver` interface receives `channel` on every operation, so the driver does not bind to a single queue at construction time.

### Why not expose `Server.Push` directly?

Publishing already belongs to `Queue`, while `Server` is the runtime entry point and queue lookup layer.

Recommended usage:

```go
queueInstance, err := server.Queue("order")
id, err := queueInstance.PushJob(ctx, job, 0)
```

### What does `DROP` mean?

- `DROP` is a consumer result. It means the business logic has decided not to continue processing the message, and the message status becomes `dropped`.
### How does `Cancel` behave?
- `Cancel` is for giving up dispatch while the message is still in `delayed`.
- If the message is already in `waiting`, it has become dispatch-ready and cancellation is rejected with `ErrMessageAlreadyReadyForDispatch`.
- If the message is already in `reserved`, it has been claimed by a consumer and cancellation is rejected with `ErrMessageAlreadyInExecution`.
