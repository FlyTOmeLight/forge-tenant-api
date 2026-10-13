package service

import (
	"context"
	"errors"
	"time"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/synchronization"
)

// Worker owns the synchronizer goroutine and waits before database teardown.
type Worker struct {
	synchronizer *synchronization.Service
	cancel       context.CancelFunc
	done         chan struct{}
	result       error
}

func NewWorker(synchronizer *synchronization.Service) *Worker {
	return &Worker{synchronizer: synchronizer}
}

func (w *Worker) Start(ctx context.Context) error {
	if w.done != nil {
		return errors.New("worker already started")
	}
	runtime, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	w.done = make(chan struct{})
	go func() { w.result = w.synchronizer.Run(runtime); close(w.done) }()
	return nil
}

func (w *Worker) Stop(ctx context.Context) error {
	if w.done == nil {
		return nil
	}
	w.cancel()
	select {
	case <-w.done:
		return w.result
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Worker) Run(ctx context.Context) error {
	if err := w.Start(ctx); err != nil {
		return err
	}
	select {
	case <-w.done:
		return w.result
	case <-ctx.Done():
	}
	// An in-flight operation can take up to 90 seconds and persists its outcome.
	shutdown, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	return w.Stop(shutdown)
}
