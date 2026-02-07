package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AfshinJalili/goex/libs/kafka"
	ledgerpb "github.com/AfshinJalili/goex/services/ledger/proto/ledger/v1"
	"github.com/AfshinJalili/goex/services/order-ingest/internal/storage"
	riskpb "github.com/AfshinJalili/goex/services/risk/proto/risk/v1"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
	"log/slog"
)

const (
	statusAccepted = "accepted"
	statusRejected = "rejected"
)

var (
	ErrAccountNotFound = errors.New("account not found")
)

type Topics struct {
	OrdersAccepted  string
	OrdersRejected  string
	OrdersCancelled string
}

type OrderStore interface {
	GetAccountIDForUser(ctx context.Context, userID uuid.UUID) (uuid.UUID, error)
	GetOrderByClientID(ctx context.Context, accountID uuid.UUID, clientOrderID string) (*storage.Order, error)
	GetOrderByID(ctx context.Context, orderID uuid.UUID) (*storage.Order, error)
	CreateOrder(ctx context.Context, order storage.Order) (*storage.Order, bool, error)
	ListOrders(ctx context.Context, accountID uuid.UUID, filter storage.OrderFilter) ([]storage.Order, string, error)
	CancelOrder(ctx context.Context, orderID, accountID uuid.UUID) (*storage.Order, error)
	InsertAudit(ctx context.Context, log storage.AuditLog) error
}

type RiskClient interface {
	PreTradeCheck(ctx context.Context, in *riskpb.PreTradeCheckRequest, opts ...grpc.CallOption) (*riskpb.PreTradeCheckResponse, error)
}

type LedgerClient interface {
	GetBalance(ctx context.Context, in *ledgerpb.GetBalanceRequest, opts ...grpc.CallOption) (*ledgerpb.GetBalanceResponse, error)
}

type OrderService struct {
	store    OrderStore
	risk     RiskClient
	ledger   LedgerClient
	producer kafka.Publisher
	logger   *slog.Logger
	metrics  *Metrics
	topics   Topics
}

type SubmitOrderInput struct {
	UserID        uuid.UUID
	ClientOrderID string
	Symbol        string
	Side          string
	OrderType     string
	TimeInForce   string
	Quantity      decimal.Decimal
	Price         *decimal.Decimal
	IP            string
	UserAgent     string
	CorrelationID string
}

type SubmitOrderResult struct {
	Order    *storage.Order
	Status   string
	Reasons  []string
	Details  map[string]string
	Existing bool
}

type CancelOrderInput struct {
	UserID        uuid.UUID
	OrderID       uuid.UUID
	IP            string
	UserAgent     string
	CorrelationID string
}

type ListOrdersInput struct {
	UserID uuid.UUID
	Filter storage.OrderFilter
}

type GetOrderInput struct {
	UserID  uuid.UUID
	OrderID uuid.UUID
}

func NewOrderService(store OrderStore, risk RiskClient, ledger LedgerClient, producer kafka.Publisher, logger *slog.Logger, metrics *Metrics, topics Topics) *OrderService {
	if logger == nil {
		logger = slog.Default()
	}
	return &OrderService{
		store:    store,
		risk:     risk,
		ledger:   ledger,
		producer: producer,
		logger:   logger,
		metrics:  metrics,
		topics:   topics,
	}
}

