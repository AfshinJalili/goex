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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	GetLastTradePrice(ctx context.Context, symbol string) (decimal.Decimal, error)
	CreateOrder(ctx context.Context, order storage.Order) (*storage.Order, bool, error)
	ListOrders(ctx context.Context, accountID uuid.UUID, filter storage.OrderFilter) ([]storage.Order, string, error)
	CancelOrder(ctx context.Context, orderID, accountID uuid.UUID) (*storage.Order, error)
	InsertAudit(ctx context.Context, log storage.AuditLog) error
}

type RiskClient interface {
	PreTradeCheck(ctx context.Context, in *riskpb.PreTradeCheckRequest, opts ...grpc.CallOption) (*riskpb.PreTradeCheckResponse, error)
}

type LedgerClient interface {
	ReserveBalance(ctx context.Context, in *ledgerpb.ReserveBalanceRequest, opts ...grpc.CallOption) (*ledgerpb.ReserveBalanceResponse, error)
	ReleaseBalance(ctx context.Context, in *ledgerpb.ReleaseBalanceRequest, opts ...grpc.CallOption) (*ledgerpb.ReleaseBalanceResponse, error)
}

type OrderService struct {
	store                OrderStore
	risk                 RiskClient
	ledger               LedgerClient
	producer             kafka.Publisher
	logger               *slog.Logger
	metrics              *Metrics
	topics               Topics
	marketBuySlippageBps int
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

func NewOrderService(store OrderStore, risk RiskClient, ledger LedgerClient, producer kafka.Publisher, logger *slog.Logger, metrics *Metrics, topics Topics, marketBuySlippageBps int) *OrderService {
	if logger == nil {
		logger = slog.Default()
	}
	if marketBuySlippageBps < 0 {
		marketBuySlippageBps = 0
	}
	return &OrderService{
		store:                store,
		risk:                 risk,
		ledger:               ledger,
		producer:             producer,
		logger:               logger,
		metrics:              metrics,
		topics:               topics,
		marketBuySlippageBps: marketBuySlippageBps,
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
	orderID := uuid.New()

	effectiveType := strings.ToLower(strings.TrimSpace(input.OrderType))
	effectiveTIF := strings.ToUpper(strings.TrimSpace(input.TimeInForce))
	if effectiveTIF == "" {
		effectiveTIF = "GTC"
	}
	effectivePrice := input.Price
	if effectiveType == "market" {
		effectivePrice = nil
	}

	priceStr := ""
	if effectivePrice != nil {
		priceStr = effectivePrice.String()
	}

	riskStatus := "error"
	riskStart := time.Now()
	riskCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	riskResp, err := s.risk.PreTradeCheck(riskCtx, &riskpb.PreTradeCheckRequest{
		AccountId: accountID.String(),
		Symbol:    input.Symbol,
		Side:      input.Side,
		OrderType: effectiveType,
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

	orderPrice := effectivePrice
	if effectiveType == "market" {
		orderPrice = nil
	}

	order := storage.Order{
		ID:             orderID,
		ClientOrderID:  clientOrderID,
		AccountID:      accountID,
		Symbol:         input.Symbol,
		Side:           input.Side,
		Type:           effectiveType,
		Price:          orderPrice,
		Quantity:       input.Quantity,
		FilledQuantity: decimal.Zero,
		TimeInForce:    effectiveTIF,
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

	ledgerOK, ledgerDetails, err := s.checkLedgerBalance(ctx, orderID, accountID, input.Side, effectiveType, input.Symbol, input.Quantity, effectivePrice)
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

	reserved := s.ledger != nil
	order.Status = storage.OrderStatusPending
	stored, created, err := s.store.CreateOrder(ctx, order)
	if err != nil {
		if reserved {
			if releaseErr := s.releaseReservation(ctx, order.ID); releaseErr != nil {
				return nil, releaseErr
			}
		}
		if s.metrics != nil {
			s.metrics.OrderSubmissions.WithLabelValues("error").Inc()
			s.metrics.OrderSubmissionLatency.WithLabelValues("error").Observe(time.Since(start).Seconds())
		}
		return nil, err
	}
	if !created {
		if reserved {
			if err := s.releaseReservation(ctx, order.ID); err != nil {
				return nil, err
			}
		}
		if s.metrics != nil {
			label := responseStatus(stored.Status)
			s.metrics.OrderSubmissions.WithLabelValues(label).Inc()
			s.metrics.OrderSubmissionLatency.WithLabelValues(label).Observe(time.Since(start).Seconds())
		}
		return &SubmitOrderResult{
			Order:    stored,
			Status:   responseStatus(stored.Status),
			Existing: true,
		}, nil
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
		if errors.Is(err, storage.ErrInvalidStatus) {
			existing, fetchErr := s.store.GetOrderByID(ctx, input.OrderID)
			if fetchErr == nil && existing.AccountID == accountID && existing.Status == storage.OrderStatusCancelled {
				order = existing
			} else {
				if s.metrics != nil {
					s.metrics.OrderCancellations.WithLabelValues("error").Inc()
				}
				return nil, err
			}
		} else {
			if s.metrics != nil {
				s.metrics.OrderCancellations.WithLabelValues("error").Inc()
			}
			return nil, err
		}
	}

	if order == nil {
		if s.metrics != nil {
			s.metrics.OrderCancellations.WithLabelValues("error").Inc()
		}
		return nil, storage.ErrNotFound
	}

	if err := s.releaseReservation(ctx, order.ID); err != nil {
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

func (s *OrderService) checkLedgerBalance(ctx context.Context, orderID, accountID uuid.UUID, side, orderType, symbol string, quantity decimal.Decimal, price *decimal.Decimal) (bool, map[string]string, error) {
	if s.ledger == nil {
		return true, nil, nil
	}

	base, quote, err := splitSymbol(symbol)
	if err != nil {
		return true, nil, nil
	}

	var asset string
	var required decimal.Decimal

	side = strings.ToLower(strings.TrimSpace(side))
	orderType = strings.ToLower(strings.TrimSpace(orderType))
	switch side {
	case "sell":
		asset = base
		required = quantity
	case "buy":
		asset = quote
		if orderType == "market" {
			refPrice, err := s.marketBuyReferencePrice(ctx, symbol)
			if err != nil {
				details := map[string]string{
					"required_asset": asset,
				}
				return false, details, status.Error(codes.FailedPrecondition, "market price unavailable")
			}
			required = quantity.Mul(refPrice)
		} else {
			if price == nil {
				return false, nil, status.Error(codes.InvalidArgument, "price is required for buy orders")
			}
			required = quantity.Mul(*price)
		}
	default:
		return true, nil, nil
	}

	if required.LessThanOrEqual(decimal.Zero) {
		return false, nil, status.Error(codes.InvalidArgument, "required amount must be positive")
	}

	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	resp, err := s.ledger.ReserveBalance(checkCtx, &ledgerpb.ReserveBalanceRequest{
		AccountId: accountID.String(),
		OrderId:   orderID.String(),
		Asset:     asset,
		Amount:    required.String(),
	})
	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.FailedPrecondition {
			details := map[string]string{
				"required_asset":  asset,
				"required_amount": required.String(),
			}
			return false, details, nil
		}
		return false, nil, err
	}

	details := map[string]string{
		"required_asset":  asset,
		"required_amount": required.String(),
		"available":       strings.TrimSpace(resp.GetAvailable()),
		"locked":          strings.TrimSpace(resp.GetLocked()),
	}
	return true, details, nil
}

func (s *OrderService) marketBuyReferencePrice(ctx context.Context, symbol string) (decimal.Decimal, error) {
	if s.store == nil {
		return decimal.Zero, fmt.Errorf("store not configured")
	}
	price, err := s.store.GetLastTradePrice(ctx, symbol)
	if err != nil {
		return decimal.Zero, err
	}
	if price.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, fmt.Errorf("invalid reference price")
	}
	slippage := decimal.NewFromInt(int64(s.marketBuySlippageBps)).Div(decimal.NewFromInt(10000))
	factor := decimal.NewFromInt(1).Add(slippage)
	return price.Mul(factor), nil
}

func (s *OrderService) releaseReservation(ctx context.Context, orderID uuid.UUID) error {
	if s.ledger == nil {
		return nil
	}
	releaseCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := s.ledger.ReleaseBalance(releaseCtx, &ledgerpb.ReleaseBalanceRequest{
		OrderId: orderID.String(),
	})
	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
			return nil
		}
		return err
	}
	return nil
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
