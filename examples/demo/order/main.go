package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os/signal"
	"sync"
	"syscall"
	"time"

	asyncqueue "github.com/liuxiaozhicn/async-queue-go/asyncqueue"
	"github.com/liuxiaozhicn/async-queue-go/pkg/core"
	"github.com/liuxiaozhicn/async-queue-go/pkg/queue"
	"github.com/redis/go-redis/v9"
)

const (
	queueOrderPayment = "order:payment"
	queueChannel      = "order:payment"
	defaultHTTPAddr   = ":8080"
	defaultRedisAddr  = "127.0.0.1:6379"
	defaultQueryDelay = 30
)

type OrderPaymentQueryJob struct {
	OrderNo string `json:"order_no"`
}

func (j *OrderPaymentQueryJob) GetType() string { return queueOrderPayment }

type OrderStatus string

const (
	OrderStatusUnpaid   OrderStatus = "unpaid"
	OrderStatusPaid     OrderStatus = "paid"
	OrderStatusCanceled OrderStatus = "canceled"
)

type Order struct {
	OrderNo string      `json:"order_no"`
	Status  OrderStatus `json:"status"`
	JobID   string      `json:"job_id,omitempty"`
}

type orderStore struct {
	mu     sync.RWMutex
	orders map[string]Order
}

func newOrderStore() *orderStore {
	return &orderStore{orders: make(map[string]Order)}
}

// OrderStore is temporary in-memory storage for this demo.
// Replace with DB persistence in production.
var OrderStore = newOrderStore()

func (s *orderStore) save(order Order) Order {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.orders[order.OrderNo] = order
	return order
}

func (s *orderStore) get(orderNo string) (Order, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	o, ok := s.orders[orderNo]
	if !ok {
		return Order{}, false
	}
	return o, true
}

type orderRequest struct {
	OrderNo string `json:"order_no"`
}

type OrderQueryHandler struct{}

func (h *OrderQueryHandler) Handle(ctx context.Context, m *core.Message) (core.Result, error) {
	var job OrderPaymentQueryJob
	if err := json.Unmarshal(m.Payload, &job); err != nil {
		log.Printf("[job:order.payment.query] decode failed id=%s err=%v", m.ID, err)
		return core.DROP, nil
	}

	order, ok := OrderStore.get(job.OrderNo)
	if !ok {
		log.Printf("[job:order.payment.query] order_no=%s skip err=order not found id=%s", job.OrderNo, m.ID)
		return core.ACK, nil
	}
	if order.Status != OrderStatusUnpaid {
		log.Printf("[job:order.payment.query] order_no=%s done status=%s id=%s", order.OrderNo, order.Status, m.ID)
		return core.ACK, nil
	}

	if rand.Intn(4) == 0 || m.Attempts >= m.MaxAttempts {
		order.Status = OrderStatusPaid
		OrderStore.save(order)
		log.Printf("[job:order.payment.query] order_no=%s paid_by_query attempts=%d/%d id=%s", order.OrderNo, m.Attempts, m.MaxAttempts, m.ID)
		return core.ACK, nil
	}

	OrderStore.save(order)
	log.Printf("[job:order.payment.query] order_no=%s unpaid attempts=%d/%d id=%s -> retry", order.OrderNo, m.Attempts, m.MaxAttempts, m.ID)
	return core.RETRY, nil
}

func orderCreate(w http.ResponseWriter, r *http.Request) {
	var req orderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	orderNo := req.OrderNo
	if orderNo == "" {
		orderNo = fmt.Sprintf("ORD-%d", time.Now().UnixNano())
	}

	q, err := asyncqueue.GetQueue(queueOrderPayment)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("queue not ready: %v", err))
		return
	}
	jobID, err := q.PushJob(context.Background(), &OrderPaymentQueryJob{OrderNo: orderNo}, defaultQueryDelay)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("push payment-query job failed: %v", err))
		return
	}
	order := OrderStore.save(Order{
		OrderNo: orderNo,
		Status:  OrderStatusUnpaid,
		JobID:   jobID,
	})
	writeJSON(w, http.StatusOK, order)
}

func paymentCallback(w http.ResponseWriter, r *http.Request) {
	var req orderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	order, ok := OrderStore.get(req.OrderNo)
	if !ok {
		writeError(w, http.StatusNotFound, "order not found")
		return
	}

	if order.Status == OrderStatusPaid {
		writeJSON(w, http.StatusOK, order)
		return
	}

	if order.Status == OrderStatusCanceled {
		writeError(w, http.StatusConflict, "order already canceled")
		return
	}

	order.Status = OrderStatusPaid
	order = OrderStore.save(order)

	q, err := asyncqueue.GetQueue(queueOrderPayment)
	if err != nil {
		log.Printf("[payment.callback] order_no=%s get queue failed: %v", order.OrderNo, err)
	} else if _, err := q.Cancel(r.Context(), order.JobID); err != nil {
		log.Printf("[payment.callback] order_no=%s cancel job failed: %v", order.OrderNo, err)
	}

	writeJSON(w, http.StatusOK, order)
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]interface{}{"error": msg})
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	redisClient := redis.NewClient(&redis.Options{Addr: defaultRedisAddr})
	defer redisClient.Close()

	cfg := &asyncqueue.Config{
		Queues: map[string]asyncqueue.QueueConfig{
			queueOrderPayment: {
				Driver:          "redis",
				Channel:         queueChannel,
				Enabled:         true,
				Processes:       2,
				Concurrent:      20,
				PopTimeout:      3,
				HandleTimeout:   180,
				ShutdownTimeout: 240,
				MessageTTL:      864000,
				MaxAttempts:     3,
				RetrySeconds:    []int{30, 90, 180},
			},
		},
	}

	s, err := asyncqueue.NewServer(cfg, asyncqueue.WithDriver("redis", queue.NewRedisDriver(redisClient)))
	if err != nil {
		log.Fatalf("create server failed: %v", err)
	}

	serveMux := asyncqueue.NewServeMux()
	serveMux.Handle(queueOrderPayment, &OrderQueryHandler{})

	go func() {
		if err := s.Run(ctx, serveMux); err != nil && !errors.Is(err, context.Canceled) {
			log.Fatalf("queue server run failed: %v", err)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/order/create", orderCreate)
	mux.HandleFunc("/payment/callback", paymentCallback)

	httpServer := &http.Server{Addr: defaultHTTPAddr, Handler: mux}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("http server failed: %v", err)
	}
}
