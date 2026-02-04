package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/AfshinJalili/goex/libs/apikey"
	"github.com/AfshinJalili/goex/libs/auth"
	"github.com/AfshinJalili/goex/services/user/internal/storage"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"log/slog"
)

type Handler struct {
	Store  *storage.Store
	Logger *slog.Logger
	Env    string
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

type createAPIKeyRequest struct {
	Label       string   `json:"label"`
	Scopes      []string `json:"scopes"`
	IPWhitelist []string `json:"ip_whitelist"`
}

type createAPIKeyResponse struct {
	ID          uuid.UUID `json:"id"`
	Prefix      string    `json:"prefix"`
	Secret      string    `json:"secret"`
	Scopes      []string  `json:"scopes"`
	IPWhitelist []string  `json:"ip_whitelist"`
	CreatedAt   string    `json:"created_at"`
}

type apiKeyItem struct {
	ID          uuid.UUID `json:"id"`
	Prefix      string    `json:"prefix"`
	Scopes      []string  `json:"scopes"`
	IPWhitelist []string  `json:"ip_whitelist"`
	LastUsedAt  *string   `json:"last_used_at,omitempty"`
	RevokedAt   *string   `json:"revoked_at,omitempty"`
	CreatedAt   string    `json:"created_at"`
}

type apiKeysResponse struct {
	Keys []apiKeyItem `json:"keys"`
}

var allowedScopes = map[string]struct{}{
	"read":     {},
	"trade":    {},
	"withdraw": {},
}

func New(store *storage.Store, logger *slog.Logger, env string) *Handler {
	return &Handler{Store: store, Logger: logger, Env: env}
}

func (h *Handler) Register(r *gin.Engine, jwtSecret []byte) {
	authGroup := r.Group("/", auth.Middleware(jwtSecret))
	authGroup.GET("/me", h.Me)
	authGroup.GET("/accounts", h.Accounts)
	authGroup.GET("/balances", h.Balances)
	authGroup.POST("/api-keys", h.CreateAPIKey)
	authGroup.GET("/api-keys", h.ListAPIKeys)
	authGroup.DELETE("/api-keys/:id", h.RevokeAPIKey)
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

func (h *Handler) CreateAPIKey(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorResponse{Code: "UNAUTHORIZED", Message: "missing user"})
		return
	}

	var req createAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse{Code: "INVALID_REQUEST", Message: "invalid payload"})
		return
	}

	if err := validateScopes(req.Scopes); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse{Code: "INVALID_REQUEST", Message: "invalid scopes"})
		return
	}

	if err := apikey.ValidateIPWhitelist(req.IPWhitelist); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse{Code: "INVALID_REQUEST", Message: "invalid ip whitelist"})
		return
	}

	fullKey, prefix, hash, err := apikey.Generate(h.Env)
	if err != nil {
		h.Logger.Error("api key generation failed", "error", err)
		c.JSON(http.StatusInternalServerError, errorResponse{Code: "INTERNAL_ERROR", Message: "internal error"})
		return
	}

	key, err := h.Store.CreateAPIKey(c.Request.Context(), userID, prefix, hash, req.Scopes, req.IPWhitelist)
	if err != nil {
		h.Logger.Error("api key insert failed", "error", err)
		c.JSON(http.StatusInternalServerError, errorResponse{Code: "INTERNAL_ERROR", Message: "internal error"})
		return
	}

	if err := h.Store.InsertAudit(c.Request.Context(), storage.AuditLog{
		ActorID:    userID,
		ActorType:  "user",
		Action:     "api_keys.create",
		EntityType: "api_key",
		EntityID:   &key.ID,
		IP:         c.ClientIP(),
		UserAgent:  c.Request.UserAgent(),
	}); err != nil {
		h.Logger.Error("audit log failed", "error", err)
	}

	c.JSON(http.StatusOK, createAPIKeyResponse{
		ID:          key.ID,
		Prefix:      key.Prefix,
		Secret:      fullKey,
		Scopes:      key.Scopes,
		IPWhitelist: key.IPWhitelist,
		CreatedAt:   key.CreatedAt.UTC().Format(time.RFC3339),
	})
}

func (h *Handler) ListAPIKeys(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorResponse{Code: "UNAUTHORIZED", Message: "missing user"})
		return
	}

	keys, err := h.Store.ListAPIKeys(c.Request.Context(), userID)
	if err != nil {
		h.Logger.Error("api key list failed", "error", err)
		c.JSON(http.StatusInternalServerError, errorResponse{Code: "INTERNAL_ERROR", Message: "internal error"})
		return
	}

	items := make([]apiKeyItem, 0, len(keys))
	for _, key := range keys {
		item := apiKeyItem{
			ID:          key.ID,
			Prefix:      key.Prefix,
			Scopes:      key.Scopes,
			IPWhitelist: key.IPWhitelist,
			CreatedAt:   key.CreatedAt.UTC().Format(time.RFC3339),
		}
		if key.LastUsedAt != nil {
			val := key.LastUsedAt.UTC().Format(time.RFC3339)
			item.LastUsedAt = &val
		}
		if key.RevokedAt != nil {
			val := key.RevokedAt.UTC().Format(time.RFC3339)
			item.RevokedAt = &val
		}
		items = append(items, item)
	}

	if err := h.Store.InsertAudit(c.Request.Context(), storage.AuditLog{
		ActorID:    userID,
		ActorType:  "user",
		Action:     "api_keys.list",
		EntityType: "api_key",
		EntityID:   nil,
		IP:         c.ClientIP(),
		UserAgent:  c.Request.UserAgent(),
	}); err != nil {
		h.Logger.Error("audit log failed", "error", err)
	}

	c.JSON(http.StatusOK, apiKeysResponse{Keys: items})
}

func (h *Handler) RevokeAPIKey(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorResponse{Code: "UNAUTHORIZED", Message: "missing user"})
		return
	}

	keyID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse{Code: "INVALID_REQUEST", Message: "invalid key id"})
		return
	}

	revoked, err := h.Store.RevokeAPIKey(c.Request.Context(), userID, keyID)
	if err != nil {
		h.Logger.Error("api key revoke failed", "error", err)
		c.JSON(http.StatusInternalServerError, errorResponse{Code: "INTERNAL_ERROR", Message: "internal error"})
		return
	}
	if !revoked {
		c.JSON(http.StatusNotFound, errorResponse{Code: "NOT_FOUND", Message: "key not found"})
		return
	}

	if err := h.Store.InsertAudit(c.Request.Context(), storage.AuditLog{
		ActorID:    userID,
		ActorType:  "user",
		Action:     "api_keys.revoke",
		EntityType: "api_key",
		EntityID:   &keyID,
		IP:         c.ClientIP(),
		UserAgent:  c.Request.UserAgent(),
	}); err != nil {
		h.Logger.Error("audit log failed", "error", err)
	}

	c.JSON(http.StatusOK, gin.H{"status": "revoked"})
}

func validateScopes(scopes []string) error {
	for _, scope := range scopes {
		if _, ok := allowedScopes[scope]; !ok {
			return errors.New("invalid scope")
		}
	}
	return nil
}

func parseLimit(raw string) int {
	if raw == "" {
		return 0
	}
	val, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	if val <= 0 {
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
