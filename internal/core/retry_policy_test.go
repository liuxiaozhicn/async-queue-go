package core

import "testing"

func TestRetrySecondsFixed(t *testing.T) {
	if got := RetrySeconds([]int{5}, 1); got != 5 {
		t.Fatalf("want 5 got %d", got)
	}
	if got := RetrySeconds([]int{5}, 99); got != 5 {
		t.Fatalf("want 5 got %d", got)
	}
}

func TestRetrySecondsArray(t *testing.T) {
	items := []int{1, 2, 4}
	if got := RetrySeconds(items, 1); got != 1 {
		t.Fatalf("want 1 got %d", got)
	}
	if got := RetrySeconds(items, 2); got != 2 {
		t.Fatalf("want 2 got %d", got)
	}
	if got := RetrySeconds(items, 3); got != 4 {
		t.Fatalf("want 4 got %d", got)
	}
	if got := RetrySeconds(items, 9); got != 4 {
		t.Fatalf("want 4 got %d", got)
	}
}

func TestRetrySecondsFallback(t *testing.T) {
	if got := RetrySeconds(nil, 1); got != 10 {
		t.Fatalf("want 10 got %d", got)
	}
}
