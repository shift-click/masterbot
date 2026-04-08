package command

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

var (
	ErrSummaryExecutorBusy        = errors.New("summary executor busy")
	ErrSummaryExecutorUnavailable = errors.New("summary executor unavailable")
)

type SummaryExecutor struct {
	logger     *slog.Logger
	maxWorkers int
	timeout    time.Duration

	mu      sync.RWMutex
	rootCtx context.Context
	wg      sync.WaitGroup
	sem     chan struct{}
}

func NewSummaryExecutor(logger *slog.Logger, maxWorkers int, timeout time.Duration) *SummaryExecutor {
	if logger == nil {
		logger = slog.Default()
	}
	if maxWorkers <= 0 {
		maxWorkers = 1
	}
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	return &SummaryExecutor{
		logger:     logger.With("component", "summary_executor"),
		maxWorkers: maxWorkers,
		timeout:    timeout,
		rootCtx:    context.Background(),
		sem:        make(chan struct{}, maxWorkers),
	}
}

func (e *SummaryExecutor) Run(ctx context.Context) error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	e.rootCtx = ctx
	e.mu.Unlock()

	<-ctx.Done()
	e.wg.Wait()
	return nil
}

func (e *SummaryExecutor) Submit(ctx context.Context, jobName string, job func(context.Context)) error {
	if e == nil || job == nil {
		return ErrSummaryExecutorUnavailable
	}
	rootCtx := e.currentRootContext()
	if rootCtx == nil || rootCtx.Err() != nil {
		return ErrSummaryExecutorUnavailable
	}
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}

	select {
	case e.sem <- struct{}{}:
	default:
		return ErrSummaryExecutorBusy
	}

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer func() {
			<-e.sem
			if recovered := recover(); recovered != nil {
				e.logger.Error("summary job panic", "job", jobName, "recover", recovered)
			}
		}()

		jobCtx, cancel := context.WithTimeout(rootCtx, e.timeout)
		defer cancel()
		if ctx != nil {
			stop := context.AfterFunc(ctx, cancel)
			defer stop()
		}
		job(jobCtx)
	}()
	return nil
}

func (e *SummaryExecutor) currentRootContext() context.Context {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.rootCtx
}
