package app

import (
	"context"
	"errors"
	"sync"
)

type component struct {
	name string
	run  func(context.Context) error
}

type Lifecycle struct {
	mu         sync.Mutex
	components []component
}

type lifecycleRun struct {
	errCh  chan error
	doneCh chan struct{}
}

func (l *Lifecycle) Add(name string, run func(context.Context) error) {
	if run == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.components = append(l.components, component{name: name, run: run})
}

func (l *Lifecycle) Start(ctx context.Context) *lifecycleRun {
	l.mu.Lock()
	components := append([]component(nil), l.components...)
	l.mu.Unlock()

	run := &lifecycleRun{
		errCh:  make(chan error, len(components)),
		doneCh: make(chan struct{}),
	}
	if len(components) == 0 {
		close(run.doneCh)
		close(run.errCh)
		return run
	}

	var wg sync.WaitGroup
	for _, component := range components {
		c := component
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				run.errCh <- err
			}
		}()
	}
	go func() {
		wg.Wait()
		close(run.doneCh)
		close(run.errCh)
	}()
	return run
}
