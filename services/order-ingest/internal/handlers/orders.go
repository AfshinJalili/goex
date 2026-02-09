package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/AfshinJalili/goex/libs/auth"
	"github.com/AfshinJalili/goex/services/order-ingest/internal/service"
	"github.com/AfshinJalili/goex/services/order-ingest/internal/storage"
	"github.com/AfshinJalili/goex/services/order-ingest/internal/validation"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"log/slog"
)

type OrderService interface {
	SubmitOrder(ctx context.Context, input service.SubmitOrderInput) (*service.SubmitOrderResult, error)
	CancelOrder(ctx context.Context, input service.CancelOrderInput) (*storage.Order, error)
	ListOrders(ctx context.Context, input service.ListOrdersInput) ([]storage.Order, string, error)
	GetOrder(ctx context.Context, input service.GetOrderInput) (*storage.Order, error)
}

type Handler struct {
	Service OrderService
	Logger  *slog.Logger
}

type createOrderRequest struct {
	ClientOrderID string `json:"client_order_id"`
	Symbol        string `json:"symbol"`
	Side          string `json:"side"`
	Type          string `json:"type"`
	Price         string `json:"price"`
	Quantity      string `json:"quantity"`
	TimeInForce   string `json:"time_in_force"`
}

type createOrderResponse struct {
	OrderID   string `json:"order_id"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type orderItem struct {
	OrderID       string  `json:"order_id"`
	ClientOrderID string  `json:"client_order_id"`
	Symbol        string  `json:"symbol"`
	Side          string  `json:"side"`
	Type          string  `json:"type"`
	Price         *string `json:"price,omitempty"`
	Quantity      string  `json:"quantity"`
	Filled        string  `json:"filled_quantity"`
	Status        string  `json:"status"`
	TimeInForce   string  `json:"time_in_force"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

type listOrdersResponse struct {
	Orders     []orderItem `json:"orders"`
	NextCursor string      `json:"next_cursor,omitempty"`
}

type errorResponse struct {
	Code    string                  `json:"code"`
	Message string                  `json:"message"`
	Reasons []string                `json:"reasons,omitempty"`
	Fields  []validation.FieldError `json:"fields,omitempty"`
	Details map[string]string       `json:"details,omitempty"`
}

func New(service OrderService, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{Service: service, Logger: logger}
}

func (h *Handler) Register(r *gin.Engine, jwtSecret []byte) {
	group := r.Group("/", auth.Middleware(jwtSecret))
	group.POST("/orders", h.CreateOrder)
	group.GET("/orders", h.ListOrders)
	group.GET("/orders/:id", h.GetOrder)
	group.DELETE("/orders/:id", h.CancelOrder)
}

func (h *Handler) CreateOrder(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "missing user", nil, nil, nil)
		return
	}

	var req createOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "invalid payload", nil, nil, nil)
		return
	}

	errs := validation.ValidateOrderRequest(req.Symbol, req.Side, req.Type, req.TimeInForce, req.Quantity, req.Price)
	if len(errs) > 0 {
		writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "invalid request", nil, errs, nil)
		return
	}

	symbol := validation.NormalizeSymbol(req.Symbol)
	side := strings.ToLower(strings.TrimSpace(req.Side))
	orderType := strings.ToLower(strings.TrimSpace(req.Type))
	tif := strings.ToUpper(strings.TrimSpace(req.TimeInForce))
	if tif == "" {
		tif = "GTC"
	}

	qty, _ := decimal.NewFromString(strings.TrimSpace(req.Quantity))

	var pricePtr *decimal.Decimal
	if strings.TrimSpace(req.Price) != "" {
		price, err := decimal.NewFromString(strings.TrimSpace(req.Price))
		if err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "invalid price", nil, nil, nil)
			return
		}
		pricePtr = &price
	}

	clientOrderID := strings.TrimSpace(req.ClientOrderID)
	if headerKey := strings.TrimSpace(c.GetHeader("Idempotency-Key")); headerKey != "" {
		clientOrderID = headerKey
	}

	input := service.SubmitOrderInput{
		UserID:        userID,
		ClientOrderID: clientOrderID,
		Symbol:        symbol,
		Side:          side,
		OrderType:     orderType,
		TimeInForce:   tif,
		Quantity:      qty,
		Price:         pricePtr,
		IP:            c.ClientIP(),
		UserAgent:     c.Request.UserAgent(),
		CorrelationID: requestIDFromContext(c),
	}

	result, err := h.Service.SubmitOrder(c.Request.Context(), input)
	if err != nil {
		if errors.Is(err, service.ErrAccountNotFound) {
			writeError(c, http.StatusForbidden, "FORBIDDEN", "account not found", nil, nil, nil)
			return
		}
		if st, ok := status.FromError(err); ok {
			switch st.Code() {
			case codes.InvalidArgument:
				writeError(c, http.StatusBadRequest, "INVALID_REQUEST", st.Message(), nil, nil, nil)
				return
			case codes.NotFound:
				writeError(c, http.StatusBadRequest, "INVALID_REQUEST", st.Message(), nil, nil, nil)
				return
			case codes.FailedPrecondition:
				writeError(c, http.StatusBadRequest, "INVALID_REQUEST", st.Message(), nil, nil, nil)
				return
			}
		}
		h.Logger.Error("submit order failed", "error", err)
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error", nil, nil, nil)
		return
	}

	if result != nil && result.Status == "rejected" && !result.Existing {
		code := "INVALID_REQUEST"
		message := "order rejected"
		if hasReason(result.Reasons, "insufficient_balance") {
			code = "INSUFFICIENT_BALANCE"
			message = "insufficient balance"
		} else if hasReason(result.Reasons, "market_inactive") {
			code = "SYMBOL_HALTED"
			message = "market halted"
		}
		writeError(c, http.StatusBadRequest, code, message, result.Reasons, nil, result.Details)
		return
	}

	if result == nil || result.Order == nil {
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error", nil, nil, nil)
		return
	}

	resp := createOrderResponse{
		OrderID:   result.Order.ID.String(),
		Status:    result.Status,
		CreatedAt: result.Order.CreatedAt.UTC().Format(time.RFC3339),
	}
	c.JSON(http.StatusOK, resp)
}

