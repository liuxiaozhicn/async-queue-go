# 详细文档

[English](../guide.md)

## 概览

`async-queue-go` 是一个基于 Redis 驱动的异步任务队列，重点是：

- 队列与处理器绑定清晰
- 消息状态流转原子化（Lua）
- 至少一次投递 + 超时容错恢复
- 从 `NewServer` 到投递任务接入路径简单

核心术语：

- `queue`：业务队列 key，例如 `order`
- `driver`：后端驱动注册 key，例如 `redis`
- `channel`：该队列在存储中的 key 前缀，例如 `queue:order`

当前仓库内置 Redis 实现，运行时统一抽象在 `pkg/queue.Driver` 之下。

## 快速开始

以下步骤用于直接运行 `order` 示例，快速验证完整链路：

这个示例模拟一个常见业务场景：创建订单后先标记为待支付，并异步投递“主动查询支付结果”任务；如果支付回调先到达，则取消尚未执行的查询任务；如果回调未到达，则由队列按重试策略继续查询，直到任务结束。

1. 克隆代码并进入目录：
```bash
git clone https://github.com/liuxiaozhicn/async-queue-go.git
cd async-queue-go
```
2. 启动 Redis（任选一种）：
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
3. 连通性检查（建议）：
```bash
redis-cli -h 127.0.0.1 -p 6379 ping
```
返回 `PONG` 再继续。
4. 下载依赖：
```bash
go mod tidy
```
5. 运行订单 HTTP 示例：
```bash
go run ./examples/demo/order
```
6. 创建订单（会触发延迟查询任务）：
```bash
curl -X POST http://127.0.0.1:8080/order/create \
  -H "Content-Type: application/json" \
  -d '{"order_no":"ORD-1001"}'
```
7. 模拟支付回调（取消待执行的查询任务）：
```bash
curl -X POST http://127.0.0.1:8080/order/callback \
  -H "Content-Type: application/json" \
  -d '{"order_no":"ORD-1001"}'
```

如果不回调，查询任务会按重试策略继续，直到成功或失败。

## 新手阅读路径

第一次接入建议按下面顺序阅读：

1. `5 分钟快速上手`（先跑通）
2. `Task 与 Message 关系`（理解投递对象与运行时消息）
3. `队列管理能力`（查看/重装载/取消）
4. `消息生命周期`（深入状态流转）
5. `架构说明` 与 `自定义驱动扩展`（进阶）

### 可靠性保证与边界

- 原子性：
  队列状态流转通过 Redis Lua 脚本提交，单次操作内的多 key 更新具备原子性。
- 超时容错：
  consumer 在 `reserved` 后异常退出时，forwarder 会把超时保留消息迁移到 `timeout`。
- 可重装载：
  `Reload("timeout")` 与 `Reload("failed")` 是显式的运维恢复路径，可把消息重新放回 `waiting`。
- 投递语义：
  系统提供至少一次投递（at-least-once），不是 exactly-once。
  业务 handler 需要按幂等方式设计，以处理潜在重复投递。
- 丢失边界：
  正常链路下不应出现无声丢失；在外部破坏性操作或存储层数据丢失场景下仍可能丢失。

> [!IMPORTANT]
> 投递保证是 **at-least-once**，业务 handler 应按**幂等**实现。

> [!WARNING]
> 这不等于 exactly-once，也不代表基础设施故障下绝对不丢失。
> 外部破坏性操作与存储层数据丢失不在运行时保证范围内。

## 5 分钟快速上手

最小步骤：

1. 启动 Redis（任选一种）并确认 `redis-cli ping` 返回 `PONG`。
2. 注册驱动：`WithDriver("redis", queue.NewRedisDriver(client))`。
3. 在 `Config.Queues` 定义一个队列（例如 key 为 `order`）。
4. 用同名队列 key 绑定 handler：`serveMux.Handle("order", handler)`。
5. 启动服务：`server.Run(ctx, serveMux)`。
6. 投递任务：`server.Queue("order").PushTask(...)`。

可直接运行的示例：

- 基础示例：[`examples/demo/basic/main.go`](/Users/liuxiaozhi/Desktop/async-queue-go/examples/demo/basic/main.go)
- 业务场景示例：[`examples/demo/order/main.go`](/Users/liuxiaozhi/Desktop/async-queue-go/examples/demo/order/main.go)

