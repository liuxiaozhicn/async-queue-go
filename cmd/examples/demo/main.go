package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/liuxiaozhicn/async-queue-go/pkg/asyncqueue"
	"github.com/redis/go-redis/v9"
)

// OrderJob handles order creation.
type OrderJob struct {
	OrderID     int     `json:"order_id"`
	UserID      int     `json:"user_id"`
	TotalAmount float64 `json:"total_amount"`
}

func (j *OrderJob) GetType() string { return "order" }

func (j *OrderJob) Handle(ctx context.Context) (asyncqueue.Result, error) {
	log.Printf("[OrderJob] processing order #%d for user %d, total: %.2f", j.OrderID, j.UserID, j.TotalAmount)
	time.Sleep(200 * time.Millisecond)
	log.Printf("[OrderJob] order #%d handled successfully", j.OrderID)
	return asyncqueue.ACK, nil
}

func pushSampleJobs(ctx context.Context, s *asyncqueue.Server) {
	queue, err := s.Queue("order")
	if err != nil {
		log.Printf("[Push] failed to get queue: %v", err)
		return
	}

	samples := []struct {
		job   *OrderJob
		delay int
	}{
		{&OrderJob{OrderID: 1001, UserID: 42, TotalAmount: 299.99}, 0},
		{&OrderJob{OrderID: 1002, UserID: 99, TotalAmount: 59.90}, 3},
		{&OrderJob{OrderID: 1003, UserID: 123, TotalAmount: 149.50}, 0},
	}

	log.Println("[Push] pushing sample jobs")
	for _, s := range samples {
		if err := queue.PushJob(ctx, s.job, s.delay); err != nil {
			log.Printf("[Push] failed to push order #%d: %v", s.job.OrderID, err)
		} else {
			log.Printf("[Push] order #%d pushed (delay=%ds)", s.job.OrderID, s.delay)
		}
	}
	log.Println("[Push] all sample jobs pushed")
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
		log.Println("[Worker] started, listening on order queue")
		if err := s.Run(ctx, asyncqueue.NewServeMux(&OrderJob{})); err != nil {
			log.Printf("[Worker] error: %v", err)
		}
	}()

	// Push initial sample jobs after worker is ready
	time.Sleep(1 * time.Second)
	pushSampleJobs(ctx, s)

	// Push periodic jobs every 10s
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(10 * time.Second)
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
					log.Printf("[Push] failed to push periodic job: %v", err)
				} else {
					log.Printf("[Push] periodic order #%d pushed", orderID)
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
