package main

import (
	"context"
	"encoding/json"
	"flag"
	"github.com/liuxiaozhicn/async-queue-go/pkg/core"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/liuxiaozhicn/async-queue-go/asyncqueue"
	"github.com/redis/go-redis/v9"
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
	log.Printf("[OrderJob] processing order #%d for user %d, total: %.2f", job.OrderID, job.UserID, job.TotalAmount)
	time.Sleep(10 * time.Second)
	log.Printf("[OrderJob] order #%d handled successfully", job.OrderID)
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
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
		log.Println("[Worker] started, listening on order queue")
		if err := s.Run(ctx, serveMux); err != nil {
			log.Printf("[Worker] error: %v", err)
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

		orderID := 2000
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				orderID++
				queue, err := s.Queue("order")
				if err != nil {
					log.Printf("[Push] failed to get queue: %v", err)
					continue
				}
				job := &OrderJob{
					OrderID:     orderID,
					UserID:      orderID % 100,
					TotalAmount: float64(orderID%500 + 50),
				}
				if err := queue.PushJob(ctx, job, 0); err != nil {
					log.Printf("[Push] order job error: %v", err)
				} else {
					log.Printf("[Push] order job  #%d success", orderID)
				}
			}
		}
	}()

	log.Println("[Main] running, press Ctrl+C to stop")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("[Main] shutting down...")
	cancel()
	wg.Wait()
	log.Println("[Main] stopped")
}
