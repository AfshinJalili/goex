package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/AfshinJalili/goex/services/risk/internal/storage"
	riskpb "github.com/AfshinJalili/goex/services/risk/proto/risk/v1"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"log/slog"
)

type fakeStore struct {
	account        *storage.AccountInfo
	accountErr     error
	market         *storage.Market
	marketErr      error
	balance        *storage.BalanceCheck
	balanceErr     error
	marketCalls    int
	balanceCalls   int
	lastTradePrice decimal.Decimal
	lastTradeErr   error
}

func (f *fakeStore) GetAccountInfo(ctx context.Context, accountID uuid.UUID) (*storage.AccountInfo, error) {
	return f.account, f.accountErr
}

func (f *fakeStore) GetMarketBySymbol(ctx context.Context, symbol string) (*storage.Market, error) {
	f.marketCalls++
	return f.market, f.marketErr
}

func (f *fakeStore) CheckBalance(ctx context.Context, accountID uuid.UUID, asset string, required decimal.Decimal) (*storage.BalanceCheck, error) {
	f.balanceCalls++
	return f.balance, f.balanceErr
}

func (f *fakeStore) GetLastTradePrice(ctx context.Context, symbol string) (decimal.Decimal, error) {
	if f.lastTradeErr != nil {
		return decimal.Zero, f.lastTradeErr
	}
	if f.lastTradePrice.IsZero() {
		return decimal.Zero, storage.ErrNotFound
	}
	return f.lastTradePrice, nil
}

type fakeCache struct {
	market *storage.Market
	hit    bool
}

func (f *fakeCache) GetMarket(symbol string) (*storage.Market, bool) {
	return f.market, f.hit
}