func (h *Handler) ListOrders(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "missing user", nil, nil, nil)
		return
	}

	filter := storage.OrderFilter{
		Symbol: validation.NormalizeSymbol(c.Query("symbol")),
		Status: strings.ToLower(strings.TrimSpace(c.Query("status"))),
		Cursor: strings.TrimSpace(c.Query("cursor")),
	}

	if limitStr := strings.TrimSpace(c.Query("limit")); limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil {
			filter.Limit = n
		} else {
			writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "invalid limit", nil, nil, nil)
			return
		}
	}

	if fromStr := strings.TrimSpace(c.Query("from")); fromStr != "" {
		parsed, err := time.Parse(time.RFC3339, fromStr)
		if err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "invalid from", nil, nil, nil)
			return
		}
		filter.From = &parsed
	}
	if toStr := strings.TrimSpace(c.Query("to")); toStr != "" {
		parsed, err := time.Parse(time.RFC3339, toStr)
		if err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "invalid to", nil, nil, nil)
			return
		}
		filter.To = &parsed
	}

	orders, nextCursor, err := h.Service.ListOrders(c.Request.Context(), service.ListOrdersInput{UserID: userID, Filter: filter})
	if err != nil {
		if errors.Is(err, service.ErrAccountNotFound) {
			writeError(c, http.StatusForbidden, "FORBIDDEN", "account not found", nil, nil, nil)
			return
		}
		if errors.Is(err, storage.ErrInvalidCursor) {
			writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "invalid cursor", nil, nil, nil)
			return
		}
		h.Logger.Error("list orders failed", "error", err)
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error", nil, nil, nil)
		return
	}

	items := make([]orderItem, 0, len(orders))
	for _, order := range orders {
		items = append(items, orderToItem(order))
	}

	c.JSON(http.StatusOK, listOrdersResponse{Orders: items, NextCursor: nextCursor})
}

