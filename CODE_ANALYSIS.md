# async-queue-go 代码分析报告

## 一、项目结构

```
async-queue-go/
├── pkg/                          # 核心底层包
│   ├── core/                     # 基础类型
│   │   ├── types.go              # Result 枚举 (ACK/RETRY/REQUEUE/DROP)
│   │   ├── message.go            # 消息编解码、重试计数
│   │   └── retry_policy.go       # 重试延迟策略
│   ├── clock/
│   │   └── clock.go              # 时钟抽象（便于测试）
│   ├── queue/
│   │   ├── driver.go             # Driver 接口定义
│   │   ├── keys.go               # Redis key 管理
│   │   ├── redis_driver.go       # Redis Driver 实现
│   │   └── consumer.go           # 消费者（并发控制、统计、Hooks）
│   └── worker/
│       └── worker.go             # Worker 生命周期管理
│
├── asyncqueue/                   # 高层 API
│   ├── job.go                    # Job 接口
│   ├── asyncqueue.go             # Queue 封装（Push/Delete/Info）
│   ├── config.go                 # JSON 配置加载
│   ├── servemux.go               # Handler 路由
│   ├── server.go                 # Server 入口
│   ├── defaultserver.go          # 全局单例
│   └── manager.go                # 多队列编排 + Auto-Restart
│
└── examples/                     # 示例
    ├── demo/                     # 完整 demo
    └── worker/                   # 独立 worker 示例
```

**架构分层**：`core` → `queue/worker` → `asyncqueue` → `examples`，整体分层清晰。

---

## 二、发现的 Bug

### Bug 1：RETRY 耗尽次数后消息静默丢失（严重）

**文件**: `pkg/queue/consumer.go:206-218`

当 Handler 返回 `core.RETRY` 但 `AttemptsAllowed()` 返回 `false`（重试次数已用完）时，消息被 Ack 后既没有进入 failed 队列，也没有触发 `OnDrop` / `OnFail` hook，消息**静默消失**。

```go
case core.RETRY:
    if err := c.ackAndHook(atomicCtx, data, message); err != nil {
        return err
    }
    if message.AttemptsAllowed() {
        // 有重试次数，正常 retry
        ...
    }
    // ← 没有 else 分支！次数耗尽时消息直接丢了
    return nil
```

**修复建议**：添加 `else` 分支，将消息移入 failed 队列或触发 `OnDrop` hook。

---

### Bug 2：多队列共享 Redis Client 被重复关闭（严重）

**文件**: `asyncqueue/manager.go:246-251` + `asyncqueue/asyncqueue.go:43-48`

`Manager` 使用同一个 `redis.UniversalClient` 创建多个 `Queue`。在 `closeQueuesLocked()` 中，每个 `Queue.Close()` 都会调用 `client.Close()`。第一个 close 之后，其他队列和正在运行的 worker 会因连接关闭而报错。

```go
func (m *Manager) closeQueuesLocked() {
    for name, q := range m.queues {
        _ = q.Close()  // 每个 queue 都 close 同一个 redis client!
        delete(m.queues, name)
    }
}
```

**修复建议**：`Queue` 不应负责关闭共享的 Redis client。可以添加 `ownsClient` 标志，或让 `Manager` 在所有 queue 关闭后统一关闭 client。

---

### Bug 3：Worker.Stop 遗留调试输出

**文件**: `pkg/worker/worker.go:67`

```go
func (w *Worker) Stop(graceTimeout time.Duration) error {
    fmt.Printf("stop 执行")  // ← 调试代码未删除
```

---

### Bug 4：`move()` 非原子操作存在竞态条件

**文件**: `pkg/queue/redis_driver.go:181-198`

`ZRevRangeByScore` → `ZRem` → `LPush` 不是原子操作。多个消费者同时执行时，两个进程可能同时读到相同的过期消息。虽然 `removed > 0` 做了幂等保护，但在高并发下仍可能产生竞态窗口。

**修复建议**：使用 Lua 脚本保证原子性。

---

### Bug 5：`Pop()` 中 decode 失败时消息留在 Reserved 队列

