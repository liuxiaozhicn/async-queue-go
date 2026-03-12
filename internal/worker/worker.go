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

type SignalContextProvider func() (context.Context, context.CancelFunc)

type Worker struct {
	consumer Consumer

	mu      sync.Mutex
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan error
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
	w.done = make(chan error, 1)
	w.started = true
	go func() {
		w.done <- w.consumer.Run(w.ctx)
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
	return <-done
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
		err := <-done
		w.reset()
		return err
	}

	timer := time.NewTimer(graceTimeout)
	defer timer.Stop()
	select {
	case err := <-done:
		w.reset()
		return err
	case <-timer.C:
		w.reset()
		return fmt.Errorf("worker shutdown timeout exceeded")
	}
}

func (w *Worker) reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.started = false
	w.ctx = nil
	w.cancel = nil
	w.done = nil
}
