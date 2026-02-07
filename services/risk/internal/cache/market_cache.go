package cache

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/AfshinJalili/goex/services/risk/internal/storage"
)

type MarketStore interface {
	ListActiveMarkets(ctx context.Context) ([]storage.Market, error)
}

type MarketCache struct {
	mu          sync.RWMutex
	markets     map[string]storage.Market
	lastRefresh time.Time
}

func NewMarketCache() *MarketCache {
	return &MarketCache{
		markets: make(map[string]storage.Market),
	}
}

func (c *MarketCache) Load(ctx context.Context, store MarketStore) error {
	markets, err := store.ListActiveMarkets(ctx)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.markets = make(map[string]storage.Market, len(markets))
	for _, market := range markets {
		symbol := strings.ToUpper(strings.TrimSpace(market.Symbol))
		if symbol == "" {
			continue
		}
		market.Symbol = symbol
		c.markets[symbol] = market
	}
	c.lastRefresh = time.Now().UTC()
	return nil
}

func (c *MarketCache) Refresh(ctx context.Context, store MarketStore) error {
	return c.Load(ctx, store)
}

func (c *MarketCache) GetMarket(symbol string) (*storage.Market, bool) {
	key := strings.ToUpper(strings.TrimSpace(symbol))
	if key == "" {
		return nil, false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	market, ok := c.markets[key]
	if !ok {
		return nil, false
	}
	copy := market
	return &copy, true
}

func (c *MarketCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.markets)
}

func (c *MarketCache) LastRefresh() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastRefresh
}
