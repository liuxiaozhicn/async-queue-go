# async-queue-go

[English](README.md) | 简体中文

`async-queue-go` 是一个面向 Go 服务的异步任务队列库，当前内置 Redis 驱动，并保留了可扩展的 `Driver` 抽象。

它提供：

- 高层运行时 API：`asyncqueue.Server`、`Manager`、`Queue`
- 低层构建块：`pkg/queue.Driver`、`Consumer`、`Forwarder`
- 至少一次投递语义，支持延迟、重试、超时恢复和失败重装载

## 特性

- 按 `driver name` 注册可插拔驱动
- 内置 Redis 驱动实现
- 支持并发消费与自动重启
- 提供查询、删除、重试、重装载、清理等管理能力
- 支持 JSON / YAML 配置
- 支持优雅停机

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
| `queue name` | `order` | 业务队列名，用于配置项 key、handler 注册和 `server.Queue("order")` |
| `driver name` | `redis` | 后端驱动注册名，用于 `WithDriver("redis", driver)` 和配置中的 `driver` 字段 |
| `channel` | `queue:order` | 后端存储命名空间；生产端和消费端必须一致 |

## 推荐用法

主推荐路径是：

1. 启动 `Server`
2. 通过 `ServeMux` 注册 handler
3. `server.Run(...)` 启动后，通过 `server.Queue("order")` 获取队列实例
4. 使用 `Queue.PushJob(...)` 或 `Queue.PushMessage(...)` 投递

示例：

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

如果进程只负责生产、不启动 worker，可以直接使用 `NewAsyncQueue(...)`。

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

关键参数建议：

- `channel`：后端命名空间，生产端和消费端必须一致
- `handle_timeout`：单条消息处理超时，通常设在 `60~300` 秒
- `retry_seconds`：重试退避序列，建议递增
- `max_attempts`：进入 `failed` 前的最大尝试次数
- `processes` / `concurrent`：吞吐调节参数，按下游承载能力调优

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

## 从配置文件加载（可选）

文件加载是可选方式，主推荐仍然是 `NewServer(cfg, ...)`。

```go
cfg, err := asyncqueue.LoadConfig("config.json")
if err != nil {
    return err
}
server, err := asyncqueue.NewServer(
    cfg,
    asyncqueue.WithDriver("redis", queue.NewRedisDriver(redisClient)),
)
```

## 示例

- 高层完整示例：[`examples/demo/main.go`](examples/demo/main.go)
- Demo 配置：[`examples/demo/config.json`](examples/demo/config.json)
- 低层 worker 示例：[`examples/worker/main.go`](examples/worker/main.go)

运行 demo：

```bash
go run ./examples/demo
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