func TestPreTradeCheckScenarios(t *testing.T) {
	accountID := uuid.New()
	activeAccount := &storage.AccountInfo{ID: accountID, UserID: uuid.New(), Status: "active", KYCLevel: "verified"}
	activeMarket := &storage.Market{ID: uuid.New(), Symbol: "BTC-USD", BaseAsset: "BTC", QuoteAsset: "USD", Status: "active"}

	tests := []struct {
		name          string
		store         *fakeStore
		cache         *fakeCache
		req           *riskpb.PreTradeCheckRequest
		expectAllowed bool
		expectReason  string
		expectCode    codes.Code
		balanceCalls  int
	}{
		{
			name: "valid limit buy",
			store: &fakeStore{
				account: activeAccount,
				market:  activeMarket,
				balance: &storage.BalanceCheck{Sufficient: true},
			},
			cache:         &fakeCache{market: activeMarket, hit: true},
			req:           validRequest(accountID),
			expectAllowed: true,
			expectCode:    codes.OK,
			balanceCalls:  1,
		},
		{
			name: "market buy without price uses reference",
			store: &fakeStore{
				account:        activeAccount,
				market:         activeMarket,
				balance:        &storage.BalanceCheck{Sufficient: true},
				lastTradePrice: decimal.RequireFromString("100"),
			},
			cache:         &fakeCache{market: activeMarket, hit: true},
			req:           marketRequest(accountID),
			expectAllowed: true,
			expectCode:    codes.OK,
			balanceCalls:  1,
		},
		{
			name: "market buy without price missing reference",
			store: &fakeStore{
				account:      activeAccount,
				market:       activeMarket,
				balance:      &storage.BalanceCheck{Sufficient: true},
				lastTradeErr: storage.ErrNotFound,
			},
			cache:         &fakeCache{market: activeMarket, hit: true},
			req:           marketRequest(accountID),
			expectAllowed: false,
			expectReason:  "market_price_unavailable",
			expectCode:    codes.OK,
			balanceCalls:  0,
		},
		{
			name: "valid market buy with price",
			store: &fakeStore{
				account:        activeAccount,
				market:         activeMarket,
				balance:        &storage.BalanceCheck{Sufficient: true},
				lastTradePrice: decimal.RequireFromString("100"),
			},
			cache:         &fakeCache{market: activeMarket, hit: true},
			req:           marketRequestWithPrice(accountID),
			expectAllowed: true,
			expectCode:    codes.OK,
			balanceCalls:  1,
		},
		{
			name: "inactive account",
			store: &fakeStore{
				account: &storage.AccountInfo{ID: accountID, UserID: uuid.New(), Status: "suspended", KYCLevel: "verified"},
				market:  activeMarket,
				balance: &storage.BalanceCheck{Sufficient: true},
			},
			cache:         &fakeCache{market: activeMarket, hit: true},
			req:           validRequest(accountID),
			expectAllowed: false,
			expectReason:  "account_inactive",
			expectCode:    codes.OK,
			balanceCalls:  0,
		},
		{
			name: "kyc insufficient",
			store: &fakeStore{
				account: &storage.AccountInfo{ID: accountID, UserID: uuid.New(), Status: "active", KYCLevel: "none"},
				market:  activeMarket,
				balance: &storage.BalanceCheck{Sufficient: true},
			},
			cache:         &fakeCache{market: activeMarket, hit: true},
			req:           validRequest(accountID),
			expectAllowed: false,
			expectReason:  "kyc_insufficient",
			expectCode:    codes.OK,
			balanceCalls:  0,
		},
		{
			name: "market inactive",
			store: &fakeStore{
				account: activeAccount,
				market:  &storage.Market{ID: uuid.New(), Symbol: "BTC-USD", BaseAsset: "BTC", QuoteAsset: "USD", Status: "halted"},
				balance: &storage.BalanceCheck{Sufficient: true},
			},
			cache:         &fakeCache{market: &storage.Market{ID: uuid.New(), Symbol: "BTC-USD", BaseAsset: "BTC", QuoteAsset: "USD", Status: "halted"}, hit: true},
			req:           validRequest(accountID),
			expectAllowed: false,
			expectReason:  "market_inactive",
			expectCode:    codes.OK,
			balanceCalls:  0,
		},
		{
			name: "market not found",
			store: &fakeStore{
				account:   activeAccount,
				marketErr: storage.ErrNotFound,
			},
			cache:         &fakeCache{hit: false},
			req:           validRequest(accountID),
			expectAllowed: false,
			expectReason:  "market_not_found",
			expectCode:    codes.OK,
			balanceCalls:  0,
		},
		{
			name: "insufficient balance",
			store: &fakeStore{
				account: activeAccount,
				market:  activeMarket,
				balance: &storage.BalanceCheck{Sufficient: false, Asset: "USD", Available: decimal.NewFromInt(10)},
			},
			cache:         &fakeCache{market: activeMarket, hit: true},
			req:           validRequest(accountID),
			expectAllowed: false,
			expectReason:  "insufficient_balance",
			expectCode:    codes.OK,
			balanceCalls:  1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			svc := NewRiskService(test.store, test.cache, slog.Default(), nil, 50)
			resp, err := svc.PreTradeCheck(context.Background(), test.req)
			if test.expectCode != codes.OK {
				if status.Code(err) != test.expectCode {
					t.Fatalf("expected code %v, got %v", test.expectCode, status.Code(err))
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.Allowed != test.expectAllowed {
				t.Fatalf("expected allowed=%v, got %v", test.expectAllowed, resp.Allowed)
			}
			if !test.expectAllowed {
				found := false
				for _, reason := range resp.Reasons {
					if reason == test.expectReason {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected reason %s", test.expectReason)
				}
			}
			if test.store.balanceCalls != test.balanceCalls {
				t.Fatalf("expected balance calls %d, got %d", test.balanceCalls, test.store.balanceCalls)
			}
		})
	}
}

func TestPreTradeCheckInvalidInput(t *testing.T) {
	svc := NewRiskService(&fakeStore{}, &fakeCache{}, slog.Default(), nil, 50)
	_, err := svc.PreTradeCheck(context.Background(), &riskpb.PreTradeCheckRequest{
		AccountId: "invalid",
		Symbol:    "BTC-USD",
		Side:      "buy",
		OrderType: "limit",
		Quantity:  "1",
		Price:     "100",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected invalid argument, got %v", status.Code(err))
	}
}

func TestPreTradeCheckCacheMissCallsStore(t *testing.T) {
	accountID := uuid.New()
	store := &fakeStore{
		account: &storage.AccountInfo{ID: accountID, UserID: uuid.New(), Status: "active", KYCLevel: "verified"},
		market:  &storage.Market{ID: uuid.New(), Symbol: "BTC-USD", BaseAsset: "BTC", QuoteAsset: "USD", Status: "active"},
		balance: &storage.BalanceCheck{Sufficient: true},
	}
	cache := &fakeCache{hit: false}
	svc := NewRiskService(store, cache, slog.Default(), nil, 50)

	_, err := svc.PreTradeCheck(context.Background(), validRequest(accountID))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.marketCalls == 0 {
		t.Fatalf("expected market lookup on cache miss")
	}
}

func TestPreTradeCheckCircuitBreaker(t *testing.T) {
	accountID := uuid.New()
	activeAccount := &storage.AccountInfo{ID: accountID, UserID: uuid.New(), Status: "active", KYCLevel: "verified"}
	activeMarket := &storage.Market{ID: uuid.New(), Symbol: "BTC-USD", BaseAsset: "BTC", QuoteAsset: "USD", Status: "active"}

	store := &fakeStore{
		account:    activeAccount,
		market:     activeMarket,
		balanceErr: errors.New("ledger down"),
	}
	cache := &fakeCache{market: activeMarket, hit: true}
	svc := NewRiskService(store, cache, slog.Default(), nil, 50)
	svc.balanceBreaker = newCircuitBreaker(1, time.Minute)

	_, err := svc.PreTradeCheck(context.Background(), validRequest(accountID))
	if status.Code(err) != codes.Internal {
		t.Fatalf("expected internal on first failure, got %v", status.Code(err))
	}

	_, err = svc.PreTradeCheck(context.Background(), validRequest(accountID))
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("expected unavailable after breaker opens, got %v", status.Code(err))
	}
	if store.balanceCalls != 1 {
		t.Fatalf("expected one balance call, got %d", store.balanceCalls)
	}
}

func TestPreTradeCheckConcurrent(t *testing.T) {
	accountID := uuid.New()
	store := &fakeStore{
		account: &storage.AccountInfo{ID: accountID, UserID: uuid.New(), Status: "active", KYCLevel: "verified"},
		market:  &storage.Market{ID: uuid.New(), Symbol: "BTC-USD", BaseAsset: "BTC", QuoteAsset: "USD", Status: "active"},
		balance: &storage.BalanceCheck{Sufficient: true},
	}
	cache := &fakeCache{market: store.market, hit: true}
	svc := NewRiskService(store, cache, slog.Default(), nil, 50)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.PreTradeCheck(context.Background(), validRequest(accountID)); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
}

func validRequest(accountID uuid.UUID) *riskpb.PreTradeCheckRequest {
	return &riskpb.PreTradeCheckRequest{
		AccountId: accountID.String(),
		Symbol:    "BTC-USD",
		Side:      "buy",
		OrderType: "limit",
		Quantity:  "1",
		Price:     "100",
	}
}

func marketRequest(accountID uuid.UUID) *riskpb.PreTradeCheckRequest {
	return &riskpb.PreTradeCheckRequest{
		AccountId: accountID.String(),
		Symbol:    "BTC-USD",
		Side:      "buy",
		OrderType: "market",
		Quantity:  "1",
		Price:     "",
	}
}

func marketRequestWithPrice(accountID uuid.UUID) *riskpb.PreTradeCheckRequest {
	return &riskpb.PreTradeCheckRequest{
		AccountId: accountID.String(),
		Symbol:    "BTC-USD",
		Side:      "buy",
		OrderType: "market",
		Quantity:  "1",
		Price:     "100",
	}
}
