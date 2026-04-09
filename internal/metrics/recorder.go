package metrics

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Recorder interface {
	Record(context.Context, Event)
}

type NoopRecorder struct{}

func (NoopRecorder) Record(context.Context, Event) {
	// Intentionally no-op: used when metrics collection is disabled.
}

type AsyncRecorder struct {
	store          *SQLiteStore
	logger         *slog.Logger
	secret         []byte
	roomAliases    map[string]string
	flushInterval  time.Duration
	rollupInterval time.Duration
	retention      RetentionPolicy
	queue          chan Event
	wg             sync.WaitGroup
	droppedEvents  atomic.Int64
}

const recorderOperationTimeout = 10 * time.Second

func NewAsyncRecorder(
	store *SQLiteStore,
	secret string,
	roomAliases map[string]string,
	flushInterval, rollupInterval time.Duration,
	retention RetentionPolicy,
	logger *slog.Logger,
) *AsyncRecorder {
	if logger == nil {
		logger = slog.Default()
	}
	clonedAliases := make(map[string]string, len(roomAliases))
	for key, value := range roomAliases {
		clonedAliases[key] = strings.TrimSpace(value)
	}
	return &AsyncRecorder{
		store:          store,
		logger:         logger.With("component", "metrics_recorder"),
		secret:         []byte(secret),
		roomAliases:    clonedAliases,
		flushInterval:  flushInterval,
		rollupInterval: rollupInterval,
		retention:      retention,
		queue:          make(chan Event, 2048),
	}
}

func (r *AsyncRecorder) Start(ctx context.Context) {
	r.wg.Add(1)
	go r.run(ctx)
}

func (r *AsyncRecorder) Wait() {
	r.wg.Wait()
}

func (r *AsyncRecorder) Record(_ context.Context, event Event) {
	if r == nil {
		return
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now()
	}
	select {
	case r.queue <- event:
	default:
		r.droppedEvents.Add(1)
		r.logger.Warn("metrics queue full; dropping event",
			"event", event.EventName,
			"request_id", event.RequestID,
			"total_dropped", r.droppedEvents.Load(),
		)
	}
}

func (r *AsyncRecorder) run(ctx context.Context) {
	defer r.wg.Done()

	flushTicker := time.NewTicker(r.flushInterval)
	defer flushTicker.Stop()
	rollupTicker := time.NewTicker(r.rollupInterval)
	defer rollupTicker.Stop()

	buffer := make([]StoredEvent, 0, 256)
	flush := func() {
		if len(buffer) == 0 {
			return
		}
		opCtx, cancel := context.WithTimeout(context.Background(), recorderOperationTimeout)
		defer cancel()
		if err := r.store.InsertEvents(opCtx, buffer); err != nil {
			r.logger.Error("failed to insert metric events", "error", err, "count", len(buffer))
		}
		buffer = buffer[:0]
	}
	maintain := func(now time.Time) {
		opCtx, cancel := context.WithTimeout(context.Background(), recorderOperationTimeout)
		defer cancel()
		if err := r.store.RebuildRollups(opCtx); err != nil {
			r.logger.Error("failed to rebuild metric rollups", "error", err)
		}
		if err := r.store.Cleanup(opCtx, r.retention, now); err != nil {
			r.logger.Error("failed to cleanup metric data", "error", err)
		}
	}

	for {
		select {
		case <-ctx.Done():
			drain := true
			for drain {
				select {
				case event := <-r.queue:
					buffer = append(buffer, r.toStoredEvent(event))
				default:
					drain = false
				}
			}
			flush()
			maintain(time.Now())
			return
		case event := <-r.queue:
			buffer = append(buffer, r.toStoredEvent(event))
			if len(buffer) >= 256 {
				flush()
			}
		case <-flushTicker.C:
			flush()
		case now := <-rollupTicker.C:
			flush()
			maintain(now)
		}
	}
}

func (r *AsyncRecorder) toStoredEvent(event Event) StoredEvent {
	return buildStoredEvent(event, r.secret, r.roomAliases)
}
