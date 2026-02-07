package service

import (
	"context"
	"errors"
	"testing"
	"time"

	ledgerpb "github.com/AfshinJalili/goex/services/ledger/proto/ledger/v1"
	"github.com/AfshinJalili/goex/services/order-ingest/internal/storage"
	riskpb "github.com/AfshinJalili/goex/services/risk/proto/risk/v1"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
)

type fakeStore struct {
	accountID         uuid.UUID
	getClientOrderErr error
	existingOrder     *storage.Order
	createOrder       *storage.Order
	createCreated     bool
	cancelOrder       *storage.Order
}

func (f *fakeStore) GetAccountIDForUser(ctx context.Context, userID uuid.UUID) (uuid.UUID, error) {
	return f.accountID, nil
}

func (f *fakeStore) GetOrderByClientID(ctx context.Context, accountID uuid.UUID, clientOrderID string) (*storage.Order, error) {
	if f.existingOrder != nil {
		return f.existingOrder, nil
	}
	if f.getClientOrderErr != nil {
		return nil, f.getClientOrderErr
	}
	return nil, storage.ErrNotFound
}

func (f *fakeStore) GetOrderByID(ctx context.Context, orderID uuid.UUID) (*storage.Order, error) {
	return nil, storage.ErrNotFound
}

func (f *fakeStore) CreateOrder(ctx context.Context, order storage.Order) (*storage.Order, bool, error) {
	if f.createOrder != nil {
		return f.createOrder, f.createCreated, nil
	}
	return nil, false, errors.New("no order")
}

func (f *fakeStore) ListOrders(ctx context.Context, accountID uuid.UUID, filter storage.OrderFilter) ([]storage.Order, string, error) {
	return nil, "", nil
}

func (f *fakeStore) CancelOrder(ctx context.Context, orderID, accountID uuid.UUID) (*storage.Order, error) {
	if f.cancelOrder != nil {
		return f.cancelOrder, nil
	}
	return nil, storage.ErrNotFound
}

func (f *fakeStore) InsertAudit(ctx context.Context, log storage.AuditLog) error {
	return nil
}

type fakeRisk struct {
	resp *riskpb.PreTradeCheckResponse
	err  error
}

