# Demo Example

This demo runs both producer and worker in one process.

## What It Demonstrates

- Registering a queue handler with `asyncqueue.Server`
- Pushing delayed jobs with `PushJob`
- Reading message state by `message_id` via `GetMessage`
- Retrying and canceling messages via `RetryByID` and `Cancel`
- Graceful shutdown with `SIGINT/SIGTERM`

## Configuration

The example configuration is in [config.json](/Users/liuxiaozhi/Desktop/async-queue-go/examples/demo/config.json).

## Run

```bash
go run ./examples/demo
```

Make sure Redis is available at `127.0.0.1:6379`.

### Validate Consumer Return Paths

You can force handler return behavior via `DEMO_RESULT_MODE`:

- `ack`
- `retry`
- `requeue`
- `drop`
- `error`
- `mixed` (default, cycles through all of the above)

Examples:

```bash
DEMO_RESULT_MODE=retry go run ./examples/demo
DEMO_RESULT_MODE=requeue go run ./examples/demo
DEMO_RESULT_MODE=drop go run ./examples/demo
DEMO_RESULT_MODE=error go run ./examples/demo
```

## Test

```bash
go test ./examples/demo
```
