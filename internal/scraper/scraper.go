package scraper

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"
)

type Scraper interface {
	Fetch(context.Context, string) (Result, error)
	Name() string
}

type Result struct {
	Data      json.RawMessage `json:"data"`
	Source    string          `json:"source"`
	FetchedAt time.Time       `json:"fetched_at"`
	IsCached  bool            `json:"is_cached"`
}

type Cache interface {
	Get(context.Context, string) (Result, bool, error)
	Set(context.Context, string, Result, time.Duration) error
	GetStale(context.Context, string) (Result, bool, error)
}

type FallbackChain struct {
	providers []Scraper
	cache     Cache
	ttl       time.Duration
	logger    *slog.Logger
}

func NewFallbackChain(providers []Scraper, cache Cache, ttl time.Duration, logger *slog.Logger) *FallbackChain {
	if logger == nil {
		logger = slog.Default()
	}

	return &FallbackChain{
		providers: append([]Scraper(nil), providers...),
		cache:     cache,
		ttl:       ttl,
		logger:    logger.With("component", "scraper"),
	}
}

func (f *FallbackChain) Fetch(ctx context.Context, key, query string) (Result, error) {
	if f.cache != nil {
		if cached, ok, err := f.cache.Get(ctx, key); err == nil && ok {
			cached.IsCached = true
			return cached, nil
		}
	}

	var errs []error
	for _, provider := range f.providers {
		result, err := provider.Fetch(ctx, query)
		if err != nil {
			f.logger.Warn("provider fetch failed", "provider", provider.Name(), "error", err)
			errs = append(errs, err)
			continue
		}

		result.Source = provider.Name()
		result.FetchedAt = time.Now()
		if f.cache != nil {
			_ = f.cache.Set(ctx, key, result, f.ttl)
		}
		return result, nil
	}

	if f.cache != nil {
		if stale, ok, err := f.cache.GetStale(ctx, key); err == nil && ok {
			stale.IsCached = true
			return stale, nil
		}
	}

	if len(errs) == 0 {
		return Result{}, errors.New("no scraper providers configured")
	}

	return Result{}, errors.Join(errs...)
}
