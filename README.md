# async-queue-go

English | [简体中文](README.zh-CN.md)

`async-queue-go` is an asynchronous job queue library for Go services with a built-in Redis driver and an extensible `Driver` abstraction.

It provides:

- High-level runtime APIs: `asyncqueue.Server`, `Manager`, `Queue`
- Low-level building blocks: `pkg/queue.Driver`, `Consumer`, `Forwarder`
- At-least-once delivery with retry, delay, timeout recovery, and failed-message reload

## Features

- Pluggable drivers registered by `driver`
- Built-in Redis driver
- Concurrent consumers with auto-restart support
- Atomic queue-state transitions via Redis Lua scripts, with timeout recovery (`reserved -> timeout`) and at-least-once delivery semantics
- Queue management APIs for info, delete, retry, reload, and flush
- Graceful shutdown support

## Reliability Highlights

- **Atomic state transitions**:
  message commit operations (`Pop/Ack/Retry/Requeue/Drop/Fail/Cancel`) are executed by Redis Lua scripts, so multi-key changes are applied atomically.
- **Timeout fault tolerance**:
  if a consumer crashes after claiming a message, forwarder moves expired reservations from `reserved` to `timeout`.
- **Manual recovery**:
  operators can call `Reload("timeout")` / `Reload("failed")` to re-deliver stalled or failed messages.
- **Message loss semantics**:
  the system is at-least-once, not exactly-once.
  Under normal Redis durability and sane TTL settings, messages are not silently dropped from the active flow;
  data loss can still happen via external destructive conditions (manual flush/delete, key expiration policy, Redis data loss).

> [!IMPORTANT]
> Delivery guarantee is **at-least-once**. Handlers must be **idempotent**.

> [!WARNING]
> "No silent loss" applies to the normal runtime path only. External destructive actions
> (`Flush/Delete`), TTL expiration, or Redis data loss can still cause message loss.

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
| `queue` | `order` | Business queue key used for config lookup, handler binding, and `server.Queue("order")` |
| `driver` | `redis` | Backend registration key used by `WithDriver("redis", driver)` and the `driver` config field |
| `channel` | `queue:order` | Logical queue identifier (key prefix) in the driver backend; producer and consumer must use the same channel |

## Quick Start

1. Start Redis (choose one):
   - Docker (recommended):
   ```bash
   docker run --name asyncq-redis \
     -p 6379:6379 \
     -e TZ=Asia/Shanghai \
     -d redis:7 \
     redis-server --appendonly yes
   ```
   - Reuse existing container:
   ```bash
   docker start asyncq-redis
   ```
   - Local/remote Redis: ensure it is reachable with the expected address.
2. Connectivity check (recommended):
   ```bash
   redis-cli -h 127.0.0.1 -p 6379 ping
   ```
   Continue after `PONG`.
3. Create `Config` with one queue key (for example `order`).
4. Register driver: `WithDriver("redis", queue.NewRedisDriver(client))`.
5. Bind handler with the same queue key: `serveMux.Handle("order", handler)`.
6. Run server and publish via `server.Queue("order").PushTask(...)`.

New users can start from:

- [`examples/demo/basic/main.go`](/Users/liuxiaozhi/Desktop/async-queue-go/examples/demo/basic/main.go)
- [`examples/demo/order/main.go`](/Users/liuxiaozhi/Desktop/async-queue-go/examples/demo/order/main.go)

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
4. Publish through `Queue.PushTask(...)` or `Queue.PushMessage(...)`

Important: `server.Run(ctx, serveMux)` requires handler binding for every enabled queue type.  
For example: `serveMux.Handle("order", orderJobHandler)`.
Binding rule: `ServeMux.Handle(<queue>, <handler>)` must use the same `<queue>` key as `Config.Queues`.
If an enabled queue is missing this binding, worker startup fails.

> [!IMPORTANT]
> **Handler binding is mandatory**:
> `ServeMux.Handle(<queue>, <handler>)` must match the `Config.Queues` key exactly.

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
orderTaskHandler := NewOrderTaskHandler()
serveMux.Handle("order", orderTaskHandler)

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

    _, err = queueInstance.PushTask(ctx, &OrderTask{
        OrderNo: "demo-order-no",
    }, 30)
    if err != nil {
        log.Printf("push failed: %v", err)
    }
}()
```

If a process is producer-only and does not run workers, use `NewAsyncQueue(...)` directly.

## Task and Message

- `Task` is your business payload struct (for example `OrderTask`).
- `Message` is the runtime envelope persisted in backend storage (`id`, `payload`, `attempts`, `max_attempts`, `status`, timestamps).
- `Queue.PushTask(ctx, task, delay)` serializes `task` into `Message.Payload` and enqueues it.
- `Queue.PushMessage(ctx, msg, delay)` lets you publish a pre-built envelope directly.
- Consumer handlers always receive `*core.Message`; business payload is recovered by unmarshalling `m.Payload`.
- `messageID` is generated by the driver at publish time and is used by management operations (`GetMessage`, `Cancel`, `Reload`, `Flush`).

## Examples

- High-level example: [`examples/demo/basic/main.go`](examples/demo/basic/main.go)
- Business scenario example: [`examples/demo/order/main.go`](examples/demo/order/main.go)
- Low-level worker example: [`examples/worker/main.go`](examples/worker/main.go)

Run the demo:

```bash
go run ./examples/demo/basic
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
