package main

import (
	"context"
	"encoding/json"
	"flag"
	"github.com/liuxiaozhicn/async-queue-go/pkg/core"
	"log"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/liuxiaozhicn/async-queue-go/asyncqueue"
	"github.com/redis/go-redis/v9"
	"math/rand"
)

// OrderJob handles order creation.
type OrderJob struct {
	OrderID     int     `json:"order_id"`
	UserID      int     `json:"user_id"`
	TotalAmount float64 `json:"total_amount"`
}

func (j *OrderJob) GetType() string { return "order" }

// OrderJobHandler handles order creation.
type OrderJobHandler struct{}

func (h *OrderJobHandler) Handle(ctx context.Context, m *core.Message) (core.Result, error) {
	job := &OrderJob{}
	_ = json.Unmarshal(m.Payload, job)
	duration := time.Duration(60+rand.Intn(61)) * time.Second
	select {
	case <-time.After(duration * time.Second):
		return core.ACK, nil
	case <-ctx.Done():
		return core.RETRY, ctx.Err()
	}
	return core.ACK, nil
}

func main() {
	configFile := flag.String("config", "config.json", "config file path")
	flag.Parse()

	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	s, err := asyncqueue.LoadServer(*configFile, client)
	if err != nil {
		log.Fatalf("[Main] failed to load server: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup

	// Start worker
	wg.Add(1)
	go func() {
		defer wg.Done()
		serveMux := asyncqueue.NewServeMux()
		orderJob := &OrderJob{}

		orderJobHandler := &OrderJobHandler{}
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

		queue, err := s.Queue("order")
		if err != nil {
			log.Printf("[Push] failed to get queue: %v", err)
		}

		orderID := 8000
		for {
			select {
			case <-ctx.Done():
				log.Println("[Main] shutting down...")
				return
			case <-ticker.C:
				orderID++
				job := &OrderJob{
					OrderID:     orderID,
					UserID:      orderID % 100,
					TotalAmount: float64(orderID%500 + 50),
				}
				queue.PushJob(ctx, job, 0)
			}
		}
	}()
	log.Println("[Main] running, press Ctrl+C to stop")
	wg.Wait()

	log.Println("[Main] stopped")
}
