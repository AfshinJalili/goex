package storage

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/AfshinJalili/goex/services/testutil"
	"github.com/google/uuid"
)

func TestFeeTierQueries(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION") == "" {
		t.Skip("set RUN_DB_INTEGRATION=1 to run")
	}

	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Skipf("db connection failed: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()
	store := New(pool)
	suffix := uuid.New().String()[:8]

	tiers := []struct {
		name      string
		makerBps  int
		takerBps  int
		minVolume string
	}{
		{fmt.Sprintf("test_fee_basic_%s", suffix), 10, 20, "0"},
		{fmt.Sprintf("test_fee_pro_%s", suffix), 5, 10, "1000"},
	}

	for _, tier := range tiers {
		_, err := pool.Exec(ctx, `
			INSERT INTO fee_tiers (name, maker_fee_bps, taker_fee_bps, min_volume)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (name) DO NOTHING
		`, tier.name, tier.makerBps, tier.takerBps, tier.minVolume)
		if err != nil {
			t.Fatalf("insert fee tier: %v", err)
		}
	}

	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM fee_tiers WHERE name LIKE 'test_fee_%'")
	}()

	ctxTimeout, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	all, err := store.GetAllFeeTiers(ctxTimeout)
	if err != nil {
		t.Fatalf("GetAllFeeTiers: %v", err)
	}
	if len(all) < len(tiers) {
		t.Fatalf("expected at least %d tiers, got %d", len(tiers), len(all))
	}

	byVol, err := store.GetFeeTierByVolume(ctxTimeout, "500")
	if err != nil {
		t.Fatalf("GetFeeTierByVolume: %v", err)
	}
	if byVol == nil || byVol.Name != tiers[0].name {
		t.Fatalf("expected %s for volume 500", tiers[0].name)
	}

	byVol, err = store.GetFeeTierByVolume(ctxTimeout, "1000")
	if err != nil {
		t.Fatalf("GetFeeTierByVolume: %v", err)
	}
	if byVol == nil || byVol.Name != tiers[1].name {
		t.Fatalf("expected %s for volume 1000", tiers[1].name)
	}

	defaultTier, err := store.GetDefaultFeeTier(ctxTimeout)
	if err != nil {
		t.Fatalf("GetDefaultFeeTier: %v", err)
	}
	if defaultTier == nil || defaultTier.Name == "" {
		t.Fatalf("expected default tier")
	}
}
