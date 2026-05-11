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
| `channel` | `queue:order` | Backend storage namespace; producer and consumer must use the same channel |

## Recommended Usage

The primary path is:

1. Start a `Server`
2. Register handlers through `ServeMux`
3. After `server.Run(...)` starts, get the queue instance through `server.Queue("order")`
4. Publish through `Queue.PushJob(...)` or `Queue.PushMessage(...)`

Example:

```go
server, err := asyncqueue.NewServer(
    cfg,
    asyncqueue.WithDriver("redis", queue.NewRedisDriver(redisClient)),
)
if err != nil {
    log.Fatal(err)
}

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