### 首次运行自检

启动后可按下面检查是否跑通：

1. `server.Run(...)` 未立即报错退出。
2. 每个启用队列都完成了 handler 绑定。
3. 投递返回非空 `messageID`。
4. `Info` 中 `waiting/reserved/delayed` 计数有符合预期的变化。

### 新手最常见问题

| 现象 | 常见原因 | 处理方式 |
| --- | --- | --- |
| worker 启动即失败 | 启用队列缺少 handler 绑定 | 确保每个启用队列 key 都有 `ServeMux.Handle(<queue>, <handler>)` |
| 投递成功但始终不消费 | `Config.Queues` key 与 `Handle`/`server.Queue` 使用的 key 不一致 | 配置、绑定、投递三处统一使用完全一致的队列 key |
| 消息频繁进入 `timeout` | `handle_timeout` 小于真实处理耗时 | 按 handler 的 p99 耗时上调 `handle_timeout` |

## 配置示例

```json
{
  "queues": {
    "order": {
      "driver": "redis",
      "channel": "queue:order",
      "enabled": true,
      "pop_timeout": 1,
      "handle_timeout": 30,
      "retry_seconds": [5, 10, 30],
      "message_ttl": 86400,
      "max_attempts": 3,
      "processes": 2,
      "concurrent": 20,
      "max_messages": 0,
      "auto_restart": false,
      "shutdown_timeout": 30
    }
  }
}
```

常规用法（推荐）：在代码里构建 `Config`，直接用 `NewServer`。
handler 绑定规则：`ServeMux.Handle(<queue>, <handler>)` 中的 `<queue>` 必须与 `Config.Queues` 的 key 一致，否则已启用 worker 启动会失败。

> [!IMPORTANT]
> **必须绑定 handler**：`Config.Queues` 中每个启用队列都必须在 `Run` 前完成
> `ServeMux.Handle(<queue>, <handler>)` 绑定。

完整可运行示例：

```go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/liuxiaozhicn/async-queue-go/asyncqueue"
	"github.com/liuxiaozhicn/async-queue-go/pkg/core"
	"github.com/liuxiaozhicn/async-queue-go/pkg/queue"
	"github.com/redis/go-redis/v9"
	"log"
	"math/rand"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const (
	queueName = "order"
)

var ErrUnknownProcessing = errors.New("unknown order processing error")

// OrderTask handles order creation.
type OrderTask struct {
	OrderNo     string  `json:"order_no"`
	UserID      int     `json:"user_id"`
	TotalAmount float64 `json:"total_amount"`
}

// OrderTaskHandler handles order creation.
type OrderTaskHandler struct {
}

func (h *OrderTaskHandler) nextResult() (core.Result, error) {
	switch rand.Intn(5) {
	case 0:
		return core.ACK, nil
	case 1:
		return core.RETRY, nil
	case 2:
		return core.REQUEUE, nil
	case 3:
		return core.DROP, nil
	default:
		return "", ErrUnknownProcessing
	}
}

func (h *OrderTaskHandler) Handle(ctx context.Context, m *core.Message) (core.Result, error) {
	job := &OrderTask{}
	_ = json.Unmarshal(m.Payload, job)

	duration := time.Duration(100+rand.Intn(200)) * time.Millisecond
	select {
	case <-time.After(duration):
		return h.nextResult()
	case <-ctx.Done():
		return core.RETRY, ctx.Err()
	}
}

func generateOrderNo() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	randPart := fmt.Sprintf("%08x", b) // 8位随机hex
	datePart := time.Now().Format("20060102")
	return fmt.Sprintf("bn-%s-%s", datePart, randPart)
}

func main() {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	queueCfg := &asyncqueue.Config{
		Queues: map[string]asyncqueue.QueueConfig{
			queueName: {
				Driver:          "redis",
				Channel:         "queue:order",
				Enabled:         true,
				PopTimeout:      1,
				HandleTimeout:   180,
				ShutdownTimeout: 240,
				Processes:       2,
				Concurrent:      50,
				MaxAttempts:     3,
				RetrySeconds:    []int{5, 10, 30},
				AutoRestart:     false,
				MaxMessages:     10,
			},
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	s, err := asyncqueue.NewServer(queueCfg, asyncqueue.WithDriver("redis", queue.NewRedisDriver(client)))
	if err != nil {
		log.Fatalf("[Main] failed to load server: %v", err)
	}

	var wg sync.WaitGroup

	// Start worker
	wg.Add(1)
	go func() {
		defer wg.Done()
		serveMux := asyncqueue.NewServeMux()
		orderJobHandler := &OrderTaskHandler{}
		serveMux.Handle(queueName, orderJobHandler)
		if err := s.Run(ctx, serveMux); err != nil {
			log.Fatalf("server run failed: %v", err)
		}
	}()

	// Push initial sample jobs after worker is ready
	time.Sleep(1 * time.Second)

	// Push periodic jobs every 10s
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		queue, _ := s.Queue(queueName)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				orderNo := generateOrderNo()
				job := &OrderTask{
					OrderNo:     orderNo,
					UserID:      rand.Intn(1000) + 1,
					TotalAmount: float64(rand.Intn(95000)+1000) / 100.0,
				}
				_, err := queue.PushTask(ctx, job, 30)
				if err != nil {
					continue
				}
			}
		}
	}()
	wg.Wait()
}


```