**文件**: `pkg/queue/redis_driver.go:89-92`

```go
msg, err := core.DecodeMessage(data)
if err != nil {
    return "", nil, nil  // ← 消息已加入 Reserved，但返回 nil 给调用者
}
```

消息在 `ZAdd` 到 Reserved 后如果 decode 失败，调用者得到 `nil` 消息不会处理它，导致该消息**永远留在 Reserved 队列**直到超时。

**修复建议**：decode 失败时应从 Reserved 中移除该消息，或返回 error。

---

### Bug 6：`handleError` 在 panic 恢复时用 `context.Background()`，但正常错误路径用可能已取消的 ctx

**文件**: `pkg/queue/consumer.go:177 vs 184`

panic 场景正确使用了 `context.Background()`，但正常 handler error 路径传入的 `ctx` 可能已被取消，导致 retry/fail 操作失败。应与 `handleOne` 中的 ACK 路径一样使用 `context.WithoutCancel`。

---

### Bug 7：使用已废弃的 `RPopLPush`

**文件**: `pkg/queue/redis_driver.go:133`

`RPopLPush` 在 Redis 6.2+ 已废弃，应使用 `LMove`。

---

## 三、优化建议

### 优化 1：`Info()` 5 次串行 Redis 调用 → Pipeline

**文件**: `pkg/queue/redis_driver.go:157-179`

当前 `Info()` 对 5 个 key 做 5 次独立 Redis 调用。使用 Pipeline 可以合并为 1 次网络往返，显著降低延迟。

```go
// 建议
pipe := d.client.Pipeline()
waitingCmd := pipe.LLen(ctx, d.keys.Waiting)
reservedCmd := pipe.ZCard(ctx, d.keys.Reserved)
// ...
pipe.Exec(ctx)
```

---

### 优化 2：`Pop()` 每次调用都执行 `move()`

**文件**: `pkg/queue/redis_driver.go:67-70`

每次 `Pop` 都执行两次 `move()`（delayed→waiting, reserved→timeout），在高 QPS 下增加大量 Redis 开销。可以改为：
- 按时间间隔节流（例如每秒一次）
- 或用独立 goroutine 定时执行 move

---

### 优化 3：`Reload()` 逐条 `RPopLPush`

**文件**: `pkg/queue/redis_driver.go:131-142`

逐条移动消息效率低。可以使用 Pipeline 批量操作，或使用 Lua 脚本一次性移动。

---

### 优化 4：`move()` 逐条 ZRem+LPush

**文件**: `pkg/queue/redis_driver.go:187-196`

对每个过期消息做独立 `ZRem` + `LPush`。应使用 Pipeline 或 Lua 脚本批量处理，减少网络往返。

---

### 优化 5：缺少结构化日志

全项目使用 `log.Printf`，建议接入 `slog`（Go 1.21 标准库）或 `zerolog`/`zap`，支持 JSON 格式输出和日志级别控制。

---

### 优化 6：缺少 Metrics/可观测性

`ConsumerStats` 仅在内存中维护，无法被外部监控系统采集。建议：
- 暴露 Prometheus metrics
- 或提供 stats 回调 hook

---

### 优化 7：`Handle` 和 `Bind` 方法完全重复

**文件**: `asyncqueue/server.go:81-95`

`Handle()` 和 `Bind()` 实现完全相同，应删除一个或让 `Bind` 提供差异化语义。

---

### 优化 8：Config 不支持环境变量覆盖

当前只支持 JSON 文件配置。生产环境中通常需要用环境变量覆盖部分配置（如 Redis 地址），建议增加环境变量支持。

---

## 四、总结

| 类别 | 数量 | 严重程度 |
|------|------|---------|
| Bug  | 7    | 2 严重 + 5 中等 |
| 优化 | 8    | 性能 + 可维护性 |

**最需要优先修复的问题**：
1. RETRY 耗尽次数后消息丢失 — 数据丢失风险
2. 共享 Redis Client 重复关闭 — 运行时崩溃风险
3. Pop decode 失败消息滞留 Reserved — 队列泄漏
