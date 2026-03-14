package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Consumer interface {
	Run(context.Context) error
}

type Worker struct {
	consumer Consumer

	mu      sync.Mutex
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{} // closed when consumer exits; all waiters unblock
	err     error         // consumer result, guarded by mu
}

func NewWorker(consumer Consumer) *Worker {
	return &Worker{consumer: consumer}
}

func (w *Worker) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.started {
		return errors.New("worker already started")
	}
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.done = make(chan struct{})
	w.err = nil
	w.started = true
	go func() {
		err := w.consumer.Run(w.ctx)
		w.mu.Lock()
		w.err = err
		w.mu.Unlock()
		close(w.done)
	}()
	return nil
}

func (w *Worker) Wait() error {
	w.mu.Lock()
	done := w.done
	started := w.started
	w.mu.Unlock()
	if !started || done == nil {
		return errors.New("worker not started")
	}
	<-done
	w.mu.Lock()
	defer w.mu.Unlock()
	err := w.err
	w.resetLocked()
	return err
}

func (w *Worker) Stop(graceTimeout time.Duration) error {
	w.mu.Lock()
	done := w.done
	cancel := w.cancel
	started := w.started
	w.mu.Unlock()
	if !started || done == nil || cancel == nil {
		return errors.New("worker not started")
	}

	cancel()

	if graceTimeout <= 0 {
		<-done
		w.mu.Lock()
		defer w.mu.Unlock()
		err := w.err
		w.resetLocked()
		return err
	}

	timer := time.NewTimer(graceTimeout)
	defer timer.Stop()
	select {
	case <-done:
		w.mu.Lock()
		defer w.mu.Unlock()
		err := w.err
		w.resetLocked()
		return err
	case <-timer.C:
		w.mu.Lock()
		defer w.mu.Unlock()
		w.resetLocked()
		return fmt.Errorf("worker shutdown timeout exceeded")
	}
}

// resetLocked clears state so the Worker can be re-started. Caller must hold mu.
func (w *Worker) resetLocked() {
	w.started = false
	w.ctx = nil
	w.cancel = nil
	w.done = nil
	w.err = nil
}
