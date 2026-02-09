package storage

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/AfshinJalili/goex/services/testutil"
	"github.com/google/uuid"
)

func TestLoadOpenOrders(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION") == "" {
		t.Skip("set RUN_DB_INTEGRATION=1 to run")
	}

	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Skipf("db connection failed: %v", err)
	}
	defer pool.Close()
	ctx := context.Background()
	defer testutil.CleanupTestData(ctx, pool)

	orderID := uuid.New()
	partialID := uuid.New()
	accountID := uuid.MustParse("00000000-0000-0000-0000-000000000101")
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id = $1`, orderID)
		_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id = $1`, partialID)
	}()

	_, err = pool.Exec(ctx, `
		INSERT INTO orders (id, client_order_id, account_id, symbol, side, type, price, quantity, filled_quantity, status, time_in_force, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12)
	`, orderID, "client-1", accountID, "BTC-USD", "buy", "limit", "100", "1", "0", "open", "GTC", time.Now())
	if err != nil {
		t.Fatalf("insert order: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO orders (id, client_order_id, account_id, symbol, side, type, price, quantity, filled_quantity, status, time_in_force, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12)
	`, partialID, "client-2", accountID, "BTC-USD", "sell", "limit", "120", "2", "1", "open", "GTC", time.Now())
	if err != nil {
		t.Fatalf("insert partial order: %v", err)
	}

	store := NewSnapshotStore(pool)
	orders, err := store.LoadOpenOrders(ctx, "BTC-USD")
	if err != nil {
		t.Fatalf("load open orders: %v", err)
	}
	if len(orders) != 2 {
		t.Fatalf("expected 2 orders, got %d", len(orders))
	}
	ids := map[string]struct{}{}
	for _, order := range orders {
		ids[order.ID] = struct{}{}
	}
	if _, ok := ids[orderID.String()]; !ok {
		t.Fatalf("missing open order")
	}
	if _, ok := ids[partialID.String()]; !ok {
		t.Fatalf("missing partially filled order")
	}
}