func (s *OrderService) SubmitOrder(ctx context.Context, input SubmitOrderInput) (*SubmitOrderResult, error) {
	start := time.Now()
	if s.risk == nil {
		return nil, errors.New("risk client not configured")
	}
	accountID, err := s.store.GetAccountIDForUser(ctx, input.UserID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, ErrAccountNotFound
		}
		return nil, err
	}

	clientOrderID := strings.TrimSpace(input.ClientOrderID)
	if clientOrderID != "" {
		existing, err := s.store.GetOrderByClientID(ctx, accountID, clientOrderID)
		if err == nil {
			if s.metrics != nil {
				label := responseStatus(existing.Status)
				s.metrics.OrderSubmissions.WithLabelValues(label).Inc()
				s.metrics.OrderSubmissionLatency.WithLabelValues(label).Observe(time.Since(start).Seconds())
			}
			return &SubmitOrderResult{
				Order:    existing,
				Status:   responseStatus(existing.Status),
				Existing: true,
			}, nil
		}
		if !errors.Is(err, storage.ErrNotFound) {
			return nil, err
		}
	}
	if clientOrderID == "" {
		clientOrderID = uuid.NewString()
	}

	priceStr := ""
	if input.Price != nil {
		priceStr = input.Price.String()
	}

	riskStatus := "error"
	riskStart := time.Now()
	riskCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	riskResp, err := s.risk.PreTradeCheck(riskCtx, &riskpb.PreTradeCheckRequest{
		AccountId: accountID.String(),
		Symbol:    input.Symbol,
		Side:      input.Side,
		OrderType: input.OrderType,
		Quantity:  input.Quantity.String(),
		Price:     priceStr,
	})
	if err == nil {
		if riskResp.Allowed {
			riskStatus = "allowed"
		} else {
			riskStatus = "denied"
		}
	}
	if s.metrics != nil {
		s.metrics.RiskCheckDuration.WithLabelValues(riskStatus).Observe(time.Since(riskStart).Seconds())
	}
	if err != nil {
		if s.metrics != nil {
			s.metrics.OrderSubmissions.WithLabelValues("error").Inc()
			s.metrics.OrderSubmissionLatency.WithLabelValues("error").Observe(time.Since(start).Seconds())
		}
		return nil, err
	}

	orderPrice := input.Price
	if input.OrderType == "market" {
		orderPrice = nil
	}

	order := storage.Order{
		ClientOrderID:  clientOrderID,
		AccountID:      accountID,
		Symbol:         input.Symbol,
		Side:           input.Side,
		Type:           input.OrderType,
		Price:          orderPrice,
		Quantity:       input.Quantity,
		FilledQuantity: decimal.Zero,
		TimeInForce:    input.TimeInForce,
	}

	if !riskResp.Allowed {
		order.Status = storage.OrderStatusRejected
		stored, created, err := s.store.CreateOrder(ctx, order)
		if err != nil {
			if s.metrics != nil {
				s.metrics.OrderSubmissions.WithLabelValues("error").Inc()
				s.metrics.OrderSubmissionLatency.WithLabelValues("error").Observe(time.Since(start).Seconds())
			}
			return nil, err
		}
		if created {
			s.publishOrderRejected(ctx, input.CorrelationID, stored, riskResp.Reasons)
			s.insertAudit(ctx, input.UserID, "orders.reject", "order", stored.ID, input)
		}
		if s.metrics != nil {
			s.metrics.OrderSubmissions.WithLabelValues(statusRejected).Inc()
			s.metrics.OrderSubmissionLatency.WithLabelValues(statusRejected).Observe(time.Since(start).Seconds())
		}
		return &SubmitOrderResult{
			Order:   stored,
			Status:  statusRejected,
			Reasons: riskResp.Reasons,
			Details: riskResp.Details,
		}, nil
	}

	ledgerOK, ledgerDetails, err := s.checkLedgerBalance(ctx, accountID, input.Side, input.Symbol, input.Quantity, input.Price)
	if err != nil {
		if s.metrics != nil {
			s.metrics.OrderSubmissions.WithLabelValues("error").Inc()
			s.metrics.OrderSubmissionLatency.WithLabelValues("error").Observe(time.Since(start).Seconds())
		}
		return nil, err
	}
	if !ledgerOK {
		order.Status = storage.OrderStatusRejected
		stored, created, err := s.store.CreateOrder(ctx, order)
		if err != nil {
			if s.metrics != nil {
				s.metrics.OrderSubmissions.WithLabelValues("error").Inc()
				s.metrics.OrderSubmissionLatency.WithLabelValues("error").Observe(time.Since(start).Seconds())
			}
			return nil, err
		}
		if created {
			s.publishOrderRejected(ctx, input.CorrelationID, stored, []string{"insufficient_balance"})
			s.insertAudit(ctx, input.UserID, "orders.reject", "order", stored.ID, input)
		}
		if s.metrics != nil {
			s.metrics.OrderSubmissions.WithLabelValues(statusRejected).Inc()
			s.metrics.OrderSubmissionLatency.WithLabelValues(statusRejected).Observe(time.Since(start).Seconds())
		}
		return &SubmitOrderResult{
			Order:   stored,
			Status:  statusRejected,
			Reasons: []string{"insufficient_balance"},
			Details: ledgerDetails,
		}, nil
	}

	order.Status = storage.OrderStatusPending
	stored, created, err := s.store.CreateOrder(ctx, order)
	if err != nil {
		if s.metrics != nil {
			s.metrics.OrderSubmissions.WithLabelValues("error").Inc()
			s.metrics.OrderSubmissionLatency.WithLabelValues("error").Observe(time.Since(start).Seconds())
		}
		return nil, err
	}
	if created {
		s.publishOrderAccepted(ctx, input.CorrelationID, stored)
		s.insertAudit(ctx, input.UserID, "orders.create", "order", stored.ID, input)
	}
	if s.metrics != nil {
		s.metrics.OrderSubmissions.WithLabelValues(statusAccepted).Inc()
		s.metrics.OrderSubmissionLatency.WithLabelValues(statusAccepted).Observe(time.Since(start).Seconds())
	}
	return &SubmitOrderResult{
		Order:  stored,
		Status: statusAccepted,
	}, nil
}

