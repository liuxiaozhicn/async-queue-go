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
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// OrderJob handles order creation.
type OrderJob struct {
	OrderNo     string  `json:"order_no"`
	UserID      int     `json:"user_id"`
	TotalAmount float64 `json:"total_amount"`
}

func (j *OrderJob) GetType() string { return "order" }

// OrderJobHandler handles order creation.
type OrderJobHandler struct {
	mode demoResultMode
	seq  atomic.Uint64
}

type demoResultMode string

const (
	modeAck     demoResultMode = "ack"
	modeRetry   demoResultMode = "retry"
	modeRequeue demoResultMode = "requeue"
	modeDrop    demoResultMode = "drop"
	modeError   demoResultMode = "error"
	modeMixed   demoResultMode = "mixed"
)

func parseDemoResultMode(v string) demoResultMode {
	switch demoResultMode(strings.ToLower(strings.TrimSpace(v))) {
	case modeAck, modeRetry, modeRequeue, modeDrop, modeError, modeMixed:
		return demoResultMode(strings.ToLower(strings.TrimSpace(v)))
	default:
		return modeMixed
	}
}

func (h *OrderJobHandler) nextResult() (core.Result, error) {
	switch h.mode {
	case modeAck:
		return core.ACK, nil
	case modeRetry:
		return core.RETRY, nil
	case modeRequeue:
		return core.REQUEUE, nil
	case modeDrop:
		return core.DROP, nil
	case modeError:
		return core.ACK, errors.New("demo forced error")
	default:
		n := h.seq.Add(1)
		switch n % 5 {
		case 1:
			return core.ACK, nil
		case 2:
			return core.RETRY, nil
		case 3:
			return core.REQUEUE, nil
		case 4:
			return core.DROP, nil
		default:
			return core.ACK, errors.New("demo forced error")
		}
	}
}

func (h *OrderJobHandler) Handle(ctx context.Context, m *core.Message) (core.Result, error) {
	job := &OrderJob{}
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
			"order": {
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

	resultMode := parseDemoResultMode(os.Getenv("DEMO_RESULT_MODE"))
	log.Printf("[Main] DEMO_RESULT_MODE=%s", resultMode)

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
		orderJob := &OrderJob{}

		orderJobHandler := &OrderJobHandler{mode: resultMode}
		// 1.queue.HandlerFunc func
		//serveMux.Register(orderJob.GetType(), queue.HandlerFunc(func(ctx context.Context, m *core.Message) (core.Result, error) {
		//	job := &OrderJob{}
		//	_ = json.Unmarshal(m.Payload, job)
		//	log.Printf("[OrderJob] processing order #%d for user %d, total: %.2f", job.OrderID, job.UserID, job.TotalAmount)
		//	time.Sleep(10 * time.Second)
		//	log.Printf("[OrderJob] order #%d handled successfully", job.OrderID)
		//	return core.ACK, nil
		//}))
		// 2.queue.Handler interface
		serveMux.Handle(orderJob.GetType(), orderJobHandler)
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
		queue, _ := s.Queue("order")
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				orderNo := generateOrderNo()
				job := &OrderJob{
					OrderNo:     orderNo,
					UserID:      rand.Intn(1000) + 1,
					TotalAmount: float64(rand.Intn(95000)+1000) / 100.0,
				}
				_, err := queue.PushJob(ctx, job, 30)
				if err != nil {
					continue
				}
			}
		}
	}()
	wg.Wait()
}
