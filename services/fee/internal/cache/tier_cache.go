package cache

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AfshinJalili/goex/services/fee/internal/storage"
	"github.com/shopspring/decimal"
)

type TierStore interface {
	GetAllFeeTiers(ctx context.Context) ([]storage.FeeTier, error)
}

type TierCache struct {
	mu          sync.RWMutex
	tiers       map[string]storage.FeeTier
	thresholds  []decimal.Decimal
	lastRefresh time.Time
}

type RefreshMetrics interface {
	ObserveRefresh(duration time.Duration)
	SetCacheSize(size int)
	IncRefreshError()
}

func NewTierCache() *TierCache {
	return &TierCache{
		tiers: make(map[string]storage.FeeTier),
	}
}

func (c *TierCache) Load(ctx context.Context, store TierStore) error {
	tiers, err := store.GetAllFeeTiers(ctx)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.tiers = make(map[string]storage.FeeTier, len(tiers))
	c.thresholds = c.thresholds[:0]
	for _, tier := range tiers {
		vol, err := decimal.NewFromString(tier.MinVolume)
		if err != nil {
			continue
		}
		normalized := vol.String()
		tier.MinVolume = normalized
		c.tiers[normalized] = tier
		c.thresholds = append(c.thresholds, vol)
	}
	if len(c.thresholds) > 1 {
		sort.Slice(c.thresholds, func(i, j int) bool {
			return c.thresholds[i].GreaterThan(c.thresholds[j])
		})
	}

	c.lastRefresh = time.Now()
	return nil
}

func (c *TierCache) Refresh(ctx context.Context, store TierStore) error {
	return c.Load(ctx, store)
}

func (c *TierCache) SetTierByVolume(volume string, tier storage.FeeTier) {
	vol, err := decimal.NewFromString(strings.TrimSpace(volume))
	if err != nil {
		return
	}
	normalized := vol.String()
	tier.MinVolume = normalized

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.tiers == nil {
		c.tiers = make(map[string]storage.FeeTier)
	}
	c.tiers[normalized] = tier

	exists := false
	for _, existing := range c.thresholds {
		if existing.Equal(vol) {
			exists = true
			break
		}
	}
	if !exists {
		c.thresholds = append(c.thresholds, vol)
		sort.Slice(c.thresholds, func(i, j int) bool {
			return c.thresholds[i].GreaterThan(c.thresholds[j])
		})
	}
}

func (c *TierCache) GetTierByVolume(volume string) (*storage.FeeTier, bool) {
	vol, err := decimal.NewFromString(volume)
	if err != nil {
		return nil, false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.thresholds) == 0 {
		return nil, false
	}

	idx := sort.Search(len(c.thresholds), func(i int) bool {
		return c.thresholds[i].LessThanOrEqual(vol)
	})
	if idx >= len(c.thresholds) {
		return nil, false
	}

	key := c.thresholds[idx].String()
	tier, ok := c.tiers[key]
	if !ok {
		return nil, false
	}

	copy := tier
	return &copy, true
}

func (c *TierCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.thresholds)
}

func (c *TierCache) LastRefresh() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastRefresh
}

func (c *TierCache) StartAutoRefresh(ctx context.Context, store TierStore, interval time.Duration, metrics RefreshMetrics, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		logger.Warn("fee cache refresh disabled")
		return
	}

	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				refreshCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				start := time.Now()
				err := c.Refresh(refreshCtx, store)
				cancel()
				if err != nil {
					logger.Error("fee cache refresh failed", "error", err)
					if metrics != nil {
						metrics.IncRefreshError()
					}
					continue
				}
				if metrics != nil {
					metrics.ObserveRefresh(time.Since(start))
					metrics.SetCacheSize(c.Size())
				}
				logger.Info("fee tier cache refreshed", "tiers", c.Size())
			}
		}
	}()
}
