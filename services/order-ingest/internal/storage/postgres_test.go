package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/AfshinJalili/goex/services/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

func TestCreateOrderIdempotent(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION") == "" {
		t.Skip("set RUN_DB_INTEGRATION=1 to run")
	}

	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Skipf("db connection failed: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()
	userID, accountID := createTestAccount(t, ctx, pool, "orders")
	defer cleanupTestAccount(ctx, pool, userID, accountID)

	store := New(pool)
	price := decimal.NewFromInt(100)
	order := Order{
		ClientOrderID:  "client-1",
		AccountID:      accountID,
		Symbol:         "BTC-USD",
		Side:           "buy",
		Type:           "limit",
		Price:          &price,
		Quantity:       decimal.NewFromInt(1),
		FilledQuantity: decimal.Zero,
		Status:         OrderStatusPending,
		TimeInForce:    "GTC",
	}

	first, created, err := store.CreateOrder(ctx, order)
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if !created {
		t.Fatalf("expected created order")
	}

	second, created, err := store.CreateOrder(ctx, order)
	if err != nil {
		t.Fatalf("CreateOrder duplicate: %v", err)
	}
	if created {
		t.Fatalf("expected duplicate to not be created")
	}
	if second.ID != first.ID {
		t.Fatalf("expected same order ID on duplicate")
	}
}

func TestCreateOrderWithID(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION") == "" {
		t.Skip("set RUN_DB_INTEGRATION=1 to run")
	}

	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Skipf("db connection failed: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()
	userID, accountID := createTestAccount(t, ctx, pool, "orders-id")
	defer cleanupTestAccount(ctx, pool, userID, accountID)

	store := New(pool)
	price := decimal.NewFromInt(100)
	orderID := uuid.New()
	order := Order{
		ID:             orderID,
		ClientOrderID:  "client-1-id",
		AccountID:      accountID,
		Symbol:         "BTC-USD",
		Side:           "buy",
		Type:           "limit",
		Price:          &price,
		Quantity:       decimal.NewFromInt(1),
		FilledQuantity: decimal.Zero,
		Status:         OrderStatusPending,
		TimeInForce:    "GTC",
	}

	stored, created, err := store.CreateOrder(ctx, order)
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if !created {
		t.Fatalf("expected created order")
	}
	if stored.ID != orderID {
		t.Fatalf("expected order ID %s, got %s", orderID, stored.ID)
	}
}

func TestApplyTradeExecutionIdempotent(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION") == "" {
		t.Skip("set RUN_DB_INTEGRATION=1 to run")
	}

	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Skipf("db connection failed: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()
	userID, accountID := createTestAccount(t, ctx, pool, "fills")
	defer cleanupTestAccount(ctx, pool, userID, accountID)

	store := New(pool)
	price := decimal.NewFromInt(100)

	order1, _, err := store.CreateOrder(ctx, Order{
		ClientOrderID:  "client-fill-1",
		AccountID:      accountID,
		Symbol:         "BTC-USD",
		Side:           "buy",
		Type:           "limit",
		Price:          &price,
		Quantity:       decimal.NewFromInt(2),
		FilledQuantity: decimal.Zero,
		Status:         OrderStatusPending,
		TimeInForce:    "GTC",
	})
	if err != nil {
		t.Fatalf("CreateOrder 1: %v", err)
	}

	order2, _, err := store.CreateOrder(ctx, Order{
		ClientOrderID:  "client-fill-2",
		AccountID:      accountID,
		Symbol:         "BTC-USD",
		Side:           "sell",
		Type:           "limit",
		Price:          &price,
		Quantity:       decimal.NewFromInt(2),
		FilledQuantity: decimal.Zero,
		Status:         OrderStatusPending,
		TimeInForce:    "GTC",
	})
	if err != nil {
		t.Fatalf("CreateOrder 2: %v", err)
	}

	eventID := uuid.NewString()
	result, err := store.ApplyTradeExecution(ctx, eventID, []OrderFill{
		{OrderID: order1.ID, Quantity: decimal.NewFromInt(2)},
		{OrderID: order2.ID, Quantity: decimal.NewFromInt(2)},
	})
	if err != nil {
		t.Fatalf("ApplyTradeExecution: %v", err)
	}
	if result.AlreadyProcessed {
		t.Fatalf("expected first execution to not be already processed")
	}
	if len(result.FilledOrderIDs) != 2 {
		t.Fatalf("expected filled order IDs, got %d", len(result.FilledOrderIDs))
	}

	order1Updated, err := store.GetOrderByID(ctx, order1.ID)
	if err != nil {
		t.Fatalf("GetOrderByID: %v", err)
	}
	if !order1Updated.FilledQuantity.Equal(decimal.NewFromInt(2)) {
		t.Fatalf("expected filled quantity 2, got %s", order1Updated.FilledQuantity.String())
	}

	result, err = store.ApplyTradeExecution(ctx, eventID, []OrderFill{
		{OrderID: order1.ID, Quantity: decimal.NewFromInt(2)},
		{OrderID: order2.ID, Quantity: decimal.NewFromInt(2)},
	})
	if err != nil {
		t.Fatalf("ApplyTradeExecution duplicate: %v", err)
	}
	if !result.AlreadyProcessed {
		t.Fatalf("expected already processed on duplicate")
	}
}