如果启用队列对应的任务类型没有在 `ServeMux` 绑定，worker 启动会失败。
你可以保留这个 `messageID`，后续用于 `GetMessage`、`Cancel`、`RetryByID` 等管理操作。

## Task 与 Message 关系

- `Task` 是业务层定义的任务结构体。
- `Message` 是队列运行时使用并持久化的消息封装。
- `PushTask` 会把 `Task` 序列化到 `Message.Payload` 后入队。
- `PushMessage` 用于直接投递调用方构造好的消息封装。
- 消费者 handler 收到的始终是 `*core.Message`，业务层需要把 `m.Payload` 反序列化为自己的 `Task` 类型。
- `messageID` 由 driver 在投递时生成，是后续管理接口定位消息的主键。

## 配置项说明

### 完整示例

```json
{
  "queues": {
    "order": {
      "driver": "redis",
      "channel": "queue:order",
      "enabled": true,
      "pop_timeout": 3,
      "handle_timeout": 180,
      "retry_seconds": [10, 30, 60, 120, 300],
      "message_ttl": 864000,
      "max_attempts": 5,
      "processes": 2,
      "concurrent": 50,
      "max_messages": 0,
      "auto_restart": false,
      "shutdown_timeout": 240
    }
  }
}
```

### 参数明细

| 字段 | 默认值 | 说明 | 推荐范围 / 建议 |
| --- | --- | --- | --- |
| `driver` | `redis`（配置文件加载时自动补） | 通过 `WithDriver(name, driver)` 查找驱动注册名 | 除非有自定义驱动，否则保持 `redis` |
| `channel` | 无（必填） | 后端存储命名空间 | 使用稳定业务名，如 `queue:order` |
| `enabled` | `false` | 是否启用该队列的消费与转发 | 生产队列一般设为 `true` |
| `pop_timeout` | `1`（配置加载回退） | 空轮询等待秒数 | `1~5`，越大空轮询压力越低 |
| `handle_timeout` | `10`（配置加载回退） | 单条消息处理超时秒数 | 常用 `60~300`，按 handler 的 p99 耗时设定 |
| `retry_seconds` | `[5]`（配置加载回退） | 重试退避序列（秒） | 建议递增，如 `[10,30,60,120,300]` |
| `message_ttl` | `864000` | `message:<id>` 实体 TTL（秒） | 按审计需要设 `1~30` 天；`0` 表示不过期 |
| `max_attempts` | `3` | 最大投递尝试次数 | 常用 `3~8`，结合幂等性与业务 SLA 调整 |
| `processes` | `1` | 当前进程内 consumer 实例数 | 可从 CPU 核数或更低起步，再按吞吐扩容 |
| `concurrent` | `10` | 每个 consumer 实例并发数 | 按 DB/RPC 承载能力调，避免压垮下游 |
| `max_messages` | `0` | 单个 worker 处理上限（`0` 不限制） | 长驻 worker 通常保持 `0` |
| `auto_restart` | `false` | 达到 `max_messages` 后是否重启 worker | 仅在有意做短生命周期 worker 时开启 |
| `shutdown_timeout` | `30` | 优雅停机等待秒数 | 生产常见 `60~300` |

### 快速调优指引

