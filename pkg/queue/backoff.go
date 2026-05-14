package queue

import "time"

type backoff struct {
	initial time.Duration
	max     time.Duration
	current time.Duration
}

func newBackoff(initial time.Duration, max time.Duration) *backoff {
	if initial <= 0 {
		initial = time.Second
	}
	if max <= 0 || max < initial {
		max = initial
	}
	return &backoff{
		initial: initial,
		max:     max,
		current: initial,
	}
}

func (b *backoff) Current() time.Duration {
	if b == nil {
		return 0
	}
	return b.current
}

func (b *backoff) Advance() {
	if b == nil {
		return
	}
	if b.current <= 0 {
		b.current = b.max
		return
	}
	next := b.current * 2
	if next > b.max {
		next = b.max
	}
	b.current = next
}

func (b *backoff) Reset() {
	if b == nil {
		return
	}
	b.current = b.initial
}
