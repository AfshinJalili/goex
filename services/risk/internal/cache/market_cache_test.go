package cache

import (
	"context"
	"testing"

	"github.com/AfshinJalili/goex/services/risk/internal/storage"
	"github.com/google/uuid"
)

type fakeMarketStore struct {
	markets []storage.Market
	err     error
}

func (f *fakeMarketStore) ListActiveMarkets(ctx context.Context) ([]storage.Market, error) {
	return f.markets, f.err
}

func TestMarketCacheLoadAndGet(t *testing.T) {
	store := &fakeMarketStore{
		markets: []storage.Market{
			{ID: uuid.New(), Symbol: "BTC-USD", BaseAsset: "BTC", QuoteAsset: "USD", Status: "active"},
			{ID: uuid.New(), Symbol: "eth-usd", BaseAsset: "ETH", QuoteAsset: "USD", Status: "active"},
		},
	}

	cache := NewMarketCache()
	if err := cache.Load(context.Background(), store); err != nil {
		t.Fatalf("load: %v", err)
	}

	market, ok := cache.GetMarket("btc-usd")
	if !ok {
		t.Fatalf("expected market hit")
	}
	if market.Symbol != "BTC-USD" {
		t.Fatalf("expected normalized symbol, got %s", market.Symbol)
	}

	if _, ok := cache.GetMarket("missing"); ok {
		t.Fatalf("expected cache miss")
	}
}

func TestMarketCacheRefresh(t *testing.T) {
	cache := NewMarketCache()
	store := &fakeMarketStore{markets: []storage.Market{{ID: uuid.New(), Symbol: "BTC-USD"}}}
	if err := cache.Load(context.Background(), store); err != nil {
		t.Fatalf("load: %v", err)
	}
	if cache.Size() != 1 {
		t.Fatalf("expected size 1")
	}

	store.markets = []storage.Market{{ID: uuid.New(), Symbol: "ETH-USD"}}
	if err := cache.Refresh(context.Background(), store); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if cache.Size() != 1 {
		t.Fatalf("expected size 1 after refresh")
	}
	if _, ok := cache.GetMarket("ETH-USD"); !ok {
		t.Fatalf("expected refreshed market")
	}
}