func TestCancelOrderInvalidStatus(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION") == "" {
		t.Skip("set RUN_DB_INTEGRATION=1 to run")
	}

	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Skipf("db connection failed: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()
	userID, accountID := createTestAccount(t, ctx, pool, "cancel-status")
	defer cleanupTestAccount(ctx, pool, userID, accountID)

	store := New(pool)
	price := decimal.NewFromInt(100)

	order, _, err := store.CreateOrder(ctx, Order{
		ClientOrderID:  "client-cancel-1",
		AccountID:      accountID,
		Symbol:         "BTC-USD",
		Side:           "buy",
		Type:           "limit",
		Price:          &price,
		Quantity:       decimal.NewFromInt(1),
		FilledQuantity: decimal.NewFromInt(1),
		Status:         OrderStatusFilled,
		TimeInForce:    "GTC",
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}

	_, err = store.CancelOrder(ctx, order.ID, accountID)
	if err == nil || !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("expected ErrInvalidStatus, got %v", err)
	}
}

func TestCancelOrderRaceDoesNotOverwriteFill(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION") == "" {
		t.Skip("set RUN_DB_INTEGRATION=1 to run")
	}

	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Skipf("db connection failed: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()
	userID, accountID := createTestAccount(t, ctx, pool, "cancel-race")
	defer cleanupTestAccount(ctx, pool, userID, accountID)

	store := New(pool)
	price := decimal.NewFromInt(100)

	order, _, err := store.CreateOrder(ctx, Order{
		ClientOrderID:  "client-cancel-race",
		AccountID:      accountID,
		Symbol:         "BTC-USD",
		Side:           "buy",
		Type:           "limit",
		Price:          &price,
		Quantity:       decimal.NewFromInt(1),
		FilledQuantity: decimal.Zero,
		Status:         OrderStatusOpen,
		TimeInForce:    "GTC",
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}

	if _, err := store.CancelOrder(ctx, order.ID, accountID); err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}

	_, err = store.UpdateOrderStatus(ctx, order.ID, OrderStatusFilled, decimal.NewFromInt(1))
	if err == nil || !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("expected ErrInvalidStatus after cancel, got %v", err)
	}
}

func createTestAccount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix string) (uuid.UUID, uuid.UUID) {
	t.Helper()

	userID := uuid.New()
	accountID := uuid.New()
	email := fmt.Sprintf("order_%s_%s@example.com", suffix, userID.String()[:8])
	now := time.Now().UTC()

	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, status, kyc_level, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, userID, email, "test-hash", "active", "verified", now, now)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (id, user_id, type, status, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, accountID, userID, "spot", "active", now)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	return userID, accountID
}

func cleanupTestAccount(ctx context.Context, pool *pgxpool.Pool, userID, accountID uuid.UUID) {
	_, _ = pool.Exec(ctx, `DELETE FROM processed_events WHERE event_id LIKE 'order-ingest:evt_%'`)
	_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE account_id = $1`, accountID)
	_, _ = pool.Exec(ctx, `DELETE FROM accounts WHERE id = $1`, accountID)
	_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
}
