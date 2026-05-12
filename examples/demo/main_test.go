package main

import (
	"github.com/liuxiaozhicn/async-queue-go/pkg/core"
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
		OrderNo:     "order-1001",
		UserID:      42,
		TotalAmount: 299.99,
	}

	if job.OrderNo != "order-1001" {
		t.Errorf("OrderNo = %v, want order-1001", job.OrderNo)
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

func TestParseDemoResultMode(t *testing.T) {
	tests := []struct {
		in   string
		want demoResultMode
	}{
		{in: "ack", want: modeAck},
		{in: "retry", want: modeRetry},
		{in: "requeue", want: modeRequeue},
		{in: "drop", want: modeDrop},
		{in: "error", want: modeError},
		{in: "mixed", want: modeMixed},
		{in: "", want: modeMixed},
		{in: "unknown", want: modeMixed},
	}

	for _, tt := range tests {
		if got := parseDemoResultMode(tt.in); got != tt.want {
			t.Fatalf("parseDemoResultMode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestOrderJobHandlerNextResultByMode(t *testing.T) {
	tests := []struct {
		mode       demoResultMode
		wantResult core.Result
		wantErr    bool
	}{
		{mode: modeAck, wantResult: core.ACK},
		{mode: modeRetry, wantResult: core.RETRY},
		{mode: modeRequeue, wantResult: core.REQUEUE},
		{mode: modeDrop, wantResult: core.DROP},
		{mode: modeError, wantResult: core.ACK, wantErr: true},
	}

	for _, tt := range tests {
		h := &OrderJobHandler{mode: tt.mode}
		gotRes, gotErr := h.nextResult()
		if gotRes != tt.wantResult {
			t.Fatalf("mode=%s result=%v, want %v", tt.mode, gotRes, tt.wantResult)
		}
		if (gotErr != nil) != tt.wantErr {
			t.Fatalf("mode=%s err=%v, wantErr=%v", tt.mode, gotErr, tt.wantErr)
		}
	}
}

func TestOrderJobHandlerNextResultMixedCoversAll(t *testing.T) {
	h := &OrderJobHandler{mode: modeMixed}
	seen := map[core.Result]bool{}
	seenErr := false

	for i := 0; i < 10; i++ {
		res, err := h.nextResult()
		if err != nil {
			seenErr = true
		}
		seen[res] = true
	}

	if !seen[core.ACK] || !seen[core.RETRY] || !seen[core.REQUEUE] || !seen[core.DROP] || !seenErr {
		t.Fatalf("mixed mode did not cover all branches: seen=%v seenErr=%v", seen, seenErr)
	}
}
