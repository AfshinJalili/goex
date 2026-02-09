package storage

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/AfshinJalili/goex/services/fee/proto/fee/v1"
	"github.com/AfshinJalili/goex/services/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeFeeClient struct {
	amount string
	asset  string
	err    error
	calls  []*feepb.CalculateFeesRequest
}

func (f *fakeFeeClient) CalculateFees(ctx context.Context, in *feepb.CalculateFeesRequest, opts ...grpc.CallOption) (*feepb.CalculateFeesResponse, error) {
	f.calls = append(f.calls, in)
	if f.err != nil {
		return nil, f.err
	}
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
	store := New(pool, feeClient, nil, nil)
	balance, err := store.GetBalance(ctx, accountID, "USD")
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if balance.BalanceAvailable.String() != "100" {
		t.Fatalf("expected 100, got %s", balance.BalanceAvailable.String())
	}
}

func TestReserveAndReleaseReservation(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION") == "" {
		t.Skip("set RUN_DB_INTEGRATION=1 to run")
	}

	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Skipf("db connection failed: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()
	userID, accountID := createTestAccount(t, ctx, pool, "reserve")
	defer cleanupTestAccount(ctx, pool, userID, accountID)

	if err := insertLedgerAccount(ctx, pool, accountID, "USD", "100", "0"); err != nil {
		t.Fatalf("insert ledger account: %v", err)
	}

	store := New(pool, &fakeFeeClient{amount: "0", asset: "USD"}, nil, nil)
	orderID := uuid.New()
	res, err := store.ReserveBalance(ctx, accountID, orderID, "USD", decimal.NewFromInt(40))
	if err != nil {
		t.Fatalf("ReserveBalance: %v", err)
	}
	if res.Status != "active" {
		t.Fatalf("expected active reservation, got %s", res.Status)
	}

	balance, err := store.GetBalance(ctx, accountID, "USD")
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if balance.BalanceAvailable.String() != "60" || balance.BalanceLocked.String() != "40" {
		t.Fatalf("unexpected balances available=%s locked=%s", balance.BalanceAvailable.String(), balance.BalanceLocked.String())
	}

	_, released, err := store.ReleaseReservation(ctx, orderID)
	if err != nil {
		t.Fatalf("ReleaseReservation: %v", err)
	}
	if released.String() != "40" {
		t.Fatalf("expected released 40, got %s", released.String())
	}

	balance, err = store.GetBalance(ctx, accountID, "USD")
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if balance.BalanceAvailable.String() != "100" || balance.BalanceLocked.String() != "0" {
		t.Fatalf("unexpected balances after release available=%s locked=%s", balance.BalanceAvailable.String(), balance.BalanceLocked.String())
	}
}

func TestReserveBalanceDuplicate(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION") == "" {
		t.Skip("set RUN_DB_INTEGRATION=1 to run")
	}

	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Skipf("db connection failed: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()
	userID, accountID := createTestAccount(t, ctx, pool, "reserve-dup")
	defer cleanupTestAccount(ctx, pool, userID, accountID)

	if err := insertLedgerAccount(ctx, pool, accountID, "USD", "100", "0"); err != nil {
		t.Fatalf("insert ledger account: %v", err)
	}

	store := New(pool, &fakeFeeClient{amount: "0", asset: "USD"}, nil, nil)
	orderID := uuid.New()

	first, err := store.ReserveBalance(ctx, accountID, orderID, "USD", decimal.NewFromInt(40))
	if err != nil {
		t.Fatalf("ReserveBalance first: %v", err)
	}
	second, err := store.ReserveBalance(ctx, accountID, orderID, "USD", decimal.NewFromInt(40))
	if err != nil {
		t.Fatalf("ReserveBalance duplicate: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected same reservation ID, got %s vs %s", first.ID, second.ID)
	}

	balance, err := store.GetBalance(ctx, accountID, "USD")
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if balance.BalanceAvailable.String() != "60" || balance.BalanceLocked.String() != "40" {
		t.Fatalf("unexpected balances available=%s locked=%s", balance.BalanceAvailable.String(), balance.BalanceLocked.String())
	}
}

