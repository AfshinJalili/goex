package storage

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/AfshinJalili/goex/services/fee/proto/fee/v1"
	"github.com/AfshinJalili/goex/services/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
)

type fakeFeeClient struct {
	amount string
	asset  string
	calls  []*feepb.CalculateFeesRequest
}

func (f *fakeFeeClient) CalculateFees(ctx context.Context, in *feepb.CalculateFeesRequest, opts ...grpc.CallOption) (*feepb.CalculateFeesResponse, error) {
	f.calls = append(f.calls, in)
	return &feepb.CalculateFeesResponse{
		FeeAmount:   f.amount,
		FeeAsset:    f.asset,
		TierApplied: "default",
	}, nil
}

func TestGetBalance(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION") == "" {
		t.Skip("set RUN_DB_INTEGRATION=1 to run")
	}

	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Skipf("db connection failed: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()
	userID, accountID := createTestAccount(t, ctx, pool, "balance")
	defer cleanupTestAccount(ctx, pool, userID, accountID)

	if err := insertLedgerAccount(ctx, pool, accountID, "USD", "100", "0"); err != nil {
		t.Fatalf("insert ledger account: %v", err)
	}

	feeClient := &fakeFeeClient{amount: "0", asset: "USD"}
	store := New(pool, feeClient)
	balance, err := store.GetBalance(ctx, accountID, "USD")
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if balance.BalanceAvailable.String() != "100" {
		t.Fatalf("expected 100, got %s", balance.BalanceAvailable.String())
	}
}

func TestApplySettlementAndIdempotency(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION") == "" {
		t.Skip("set RUN_DB_INTEGRATION=1 to run")
	}

	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Skipf("db connection failed: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()
	makerUser, makerAccount := createTestAccount(t, ctx, pool, "maker")
	takerUser, takerAccount := createTestAccount(t, ctx, pool, "taker")
	defer cleanupTestAccount(ctx, pool, makerUser, makerAccount)
	defer cleanupTestAccount(ctx, pool, takerUser, takerAccount)

	if err := insertLedgerAccount(ctx, pool, makerAccount, "USD", "1000", "0"); err != nil {
		t.Fatalf("insert maker USD: %v", err)
	}
	if err := insertLedgerAccount(ctx, pool, makerAccount, "BTC", "0", "0"); err != nil {
		t.Fatalf("insert maker BTC: %v", err)
	}
	if err := insertLedgerAccount(ctx, pool, takerAccount, "USD", "0", "0"); err != nil {
		t.Fatalf("insert taker USD: %v", err)
	}
	if err := insertLedgerAccount(ctx, pool, takerAccount, "BTC", "10", "0"); err != nil {
		t.Fatalf("insert taker BTC: %v", err)
	}

	feeClient := &fakeFeeClient{amount: "1", asset: "USD"}
	store := New(pool, feeClient)
	tradeID := uuid.New()
	eventID := uuid.NewString()
	result, err := store.ApplySettlement(ctx, SettlementRequest{
		TradeID:        tradeID,
		MakerAccountID: makerAccount,
		TakerAccountID: takerAccount,
		Symbol:         "BTC-USD",
		Price:          decimal.NewFromInt(100),
		Quantity:       decimal.NewFromInt(2),
		MakerSide:      "buy",
		EventID:        eventID,
	})
	if err != nil {
		t.Fatalf("ApplySettlement: %v", err)
	}
	if result.AlreadyProcessed {
		t.Fatalf("expected settlement to process")
	}
	if len(result.EntryIDs) == 0 {
		t.Fatalf("expected ledger entries")
	}

	if len(feeClient.calls) != 2 {
		t.Fatalf("expected 2 fee calls, got %d", len(feeClient.calls))
	}
	if feeClient.calls[0].Side == "" || feeClient.calls[1].Side == "" {
		t.Fatalf("expected side to be populated in fee calls")
	}
	if feeClient.calls[0].OrderType != "maker" || feeClient.calls[1].OrderType != "taker" {
		t.Fatalf("expected maker/taker order_type, got %s/%s", feeClient.calls[0].OrderType, feeClient.calls[1].OrderType)
	}

	duplicate, err := store.ApplySettlement(ctx, SettlementRequest{
		TradeID:        tradeID,
		MakerAccountID: makerAccount,
		TakerAccountID: takerAccount,
		Symbol:         "BTC-USD",
		Price:          decimal.NewFromInt(100),
		Quantity:       decimal.NewFromInt(2),
		MakerSide:      "buy",
		EventID:        eventID,
	})
	if err != nil {
		t.Fatalf("ApplySettlement duplicate: %v", err)
	}
	if !duplicate.AlreadyProcessed {
		t.Fatalf("expected duplicate to be treated as already processed")
	}
}

