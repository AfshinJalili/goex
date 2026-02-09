package service

import (
	"context"
	"testing"

	"github.com/AfshinJalili/goex/services/ledger/internal/storage"
	ledgerpb "github.com/AfshinJalili/goex/services/ledger/proto/ledger/v1"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"log/slog"
)

type fakeStore struct {
	balance          storage.LedgerAccount
	balanceErr       error
	reservation      *storage.BalanceReservation
	reserveErr       error
	releaseErr       error
	settlementResult *storage.SettlementResult
	settlementErr    error
	lastReq          storage.SettlementRequest
	entries          map[uuid.UUID][]storage.LedgerEntry
}

func (f *fakeStore) GetBalance(ctx context.Context, accountID uuid.UUID, asset string) (storage.LedgerAccount, error) {
	return f.balance, f.balanceErr
}

func (f *fakeStore) ReserveBalance(ctx context.Context, accountID, orderID uuid.UUID, asset string, amount decimal.Decimal) (*storage.BalanceReservation, error) {
	if f.reserveErr != nil {
		return nil, f.reserveErr
	}
	if f.reservation != nil {
		return f.reservation, nil
	}
	return &storage.BalanceReservation{
		ID:             uuid.New(),
		OrderID:        orderID,
		AccountID:      accountID,
		Asset:          asset,
		Amount:         amount,
		ConsumedAmount: decimal.Zero,
		Status:         "active",
	}, nil
}

func (f *fakeStore) ReleaseReservation(ctx context.Context, orderID uuid.UUID) (*storage.BalanceReservation, decimal.Decimal, error) {
	if f.releaseErr != nil {
		return nil, decimal.Zero, f.releaseErr
	}
	res := f.reservation
	if res == nil {
		res = &storage.BalanceReservation{OrderID: orderID, AccountID: uuid.New(), Asset: "USD", Amount: decimal.NewFromInt(1)}
	}
	return res, res.Amount, nil
}

func (f *fakeStore) ApplySettlement(ctx context.Context, req storage.SettlementRequest) (*storage.SettlementResult, error) {
	f.lastReq = req
	return f.settlementResult, f.settlementErr
}

func (f *fakeStore) GetEntriesByReference(ctx context.Context, referenceID uuid.UUID) ([]storage.LedgerEntry, error) {
	if f.entries == nil {
		return nil, nil
	}
	return f.entries[referenceID], nil
}