| 现象 | 优先调整项 |
| --- | --- |
| 消息频繁进入 `timeout` | 增大 `handle_timeout` |
| 故障时重试风暴明显 | 拉长 `retry_seconds`、必要时降低 `max_attempts` |
| 队列空闲时 Redis 压力偏高 | 增大 `pop_timeout` |
| 下游 DB/RPC 被打满 | 降低 `concurrent` 或 `processes` |
| 发布/停机时退出太慢 | 增大 `shutdown_timeout` |


## 架构说明

### 分层结构

```mermaid
flowchart TD
    App[业务应用]
    Server[asyncqueue.Server]
    Manager[asyncqueue.Manager]
    Queue[asyncqueue.Queue]
    Consumer[pkg/queue.Consumer]
    Forwarder[pkg/queue.forwarder]
    Driver[pkg/queue.Driver]
    RedisDriver[pkg/queue.RedisDriver]
    Redis[(Redis)]

    App --> Server
    App --> Queue
    Server --> Manager
    Manager --> Queue
    Manager --> Consumer
    Manager --> Forwarder
    Queue --> Driver
    Consumer --> Driver
    Forwarder --> Driver
    Driver --> RedisDriver
    RedisDriver --> Redis
```

### 启动流程

```mermaid
flowchart LR
    A[加载 Config] --> B[通过 WithDriver 注册驱动]
    B --> C[NewServer]
    C --> D[创建 Manager]
    D --> E[Run / StartWorker]
    E --> F[校验 handler 和 driver]
    F --> G[按启用配置创建 Queue]
    G --> H[启动 Forwarder]
    G --> I[启动 Consumer x Processes]
```

### 运行职责

| 组件 | 职责 |
| --- | --- |
| `Server` | 高层入口，负责配置、驱动注册、handler 注册和生命周期 |
| `Manager` | 根据配置创建队列、消费者、forwarder，并管理启停 |
| `Queue` | 生产侧 API，负责投递、查询、删除、重试、重装载和统计 |
| `Consumer` | 消费循环，调用 handler 并提交 ACK / RETRY / REQUEUE / DROP |
| `Forwarder` | 后台搬运延迟到期消息和超时保留消息 |
| `Driver` | 后端抽象层，定义队列操作和状态流转能力 |
| `RedisDriver` | 当前内置后端实现 |

## 消息生命周期

投递结果分支：
```mermaid
flowchart LR
    P[Producer] -->|delay = 0| W[waiting]
    P -->|delay > 0| D[delayed]
    D -->|Forwarder 转发到期消息| W
    W -->|Consumer Pop| R[reserved]
```

消费结果分支：

```mermaid
flowchart LR
    R[reserved]
    R -->|Handler 返回 ACK| ACK[done]
    R -->|Handler 返回 DROP| DROP[dropped]
    R -->|Handler 返回 REQUEUE| W[waiting]
    R -->|Handler 返回 RETRY 或 error 且仍可重试| D[delayed]
    R -->|Handler 返回 RETRY 或 error 且重试次数耗尽| F[failed]
    R -->|超过 handleTimeout| T[timeout]
```

失败或超时重试分支：

```mermaid
flowchart LR
    T[timeout] -->|手动重装载 timeout 队列| W[waiting]
    F[failed]  -->|手动重装载 failed 队列| W
```

说明：

- `waiting` 是主消费入口
- `reserved` 表示消息已被某个 consumer 取走，但还没有提交结果
- `delayed` 同时承载主动延迟和重试退避
- `timeout` 和 `failed` 都不会自动回到 `waiting`
- 手动重装载是显式操作，所以单独拆成一张恢复图

### 状态流转矩阵

