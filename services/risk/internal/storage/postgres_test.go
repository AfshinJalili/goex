package storage

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	ledgerpb "github.com/AfshinJalili/goex/services/ledger/proto/ledger/v1"
	"github.com/AfshinJalili/goex/services/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
)

type fakeLedgerClient struct {
	available string
}

func (f *fakeLedgerClient) GetBalance(ctx context.Context, in *ledgerpb.GetBalanceRequest, opts ...grpc.CallOption) (*ledgerpb.GetBalanceResponse, error) {
	return &ledgerpb.GetBalanceResponse{
		AccountId: in.AccountId,
		Asset:     in.Asset,
		Available: f.available,
		Locked:    "0",
	}, nil
}

func TestGetAccountInfo(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION") == "" {
		t.Skip("set RUN_DB_INTEGRATION=1 to run")
	}

	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Skipf("db connection failed: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()
	userID, accountID := createTestAccount(t, ctx, pool, "risk-account")
	defer cleanupTestAccount(ctx, pool, userID, accountID)

	store := New(pool, &fakeLedgerClient{available: "0"})
	info, err := store.GetAccountInfo(ctx, accountID)
	if err != nil {
		t.Fatalf("GetAccountInfo: %v", err)
	}
	if info.ID != accountID {
		t.Fatalf("expected account %s, got %s", accountID, info.ID)
	}
	if info.Status != "active" {
		t.Fatalf("expected status active, got %s", info.Status)
	}
}

func TestGetMarketBySymbol(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION") == "" {
		t.Skip("set RUN_DB_INTEGRATION=1 to run")
	}

	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Skipf("db connection failed: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()
	symbol := fmt.Sprintf("TST-%s", uuid.New().String()[:8])
	if err := insertAsset(ctx, pool, "TST"); err != nil {
		t.Fatalf("insert asset: %v", err)
	}
	if err := insertAsset(ctx, pool, "USD"); err != nil {
		t.Fatalf("insert asset: %v", err)
	}
	if err := insertMarket(ctx, pool, symbol, "TST", "USD", "active"); err != nil {
		t.Fatalf("insert market: %v", err)
	}
	defer cleanupMarket(ctx, pool, symbol)

	store := New(pool, &fakeLedgerClient{available: "0"})
	market, err := store.GetMarketBySymbol(ctx, symbol)
	if err != nil {
		t.Fatalf("GetMarketBySymbol: %v", err)
	}
	if market.Symbol != symbol {
		t.Fatalf("expected symbol %s, got %s", symbol, market.Symbol)
	}
}

func TestCheckBalance(t *testing.T) {
	store := New(nil, &fakeLedgerClient{available: "100"})
	required := decimal.NewFromInt(25)

	balance, err := store.CheckBalance(context.Background(), uuid.New(), "USD", required)
	if err != nil {
		t.Fatalf("CheckBalance: %v", err)
	}
	if !balance.Sufficient {
		t.Fatalf("expected sufficient balance")
	}
	if balance.Asset != "USD" {
		t.Fatalf("expected asset USD")
	}
}

func createTestAccount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix string) (uuid.UUID, uuid.UUID) {
	t.Helper()

	userID := uuid.New()
	accountID := uuid.New()
	email := fmt.Sprintf("risk_%s_%s@example.com", suffix, userID.String()[:8])
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
	_, _ = pool.Exec(ctx, `DELETE FROM accounts WHERE id = $1`, accountID)
	_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
}

func insertAsset(ctx context.Context, pool *pgxpool.Pool, symbol string) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO assets (symbol, precision, status)
		VALUES ($1, 8, 'active')
		ON CONFLICT (symbol) DO NOTHING
	`, symbol)
	return err
}

func insertMarket(ctx context.Context, pool *pgxpool.Pool, symbol, baseAsset, quoteAsset, status string) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO markets (symbol, base_asset, quote_asset, status)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (symbol) DO NOTHING
	`, symbol, baseAsset, quoteAsset, status)
	return err
}

func cleanupMarket(ctx context.Context, pool *pgxpool.Pool, symbol string) {
	_, _ = pool.Exec(ctx, `DELETE FROM markets WHERE symbol = $1`, symbol)
}
