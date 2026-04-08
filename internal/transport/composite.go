package transport

import (
	"context"
	"fmt"
	"sync"

	"github.com/shift-click/masterbot/internal/metrics"
)

// CompositeAdapter wraps multiple RuntimeAdapters and routes replies by AdapterID.
type CompositeAdapter struct {
	adapters map[string]RuntimeAdapter
	order    []string // deterministic iteration order
}

// NewCompositeAdapter creates a composite from the given id→adapter pairs.
// It returns an error if any ID is duplicated or the map is empty.
func NewCompositeAdapter(adapters map[string]RuntimeAdapter) (*CompositeAdapter, error) {
	if len(adapters) == 0 {
		return nil, fmt.Errorf("composite adapter requires at least one sub-adapter")
	}
	order := make([]string, 0, len(adapters))
	for id := range adapters {
		order = append(order, id)
	}
	return &CompositeAdapter{
		adapters: adapters,
		order:    order,
	}, nil
}

// Start launches all sub-adapters concurrently. Each adapter's inbound messages
// are tagged with the adapter's ID before forwarding to onMessage.
// Start blocks until ctx is cancelled or any sub-adapter returns an error.
func (c *CompositeAdapter) Start(ctx context.Context, onMessage func(context.Context, Message) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, len(c.adapters))
	var wg sync.WaitGroup

	for _, id := range c.order {
		adapter := c.adapters[id]
		adapterID := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			wrapped := func(msgCtx context.Context, msg Message) error {
				msg.Origin = Origin{AdapterID: adapterID}
				return onMessage(msgCtx, msg)
			}
			if err := adapter.Start(ctx, wrapped); err != nil && ctx.Err() == nil {
				errCh <- fmt.Errorf("adapter %q: %w", adapterID, err)
				cancel()
			}
		}()
	}

	// Wait for first error or context cancellation.
	select {
	case err := <-errCh:
		cancel()
		wg.Wait()
		return err
	case <-ctx.Done():
		wg.Wait()
		return ctx.Err()
	}
}

// Reply routes the request to the sub-adapter identified by req.AdapterID.
func (c *CompositeAdapter) Reply(ctx context.Context, req ReplyRequest) error {
	adapter, ok := c.adapters[req.AdapterID]
	if !ok {
		return fmt.Errorf("unknown adapter ID %q", req.AdapterID)
	}
	return adapter.Reply(ctx, req)
}

// Close closes all sub-adapters, collecting the first error.
func (c *CompositeAdapter) Close() error {
	var firstErr error
	for _, id := range c.order {
		if err := c.adapters[id].Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (c *CompositeAdapter) SetMetricsRecorder(recorder metrics.Recorder) {
	if c == nil {
		return
	}
	for _, id := range c.order {
		if configured, ok := c.adapters[id].(interface{ SetMetricsRecorder(metrics.Recorder) }); ok {
			configured.SetMetricsRecorder(recorder)
		}
	}
}
