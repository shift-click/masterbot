package scraper

import (
	"context"
	"time"

	otter "github.com/maypok86/otter/v2"
)

const defaultCacheCapacity = 10_000

type MemoryCache struct {
	cache    *otter.Cache[string, cacheItem]
	staleTTL time.Duration
}

type cacheItem struct {
	result     Result
	expiresAt  time.Time
	staleUntil time.Time
}

func NewMemoryCache(staleTTL time.Duration) *MemoryCache {
	if staleTTL <= 0 {
		staleTTL = time.Hour
	}
	cache := otter.Must(&otter.Options[string, cacheItem]{
		MaximumSize: defaultCacheCapacity,
		ExpiryCalculator: otter.ExpiryCreatingFunc(func(entry otter.Entry[string, cacheItem]) time.Duration {
			ttl := time.Until(entry.Value.staleUntil)
			if ttl <= 0 {
				ttl = time.Millisecond
			}
			return ttl
		}),
	})
	return &MemoryCache{cache: cache, staleTTL: staleTTL}
}

func (c *MemoryCache) Get(_ context.Context, key string) (Result, bool, error) {
	item, ok := c.cache.GetIfPresent(key)
	if !ok || time.Now().After(item.expiresAt) {
		return Result{}, false, nil
	}

	result := cloneResult(item.result)
	result.IsCached = true
	return result, true, nil
}

func (c *MemoryCache) Set(_ context.Context, key string, result Result, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = time.Minute
	}

	now := time.Now()
	item := cacheItem{
		result:     cloneResult(result),
		expiresAt:  now.Add(ttl),
		staleUntil: now.Add(ttl + c.staleTTL),
	}
	c.cache.Compute(key, func(_ cacheItem, _ bool) (cacheItem, otter.ComputeOp) {
		return item, otter.WriteOp
	})
	return nil
}

func (c *MemoryCache) GetStale(_ context.Context, key string) (Result, bool, error) {
	item, ok := c.cache.GetIfPresent(key)
	if !ok || time.Now().After(item.staleUntil) {
		return Result{}, false, nil
	}

	result := cloneResult(item.result)
	result.IsCached = true
	return result, true, nil
}

func cloneResult(result Result) Result {
	cloned := result
	if result.Data != nil {
		cloned.Data = append([]byte(nil), result.Data...)
	}
	return cloned
}
