# async-queue-go

[English](README.md) | 简体中文

`async-queue-go` 是一个面向 Go 服务的异步任务队列库，当前内置 Redis 驱动，并保留了可扩展的`Driver`抽象。

它提供：

- 高层运行时 API：`asyncqueue.Server`、`Manager`、`Queue`
- 低层构建块：`pkg/queue.Driver`、`Consumer`、`Forwarder`
- 至少一次投递语义，支持延迟、重试、超时恢复和失败重装载

## 特性

- 按 `driver` 注册可插拔驱动
- 内置 Redis 驱动实现
- 支持并发消费与自动重启
- 基于 Redis Lua 的原子状态流转，支持超时恢复（`reserved -> timeout`）并提供至少一次投递语义
- 提供查询、删除、重试、重装载、清理等管理能力
- 支持优雅停机

## 可靠性关键特性

- **原子状态提交**：
  `Pop/Ack/Retry/Requeue/Drop/Fail/Cancel` 等动作通过 Redis Lua 脚本完成，多 key 变更在单次脚本内原子生效。
- **超时容错**：
  consumer 取走消息后若崩溃或卡死，forwarder 会把超时消息从 `reserved` 转移到 `timeout`。
- **人工重装载**：
  可通过 `Reload("timeout")` / `Reload("failed")` 把积压消息重新放回 `waiting`。
- **消息丢失语义**：
  系统语义是至少一次投递（at-least-once），不是 exactly-once。
  在 Redis 持久化和 TTL 配置合理前提下，不会在正常路径中无声丢消息；
  外部破坏性条件下仍可能丢失（如人工 `Flush/Delete`、TTL 到期删除、Redis 数据丢失）。

> [!IMPORTANT]
> 投递保证是 **at-least-once**，业务 handler 必须按**幂等**设计。

> [!WARNING]
> “不无声丢失”仅针对正常运行路径。外部破坏性操作（`Flush/Delete`）、
> TTL 到期删除或 Redis 数据丢失仍可能导致消息丢失。

## 安装

要求：

- Go `1.21+`
- Redis `6+` 或兼容版本

```bash
go get github.com/liuxiaozhicn/async-queue-go
```

## 核心概念

| 概念 | 示例 | 作用 |
| --- | --- | --- |
| `queue` | `order` | 业务队列 key，用于配置项查找、handler 绑定和 `server.Queue("order")` |
| `driver` | `redis` | 后端驱动注册 key，用于 `WithDriver("redis", driver)` 和配置中的 `driver` 字段 |
| `channel` | `queue:order` | 队列在驱动中的逻辑队列标识（key 前缀）；生产端和消费端必须一致 |

## 快速上手

1. 启动 Redis（任选一种）：
   - Docker（推荐）：
   ```bash
   docker run --name asyncq-redis \
     -p 6379:6379 \
     -e TZ=Asia/Shanghai \
     -d redis:7 \
     redis-server --appendonly yes
   ```
   - 已有容器直接启动：
   ```bash
   docker start asyncq-redis
   ```
   - 本机/远程 Redis：确保服务可达并监听正确地址。
2. 连通性检查（建议）：
   ```bash
   redis-cli -h 127.0.0.1 -p 6379 ping
   ```
   返回 `PONG` 再继续。
3. 构建一个最小 `Config`（例如队列 key 为 `order`）。
4. 注册驱动：`WithDriver("redis", queue.NewRedisDriver(client))`。
5. 用同名队列 key 绑定 handler：`serveMux.Handle("order", handler)`。
6. 启动服务并通过 `server.Queue("order").PushTask(...)` 投递任务。

新用户建议直接从下面示例开始：

- [`examples/demo/basic/main.go`](/Users/liuxiaozhi/Desktop/async-queue-go/examples/demo/basic/main.go)
- [`examples/demo/order/main.go`](/Users/liuxiaozhi/Desktop/async-queue-go/examples/demo/order/main.go)

## 配置快速说明

代码内最小配置示例：

```go
cfg := &asyncqueue.Config{
    Queues: map[string]asyncqueue.QueueConfig{
        "order": {
            Driver:          "redis",
            Channel:         "queue:order",
            Enabled:         true,
            PopTimeout:      3,
            HandleTimeout:   180,
            RetrySeconds:    []int{10, 30, 60, 120, 300},
            MessageTTL:      864000,
            MaxAttempts:     5,
            Processes:       2,
            Concurrent:      50,
            ShutdownTimeout: 240,
        },
    },
}
```

