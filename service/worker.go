package service

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/bucketprovision"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/synchronization"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/client"
)

// Worker owns the synchronizer goroutine and waits before database teardown.
type Worker struct {
	transport    *http.Client
	synchronizer *synchronization.Service
	cancel       context.CancelFunc
	done         chan struct{}
	result       error
	buckets      *bucketprovision.Service
}

func newWorker(synchronizer *synchronization.Service) *Worker {
	return &Worker{synchronizer: synchronizer}
}

func (w *Worker) Start(ctx context.Context) error {
	if w.done != nil {
		return errors.New("worker already started")
	}
	runtime, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	w.done = make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		wg.Add(1)
		go func() { defer wg.Done(); w.result = w.synchronizer.Run(runtime) }()
		if w.buckets != nil {
			wg.Add(1)
			go func() { defer wg.Done(); _ = w.buckets.Run(runtime) }()
		}
		wg.Wait()
		close(w.done)
	}()
	return nil
}

func (w *Worker) Stop(ctx context.Context) error {
	if w.done == nil {
		if w.transport != nil {
			w.transport.CloseIdleConnections()
		}
		return nil
	}
	w.cancel()
	select {
	case <-w.done:
		if w.transport != nil {
			w.transport.CloseIdleConnections()
		}
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
	case <-ctx.Done():
	}
	// An in-flight operation can take up to 90 seconds and persists its outcome.
	shutdown, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	return w.Stop(shutdown)
}

// NewWorker constructs a synchronizer borrowing database and owning its transport.
// Start receives the running context; Stop cancels and waits before releasing it.
func NewWorker(database *gorm.DB, cfg Config) (*Worker, error) {
	if database == nil {
		return nil, errors.New("worker requires a database")
	}
	c, err := cfg.Normalize()
	if err != nil {
		return nil, err
	}
	if err := c.RequireWorker(); err != nil {
		return nil, err
	}
	transport, err := client.NewHTTP(c.CAFile)
	if err != nil {
		return nil, err
	}
	gitlab := &client.GitLab{
		URL:       c.Gitlab.URL,
		Token:     c.Gitlab.Token,
		TokenFile: c.Gitlab.TokenFile,
		HTTP:      transport,
	}
	worker := newWorker(synchronization.New(database, gitlab))
	worker.transport = transport
	if c.Storage.URL != "" {
		// Never follow management redirects or carry a cookie jar.
		storageHTTP := *transport
		storageHTTP.Jar = nil
		storageHTTP.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		worker.buckets = &bucketprovision.Service{
			DB: database,
			Client: &bucketprovision.Client{
				URL:     c.Storage.URL,
				Token:   c.Storage.ManagementToken,
				HTTP:    &storageHTTP,
				Timeout: c.Storage.Timeout,
			},
		}
	}
	return worker, nil
}