| 阶段 | 触发条件 | 队列流转 | 持久化状态 |
| --- | --- | --- | --- |
| 生产 | `Push(delay=0)` | `-> waiting` | `waiting` |
| 生产 | `Push(delay>0)` | `-> delayed` | `delayed` |
| 转发 | delayed 到期 | `delayed -> waiting` | `waiting` |
| 消费获取 | `Pop` | `waiting -> reserved` | `reserved` |
| 消费提交 | `ACK` | `reserved -> (移除)` | `done` |
| 消费提交 | `DROP` | `reserved -> (移除)` | `dropped` |
| 消费提交 | `REQUEUE` | `reserved -> waiting` | `waiting` |
| 消费提交 | `RETRY` 且 `delay>0` | `reserved -> delayed` | `delayed` |
| 消费提交 | `RETRY` 且 `delay<=0` | `reserved -> waiting` | `waiting` |
| 消费异常 | error 且可重试，`delay>0` | `reserved -> delayed` | `delayed` |
| 消费异常 | error 且可重试，`delay<=0` | `reserved -> waiting` | `waiting` |
| 消费终态 | error/RETRY 且重试耗尽 | `reserved -> failed` | `failed` |
| 转发 | 保留超时 | `reserved -> timeout` | `timeout` |
| 人工操作 | `Reload("timeout")` | `timeout -> waiting` | `waiting` |
| 人工操作 | `Reload("failed")` | `failed -> waiting` | `waiting` |
| 人工操作 | `Cancel`（仅 `delayed`） | `delayed -> (移除)` | `canceled` |

### 并发与一致性规则

| 规则 | 原因 |
| --- | --- |
| 队列位置是调度状态的唯一真相 | `waiting/reserved/delayed` 直接决定消息能否被消费、重试或取消 |
| `status` 是持久化视图，不应单独作为决策依据 | 高吞吐下应先用 Lua 队列操作判定，再回写状态 |
| 每条已消费消息只允许一次提交动作 | 消息进入 `reserved` 后，语义上只能落到 `ACK/RETRY/REQUEUE/DROP/FAIL` 之一 |
| 重试路径固定为 `reserved -> waiting/delayed` | `delay<=0` 进入下一轮立即调度（`waiting`），`delay>0` 进入退避（`delayed`） |
| 取消仅允许在 `delayed` | 消息进入 `waiting` 或 `reserved` 后按设计拒绝取消 |

### Lua 原子性、超时容错与消息丢失语义

- Lua 原子性：
  `pop`、`ack`、`retry`、`requeue`、`fail`、`drop`、`cancel` 等状态提交都通过 Redis Lua 脚本执行。
  单次脚本内的多 key 更新具备原子性，要么全部成功，要么全部不生效。
- 超时容错：
  当消息已进入 `reserved`，若 consumer 崩溃、卡死或进程异常退出，forwarder 会把超时保留消息转移到 `timeout`。
  之后可通过 `Reload("timeout")` 重新投递。
- 消息丢失语义：
  系统目标是至少一次投递（at-least-once），不是 exactly-once。
  在 Redis 持久化与 TTL 配置合理的前提下，不会在正常路径中无声丢消息。
  但在外部条件下仍可能丢失（例如 Redis 数据丢失、显式 `Flush`、TTL 到期删除、人工破坏性操作）。

### 排障检查清单

| 现象 | 优先检查项 |
| --- | --- |
| 消息长时间停在 `reserved` | 检查 handler 超时与提交动作（`ACK/RETRY/...`）是否报错 |
| 消息频繁进入 `timeout` | 检查 `handleTimeout` 是否小于真实处理 p99 |
| 重试后没有进入延迟 | 检查重试延迟值；`delay<=0` 本来就会回 `waiting` |
| 取消返回 false | 确认消息是否仍在 `delayed`（而不是 `waiting/reserved`） |
| 状态字段看起来不一致 | 先查队列归属，再看 `message:<id>` 的状态内容 |

## Handler 返回值语义

| 返回值 | 含义 |
| --- | --- |
| `core.ACK` | 成功完成，从保留队列移除 |
| `core.RETRY` | 按重试策略进入延迟队列 |
| `core.REQUEUE` | 立即回到等待队列 |
| `core.DROP` | 把消息标记为 `dropped`，并停止后续重试 |

如果 handler 返回 `error`，框架会走错误处理路径，而不是使用显式返回的 `Result`。

`DROP` 是业务层面的丢弃决策，不是物理删除。消息会离开活动处理队列，但消息实体仍可能保留到 TTL 到期。

## Redis 存储模型

Redis 驱动会按 `channel` 生成一组 key：

```text
{queue:order}:waiting
{queue:order}:reserved
{queue:order}:delayed
{queue:order}:timeout
{queue:order}:failed
{queue:order}:message:<id>
{queue:order}:msg_seq
{queue:order}:msg_seq_epoch
```

说明：

