package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AfshinJalili/goex/services/order-ingest/internal/service"
	"github.com/AfshinJalili/goex/services/order-ingest/internal/storage"
	"github.com/AfshinJalili/goex/services/testutil"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type fakeService struct {
	result *service.SubmitOrderResult
	err    error
	last   *service.SubmitOrderInput
}

func (f *fakeService) SubmitOrder(ctx context.Context, input service.SubmitOrderInput) (*service.SubmitOrderResult, error) {
	f.last = &input
	return f.result, f.err
}

func (f *fakeService) CancelOrder(ctx context.Context, input service.CancelOrderInput) (*storage.Order, error) {
	return nil, nil
}

func (f *fakeService) ListOrders(ctx context.Context, input service.ListOrdersInput) ([]storage.Order, string, error) {
	return nil, "", nil
}

func (f *fakeService) GetOrder(ctx context.Context, input service.GetOrderInput) (*storage.Order, error) {
	return nil, nil
}

func TestCreateOrderUnauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := New(&fakeService{}, nil)
	h.Register(router, []byte("secret"))

	resp := testutil.MakeAPIRequest(router, http.MethodPost, "/orders", map[string]string{"symbol": "BTC-USD"})
	testutil.AssertErrorCode(t, resp, testutil.ErrorCodeUnauthorized)
}

func TestCreateOrderAccepted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	orderID := uuid.New()
	createdAt := time.Now().UTC()

	svc := &fakeService{result: &service.SubmitOrderResult{
		Order: &storage.Order{
			ID:             orderID,
			ClientOrderID:  "client-1",
			AccountID:      uuid.New(),
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
		Status: "accepted",
	}}

	h := New(svc, nil)
	h.Register(router, []byte("secret"))

	jwt, err := testutil.GenerateJWT(testutil.DemoUserID, []byte("secret"), time.Hour, time.Now())
	if err != nil {
		t.Fatalf("jwt: %v", err)
	}

	resp := testutil.MakeAuthRequest(router, http.MethodPost, "/orders", map[string]string{
		"symbol":        "BTC-USD",
		"side":          "buy",
		"type":          "limit",
		"price":         "100",
		"quantity":      "1",
		"time_in_force": "GTC",
	}, jwt)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
}

func TestCreateOrderIdempotencyHeaderPrecedence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	svc := &fakeService{result: &service.SubmitOrderResult{
		Order: &storage.Order{
			ID:             uuid.New(),
			ClientOrderID:  "header-key",
			AccountID:      uuid.New(),
			Symbol:         "BTC-USD",
			Side:           "buy",
			Type:           "limit",
			Quantity:       decimal.NewFromInt(1),
			FilledQuantity: decimal.Zero,
			Status:         storage.OrderStatusPending,
			TimeInForce:    "GTC",
			CreatedAt:      time.Now().UTC(),
			UpdatedAt:      time.Now().UTC(),
		},
		Status: "accepted",
	}}

	h := New(svc, nil)
	h.Register(router, []byte("secret"))

	jwt, err := testutil.GenerateJWT(testutil.DemoUserID, []byte("secret"), time.Hour, time.Now())
	if err != nil {
		t.Fatalf("jwt: %v", err)
	}

	payload, _ := json.Marshal(map[string]string{
		"symbol":          "BTC-USD",
		"side":            "buy",
		"type":            "limit",
		"price":           "100",
		"quantity":        "1",
		"time_in_force":   "GTC",
		"client_order_id": "body-key",
	})
	req := httptest.NewRequest(http.MethodPost, "/orders", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Idempotency-Key", "header-key")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if svc.last == nil {
		t.Fatalf("expected SubmitOrder to be called")
	}
	if svc.last.ClientOrderID != "header-key" {
		t.Fatalf("expected header idempotency key to win, got %s", svc.last.ClientOrderID)
	}
}
