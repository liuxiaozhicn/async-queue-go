# Logging Framework Design

**Date**: 2026-04-03
**Status**: Approved
**Scope**: Add a GORM-style configurable logging framework to async-queue-go

---

## Background

The current codebase uses `log.Printf` calls scattered across `Consumer`, `Manager`, and `Server`. There is no way for library users to control log verbosity, redirect output, or integrate with their own logging infrastructure (zap, logrus, slog, etc.).

The goal is to introduce a logger interface and default implementation modeled after GORM's logger pattern, with minimal disruption to existing business logic.

---

## Design Constraints

- **No business logic changes**: all routing, retry, hook, and concurrency logic in `Consumer`, `Manager`, and `Server` remains identical.
- **Purely additive**: existing `log.Printf` calls are replaced one-for-one with logger method calls; no new logic branches are introduced.
- **GORM-style API**: users configure logging at `NewServer` time via a functional option; the interface is familiar to Go developers who use GORM.

---

## Package Structure

```
pkg/logger/
  logger.go       ← Interface, LogLevel constants
  default.go      ← Default implementation (writes to standard log)

asyncqueue/
  logger.go       ← Re-exports: type Logger = logger.Interface, LogLevel consts, WithLogger Option
```

`pkg/logger` has **no dependencies on any other internal package**. Both `asyncqueue` and `pkg/queue` import `pkg/logger`.

---

## Interface Definition

```go
// pkg/logger/logger.go
package logger

import (
    "context"
    "time"
)

type LogLevel int

const (
    Silent LogLevel = iota
    Error
    Warn
    Info
)

type Interface interface {
    LogMode(LogLevel) Interface
    Info(context.Context, string, ...interface{})
    Warn(context.Context, string, ...interface{})
    Error(context.Context, string, ...interface{})
    Trace(ctx context.Context, begin time.Time, fc func() (queue, disposition string), err error)
}
```

`Trace` mirrors GORM's trace method: it wraps a single message processing round-trip (from goroutine dispatch to `handleOne` completion), capturing queue name, elapsed time, and any error. The `fc` closure is lazy — if the log level is `Silent`, the closure is never called, avoiding string allocation overhead.

`disposition` is an empty string in the initial implementation. It is reserved for future use when per-message outcome tracking is added (e.g., `"ack"`, `"retry"`, `"fail"`).

---

## Default Implementation

```go
// pkg/logger/default.go
package logger

import (
    "context"
    "log"
    "time"
)

var Default Interface = &defaultLogger{level: Info}

type defaultLogger struct {
    level LogLevel
}

func (l *defaultLogger) LogMode(level LogLevel) Interface {
    return &defaultLogger{level: level}
}

func (l *defaultLogger) Info(ctx context.Context, msg string, args ...interface{}) {
    if l.level >= Info {
        log.Printf("[async-queue] INFO  "+msg, args...)
    }
}

func (l *defaultLogger) Warn(ctx context.Context, msg string, args ...interface{}) {
    if l.level >= Warn {
        log.Printf("[async-queue] WARN  "+msg, args...)
    }
}

func (l *defaultLogger) Error(ctx context.Context, msg string, args ...interface{}) {
    if l.level >= Error {
        log.Printf("[async-queue] ERROR "+msg, args...)
    }
}

func (l *defaultLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, string), err error) {
    if l.level < Info {
        return
    }
    queue, disposition := fc()
    elapsed := time.Since(begin)
    if err != nil {
        log.Printf("[async-queue] TRACE queue=%s disposition=%s elapsed=%v err=%v", queue, disposition, elapsed, err)
    } else {
        log.Printf("[async-queue] TRACE queue=%s disposition=%s elapsed=%v", queue, disposition, elapsed)
    }
}
```

---

## Public Re-export (asyncqueue package)

```go
// asyncqueue/logger.go
package asyncqueue

import "github.com/liuxiaozhicn/async-queue-go/pkg/logger"

type Logger = logger.Interface

type LogLevel = logger.LogLevel

const (
    Silent = logger.Silent
    Error  = logger.Error
    Warn   = logger.Warn
    Info   = logger.Info
)

func WithLogger(l logger.Interface) Option {
    return func(s *Server) {
        s.logger = l
    }
}
```

Users only ever import the `asyncqueue` package — they never need to import `pkg/logger` directly.

---

## Injection & Propagation

Logger flows from `Server` → `Manager` → `Consumer` via constructor parameters:

