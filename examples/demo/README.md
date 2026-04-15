# Demo Example

This demo runs both producer and worker in one process.

## What It Demonstrates

- Registering a queue handler with `asyncqueue.Server`
- Pushing delayed jobs with `PushJob`
- Reading message state by `message_id` via `GetMessage`
- Retrying and deleting messages via `RetryByID` and `DeleteByID`
- Graceful shutdown with `SIGINT/SIGTERM`

## Configuration

The example configuration is in [config.json](/Users/liuxiaozhi/Desktop/async-queue-go/examples/demo/config.json).

## Run

```bash
go run ./examples/demo
```

Make sure Redis is available at `127.0.0.1:6379`.

## Test

```bash
go test ./examples/demo
```
