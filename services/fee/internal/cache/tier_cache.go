package cache

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/AfshinJalili/goex/services/fee/internal/storage"
	"github.com/shopspring/decimal"
)

type TierStore interface {
	GetAllFeeTiers(ctx context.Context) ([]storage.FeeTier, error)
}

type TierCache struct {
	mu         sync.RWMutex
	tiers      map[string]storage.FeeTier
	thresholds []decimal.Decimal
	lastRefresh time.Time
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