```
NewServer(cfg, redisClient, WithLogger(myLogger))
    └─ Server.logger = myLogger (defaults to logger.Default if not set)
         └─ NewManagerWithRedis(cfg, serveMux, redisClient, logger)
               └─ Manager.logger = logger
                    └─ iqueue.NewConsumer(driver, handler, ..., logger)
                          └─ Consumer.logger = logger
```

If `WithLogger` is not called, `Server` uses `logger.Default` (Info level, standard log output) — identical behavior to the current codebase.

### Struct changes

| Struct | New field |
|---|---|
| `Server` | `logger logger.Interface` |
| `Manager` | `logger logger.Interface` |
| `Consumer` | `logger logger.Interface` |

### Constructor changes

| Constructor | Change |
|---|---|
| `NewManagerWithRedis` | add `logger logger.Interface` parameter |
| `NewManager` | add `logger logger.Interface` parameter |
| `NewConsumer` | add `logger logger.Interface` parameter |
| `NewConsumerWithHooks` | add `logger logger.Interface` parameter |

---

## Log Call Replacements

All replacements are one-for-one substitutions. No logic is added or removed.

| File | Current | Replacement |
|---|---|---|
| `pkg/queue/consumer.go` | `log.Printf("[Consumer] context cancelled...")` | `c.logger.Info(ctx, "context cancelled, waiting for in-flight jobs...")` |
| `pkg/queue/consumer.go` | `log.Printf("[Consumer] all in-flight jobs done...")` | `c.logger.Info(ctx, "all in-flight jobs done, waited %v", ...)` |
| `pkg/queue/consumer.go` | `log.Printf("[Consumer] handler panic: ...")` | `c.logger.Error(ctx, "handler panic: payload=%s attempts=%d/%d panic=%v", ...)` |
| `pkg/queue/consumer.go` | `log.Printf("[Consumer] handler error: ...")` | `c.logger.Error(ctx, "handler error: payload=%s attempts=%d/%d error=%v", ...)` |
| `asyncqueue/manager.go` | `log.Printf("[Manager] queue %s process %d started")` | `m.logger.Info(ctx, "queue %s process %d started", ...)` |
| `asyncqueue/manager.go` | `log.Printf("[Manager] queue %s process %d restarted...")` | `m.logger.Info(ctx, "queue %s process %d restarted (count=%d)", ...)` |
| `asyncqueue/manager.go` | `log.Printf("[Manager] queue %s process %d reached max messages...")` | `m.logger.Info(ctx, "queue %s process %d reached max messages (%d), restarting...", ...)` |
| `asyncqueue/server.go` | `log.Printf("[Async Queue Server] starting...")` | `s.logger.Info(ctx, "starting \| queues=%d", ...)` |
| `asyncqueue/server.go` | `log.Printf("[Async Queue Server] exited with error...")` | `s.logger.Error(ctx, "exited with error \| %v", ...)` |
| `asyncqueue/server.go` | `log.Printf("[Async Queue Server] stopped gracefully")` | `s.logger.Info(ctx, "stopped gracefully")` |
| `asyncqueue/defaultserver.go` | `log.Println("warn: overwriting existing default server")` | `s.logger.Warn(ctx, "overwriting existing default server")` |

---

## Trace Call Site

`Trace` is called in the goroutine inside `Consumer.Run()`, wrapping the `handleOne` call. `handleOne` itself is not modified.

```go
// Inside the goroutine in Consumer.Run()
go func(data string, msg *core.Message) {
    defer wg.Done()
    defer func() { <-sem }()

    begin := time.Now()
    err := c.handleOne(ctx, data, msg)

    c.logger.Trace(ctx, begin, func() (string, string) {
        return c.name, ""
    }, err)

    if err != nil {
        c.recordErr(err)
    }
}(data, message)
```

---

## User-Facing API

```go
// Use default logger at Warn level (suppress Info)
server, _ := asyncqueue.NewServer(cfg, redisClient,
    asyncqueue.WithLogger(logger.Default.LogMode(asyncqueue.Warn)),
)

// Silence all logs
server, _ := asyncqueue.NewServer(cfg, redisClient,
    asyncqueue.WithLogger(logger.Default.LogMode(asyncqueue.Silent)),
)

// Plug in a custom logger (implement asyncqueue.Logger interface)
server, _ := asyncqueue.NewServer(cfg, redisClient,
    asyncqueue.WithLogger(myZapAdapter),
)

// No option → uses logger.Default at Info level (current behavior preserved)
server, _ := asyncqueue.NewServer(cfg, redisClient)
```

---

## Out of Scope

- Structured / key-value logging (reserved for future)
- Per-queue log level configuration
- `disposition` tracking inside `handleOne` (reserved for future, requires separate design)
- Color output in default logger
