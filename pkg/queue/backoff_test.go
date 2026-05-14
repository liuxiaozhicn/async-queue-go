package queue

import (
	"testing"
	"time"
)

func TestBackoffAdvanceAndReset(t *testing.T) {
	b := newBackoff(time.Second, 5*time.Second)

	if got := b.Current(); got != time.Second {
		t.Fatalf("expected initial backoff=1s, got %s", got)
	}

	b.Advance()
	if got := b.Current(); got != 2*time.Second {
		t.Fatalf("expected backoff=2s after first advance, got %s", got)
	}

	b.Advance()
	if got := b.Current(); got != 4*time.Second {
		t.Fatalf("expected backoff=4s after second advance, got %s", got)
	}

	b.Advance()
	if got := b.Current(); got != 5*time.Second {
		t.Fatalf("expected backoff capped at 5s, got %s", got)
	}

	b.Reset()
	if got := b.Current(); got != time.Second {
		t.Fatalf("expected reset backoff=1s, got %s", got)
	}
}

func TestBackoffNormalizesMaxToInitial(t *testing.T) {
	b := newBackoff(3*time.Second, time.Second)

	if got := b.Current(); got != 3*time.Second {
		t.Fatalf("expected initial backoff=3s, got %s", got)
	}

	b.Advance()
	if got := b.Current(); got != 3*time.Second {
		t.Fatalf("expected capped backoff=3s, got %s", got)
	}
}
