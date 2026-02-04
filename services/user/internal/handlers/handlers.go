package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/AfshinJalili/goex/libs/auth"
	"github.com/AfshinJalili/goex/services/user/internal/storage"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"log/slog"
)

type Handler struct {
	Store  *storage.Store
	Logger *slog.Logger
}

type meResponse struct {
	ID         uuid.UUID `json:"id"`
	Email      string    `json:"email"`
	Status     string    `json:"status"`
	KYCLevel   string    `json:"kyc_level"`
	MFAEnabled bool      `json:"mfa_enabled"`
	CreatedAt  string    `json:"created_at"`
}

type accountsResponse struct {
	Accounts   []storage.Account `json:"accounts"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

type balancesResponse struct {
	Balances   []storage.Balance `json:"balances"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func New(store *storage.Store, logger *slog.Logger) *Handler {
	return &Handler{Store: store, Logger: logger}
}

func (h *Handler) Register(r *gin.Engine, jwtSecret []byte) {
	authGroup := r.Group("/", auth.Middleware(jwtSecret))
	authGroup.GET("/me", h.Me)
	authGroup.GET("/accounts", h.Accounts)
	authGroup.GET("/balances", h.Balances)
}

func (h *Handler) Me(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorResponse{Code: "UNAUTHORIZED", Message: "missing user"})
		return
	}

	user, err := h.Store.GetUserByID(c.Request.Context(), userID)
	if err != nil {
		h.Logger.Error("me lookup failed", "error", err)
		c.JSON(http.StatusInternalServerError, errorResponse{Code: "INTERNAL_ERROR", Message: "internal error"})
		return
	}

	if err := h.Store.InsertAudit(c.Request.Context(), storage.AuditLog{
		ActorID:    userID,
		ActorType:  "user",
		Action:     "read.me",
		EntityType: "user",
		EntityID:   &userID,
		IP:         c.ClientIP(),
		UserAgent:  c.Request.UserAgent(),
	}); err != nil {
		h.Logger.Error("audit log failed", "error", err)
	}

	resp := meResponse{
		ID:         user.ID,
		Email:      user.Email,
		Status:     user.Status,
		KYCLevel:   user.KYCLevel,
		MFAEnabled: user.MFAEnabled,
		CreatedAt:  user.CreatedAt.UTC().Format(time.RFC3339),
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) Accounts(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorResponse{Code: "UNAUTHORIZED", Message: "missing user"})
		return
	}

	limit := parseLimit(c.Query("limit"))
	cursor := c.Query("cursor")

	accounts, nextCursor, err := h.Store.ListAccounts(c.Request.Context(), userID, limit, cursor)
	if err != nil {
		if errors.Is(err, storage.ErrInvalidCursor) {
			c.JSON(http.StatusBadRequest, errorResponse{Code: "INVALID_REQUEST", Message: "invalid cursor"})
			return
		}
		h.Logger.Error("list accounts failed", "error", err)
		c.JSON(http.StatusInternalServerError, errorResponse{Code: "INTERNAL_ERROR", Message: "internal error"})
		return
	}

	if err := h.Store.InsertAudit(c.Request.Context(), storage.AuditLog{
		ActorID:    userID,
		ActorType:  "user",
		Action:     "read.accounts",
		EntityType: "account",
		EntityID:   nil,
		IP:         c.ClientIP(),
		UserAgent:  c.Request.UserAgent(),
	}); err != nil {
		h.Logger.Error("audit log failed", "error", err)
	}

	c.JSON(http.StatusOK, accountsResponse{Accounts: accounts, NextCursor: nextCursor})
}

func (h *Handler) Balances(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorResponse{Code: "UNAUTHORIZED", Message: "missing user"})
		return
	}

	limit := parseLimit(c.Query("limit"))
	cursor := c.Query("cursor")

	balances, nextCursor, err := h.Store.ListBalances(c.Request.Context(), userID, limit, cursor)
	if err != nil {
		if errors.Is(err, storage.ErrInvalidCursor) {
			c.JSON(http.StatusBadRequest, errorResponse{Code: "INVALID_REQUEST", Message: "invalid cursor"})
			return
		}
		h.Logger.Error("list balances failed", "error", err)
		c.JSON(http.StatusInternalServerError, errorResponse{Code: "INTERNAL_ERROR", Message: "internal error"})
		return
	}

	if err := h.Store.InsertAudit(c.Request.Context(), storage.AuditLog{
		ActorID:    userID,
		ActorType:  "user",
		Action:     "read.balances",
		EntityType: "balance",
		EntityID:   nil,
		IP:         c.ClientIP(),
		UserAgent:  c.Request.UserAgent(),
	}); err != nil {
		h.Logger.Error("audit log failed", "error", err)
	}

	c.JSON(http.StatusOK, balancesResponse{Balances: balances, NextCursor: nextCursor})
}

func parseLimit(raw string) int {
	if raw == "" {
		return 0
	}
	val, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return val
}

func userIDFromContext(c *gin.Context) (uuid.UUID, bool) {
	val, ok := c.Get(auth.ContextUserIDKey)
	if !ok {
		return uuid.Nil, false
	}
	idStr, ok := val.(string)
	if !ok {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}
