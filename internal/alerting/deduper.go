package alerting

import (
	"sync"
	"time"
)

type Deduper struct {
	window time.Duration
	mu     sync.Mutex
	lastAt map[string]time.Time
}

func NewDeduper(window time.Duration) *Deduper {
	if window <= 0 {
		window = 15 * time.Minute
	}
	return &Deduper{
		window: window,
		lastAt: make(map[string]time.Time),
	}
}

func (d *Deduper) Allow(key string, now time.Time) bool {
	if d == nil || key == "" {
		return true
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	last, ok := d.lastAt[key]
	if ok && now.Sub(last) < d.window {
		return false
	}
	d.lastAt[key] = now
	return true
}
