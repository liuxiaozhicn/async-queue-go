package main

import (
	"testing"
)

func TestOrderJob_GetType(t *testing.T) {
	job := &OrderJob{}
	expected := "order"
	if got := job.GetType(); got != expected {
		t.Errorf("GetType() = %v, want %v", got, expected)
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

func BenchmarkOrderJob_GetType(b *testing.B) {
	job := &OrderJob{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = job.GetType()
	}
}
