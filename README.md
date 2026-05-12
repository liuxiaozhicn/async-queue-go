# async-queue-go

English | [简体中文](README.zh-CN.md)

`async-queue-go` is an asynchronous job queue library for Go services with a built-in Redis driver and an extensible `Driver` abstraction.

It provides:

- High-level runtime APIs: `asyncqueue.Server`, `Manager`, `Queue`
- Low-level building blocks: `pkg/queue.Driver`, `Consumer`, `Forwarder`
- At-least-once delivery with retry, delay, timeout recovery, and failed-message reload

## Features

- Pluggable drivers registered by `driver name`
- Built-in Redis driver
- Concurrent consumers with auto-restart support
- Queue management APIs for info, delete, retry, reload, and flush
- JSON / YAML configuration support
- Graceful shutdown support

## Installation

Requirements:

- Go `1.21+`
- Redis `6+` or compatible

```bash
go get github.com/liuxiaozhicn/async-queue-go
```

## Core Concepts

| Concept | Example | Purpose |
| --- | --- | --- |
| `queue name` | `order` | Business queue name used for config lookup, handler binding, and `server.Queue("order")` |
| `driver name` | `redis` | Backend registration name used by `WithDriver("redis", driver)` and the `driver` config field |
| `channel` | `queue:order` | Logical queue identifier (key prefix) in the driver backend; producer and consumer must use the same channel |

## Configuration Quick Reference

Minimal in-code config:

```go
cfg := &asyncqueue.Config{
    Queues: map[string]asyncqueue.QueueConfig{
        "order": {
            Driver:        "redis",
            Channel:       "queue:order",
            Enabled:       true,
            PopTimeout:    3,
            HandleTimeout: 180,
            RetrySeconds:  []int{10, 30, 60, 120, 300},
            MessageTTL:    864000,
            MaxAttempts:   5,
            Processes:     2,
            Concurrent:    50,
            ShutdownTimeout: 240,
        },
    },
}
```

Field reference:

| Field | Type | Default | Notes |
| --- | --- | --- | --- |
| `driver` | `string` | `""` | Driver registration name. Must match `WithDriver("<name>", driver)`; e.g. `redis`. |
| `channel` | `string` | queue name | Backend namespace/prefix. Keep stable once in production. |
| `enabled` | `bool` | `true` | Whether this queue starts worker/forwarder in manager. |
| `pop_timeout` | `int` (seconds) | `5` | Blocking pop timeout. Typical `1~5`; lower gives faster shutdown, higher reduces Redis QPS. |
| `handle_timeout` | `int` (seconds) | `180` | Max processing window before timeout recovery; common `60~300`. |
| `retry_seconds` | `[]int` (seconds) | `[5,10,30,60,120]` | Retry backoff sequence by attempt index; should be increasing. |
| `message_ttl` | `int` (seconds) | `864000` | Message metadata TTL after creation/update; common `1~14` days. |
| `max_attempts` | `int` | `5` | Max total attempts before moving to `failed`. |
| `processes` | `int` | `1` | Consumer process count for this queue (goroutine workers). |
| `concurrent` | `int` | `1` | Max in-flight messages per consumer process. |
| `shutdown_timeout` | `int` (seconds) | `30` | Graceful shutdown wait limit for queue workers/forwarder. |

Key parameters:

- `channel`: backend namespace; producer and consumer must match
- `handle_timeout`: per-message processing timeout; usually `60~300` seconds
- `retry_seconds`: retry backoff schedule; use an increasing sequence
- `max_attempts`: max delivery attempts before moving to `failed`
- `processes` and `concurrent`: throughput knobs; tune against downstream capacity

## Recommended Usage

The primary path is:

1. Start a `Server`
2. Register handlers through `ServeMux`
3. After `server.Run(...)` starts, get the queue instance through `server.Queue("order")`
4. Publish through `Queue.PushJob(...)` or `Queue.PushMessage(...)`

Important: `server.Run(ctx, serveMux)` requires handler binding for every enabled queue type.  
For example: `serveMux.Handle(orderJob.GetType(), orderJobHandler)`.

Example:

```go
server, err := asyncqueue.NewServer(
    cfg,
    asyncqueue.WithDriver("redis", queue.NewRedisDriver(redisClient)),
)
if err != nil {
    log.Fatal(err)
}

serveMux := asyncqueue.NewServeMux()
orderJobHandler := NewOrderJobHandler()
serveMux.Handle((&OrderJob{}).GetType(), orderJobHandler)

go func() {
    if err := server.Run(ctx, serveMux); err != nil {
        log.Fatal(err)
    }
}()

go func() {
    queueInstance, err := server.Queue("order")
    if err != nil {
        log.Fatal(err)
    }

    _, err = queueInstance.PushJob(ctx, &OrderJob{
        OrderNo: "demo-order-no",
    }, 30)
    if err != nil {
        log.Printf("push failed: %v", err)
    }
}()
```

If a process is producer-only and does not run workers, use `NewAsyncQueue(...)` directly.

## Examples

- High-level example: [`examples/demo/main.go`](examples/demo/main.go)
- Demo config: [`examples/demo/config.json`](examples/demo/config.json)
- Low-level worker example: [`examples/worker/main.go`](examples/worker/main.go)

Run the demo:

```bash
go run ./examples/demo
```

## Documentation

- Detailed guide: [`docs/guide.md`](docs/guide.md)
- Chinese guide: [`docs/zh-CN/guide.md`](docs/zh-CN/guide.md)

The detailed guide includes:

- architecture and flow diagrams
- message lifecycle
- Redis storage model
- configuration reference
- queue management APIs
- custom driver extension
- FAQ

## Testing

Run all tests:

```bash
go test ./...
```

Compile-only validation:

```bash
go test ./... -run TestDoesNotExist -count=1
```
