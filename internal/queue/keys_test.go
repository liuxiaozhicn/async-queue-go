package queue

import "testing"

func TestNewKeysAndGet(t *testing.T) {
	k := NewKeys("test")
	if k.Waiting != "test:waiting" || k.Reserved != "test:reserved" || k.Delayed != "test:delayed" || k.Timeout != "test:timeout" || k.Failed != "test:failed" {
		t.Fatalf("unexpected keys: %#v", k)
	}

	for _, q := range []string{"waiting", "reserved", "delayed", "timeout", "failed"} {
		if _, err := k.Get(q); err != nil {
			t.Fatalf("unexpected error for %s: %v", q, err)
		}
	}
	if _, err := k.Get("unknown"); err == nil {
		t.Fatal("expected error for unknown queue")
	}
}
