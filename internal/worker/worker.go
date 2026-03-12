package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type Consumer interface {
	Run(context.Context) error
}

type SignalContextProvider func() (context.Context, context.CancelFunc)

type Worker struct {
	consumer Consumer
	signal   SignalContextProvider

	mu      sync.Mutex
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan error
}

func NewWorker(consumer Consumer, signalProvider SignalContextProvider) *Worker {
	if signalProvider == nil {
		signalProvider = func() (context.Context, context.CancelFunc) {
			return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		}
	}
	return &Worker{consumer: consumer, signal: signalProvider}
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
		return <-done
	}

	timer := time.NewTimer(graceTimeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		return fmt.Errorf("worker shutdown timeout exceeded")
	}
}

func (w *Worker) RunWithSignals(ctx context.Context, shutdownTimeout time.Duration) error {
	sigCtx, stop := w.signal()
	defer stop()

	baseCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		select {
		case <-sigCtx.Done():
			cancel()
		case <-baseCtx.Done():
		}
	}()

	if err := w.Start(baseCtx); err != nil {
		return err
	}

	select {
	case err := <-w.done:
		return err
	case <-sigCtx.Done():
		return w.Stop(shutdownTimeout)
	}
}