func TestGetBalanceInvalidAccount(t *testing.T) {
	svc := NewLedgerService(&fakeStore{}, slog.Default(), nil)
	_, err := svc.GetBalance(context.Background(), &ledgerpb.GetBalanceRequest{
		AccountId: "invalid",
		Asset:     "USD",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

func TestGetBalanceSuccess(t *testing.T) {
	accountID := uuid.New()
	store := &fakeStore{
		balance: storage.LedgerAccount{
			AccountID:        accountID,
			Asset:            "USD",
			BalanceAvailable: decimal.NewFromInt(100),
			BalanceLocked:    decimal.NewFromInt(5),
		},
	}

	svc := NewLedgerService(store, slog.Default(), nil)
	resp, err := svc.GetBalance(context.Background(), &ledgerpb.GetBalanceRequest{
		AccountId: accountID.String(),
		Asset:     "USD",
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if resp.Available != "100" {
		t.Fatalf("expected available 100, got %s", resp.Available)
	}
	if resp.Locked != "5" {
		t.Fatalf("expected locked 5, got %s", resp.Locked)
	}
}

func TestReserveBalanceSuccess(t *testing.T) {
	accountID := uuid.New()
	orderID := uuid.New()
	store := &fakeStore{
		balance: storage.LedgerAccount{
			AccountID:        accountID,
			Asset:            "USD",
			BalanceAvailable: decimal.NewFromInt(50),
			BalanceLocked:    decimal.NewFromInt(50),
		},
		reservation: &storage.BalanceReservation{
			ID:             uuid.New(),
			OrderID:        orderID,
			AccountID:      accountID,
			Asset:          "USD",
			Amount:         decimal.NewFromInt(50),
			ConsumedAmount: decimal.Zero,
			Status:         "active",
		},
	}

	svc := NewLedgerService(store, slog.Default(), nil)
	resp, err := svc.ReserveBalance(context.Background(), &ledgerpb.ReserveBalanceRequest{
		AccountId: accountID.String(),
		OrderId:   orderID.String(),
		Asset:     "USD",
		Amount:    "50",
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success true")
	}
	if resp.Available != "50" || resp.Locked != "50" {
		t.Fatalf("unexpected balances %s/%s", resp.Available, resp.Locked)
	}
}

func TestReserveBalanceInsufficient(t *testing.T) {
	accountID := uuid.New()
	store := &fakeStore{reserveErr: storage.ErrInsufficientBalance}
	svc := NewLedgerService(store, slog.Default(), nil)
	_, err := svc.ReserveBalance(context.Background(), &ledgerpb.ReserveBalanceRequest{
		AccountId: accountID.String(),
		OrderId:   uuid.New().String(),
		Asset:     "USD",
		Amount:    "100",
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expected failed precondition, got %v", status.Code(err))
	}
}

func TestReleaseBalanceSuccess(t *testing.T) {
	orderID := uuid.New()
	store := &fakeStore{
		reservation: &storage.BalanceReservation{
			OrderID:   orderID,
			AccountID: uuid.New(),
			Asset:     "USD",
			Amount:    decimal.NewFromInt(10),
		},
	}
	svc := NewLedgerService(store, slog.Default(), nil)
	resp, err := svc.ReleaseBalance(context.Background(), &ledgerpb.ReleaseBalanceRequest{
		OrderId: orderID.String(),
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success true")
	}
	if resp.OrderId != orderID.String() {
		t.Fatalf("expected order id %s got %s", orderID, resp.OrderId)
	}
}

func TestApplySettlementInvalid(t *testing.T) {
	svc := NewLedgerService(&fakeStore{}, slog.Default(), nil)
	_, err := svc.ApplySettlement(context.Background(), &ledgerpb.ApplySettlementRequest{
		TradeId: "invalid",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

func TestApplySettlementSuccess(t *testing.T) {
	accountID := uuid.New()
	result := &storage.SettlementResult{
		EntryIDs: []uuid.UUID{uuid.New(), uuid.New()},
	}
	store := &fakeStore{settlementResult: result}
	svc := NewLedgerService(store, slog.Default(), nil)

	resp, err := svc.ApplySettlement(context.Background(), &ledgerpb.ApplySettlementRequest{
		TradeId:        uuid.New().String(),
		MakerAccountId: accountID.String(),
		TakerAccountId: uuid.New().String(),
		Symbol:         "BTC-USD",
		Price:          "100",
		Quantity:       "1",
		MakerSide:      "buy",
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success true")
	}
	if len(resp.LedgerEntryIds) != 2 {
		t.Fatalf("expected 2 entry IDs, got %d", len(resp.LedgerEntryIds))
	}
}

func TestApplySettlementDuplicateReturnsEntries(t *testing.T) {
	tradeID := uuid.New()
	accountID := uuid.New()
	entryIDs := []uuid.UUID{uuid.New(), uuid.New()}
	entries := []storage.LedgerEntry{
		{ID: entryIDs[0]},
		{ID: entryIDs[1]},
	}

	store := &fakeStore{
		settlementResult: &storage.SettlementResult{AlreadyProcessed: true},
		entries: map[uuid.UUID][]storage.LedgerEntry{
			tradeID: entries,
		},
	}

	svc := NewLedgerService(store, slog.Default(), nil)
	resp, err := svc.ApplySettlement(context.Background(), &ledgerpb.ApplySettlementRequest{
		TradeId:        tradeID.String(),
		MakerAccountId: accountID.String(),
		TakerAccountId: uuid.New().String(),
		Symbol:         "BTC-USD",
		Price:          "100",
		Quantity:       "1",
		MakerSide:      "buy",
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if len(resp.LedgerEntryIds) != len(entryIDs) {
		t.Fatalf("expected %d entry IDs, got %d", len(entryIDs), len(resp.LedgerEntryIds))
	}
}
