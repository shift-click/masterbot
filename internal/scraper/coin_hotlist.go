package scraper

import (
	"context"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/shift-click/masterbot/internal/scraper/providers"
)

// CoinHotListConfig holds configuration for the DEX coin hot list.
type CoinHotListConfig struct {
	MaxRatePerMin int           // DexScreener rate limit (default: 300)
	BatchSize     int           // Max addresses per batch request (default: 30)
	IdleTimeout   time.Duration // Remove token after idle (default: 10m)
	MinInterval   time.Duration // Minimum poll interval (default: 200ms)
}

// DefaultCoinHotListConfig returns sensible defaults.
func DefaultCoinHotListConfig() CoinHotListConfig {
	return CoinHotListConfig{
		MaxRatePerMin: 300,
		BatchSize:     30,
		IdleTimeout:   10 * time.Minute,
		MinInterval:   200 * time.Millisecond,
	}
}

type coinHotEntry struct {
	quote      *providers.DEXQuote
	lastAccess time.Time
}

// CoinHotList maintains a set of actively-polled DEX tokens.
// Uses DexScreener batch API for efficient polling.
type CoinHotList struct {
	mu      sync.RWMutex
	entries map[string]*coinHotEntry // keyed by lowercase contract address

	dex    *providers.DexScreener
	config CoinHotListConfig
	logger *slog.Logger
}

// NewCoinHotList creates a new CoinHotList.
func NewCoinHotList(dex *providers.DexScreener, cfg CoinHotListConfig, logger *slog.Logger) *CoinHotList {
	if logger == nil {
		logger = slog.Default()
	}
	return &CoinHotList{
		entries: make(map[string]*coinHotEntry),
		dex:     dex,
		config:  cfg,
		logger:  logger.With("component", "coin_hotlist"),
	}
}

// Get returns the cached DEX quote for a contract address, or nil if not in hotlist.
func (h *CoinHotList) Get(contractAddr string) *providers.DEXQuote {
	addr := strings.ToLower(contractAddr)
	h.mu.RLock()
	entry, ok := h.entries[addr]
	h.mu.RUnlock()
	if !ok {
		return nil
	}

	h.mu.Lock()
	entry.lastAccess = time.Now()
	h.mu.Unlock()

	return entry.quote
}

// Register adds a DEX token to the hotlist with initial data.
func (h *CoinHotList) Register(quote *providers.DEXQuote) {
	if quote == nil || quote.ContractAddress == "" {
		return
	}

	addr := strings.ToLower(quote.ContractAddress)
	h.mu.Lock()
	defer h.mu.Unlock()

	h.entries[addr] = &coinHotEntry{
		quote:      quote,
		lastAccess: time.Now(),
	}
	h.logger.Debug("registered DEX token to hotlist", "symbol", quote.Symbol, "address", addr)
}

// addresses returns all currently registered contract addresses.
func (h *CoinHotList) addresses() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	addrs := make([]string, 0, len(h.entries))
	for addr := range h.entries {
		addrs = append(addrs, addr)
	}
	return addrs
}

// Start begins the background batch polling and eviction loops.
// Blocks until ctx is cancelled.
func (h *CoinHotList) Start(ctx context.Context) {
	h.logger.Info("coin hotlist polling started")

	evictTicker := time.NewTicker(30 * time.Second)
	defer evictTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			h.logger.Info("coin hotlist polling stopped")
			return
		default:
		}

		addrs := h.addresses()
		if len(addrs) == 0 {
			// Nothing to poll — wait a bit.
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			case <-evictTicker.C:
				h.evictIdle()
			}
			continue
		}

		// Poll in batches.
		h.pollBatches(ctx, addrs)

		// Evict idle entries.
		select {
		case <-evictTicker.C:
			h.evictIdle()
		default:
		}

		// Calculate adaptive sleep based on token count.
		sleepDuration := h.calcPollInterval(len(addrs))
		select {
		case <-ctx.Done():
			return
		case <-time.After(sleepDuration):
		}
	}
}

// pollBatches fetches all hot tokens in batches of BatchSize.
func (h *CoinHotList) pollBatches(ctx context.Context, addrs []string) {
	batchSize := h.config.BatchSize

	for i := 0; i < len(addrs); i += batchSize {
		select {
		case <-ctx.Done():
			return
		default:
		}

		end := i + batchSize
		if end > len(addrs) {
			end = len(addrs)
		}
		batch := addrs[i:end]

		results, err := h.dex.FetchBatch(ctx, batch)
		if err != nil {
			h.logger.Warn("coin hotlist batch poll failed", "error", err, "batch_size", len(batch))
			continue
		}

		h.mu.Lock()
		for addr, quote := range results {
			if entry, ok := h.entries[addr]; ok {
				// Preserve KRW price from previous data (will be recalculated by CoinCache).
				quote.KRWPrice = entry.quote.KRWPrice
				quote.KRWChangePct24h = entry.quote.KRWChangePct24h
				entry.quote = quote
			}
		}
		h.mu.Unlock()
	}
}

// calcPollInterval returns the adaptive poll interval based on the number of hot tokens.
// Formula: numBatches / maxRatePerMin * 60s, clamped to MinInterval.
func (h *CoinHotList) calcPollInterval(tokenCount int) time.Duration {
	numBatches := int(math.Ceil(float64(tokenCount) / float64(h.config.BatchSize)))
	// Use 80% of rate limit for safety.
	safeRate := float64(h.config.MaxRatePerMin) * 0.8
	intervalSec := float64(numBatches) / safeRate * 60.0
	interval := time.Duration(intervalSec * float64(time.Second))

	if interval < h.config.MinInterval {
		interval = h.config.MinInterval
	}
	return interval
}

// evictIdle removes tokens that haven't been accessed within idle timeout.
func (h *CoinHotList) evictIdle() {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()
	for addr, entry := range h.entries {
		if now.Sub(entry.lastAccess) > h.config.IdleTimeout {
			delete(h.entries, addr)
			h.logger.Debug("evicted idle DEX token from hotlist", "address", addr)
		}
	}
}