func (h *Handler) GetOrder(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "missing user", nil, nil, nil)
		return
	}

	orderID, err := parseUUIDParam(c.Param("id"))
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "invalid order_id", nil, nil, nil)
		return
	}

	order, err := h.Service.GetOrder(c.Request.Context(), service.GetOrderInput{UserID: userID, OrderID: orderID})
	if err != nil {
		if errors.Is(err, service.ErrAccountNotFound) {
			writeError(c, http.StatusForbidden, "FORBIDDEN", "account not found", nil, nil, nil)
			return
		}
		if errors.Is(err, storage.ErrNotFound) {
			writeError(c, http.StatusBadRequest, "ORDER_NOT_FOUND", "order not found", nil, nil, nil)
			return
		}
		h.Logger.Error("get order failed", "error", err)
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error", nil, nil, nil)
		return
	}

	c.JSON(http.StatusOK, orderToItem(*order))
}

func (h *Handler) CancelOrder(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "missing user", nil, nil, nil)
		return
	}

	orderID, err := parseUUIDParam(c.Param("id"))
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "invalid order_id", nil, nil, nil)
		return
	}

	order, err := h.Service.CancelOrder(c.Request.Context(), service.CancelOrderInput{UserID: userID, OrderID: orderID, IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), CorrelationID: requestIDFromContext(c)})
	if err != nil {
		if errors.Is(err, service.ErrAccountNotFound) {
			writeError(c, http.StatusForbidden, "FORBIDDEN", "account not found", nil, nil, nil)
			return
		}
		if errors.Is(err, storage.ErrNotFound) {
			writeError(c, http.StatusBadRequest, "ORDER_NOT_FOUND", "order not found", nil, nil, nil)
			return
		}
		if errors.Is(err, storage.ErrInvalidStatus) {
			writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "order not cancellable", nil, nil, nil)
			return
		}
		h.Logger.Error("cancel order failed", "error", err)
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error", nil, nil, nil)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"order_id":   order.ID.String(),
		"status":     order.Status,
		"updated_at": order.UpdatedAt.UTC().Format(time.RFC3339),
	})
}

func orderToItem(order storage.Order) orderItem {
	var price *string
	if order.Price != nil {
		val := order.Price.String()
		price = &val
	}

	return orderItem{
		OrderID:       order.ID.String(),
		ClientOrderID: order.ClientOrderID,
		Symbol:        order.Symbol,
		Side:          order.Side,
		Type:          order.Type,
		Price:         price,
		Quantity:      order.Quantity.String(),
		Filled:        order.FilledQuantity.String(),
		Status:        order.Status,
		TimeInForce:   order.TimeInForce,
		CreatedAt:     order.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:     order.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func userIDFromContext(c *gin.Context) (uuid.UUID, bool) {
	val, ok := c.Get(auth.ContextUserIDKey)
	if !ok {
		return uuid.Nil, false
	}
	userID, ok := val.(string)
	if !ok {
		return uuid.Nil, false
	}
	parsed, err := uuid.Parse(userID)
	if err != nil {
		return uuid.Nil, false
	}
	return parsed, true
}

func parseUUIDParam(value string) (uuid.UUID, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return uuid.Nil, errors.New("missing id")
	}
	id, err := uuid.Parse(trimmed)
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

func writeError(c *gin.Context, status int, code, message string, reasons []string, fields []validation.FieldError, details map[string]string) {
	resp := errorResponse{
		Code:    code,
		Message: message,
		Reasons: reasons,
		Fields:  fields,
		Details: details,
	}
	c.JSON(status, resp)
}

func hasReason(reasons []string, reason string) bool {
	for _, r := range reasons {
		if r == reason {
			return true
		}
	}
	return false
}

func requestIDFromContext(c *gin.Context) string {
	if val, ok := c.Get("X-Request-ID"); ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}
