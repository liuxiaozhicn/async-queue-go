package worker

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeConsumer struct {
	run func(context.Context) error
}

func (f *fakeConsumer) Run(ctx context.Context) error {
	if f.run != nil {
		return f.run(ctx)
	}
	return nil
}

func TestWorkerStartAndWait(t *testing.T) {
	w := NewWorker(&fakeConsumer{})
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := w.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerStopWithTimeout(t *testing.T) {
	w := NewWorker(&fakeConsumer{run: func(context.Context) error { select {} }})
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := w.Stop(20 * time.Millisecond)
	if err == nil || err.Error() != "worker shutdown timeout exceeded" {
		t.Fatalf("expected shutdown timeout error, got %v", err)
	}
}

func TestWorkerStopReturnsRunError(t *testing.T) {
	w := NewWorker(&fakeConsumer{run: func(ctx context.Context) error {
		<-ctx.Done()
		return errors.New("boom")
	}})
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := w.Stop(time.Second); err == nil || err.Error() != "boom" {
		t.Fatalf("expected boom, got %v", err)
	}
}