字段说明：

| 字段 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `driver` | `string` | `""` | 驱动注册名，必须和 `WithDriver("<name>", driver)` 一致，例如 `redis`。 |
| `channel` | `string` | 队列名 | 后端命名空间/前缀；生产后建议保持稳定，避免迁移复杂度。 |
| `enabled` | `bool` | `true` | 是否在 manager 启动该队列的 worker/forwarder。 |
| `pop_timeout` | `int`（秒） | `5` | 阻塞拉取超时。常用 `1~5`；越小停机响应越快，越大 Redis 轮询压力越低。 |
| `handle_timeout` | `int`（秒） | `180` | 单条消息处理最大时长，超时进入恢复流程；常用 `60~300`。 |
| `retry_seconds` | `[]int`（秒） | `[5,10,30,60,120]` | 按尝试次数索引的退避序列，建议递增配置。 |
| `message_ttl` | `int`（秒） | `864000` | 消息元数据 TTL；常用 `1~14` 天。 |
| `max_attempts` | `int` | `5` | 进入 `failed` 前的最大总尝试次数。 |
| `processes` | `int` | `1` | 该队列 consumer 进程数（goroutine worker 组）。 |
| `concurrent` | `int` | `1` | 每个 consumer 进程内的最大并发 in-flight 数。 |
| `shutdown_timeout` | `int`（秒） | `30` | 队列 worker/forwarder 的优雅停机等待上限。 |

关键参数建议：

- `channel`：后端命名空间，生产端和消费端必须一致
- `handle_timeout`：单条消息处理超时，通常设在 `60~300` 秒
- `retry_seconds`：重试退避序列，建议递增
- `max_attempts`：进入 `failed` 前的最大尝试次数
- `processes` / `concurrent`：吞吐调节参数，按下游承载能力调优

## 推荐用法

主推荐路径是：

1. 启动 `Server`
2. 通过 `ServeMux` 注册 handler
3. `server.Run(...)` 启动后，通过 `server.Queue("order")` 获取队列实例
4. 使用 `Queue.PushTask(...)` 投递

重点：`server.Run(ctx, serveMux)` 依赖 `ServeMux` 完整绑定已启用队列的任务类型。  
示例：`serveMux.Handle("order", orderJobHandler)`。
绑定规则：`ServeMux.Handle(<queue>, <handler>)` 的 `<queue>` 必须与 `Config.Queues` 的 key 完全一致。
若某个已启用队列缺少该绑定，worker 启动会失败。

> [!IMPORTANT]
> **必须完成 handler 绑定**：
> `ServeMux.Handle(<queue>, <handler>)` 的 `<queue>` 必须与 `Config.Queues` key 严格一致。

示例：

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

如果进程只负责生产、不启动 worker，可以直接使用 `NewAsyncQueue(...)`。

## Task 与 Message 的关系

- `Task` 是业务侧定义的结构体（例如 `OrderTask`）。
- `Message` 是队列运行时的消息封装，会落库保存（`id`、`payload`、`attempts`、`max_attempts`、`status`、时间戳等）。
- `Queue.PushTask(ctx, task, delay)` 会把 `task` 序列化到 `Message.Payload` 后再投递。
- `Queue.PushMessage(ctx, msg, delay)` 允许你直接投递构造好的 `Message`。
- 消费侧 handler 收到的始终是 `*core.Message`，业务数据通过反序列化 `m.Payload` 获取。
- `messageID` 由 driver 在投递时生成，后续用于 `GetMessage`、`Cancel`、`Reload`、`Flush` 等管理操作。

## 示例

- 高层完整示例：[`examples/demo/basic/main.go`](examples/demo/basic/main.go)
- 业务场景示例：[`examples/demo/order/main.go`](examples/demo/order/main.go)
- 低层 worker 示例：[`examples/worker/main.go`](examples/worker/main.go)

运行 demo：

```bash
go run ./examples/demo/basic
```

## 详细文档

- 英文详细文档：[`docs/guide.md`](docs/guide.md)
- 中文详细文档：[`docs/zh-CN/guide.md`](docs/zh-CN/guide.md)

详细文档包含：

- 架构和流程图
- 消息生命周期
- Redis 存储模型
- 配置项说明
- 队列管理能力
- 自定义驱动扩展
- FAQ

## 测试

全量测试：

```bash
go test ./...
```

仅做编译级校验：

```bash
go test ./... -run TestDoesNotExist -count=1
```