- `waiting`：等待消费的消息队列
- `reserved`：已弹出但尚未提交结果的消息
- `delayed`：延迟消息和重试消息
- `timeout`：处理超时后的消息
- `failed`：重试耗尽的消息
- `message:<id>`：消息实体

`{...}` 哈希标签用于保证同一业务队列的 key 落在同一个 Redis Cluster slot。

## 队列管理能力

| 方法                                    | 说明 |
|---------------------------------------| --- |
| `PushTask(ctx, job, delaySeconds)`     | 投递结构化 Task |
| `PushMessage(ctx, msg, delaySeconds)` | 投递原始消息 |
| `Info(ctx)`                           | 获取 waiting / reserved / delayed / timeout / failed 统计 |
| `GetMessage(ctx, id)`                 | 获取消息详情 |
| `Cancel(ctx, id)`                     | 取消仍处于 `delayed`、尚未进入调度阶段的消息，并把状态标记为 `canceled` |
| `RetryByID(ctx, id, delaySeconds)`    | 重新设定延迟后重试 |
| `Reload(ctx, "timeout" OR "failed")`  | 把 timeout 或 failed 消息重新放回 waiting |
| `Flush(ctx, queueName)`               | 清空一个内部队列 |

## 低层 Consumer 用法

如果你不想使用 `Server` / `Manager`，可以自己组合运行时：

- `queue.NewRedisDriver(...)`
- `queue.NewConsumer(...)`
- `worker.NewWorker(...)`

`NewConsumer(...)` 在不显式覆盖时的默认值：

| 选项 | 默认值 |
| --- | --- |
| `concurrentLimit` | `10` |
| `popTimeout` | `3s` |
| `handleTimeout` | `180s` |
| `retrySeconds` | `[10,30,60,120,300]` |
| `messageTTL` | `864000`（10 天） |

参考：

- [`../../examples/worker/main.go`](../../examples/worker/main.go)

## 自定义驱动扩展

你需要实现：

```go
type Driver interface {
    Ping(ctx context.Context) error
    Push(ctx context.Context, channel string, m *core.Message, delaySeconds int, messageTTL int) error
    Get(ctx context.Context, channel string, id string) (*core.Message, error)
    Cancel(ctx context.Context, channel string, id string) (bool, error)
    Pop(ctx context.Context, channel string, popTimeout time.Duration, handleTimeout time.Duration) (string, *core.Message, error)
    Ack(ctx context.Context, channel string, messageID string) error
    Fail(ctx context.Context, channel string, messageID string) error
    Drop(ctx context.Context, channel string, messageID string) error
    Requeue(ctx context.Context, channel string, messageID string) error
    Retry(ctx context.Context, channel string, id string, delaySeconds int) (bool, error)
    Reload(ctx context.Context, channel string, queue string) (int, error)
    Flush(ctx context.Context, channel string, queue string) error
    Info(ctx context.Context, channel string) (Info, error)
    ForwardMessages(ctx context.Context, channel string) (int64, int64, error)
}
```

注册方式：

```go
server, err := asyncqueue.NewServer(
    cfg,
    asyncqueue.WithDriver("custom", customDriver),
)
```

## FAQ

### 为什么配置里的 `driver` 不是队列名？

因为 `driver` 表示后端实现注册名，不是业务队列名。

### 为什么同一个 `RedisDriver` 可以服务多个队列？

因为当前 `Driver` 接口每次调用都会显式传入 `channel`，driver 自身不再在构造期绑定单个业务队列。

### 为什么不直接提供 `Server.Push`？

因为投递本身就是 `Queue` 的职责，`Server` 更适合作为运行时入口和队列实例获取入口。

推荐写法：

```go
queueInstance, err := server.Queue("order")
id, err := queueInstance.PushTask(ctx, job, 0)
```

### `DROP` 的语义是什么？

- `DROP` 是消费结果，表示业务明确决定不再继续处理这条消息，消息状态会变成 `dropped`。
### `Cancel` 的行为是什么？
- `Cancel` 用于消息还处于 `delayed` 阶段时主动放弃调度。
- 如果消息已经进入 `waiting`，说明它已经进入待调度阶段，会返回 `ErrMessageAlreadyReadyForDispatch`，不再允许取消。
- 如果消息已经进入 `reserved`，说明它已经被消费者取走并进入执行阶段，会返回 `ErrMessageAlreadyInExecution`。
