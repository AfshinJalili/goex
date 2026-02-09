package cache

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/AfshinJalili/goex/services/fee/internal/storage"
	"github.com/google/uuid"
)

type fakeStore struct {
	tiers []storage.FeeTier
}

func (f *fakeStore) GetAllFeeTiers(ctx context.Context) ([]storage.FeeTier, error) {
	return f.tiers, nil
}

type errorStore struct{}

func (e *errorStore) GetAllFeeTiers(ctx context.Context) ([]storage.FeeTier, error) {
	return nil, errors.New("boom")
}

type fakeMetrics struct {
	mu       sync.Mutex
	refresh  int
	errors   int
	lastSize int
}

func (m *fakeMetrics) ObserveRefresh(_ time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refresh++
}

func (m *fakeMetrics) SetCacheSize(size int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastSize = size
}

func (m *fakeMetrics) IncRefreshError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors++
}

func (m *fakeMetrics) Snapshot() (int, int, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.refresh, m.errors, m.lastSize
}

func TestTierCacheLookup(t *testing.T) {
	cache := NewTierCache()
	tiers := []storage.FeeTier{
		{ID: uuid.New(), Name: "vip", MakerFeeBps: 5, TakerFeeBps: 10, MinVolume: "100000"},
		{ID: uuid.New(), Name: "pro", MakerFeeBps: 8, TakerFeeBps: 15, MinVolume: "10000"},
		{ID: uuid.New(), Name: "default", MakerFeeBps: 10, TakerFeeBps: 20, MinVolume: "0"},
	}

	store := &fakeStore{tiers: tiers}
	if err := cache.Load(context.Background(), store); err != nil {
		t.Fatalf("load cache: %v", err)
	}

	cases := []struct {
		volume string
		want   string
	}{
		{"0", "default"},
		{"500", "default"},
		{"10000", "pro"},
		{"99999", "pro"},
		{"100000", "vip"},
	}

	for _, tc := range cases {
		t.Run(tc.volume, func(t *testing.T) {
			tier, ok := cache.GetTierByVolume(tc.volume)
			if !ok {
				t.Fatalf("expected tier for volume %s", tc.volume)
			}
			if tier.Name != tc.want {
				t.Fatalf("expected %s, got %s", tc.want, tier.Name)
			}
		})
	}
}

func TestTierCacheConcurrentAccess(t *testing.T) {
	cache := NewTierCache()
	store := &fakeStore{tiers: []storage.FeeTier{
		{ID: uuid.New(), Name: "default", MakerFeeBps: 10, TakerFeeBps: 20, MinVolume: "0"},
	}}
	if err := cache.Load(context.Background(), store); err != nil {
		t.Fatalf("load cache: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _ = cache.GetTierByVolume("0")
			}
		}()
	}
	wg.Wait()
}

func TestTierCacheRefresh(t *testing.T) {
	cache := NewTierCache()
	store := &fakeStore{tiers: []storage.FeeTier{
		{ID: uuid.New(), Name: "default", MakerFeeBps: 10, TakerFeeBps: 20, MinVolume: "0"},
	}}
	if err := cache.Load(context.Background(), store); err != nil {
		t.Fatalf("load cache: %v", err)
	}

	store.tiers = append(store.tiers, storage.FeeTier{ID: uuid.New(), Name: "vip", MakerFeeBps: 5, TakerFeeBps: 10, MinVolume: "1000"})
	if err := cache.Refresh(context.Background(), store); err != nil {
		t.Fatalf("refresh cache: %v", err)
	}

	if tier, ok := cache.GetTierByVolume("1000"); !ok || tier.Name != "vip" {
		t.Fatalf("expected vip after refresh")
	}

	if cache.LastRefresh().IsZero() {
		t.Fatalf("expected last refresh to be set")
	}

	if cache.Size() != 2 {
		t.Fatalf("expected cache size 2, got %d", cache.Size())
	}
}

func TestTierCacheInvalidVolume(t *testing.T) {
	cache := NewTierCache()
	store := &fakeStore{tiers: []storage.FeeTier{
		{ID: uuid.New(), Name: "default", MakerFeeBps: 10, TakerFeeBps: 20, MinVolume: "0"},
	}}
	if err := cache.Load(context.Background(), store); err != nil {
		t.Fatalf("load cache: %v", err)
	}

	if _, ok := cache.GetTierByVolume("invalid"); ok {
		t.Fatalf("expected invalid volume to miss")
	}

	if cache.LastRefresh().Before(time.Now().Add(-1 * time.Minute)) {
		t.Fatalf("unexpected last refresh time")
	}
}

func TestTierCacheAutoRefresh(t *testing.T) {
	cache := NewTierCache()
	store := &fakeStore{tiers: []storage.FeeTier{
		{ID: uuid.New(), Name: "default", MakerFeeBps: 10, TakerFeeBps: 20, MinVolume: "0"},
	}}
	if err := cache.Load(context.Background(), store); err != nil {
		t.Fatalf("load cache: %v", err)
	}

	metrics := &fakeMetrics{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cache.StartAutoRefresh(ctx, store, 10*time.Millisecond, metrics, slog.Default())
	store.tiers = append(store.tiers, storage.FeeTier{ID: uuid.New(), Name: "vip", MakerFeeBps: 5, TakerFeeBps: 10, MinVolume: "1000"})

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if cache.Size() == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	refreshes, _, size := metrics.Snapshot()
	if refreshes == 0 {
		t.Fatalf("expected refreshes to occur")
	}
	if size != cache.Size() {
		t.Fatalf("expected cache size metric %d, got %d", cache.Size(), size)
	}
	if cache.Size() != 2 {
		t.Fatalf("expected cache size 2, got %d", cache.Size())
	}
}

func TestTierCacheAutoRefreshErrors(t *testing.T) {
	cache := NewTierCache()
	if err := cache.Load(context.Background(), &fakeStore{tiers: []storage.FeeTier{
		{ID: uuid.New(), Name: "default", MakerFeeBps: 10, TakerFeeBps: 20, MinVolume: "0"},
	}}); err != nil {
		t.Fatalf("load cache: %v", err)
	}

	metrics := &fakeMetrics{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cache.StartAutoRefresh(ctx, &errorStore{}, 10*time.Millisecond, metrics, slog.Default())

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		_, errorsCount, _ := metrics.Snapshot()
		if errorsCount > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected refresh errors to be recorded")
}