func (s *OrderService) CancelOrder(ctx context.Context, input CancelOrderInput) (*storage.Order, error) {
	accountID, err := s.store.GetAccountIDForUser(ctx, input.UserID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			if s.metrics != nil {
				s.metrics.OrderCancellations.WithLabelValues("forbidden").Inc()
			}
			return nil, ErrAccountNotFound
		}
		if s.metrics != nil {
			s.metrics.OrderCancellations.WithLabelValues("error").Inc()
		}
		return nil, err
	}

	order, err := s.store.CancelOrder(ctx, input.OrderID, accountID)
	if err != nil {
		if s.metrics != nil {
			s.metrics.OrderCancellations.WithLabelValues("error").Inc()
		}
		return nil, err
	}

	s.publishOrderCancelled(ctx, input.CorrelationID, order)
	s.insertAudit(ctx, input.UserID, "orders.cancel", "order", order.ID, input)
	if s.metrics != nil {
		s.metrics.OrderCancellations.WithLabelValues("success").Inc()
	}

	return order, nil
}

func (s *OrderService) ListOrders(ctx context.Context, input ListOrdersInput) ([]storage.Order, string, error) {
	accountID, err := s.store.GetAccountIDForUser(ctx, input.UserID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, "", ErrAccountNotFound
		}
		return nil, "", err
	}
	return s.store.ListOrders(ctx, accountID, input.Filter)
}

func (s *OrderService) GetOrder(ctx context.Context, input GetOrderInput) (*storage.Order, error) {
	accountID, err := s.store.GetAccountIDForUser(ctx, input.UserID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, ErrAccountNotFound
		}
		return nil, err
	}
	order, err := s.store.GetOrderByID(ctx, input.OrderID)
	if err != nil {
		return nil, err
	}
	if order.AccountID != accountID {
		return nil, storage.ErrNotFound
	}
	return order, nil
}

func (s *OrderService) publishOrderAccepted(ctx context.Context, correlationID string, order *storage.Order) {
	if s.producer == nil || order == nil {
		return
	}
	eventID := kafka.DeterministicEventID("orders.accepted", order.ID.String())
	env, err := kafka.NewEnvelopeWithID(eventID, "orders.accepted", 1, correlationID)
	if err != nil {
		s.logger.Error("build order accepted envelope failed", "error", err)
		return
	}

	payload := OrderAcceptedEvent{
		Envelope:      env,
		OrderID:       order.ID.String(),
		ClientOrderID: order.ClientOrderID,
		AccountID:     order.AccountID.String(),
		Symbol:        order.Symbol,
		Side:          order.Side,
		Type:          order.Type,
		Price:         optionalDecimal(order.Price),
		Quantity:      order.Quantity.String(),
		TimeInForce:   order.TimeInForce,
		Status:        order.Status,
		CreatedAt:     order.CreatedAt.UTC().Format(time.RFC3339),
	}

	if _, _, err := s.producer.PublishJSON(ctx, s.topics.OrdersAccepted, order.Symbol, payload); err != nil {
		s.logger.Error("publish order accepted failed", "error", err)
	}
}

