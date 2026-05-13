# Demo Example

This demo runs both producer and worker in one process.

## What It Demonstrates

- Registering a queue handler with `asyncqueue.Server`
- Pushing delayed jobs with `PushJob`
- Reading message state by `message_id` via `GetMessage`
- Retrying and canceling messages via `RetryByID` and `Cancel`
- Random consumer outcomes (`ACK` / `RETRY` / `REQUEUE` / `DROP` / handler error)
- Graceful shutdown with `SIGINT/SIGTERM`

## Configuration

Queue config is built inline in [basic/main.go](/Users/liuxiaozhi/Desktop/async-queue-go/examples/demo/basic/main.go).

## Run

```bash
go run ./examples/demo/basic
```

Make sure Redis is available at `127.0.0.1:6379`.

## Test

```bash
go test ./examples/demo/basic
```
