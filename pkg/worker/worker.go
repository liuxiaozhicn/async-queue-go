package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Runner interface {
	Run(context.Context) error
}

type Worker struct {
	runner Runner

	mu      sync.Mutex
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{} // closed when consumer exits; all waiters unblock
	err     error         // consumer result, guarded by mu
}

func NewWorker(runner Runner) *Worker {
	return &Worker{runner: runner}
}

func (w *Worker) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.started {
		return errors.New("[Worker] already started")
	}
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.done = make(chan struct{})
	w.err = nil
	w.started = true
	go func() {
		defer func() {
			if r := recover(); r != nil {
				w.mu.Lock()
				w.err = fmt.Errorf("worker panic: %v", r)
				w.mu.Unlock()
			}
			close(w.done)
		}()
		err := w.runner.Run(w.ctx)
		w.mu.Lock()
		w.err = err
		w.mu.Unlock()
	}()
	return nil
}

func (w *Worker) Wait() error {
	w.mu.Lock()
	done := w.done
	started := w.started
	w.mu.Unlock()
	if !started || done == nil {
		return errors.New("[Worker] not started")
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
		return errors.New("[Worker] not started")
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
		return fmt.Errorf("[Worker] shutdown timeout exceeded")
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