func (s *OrderService) publishOrderRejected(ctx context.Context, correlationID string, order *storage.Order, reasons []string) {
	if s.producer == nil || order == nil {
		return
	}
	eventID := kafka.DeterministicEventID("orders.rejected", order.ID.String())
	env, err := kafka.NewEnvelopeWithID(eventID, "orders.rejected", 1, correlationID)
	if err != nil {
		s.logger.Error("build order rejected envelope failed", "error", err)
		return
	}
	payload := OrderRejectedEvent{
		Envelope:      env,
		OrderID:       order.ID.String(),
		ClientOrderID: order.ClientOrderID,
		AccountID:     order.AccountID.String(),
		Symbol:        order.Symbol,
		Side:          order.Side,
		Type:          order.Type,
		Price:         optionalDecimal(order.Price),
		Quantity:      order.Quantity.String(),
		TimeInForce:   order.TimeInForce,
		Status:        order.Status,
		Reasons:       reasons,
		RejectedAt:    order.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if _, _, err := s.producer.PublishJSON(ctx, s.topics.OrdersRejected, order.Symbol, payload); err != nil {
		s.logger.Error("publish order rejected failed", "error", err)
	}
}

func (s *OrderService) publishOrderCancelled(ctx context.Context, correlationID string, order *storage.Order) {
	if s.producer == nil || order == nil {
		return
	}
	eventID := kafka.DeterministicEventID("orders.cancelled", order.ID.String())
	env, err := kafka.NewEnvelopeWithID(eventID, "orders.cancelled", 1, correlationID)
	if err != nil {
		s.logger.Error("build order cancelled envelope failed", "error", err)
		return
	}
	payload := OrderCancelledEvent{
		Envelope:      env,
		OrderID:       order.ID.String(),
		ClientOrderID: order.ClientOrderID,
		AccountID:     order.AccountID.String(),
		Symbol:        order.Symbol,
		Status:        order.Status,
		CancelledAt:   order.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if _, _, err := s.producer.PublishJSON(ctx, s.topics.OrdersCancelled, order.Symbol, payload); err != nil {
		s.logger.Error("publish order cancelled failed", "error", err)
	}
}

func (s *OrderService) insertAudit(ctx context.Context, userID uuid.UUID, action, entityType string, entityID uuid.UUID, input any) {
	if s.store == nil {
		return
	}
	log := storage.AuditLog{
		ActorID:    userID,
		ActorType:  "user",
		Action:     action,
		EntityType: entityType,
		EntityID:   &entityID,
	}
	switch v := input.(type) {
	case SubmitOrderInput:
		log.IP = v.IP
		log.UserAgent = v.UserAgent
	case CancelOrderInput:
		log.IP = v.IP
		log.UserAgent = v.UserAgent
	}
	if err := s.store.InsertAudit(ctx, log); err != nil {
		s.logger.Error("audit log failed", "error", err)
	}
}

func optionalDecimal(val *decimal.Decimal) string {
	if val == nil {
		return ""
	}
	return val.String()
}

func responseStatus(status string) string {
	switch status {
	case storage.OrderStatusPending, storage.OrderStatusOpen:
		return statusAccepted
	case storage.OrderStatusRejected:
		return statusRejected
	default:
		return status
	}
}

func (s *OrderService) checkLedgerBalance(ctx context.Context, accountID uuid.UUID, side, symbol string, quantity decimal.Decimal, price *decimal.Decimal) (bool, map[string]string, error) {
	if s.ledger == nil {
		return true, nil, nil
	}

	base, quote, err := splitSymbol(symbol)
	if err != nil {
		return true, nil, nil
	}

	var asset string
	var required decimal.Decimal

	switch strings.ToLower(strings.TrimSpace(side)) {
	case "sell":
		asset = base
		required = quantity
	case "buy":
		asset = quote
		if price == nil {
			return true, nil, nil
		}
		required = quantity.Mul(*price)
	default:
		return true, nil, nil
	}

	if required.LessThanOrEqual(decimal.Zero) {
		return true, nil, nil
	}

	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	resp, err := s.ledger.GetBalance(checkCtx, &ledgerpb.GetBalanceRequest{
		AccountId: accountID.String(),
		Asset:     asset,
	})
	if err != nil {
		return false, nil, err
	}

	available, err := decimal.NewFromString(strings.TrimSpace(resp.GetAvailable()))
	if err != nil {
		return false, nil, fmt.Errorf("parse ledger available: %w", err)
	}

	if available.GreaterThanOrEqual(required) {
		return true, nil, nil
	}

	details := map[string]string{
		"required_asset":  asset,
		"required_amount": required.String(),
		"available":       available.String(),
	}
	return false, details, nil
}

func splitSymbol(symbol string) (string, string, error) {
	parts := strings.Split(strings.ToUpper(strings.TrimSpace(symbol)), "-")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid symbol")
	}
	return parts[0], parts[1], nil
}

// Event payloads

type OrderAcceptedEvent struct {
	kafka.Envelope
	OrderID       string `json:"order_id"`
	ClientOrderID string `json:"client_order_id"`
	AccountID     string `json:"account_id"`
	Symbol        string `json:"symbol"`
	Side          string `json:"side"`
	Type          string `json:"type"`
	Price         string `json:"price,omitempty"`
	Quantity      string `json:"quantity"`
	TimeInForce   string `json:"time_in_force"`
	Status        string `json:"status"`
	CreatedAt     string `json:"created_at"`
}

type OrderRejectedEvent struct {
	kafka.Envelope
	OrderID       string   `json:"order_id"`
	ClientOrderID string   `json:"client_order_id"`
	AccountID     string   `json:"account_id"`
	Symbol        string   `json:"symbol"`
	Side          string   `json:"side"`
	Type          string   `json:"type"`
	Price         string   `json:"price,omitempty"`
	Quantity      string   `json:"quantity"`
	TimeInForce   string   `json:"time_in_force"`
	Status        string   `json:"status"`
	Reasons       []string `json:"reasons"`
	RejectedAt    string   `json:"rejected_at"`
}

type OrderCancelledEvent struct {
	kafka.Envelope
	OrderID       string `json:"order_id"`
	ClientOrderID string `json:"client_order_id"`
	AccountID     string `json:"account_id"`
	Symbol        string `json:"symbol"`
	Status        string `json:"status"`
	CancelledAt   string `json:"cancelled_at"`
}