func TestApplySettlementDuplicateRecordsProcessedEvent(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION") == "" {
		t.Skip("set RUN_DB_INTEGRATION=1 to run")
	}

	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Skipf("db connection failed: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()
	makerUser, makerAccount := createTestAccount(t, ctx, pool, "maker-dup")
	takerUser, takerAccount := createTestAccount(t, ctx, pool, "taker-dup")
	defer cleanupTestAccount(ctx, pool, makerUser, makerAccount)
	defer cleanupTestAccount(ctx, pool, takerUser, takerAccount)

	if err := insertLedgerAccount(ctx, pool, makerAccount, "USD", "1000", "0"); err != nil {
		t.Fatalf("insert maker USD: %v", err)
	}
	if err := insertLedgerAccount(ctx, pool, makerAccount, "BTC", "0", "0"); err != nil {
		t.Fatalf("insert maker BTC: %v", err)
	}
	if err := insertLedgerAccount(ctx, pool, takerAccount, "USD", "0", "0"); err != nil {
		t.Fatalf("insert taker USD: %v", err)
	}
	if err := insertLedgerAccount(ctx, pool, takerAccount, "BTC", "10", "0"); err != nil {
		t.Fatalf("insert taker BTC: %v", err)
	}

	feeClient := &fakeFeeClient{amount: "1", asset: "USD"}
	store := New(pool, feeClient)
	tradeID := uuid.New()
	eventID1 := uuid.NewString()
	eventID2 := uuid.NewString()

	_, err = store.ApplySettlement(ctx, SettlementRequest{
		TradeID:        tradeID,
		MakerAccountID: makerAccount,
		TakerAccountID: takerAccount,
		Symbol:         "BTC-USD",
		Price:          decimal.NewFromInt(100),
		Quantity:       decimal.NewFromInt(2),
		MakerSide:      "buy",
		EventID:        eventID1,
	})
	if err != nil {
		t.Fatalf("ApplySettlement: %v", err)
	}

	dup, err := store.ApplySettlement(ctx, SettlementRequest{
		TradeID:        tradeID,
		MakerAccountID: makerAccount,
		TakerAccountID: takerAccount,
		Symbol:         "BTC-USD",
		Price:          decimal.NewFromInt(100),
		Quantity:       decimal.NewFromInt(2),
		MakerSide:      "buy",
		EventID:        eventID2,
	})
	if err != nil {
		t.Fatalf("ApplySettlement duplicate: %v", err)
	}
	if !dup.AlreadyProcessed {
		t.Fatalf("expected duplicate to be treated as already processed")
	}

	var count int
	row := pool.QueryRow(ctx, `SELECT count(*) FROM processed_events WHERE event_id = $1`, eventID2)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count processed_events: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected processed_events for duplicate event")
	}

	if _, err := pool.Exec(ctx, `DELETE FROM ledger_entries WHERE reference_id = $1`, tradeID); err != nil {
		t.Fatalf("delete ledger entries: %v", err)
	}

	dupAgain, err := store.ApplySettlement(ctx, SettlementRequest{
		TradeID:        tradeID,
		MakerAccountID: makerAccount,
		TakerAccountID: takerAccount,
		Symbol:         "BTC-USD",
		Price:          decimal.NewFromInt(100),
		Quantity:       decimal.NewFromInt(2),
		MakerSide:      "buy",
		EventID:        eventID2,
	})
	if err != nil {
		t.Fatalf("ApplySettlement duplicate again: %v", err)
	}
	if !dupAgain.AlreadyProcessed {
		t.Fatalf("expected duplicate event_id to short-circuit")
	}
}

func TestApplySettlementInsufficientBalance(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION") == "" {
		t.Skip("set RUN_DB_INTEGRATION=1 to run")
	}

	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Skipf("db connection failed: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()
	makerUser, makerAccount := createTestAccount(t, ctx, pool, "maker-low")
	takerUser, takerAccount := createTestAccount(t, ctx, pool, "taker-low")
	defer cleanupTestAccount(ctx, pool, makerUser, makerAccount)
	defer cleanupTestAccount(ctx, pool, takerUser, takerAccount)

	if err := insertLedgerAccount(ctx, pool, makerAccount, "USD", "0", "0"); err != nil {
		t.Fatalf("insert maker USD: %v", err)
	}
	if err := insertLedgerAccount(ctx, pool, makerAccount, "BTC", "0", "0"); err != nil {
		t.Fatalf("insert maker BTC: %v", err)
	}
	if err := insertLedgerAccount(ctx, pool, takerAccount, "USD", "0", "0"); err != nil {
		t.Fatalf("insert taker USD: %v", err)
	}
	if err := insertLedgerAccount(ctx, pool, takerAccount, "BTC", "0", "0"); err != nil {
		t.Fatalf("insert taker BTC: %v", err)
	}

	feeClient := &fakeFeeClient{amount: "0", asset: "USD"}
	store := New(pool, feeClient)
	tradeID := uuid.New()
	_, err = store.ApplySettlement(ctx, SettlementRequest{
		TradeID:        tradeID,
		MakerAccountID: makerAccount,
		TakerAccountID: takerAccount,
		Symbol:         "BTC-USD",
		Price:          decimal.NewFromInt(100),
		Quantity:       decimal.NewFromInt(2),
		MakerSide:      "buy",
		EventID:        uuid.NewString(),
	})
	if err == nil {
		t.Fatalf("expected insufficient balance error")
	}

	var count int
	row := pool.QueryRow(ctx, `SELECT count(*) FROM ledger_entries WHERE reference_id = $1`, tradeID)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count ledger entries: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no ledger entries after failure, got %d", count)
	}
}