func (f *fakeRisk) PreTradeCheck(ctx context.Context, in *riskpb.PreTradeCheckRequest, opts ...grpc.CallOption) (*riskpb.PreTradeCheckResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

type recordProducer struct {
	published []string
}

func (r *recordProducer) PublishJSON(ctx context.Context, topic, key string, value any) (int32, int64, error) {
	r.published = append(r.published, topic)
	return 0, 0, nil
}

func (r *recordProducer) Close() error { return nil }

type fakeLedger struct {
	available string
	err       error
}

func (f *fakeLedger) GetBalance(ctx context.Context, in *ledgerpb.GetBalanceRequest, opts ...grpc.CallOption) (*ledgerpb.GetBalanceResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &ledgerpb.GetBalanceResponse{
		AccountId: in.GetAccountId(),
		Asset:     in.GetAsset(),
		Available: f.available,
		Locked:    "0",
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func TestSubmitOrderAccepted(t *testing.T) {
	accountID := uuid.New()
	orderID := uuid.New()
	createdAt := time.Now().UTC()

	store := &fakeStore{
		accountID: accountID,
		createOrder: &storage.Order{
			ID:             orderID,
			ClientOrderID:  "client-1",
			AccountID:      accountID,
			Symbol:         "BTC-USD",
			Side:           "buy",
			Type:           "limit",
			Quantity:       decimal.NewFromInt(1),
			FilledQuantity: decimal.Zero,
			Status:         storage.OrderStatusPending,
			TimeInForce:    "GTC",
			CreatedAt:      createdAt,
			UpdatedAt:      createdAt,
		},
		createCreated: true,
	}

	risk := &fakeRisk{resp: &riskpb.PreTradeCheckResponse{Allowed: true}}
	producer := &recordProducer{}

	svc := NewOrderService(store, risk, nil, producer, nil, nil, Topics{
		OrdersAccepted:  "orders.accepted",
		OrdersRejected:  "orders.rejected",
		OrdersCancelled: "orders.cancelled",
	})

	res, err := svc.SubmitOrder(context.Background(), SubmitOrderInput{
		UserID:      uuid.New(),
		Symbol:      "BTC-USD",
		Side:        "buy",
		OrderType:   "limit",
		TimeInForce: "GTC",
		Quantity:    decimal.NewFromInt(1),
		Price:       decimalPtr("100"),
	})
	if err != nil {
		t.Fatalf("SubmitOrder: %v", err)
	}
	if res.Status != "accepted" {
		t.Fatalf("expected accepted, got %s", res.Status)
	}
	if len(producer.published) != 1 || producer.published[0] != "orders.accepted" {
		t.Fatalf("expected orders.accepted publish")
	}
}

func TestSubmitOrderRejected(t *testing.T) {
	accountID := uuid.New()
	orderID := uuid.New()
	createdAt := time.Now().UTC()

	store := &fakeStore{
		accountID: accountID,
		createOrder: &storage.Order{
			ID:             orderID,
			ClientOrderID:  "client-2",
			AccountID:      accountID,
			Symbol:         "BTC-USD",
			Side:           "buy",
			Type:           "limit",
			Quantity:       decimal.NewFromInt(1),
			FilledQuantity: decimal.Zero,
			Status:         storage.OrderStatusRejected,
			TimeInForce:    "GTC",
			CreatedAt:      createdAt,
			UpdatedAt:      createdAt,
		},
		createCreated: true,
	}

	risk := &fakeRisk{resp: &riskpb.PreTradeCheckResponse{Allowed: false, Reasons: []string{"insufficient_balance"}}}
	producer := &recordProducer{}

	svc := NewOrderService(store, risk, nil, producer, nil, nil, Topics{
		OrdersAccepted:  "orders.accepted",
		OrdersRejected:  "orders.rejected",
		OrdersCancelled: "orders.cancelled",
	})

	res, err := svc.SubmitOrder(context.Background(), SubmitOrderInput{
		UserID:      uuid.New(),
		Symbol:      "BTC-USD",
		Side:        "buy",
		OrderType:   "limit",
		TimeInForce: "GTC",
		Quantity:    decimal.NewFromInt(1),
		Price:       decimalPtr("100"),
	})
	if err != nil {
		t.Fatalf("SubmitOrder: %v", err)
	}
	if res.Status != "rejected" {
		t.Fatalf("expected rejected, got %s", res.Status)
	}
	if len(producer.published) != 1 || producer.published[0] != "orders.rejected" {
		t.Fatalf("expected orders.rejected publish")
	}
}

func TestSubmitOrderExisting(t *testing.T) {
	accountID := uuid.New()
	createdAt := time.Now().UTC()

	existing := &storage.Order{
		ID:             uuid.New(),
		ClientOrderID:  "client-3",
		AccountID:      accountID,
		Symbol:         "BTC-USD",
		Side:           "buy",
		Type:           "limit",
		Quantity:       decimal.NewFromInt(1),
		FilledQuantity: decimal.Zero,
		Status:         storage.OrderStatusPending,
		TimeInForce:    "GTC",
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
	}

	store := &fakeStore{accountID: accountID, existingOrder: existing}
	risk := &fakeRisk{resp: &riskpb.PreTradeCheckResponse{Allowed: true}}
	producer := &recordProducer{}

	svc := NewOrderService(store, risk, nil, producer, nil, nil, Topics{
		OrdersAccepted:  "orders.accepted",
		OrdersRejected:  "orders.rejected",
		OrdersCancelled: "orders.cancelled",
	})

	res, err := svc.SubmitOrder(context.Background(), SubmitOrderInput{
		UserID:        uuid.New(),
		ClientOrderID: "client-3",
		Symbol:        "BTC-USD",
		Side:          "buy",
		OrderType:     "limit",
		TimeInForce:   "GTC",
		Quantity:      decimal.NewFromInt(1),
		Price:         decimalPtr("100"),
	})
	if err != nil {
		t.Fatalf("SubmitOrder: %v", err)
	}
	if !res.Existing {
		t.Fatalf("expected existing")
	}
	if len(producer.published) != 0 {
		t.Fatalf("expected no publish for existing order")
	}
}

func TestCancelOrderPublishesEvent(t *testing.T) {
	accountID := uuid.New()
	orderID := uuid.New()
	updatedAt := time.Now().UTC()

	store := &fakeStore{
		accountID: accountID,
		cancelOrder: &storage.Order{
			ID:             orderID,
			ClientOrderID:  "client-4",
			AccountID:      accountID,
			Symbol:         "BTC-USD",
			Side:           "buy",
			Type:           "limit",
			Quantity:       decimal.NewFromInt(1),
			FilledQuantity: decimal.Zero,
			Status:         storage.OrderStatusCancelled,
			TimeInForce:    "GTC",
			UpdatedAt:      updatedAt,
		},
	}

	svc := NewOrderService(store, &fakeRisk{resp: &riskpb.PreTradeCheckResponse{Allowed: true}}, nil, &recordProducer{}, nil, nil, Topics{
		OrdersAccepted:  "orders.accepted",
		OrdersRejected:  "orders.rejected",
		OrdersCancelled: "orders.cancelled",
	})

	order, err := svc.CancelOrder(context.Background(), CancelOrderInput{UserID: uuid.New(), OrderID: orderID})
	if err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
	if order.Status != storage.OrderStatusCancelled {
		t.Fatalf("expected cancelled")
	}
}

func TestSubmitOrderLedgerInsufficient(t *testing.T) {
	accountID := uuid.New()
	orderID := uuid.New()
	createdAt := time.Now().UTC()

	store := &fakeStore{
		accountID: accountID,
		createOrder: &storage.Order{
			ID:             orderID,
			ClientOrderID:  "client-ledger-1",
			AccountID:      accountID,
			Symbol:         "BTC-USD",
			Side:           "buy",
			Type:           "limit",
			Quantity:       decimal.NewFromInt(1),
			FilledQuantity: decimal.Zero,
			Status:         storage.OrderStatusRejected,
			TimeInForce:    "GTC",
			CreatedAt:      createdAt,
			UpdatedAt:      createdAt,
		},
		createCreated: true,
	}

	risk := &fakeRisk{resp: &riskpb.PreTradeCheckResponse{Allowed: true}}
	ledger := &fakeLedger{available: "0"}
	producer := &recordProducer{}

	svc := NewOrderService(store, risk, ledger, producer, nil, nil, Topics{
		OrdersAccepted:  "orders.accepted",
		OrdersRejected:  "orders.rejected",
		OrdersCancelled: "orders.cancelled",
	})

	res, err := svc.SubmitOrder(context.Background(), SubmitOrderInput{
		UserID:      uuid.New(),
		Symbol:      "BTC-USD",
		Side:        "buy",
		OrderType:   "limit",
		TimeInForce: "GTC",
		Quantity:    decimal.NewFromInt(1),
		Price:       decimalPtr("100"),
	})
	if err != nil {
		t.Fatalf("SubmitOrder: %v", err)
	}
	if res.Status != "rejected" {
		t.Fatalf("expected rejected, got %s", res.Status)
	}
	if len(res.Reasons) == 0 || res.Reasons[0] != "insufficient_balance" {
		t.Fatalf("expected insufficient_balance, got %v", res.Reasons)
	}
	if len(producer.published) != 1 || producer.published[0] != "orders.rejected" {
		t.Fatalf("expected orders.rejected publish")
	}
}

func decimalPtr(value string) *decimal.Decimal {
	v, _ := decimal.NewFromString(value)
	return &v
}
