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
