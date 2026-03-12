package main

import (
	"context"
	"testing"
	"time"

	"github.com/liuxiaozhicn/async-queue-go/pkg/asyncqueue"
)

func TestOrderJob_GetType(t *testing.T) {
	job := &OrderJob{}
	expected := "order"
	if got := job.GetType(); got != expected {
		t.Errorf("GetType() = %v, want %v", got, expected)
	}
}

func TestOrderJob_Handle(t *testing.T) {
	job := &OrderJob{
		OrderID:     1001,
		UserID:      42,
		TotalAmount: 299.99,
	}

	ctx := context.Background()
	result, err := job.Handle(ctx)

	if err != nil {
		t.Errorf("Handle() error = %v, want nil", err)
	}
	if result != asyncqueue.ACK {
		t.Errorf("Handle() result = %v, want %v", result, asyncqueue.ACK)
	}
}

func TestOrderJob_HandleWithTimeout(t *testing.T) {
	job := &OrderJob{
		OrderID:     1002,
		UserID:      99,
		TotalAmount: 59.90,
	}

	// Test with a context that has a timeout longer than the job processing time
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	result, err := job.Handle(ctx)

	if err != nil {
		t.Errorf("Handle() with timeout error = %v, want nil", err)
	}
	if result != asyncqueue.ACK {
		t.Errorf("Handle() with timeout result = %v, want %v", result, asyncqueue.ACK)
	}
}

func TestOrderJob_Structure(t *testing.T) {
	job := OrderJob{
		OrderID:     1001,
		UserID:      42,
		TotalAmount: 299.99,
	}

	if job.OrderID != 1001 {
		t.Errorf("OrderID = %v, want 1001", job.OrderID)
	}
	if job.UserID != 42 {
		t.Errorf("UserID = %v, want 42", job.UserID)
	}
	if job.TotalAmount != 299.99 {
		t.Errorf("TotalAmount = %v, want 299.99", job.TotalAmount)
	}
}

// Benchmark tests
func BenchmarkOrderJob_Handle(b *testing.B) {
	job := &OrderJob{
		OrderID:     1001,
		UserID:      42,
		TotalAmount: 299.99,
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = job.Handle(ctx)
	}
}

func BenchmarkOrderJob_GetType(b *testing.B) {
	job := &OrderJob{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = job.GetType()
	}
}