func TestConcurrentReserveBalance(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION") == "" {
		t.Skip("set RUN_DB_INTEGRATION=1 to run")
	}

	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Skipf("db connection failed: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()
	userID, accountID := createTestAccount(t, ctx, pool, "reserve-concurrent")
	defer cleanupTestAccount(ctx, pool, userID, accountID)

	if err := insertLedgerAccount(ctx, pool, accountID, "USD", "1000", "0"); err != nil {
		t.Fatalf("insert ledger account: %v", err)
	}

	store := New(pool, &fakeFeeClient{amount: "0", asset: "USD"}, nil, nil)
	var wg sync.WaitGroup
	errCh := make(chan error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.ReserveBalance(ctx, accountID, uuid.New(), "USD", decimal.NewFromInt(10))
			if err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("unexpected reserve error: %v", err)
	}

	bal, err := store.GetBalance(ctx, accountID, "USD")
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if !bal.BalanceAvailable.Equal(decimal.NewFromInt(800)) {
		t.Fatalf("expected available 800, got %s", bal.BalanceAvailable.String())
	}
	if !bal.BalanceLocked.Equal(decimal.NewFromInt(200)) {
		t.Fatalf("expected locked 200, got %s", bal.BalanceLocked.String())
	}
}

func TestApplySettlementConsumesReservation(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION") == "" {
		t.Skip("set RUN_DB_INTEGRATION=1 to run")
	}

	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Skipf("db connection failed: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()
	makerUser, makerAccount := createTestAccount(t, ctx, pool, "maker-reserve")
	takerUser, takerAccount := createTestAccount(t, ctx, pool, "taker-reserve")
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

	store := New(pool, &fakeFeeClient{amount: "0", asset: "USD"}, nil, nil)
	makerOrderID := uuid.New()
	takerOrderID := uuid.New()

	if _, err := store.ReserveBalance(ctx, makerAccount, makerOrderID, "USD", decimal.NewFromInt(200)); err != nil {
		t.Fatalf("reserve maker: %v", err)
	}
	if _, err := store.ReserveBalance(ctx, takerAccount, takerOrderID, "BTC", decimal.NewFromInt(2)); err != nil {
		t.Fatalf("reserve taker: %v", err)
	}

	tradeID := uuid.New()
	_, err = store.ApplySettlement(ctx, SettlementRequest{
		TradeID:        tradeID,
		MakerOrderID:   makerOrderID,
		TakerOrderID:   takerOrderID,
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

	makerRes := fetchReservation(t, ctx, pool, makerOrderID)
	if makerRes.Status != "consumed" {
		t.Fatalf("expected maker reservation consumed, got %s", makerRes.Status)
	}
	if makerRes.ConsumedAmount.String() != makerRes.Amount.String() {
		t.Fatalf("expected maker reservation consumed amount %s got %s", makerRes.Amount.String(), makerRes.ConsumedAmount.String())
	}

	takerRes := fetchReservation(t, ctx, pool, takerOrderID)
	if takerRes.Status != "consumed" {
		t.Fatalf("expected taker reservation consumed, got %s", takerRes.Status)
	}
	if takerRes.ConsumedAmount.String() != takerRes.Amount.String() {
		t.Fatalf("expected taker reservation consumed amount %s got %s", takerRes.Amount.String(), takerRes.ConsumedAmount.String())
	}

	makerBal, err := store.GetBalance(ctx, makerAccount, "USD")
	if err != nil {
		t.Fatalf("GetBalance maker USD: %v", err)
	}
	if !makerBal.BalanceLocked.Equal(decimal.Zero) {
		t.Fatalf("expected maker USD locked 0, got %s", makerBal.BalanceLocked.String())
	}

	takerBal, err := store.GetBalance(ctx, takerAccount, "BTC")
	if err != nil {
		t.Fatalf("GetBalance taker BTC: %v", err)
	}
	if !takerBal.BalanceLocked.Equal(decimal.Zero) {
		t.Fatalf("expected taker BTC locked 0, got %s", takerBal.BalanceLocked.String())
	}
}

func TestReleaseReservationAfterBetterPriceFill(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION") == "" {
		t.Skip("set RUN_DB_INTEGRATION=1 to run")
	}

	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Skipf("db connection failed: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()
	makerUser, makerAccount := createTestAccount(t, ctx, pool, "maker-better")
	takerUser, takerAccount := createTestAccount(t, ctx, pool, "taker-better")
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

	store := New(pool, &fakeFeeClient{amount: "0", asset: "USD"}, nil, nil)
	makerOrderID := uuid.New()
	takerOrderID := uuid.New()

	if _, err := store.ReserveBalance(ctx, makerAccount, makerOrderID, "USD", decimal.NewFromInt(200)); err != nil {
		t.Fatalf("reserve maker: %v", err)
	}
	if _, err := store.ReserveBalance(ctx, takerAccount, takerOrderID, "BTC", decimal.NewFromInt(2)); err != nil {
		t.Fatalf("reserve taker: %v", err)
	}

	tradeID := uuid.New()
	_, err = store.ApplySettlement(ctx, SettlementRequest{
		TradeID:        tradeID,
		MakerOrderID:   makerOrderID,
		TakerOrderID:   takerOrderID,
		MakerAccountID: makerAccount,
		TakerAccountID: takerAccount,
		Symbol:         "BTC-USD",
		Price:          decimal.NewFromInt(90),
		Quantity:       decimal.NewFromInt(2),
		MakerSide:      "buy",
		EventID:        uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("ApplySettlement: %v", err)
	}

	res, released, err := store.ReleaseReservation(ctx, makerOrderID)
	if err != nil {
		t.Fatalf("ReleaseReservation: %v", err)
	}
	if res.Status != "released" {
		t.Fatalf("expected reservation released, got %s", res.Status)
	}
	if released.String() != "20" {
		t.Fatalf("expected released 20, got %s", released.String())
	}

	balance, err := store.GetBalance(ctx, makerAccount, "USD")
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if balance.BalanceAvailable.String() != "820" || balance.BalanceLocked.String() != "0" {
		t.Fatalf("unexpected balances available=%s locked=%s", balance.BalanceAvailable.String(), balance.BalanceLocked.String())
	}
}

func TestReleaseReservationAfterLimitPriceImprovement(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION") == "" {
		t.Skip("set RUN_DB_INTEGRATION=1 to run")
	}

	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Skipf("db connection failed: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()
	makerUser, makerAccount := createTestAccount(t, ctx, pool, "maker-limit-better")
	takerUser, takerAccount := createTestAccount(t, ctx, pool, "taker-limit-better")
	defer cleanupTestAccount(ctx, pool, makerUser, makerAccount)
	defer cleanupTestAccount(ctx, pool, takerUser, takerAccount)

	if err := insertLedgerAccount(ctx, pool, makerAccount, "USD", "500", "0"); err != nil {
		t.Fatalf("insert maker USD: %v", err)
	}
	if err := insertLedgerAccount(ctx, pool, makerAccount, "BTC", "0", "0"); err != nil {
		t.Fatalf("insert maker BTC: %v", err)
	}
	if err := insertLedgerAccount(ctx, pool, takerAccount, "USD", "0", "0"); err != nil {
		t.Fatalf("insert taker USD: %v", err)
	}
	if err := insertLedgerAccount(ctx, pool, takerAccount, "BTC", "5", "0"); err != nil {
		t.Fatalf("insert taker BTC: %v", err)
	}

	store := New(pool, &fakeFeeClient{amount: "0", asset: "USD"}, nil, nil)
	makerOrderID := uuid.New()
	takerOrderID := uuid.New()

	if _, err := store.ReserveBalance(ctx, makerAccount, makerOrderID, "USD", decimal.NewFromInt(150)); err != nil {
		t.Fatalf("reserve maker: %v", err)
	}
	if _, err := store.ReserveBalance(ctx, takerAccount, takerOrderID, "BTC", decimal.NewFromInt(1)); err != nil {
		t.Fatalf("reserve taker: %v", err)
	}

	tradeID := uuid.New()
	_, err = store.ApplySettlement(ctx, SettlementRequest{
		TradeID:        tradeID,
		MakerOrderID:   makerOrderID,
		TakerOrderID:   takerOrderID,
		MakerAccountID: makerAccount,
		TakerAccountID: takerAccount,
		Symbol:         "BTC-USD",
		Price:          decimal.NewFromInt(120),
		Quantity:       decimal.NewFromInt(1),
		MakerSide:      "buy",
		EventID:        uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("ApplySettlement: %v", err)
	}

	res, released, err := store.ReleaseReservation(ctx, makerOrderID)
	if err != nil {
		t.Fatalf("ReleaseReservation: %v", err)
	}
	if res.Status != "released" {
		t.Fatalf("expected reservation released, got %s", res.Status)
	}
	if released.String() != "30" {
		t.Fatalf("expected released 30, got %s", released.String())
	}

	balance, err := store.GetBalance(ctx, makerAccount, "USD")
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if balance.BalanceAvailable.String() != "380" || balance.BalanceLocked.String() != "0" {
		t.Fatalf("unexpected balances available=%s locked=%s", balance.BalanceAvailable.String(), balance.BalanceLocked.String())
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
	store := New(pool, feeClient, nil, nil)
	tradeID := uuid.New()
	makerOrderID := uuid.New()
	takerOrderID := uuid.New()
	eventID := uuid.NewString()
	result, err := store.ApplySettlement(ctx, SettlementRequest{
		TradeID:        tradeID,
		MakerOrderID:   makerOrderID,
		TakerOrderID:   takerOrderID,
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
		MakerOrderID:   makerOrderID,
		TakerOrderID:   takerOrderID,
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

func TestApplySettlementFeeFallbackZero(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION") == "" {
		t.Skip("set RUN_DB_INTEGRATION=1 to run")
	}

	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Skipf("db connection failed: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()
	makerUser, makerAccount := createTestAccount(t, ctx, pool, "maker-fallback")
	takerUser, takerAccount := createTestAccount(t, ctx, pool, "taker-fallback")
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

	feeClient := &fakeFeeClient{err: status.Error(codes.Unavailable, "fee down")}
	store := New(pool, feeClient, nil, nil)
	tradeID := uuid.New()
	result, err := store.ApplySettlement(ctx, SettlementRequest{
		TradeID:        tradeID,
		MakerOrderID:   uuid.New(),
		TakerOrderID:   uuid.New(),
		MakerAccountID: makerAccount,
		TakerAccountID: takerAccount,
		Symbol:         "BTC-USD",
		Price:          decimal.NewFromInt(100),
		Quantity:       decimal.NewFromInt(2),
		MakerSide:      "buy",
		EventID:        uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("ApplySettlement fallback: %v", err)
	}
	if result.AlreadyProcessed {
		t.Fatalf("expected settlement to process")
	}
	for _, entry := range result.Entries {
		if entry.AccountID == result.FeeAccountID {
			t.Fatalf("expected no fee entries on fallback")
		}
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
	store := New(pool, feeClient, nil, nil)
	tradeID := uuid.New()
	makerOrderID := uuid.New()
	takerOrderID := uuid.New()
	eventID1 := uuid.NewString()
	eventID2 := uuid.NewString()

	_, err = store.ApplySettlement(ctx, SettlementRequest{
		TradeID:        tradeID,
		MakerOrderID:   makerOrderID,
		TakerOrderID:   takerOrderID,
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
		MakerOrderID:   makerOrderID,
		TakerOrderID:   takerOrderID,
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
	row := pool.QueryRow(ctx, `SELECT count(*) FROM processed_events WHERE event_id = $1`, "ledger:"+eventID2)
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
		MakerOrderID:   makerOrderID,
		TakerOrderID:   takerOrderID,
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
	store := New(pool, feeClient, nil, nil)
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
	store := New(pool, feeClient, nil, nil)
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
	_, _ = pool.Exec(ctx, `DELETE FROM balance_reservations WHERE account_id = $1`, accountID)
	_, _ = pool.Exec(ctx, `DELETE FROM ledger_accounts WHERE account_id = $1`, accountID)
	_, _ = pool.Exec(ctx, `DELETE FROM accounts WHERE id = $1`, accountID)
	_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	_, _ = pool.Exec(ctx, `DELETE FROM processed_events WHERE event_id LIKE 'ledger:evt_%'`)
}

func fetchReservation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orderID uuid.UUID) BalanceReservation {
	t.Helper()
	var res BalanceReservation
	var amountStr, consumedStr string
	row := pool.QueryRow(ctx, `
		SELECT id, order_id, account_id, asset, amount::text, consumed_amount::text, status, created_at, updated_at
		FROM balance_reservations
		WHERE order_id = $1
	`, orderID)
	if err := row.Scan(&res.ID, &res.OrderID, &res.AccountID, &res.Asset, &amountStr, &consumedStr, &res.Status, &res.CreatedAt, &res.UpdatedAt); err != nil {
		t.Fatalf("scan reservation: %v", err)
	}
	amount, err := decimal.NewFromString(amountStr)
	if err != nil {
		t.Fatalf("parse reservation amount: %v", err)
	}
	consumed, err := decimal.NewFromString(consumedStr)
	if err != nil {
		t.Fatalf("parse reservation consumed: %v", err)
	}
	res.Amount = amount
	res.ConsumedAmount = consumed
	return res
}