func TestApplySettlementUsesLockedBalance(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION") == "" {
		t.Skip("set RUN_DB_INTEGRATION=1 to run")
	}

	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Skipf("db connection failed: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()
	makerUser, makerAccount := createTestAccount(t, ctx, pool, "maker-locked")
	takerUser, takerAccount := createTestAccount(t, ctx, pool, "taker-locked")
	defer cleanupTestAccount(ctx, pool, makerUser, makerAccount)
	defer cleanupTestAccount(ctx, pool, takerUser, takerAccount)

	if err := insertLedgerAccount(ctx, pool, makerAccount, "USD", "0", "200"); err != nil {
		t.Fatalf("insert maker USD: %v", err)
	}
	if err := insertLedgerAccount(ctx, pool, makerAccount, "BTC", "0", "0"); err != nil {
		t.Fatalf("insert maker BTC: %v", err)
	}
	if err := insertLedgerAccount(ctx, pool, takerAccount, "USD", "0", "0"); err != nil {
		t.Fatalf("insert taker USD: %v", err)
	}
	if err := insertLedgerAccount(ctx, pool, takerAccount, "BTC", "0", "2"); err != nil {
		t.Fatalf("insert taker BTC: %v", err)
	}

	feeClient := &fakeFeeClient{amount: "0", asset: "USD"}
	store := New(pool, feeClient)
	tradeID := uuid.New()
	_, err = store.ApplySettlement(ctx, SettlementRequest{
		TradeID:        tradeID,
		MakerAccountID: makerAccount,
		TakerAccountID: takerAccount,
		Symbol:         "BTC-USD",
		Price:          decimal.NewFromInt(100),
		Quantity:       decimal.NewFromInt(2),
		MakerSide:      "buy",
		EventID:        uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("ApplySettlement: %v", err)
	}

	makerUSD, err := store.GetBalance(ctx, makerAccount, "USD")
	if err != nil {
		t.Fatalf("GetBalance maker USD: %v", err)
	}
	if !makerUSD.BalanceLocked.Equal(decimal.Zero) {
		t.Fatalf("expected maker locked USD to be 0, got %s", makerUSD.BalanceLocked.String())
	}

	takerBTC, err := store.GetBalance(ctx, takerAccount, "BTC")
	if err != nil {
		t.Fatalf("GetBalance taker BTC: %v", err)
	}
	if !takerBTC.BalanceLocked.Equal(decimal.Zero) {
		t.Fatalf("expected taker locked BTC to be 0, got %s", takerBTC.BalanceLocked.String())
	}
}

func createTestAccount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix string) (uuid.UUID, uuid.UUID) {
	t.Helper()

	userID := uuid.New()
	accountID := uuid.New()
	email := fmt.Sprintf("ledger_%s_%s@example.com", suffix, userID.String()[:8])
	now := time.Now().UTC()

	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, status, kyc_level, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, userID, email, "test-hash", "active", "none", now, now)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (id, user_id, type, created_at)
		VALUES ($1, $2, $3, $4)
	`, accountID, userID, "spot", now)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	return userID, accountID
}

func insertLedgerAccount(ctx context.Context, pool *pgxpool.Pool, accountID uuid.UUID, asset, available, locked string) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO ledger_accounts (account_id, asset, balance_available, balance_locked)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (account_id, asset) DO UPDATE SET balance_available = EXCLUDED.balance_available
	`, accountID, asset, available, locked)
	return err
}

func cleanupTestAccount(ctx context.Context, pool *pgxpool.Pool, userID, accountID uuid.UUID) {
	_, _ = pool.Exec(ctx, `DELETE FROM ledger_entries WHERE ledger_account_id IN (SELECT id FROM ledger_accounts WHERE account_id = $1)`, accountID)
	_, _ = pool.Exec(ctx, `DELETE FROM ledger_accounts WHERE account_id = $1`, accountID)
	_, _ = pool.Exec(ctx, `DELETE FROM accounts WHERE id = $1`, accountID)
	_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	_, _ = pool.Exec(ctx, `DELETE FROM processed_events WHERE event_id LIKE 'evt_%'`)
}
