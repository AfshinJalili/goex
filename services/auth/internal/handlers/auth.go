package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/AfshinJalili/goex/services/auth/internal/rate"
	"github.com/AfshinJalili/goex/services/auth/internal/security"
	"github.com/AfshinJalili/goex/services/auth/internal/storage"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"log/slog"
)

type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

type Store interface {
	GetUserByEmail(ctx context.Context, email string) (*storage.User, error)
	GetRefreshTokenByHash(ctx context.Context, hash string) (*storage.RefreshToken, error)
	CreateRefreshToken(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time, ip string, userAgent string) (uuid.UUID, error)
	RotateToken(ctx context.Context, oldTokenID uuid.UUID, userID uuid.UUID, newHash string, expiresAt time.Time, ip string, userAgent string) (uuid.UUID, error)
	RevokeTokenByHash(ctx context.Context, hash string) error
	RevokeAllTokens(ctx context.Context, userID uuid.UUID) error
}

type AuthHandler struct {
	Store       Store
	Logger      *slog.Logger
	JWTSecret   []byte
	AccessTTL   time.Duration
	RefreshTTL  time.Duration
	RateLimiter *rate.Limiter
	TokenGen    security.TokenGenerator
	Clock       Clock
	Issuer      string
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	MFACode  string `json:"mfa_code"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type authResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func NewAuthHandler(store Store, logger *slog.Logger, jwtSecret string, accessTTL, refreshTTL time.Duration, limiter *rate.Limiter, issuer string) *AuthHandler {
	return &AuthHandler{
		Store:       store,
		Logger:      logger,
		JWTSecret:   []byte(jwtSecret),
		AccessTTL:   accessTTL,
		RefreshTTL:  refreshTTL,
		RateLimiter: limiter,
		TokenGen:    security.DefaultTokenGenerator{},
		Clock:       systemClock{},
		Issuer:      issuer,
	}
}

func (h *AuthHandler) RegisterRoutes(r *gin.Engine) {
	r.POST("/auth/login", h.Login)
	r.POST("/auth/refresh", h.Refresh)
	r.POST("/auth/logout", h.Logout)
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse{Code: "INVALID_REQUEST", Message: "invalid payload"})
		return
	}

	ip := c.ClientIP()
	if !h.RateLimiter.Allow(ip, h.Clock.Now()) {
		c.JSON(http.StatusTooManyRequests, errorResponse{Code: "RATE_LIMITED", Message: "too many requests"})
		return
	}

	user, err := h.Store.GetUserByEmail(c.Request.Context(), strings.ToLower(req.Email))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusUnauthorized, errorResponse{Code: "UNAUTHORIZED", Message: "invalid credentials"})
			return
		}
		h.Logger.Error("login lookup failed", "error", err)
		c.JSON(http.StatusInternalServerError, errorResponse{Code: "INTERNAL_ERROR", Message: "internal error"})
		return
	}

	ok, err := security.VerifyPassword(req.Password, user.PasswordHash)
	if err != nil || !ok {
		c.JSON(http.StatusUnauthorized, errorResponse{Code: "UNAUTHORIZED", Message: "invalid credentials"})
		return
	}

	if user.MFAEnabled {
		if strings.TrimSpace(req.MFACode) == "" {
			c.JSON(http.StatusUnauthorized, errorResponse{Code: "UNAUTHORIZED", Message: "mfa required"})
			return
		}
		// Stub: accept any non-empty code for now
	}

	access, err := security.NewAccessToken(user.ID.String(), []string{"user"}, []string{"read"}, h.JWTSecret, h.AccessTTL, h.Clock.Now(), h.Issuer)
	if err != nil {
		h.Logger.Error("jwt sign failed", "error", err)
		c.JSON(http.StatusInternalServerError, errorResponse{Code: "INTERNAL_ERROR", Message: "internal error"})
		return
	}

	refreshToken, refreshHash, err := h.TokenGen.New()
	if err != nil {
		h.Logger.Error("refresh token generation failed", "error", err)
		c.JSON(http.StatusInternalServerError, errorResponse{Code: "INTERNAL_ERROR", Message: "internal error"})
		return
	}

	_, err = h.Store.CreateRefreshToken(c.Request.Context(), user.ID, refreshHash, h.Clock.Now().Add(h.RefreshTTL), ip, c.Request.UserAgent())
	if err != nil {
		h.Logger.Error("refresh token insert failed", "error", err)
		c.JSON(http.StatusInternalServerError, errorResponse{Code: "INTERNAL_ERROR", Message: "internal error"})
		return
	}

	c.JSON(http.StatusOK, authResponse{
		AccessToken:  access,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(h.AccessTTL.Seconds()),
	})
}

func (h *AuthHandler) Refresh(c *gin.Context) {
	var req refreshRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.RefreshToken == "" {
		c.JSON(http.StatusBadRequest, errorResponse{Code: "INVALID_REQUEST", Message: "invalid payload"})
		return
	}

	// compute hash from provided token
	providedHash := computeHash(req.RefreshToken)

	token, err := h.Store.GetRefreshTokenByHash(c.Request.Context(), providedHash)
	if err != nil {
		c.JSON(http.StatusUnauthorized, errorResponse{Code: "UNAUTHORIZED", Message: "invalid token"})
		return
	}

	if token.RevokedAt != nil {
		_ = h.Store.RevokeAllTokens(c.Request.Context(), token.UserID)
		c.JSON(http.StatusUnauthorized, errorResponse{Code: "UNAUTHORIZED", Message: "token reuse detected"})
		return
	}

	now := h.Clock.Now()
	if token.ExpiresAt.Before(now) {
		_ = h.Store.RevokeTokenByHash(c.Request.Context(), providedHash)
		c.JSON(http.StatusUnauthorized, errorResponse{Code: "UNAUTHORIZED", Message: "token expired"})
		return
	}

	newToken, newHash, err := h.TokenGen.New()
	if err != nil {
		h.Logger.Error("refresh token generation failed", "error", err)
		c.JSON(http.StatusInternalServerError, errorResponse{Code: "INTERNAL_ERROR", Message: "internal error"})
		return
	}

	_, err = h.Store.RotateToken(c.Request.Context(), token.ID, token.UserID, newHash, now.Add(h.RefreshTTL), c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		h.Logger.Error("token rotation failed", "error", err)
		c.JSON(http.StatusInternalServerError, errorResponse{Code: "INTERNAL_ERROR", Message: "internal error"})
		return
	}

	access, err := security.NewAccessToken(token.UserID.String(), []string{"user"}, []string{"read"}, h.JWTSecret, h.AccessTTL, now, h.Issuer)
	if err != nil {
		h.Logger.Error("jwt sign failed", "error", err)
		c.JSON(http.StatusInternalServerError, errorResponse{Code: "INTERNAL_ERROR", Message: "internal error"})
		return
	}

	c.JSON(http.StatusOK, authResponse{
		AccessToken:  access,
		RefreshToken: newToken,
		ExpiresIn:    int64(h.AccessTTL.Seconds()),
	})
}

func (h *AuthHandler) Logout(c *gin.Context) {
	var req refreshRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.RefreshToken == "" {
		c.JSON(http.StatusBadRequest, errorResponse{Code: "INVALID_REQUEST", Message: "invalid payload"})
		return
	}

	hash := computeHash(req.RefreshToken)

	if err := h.Store.RevokeTokenByHash(c.Request.Context(), hash); err != nil {
		h.Logger.Error("revoke token failed", "error", err)
		c.JSON(http.StatusInternalServerError, errorResponse{Code: "INTERNAL_ERROR", Message: "internal error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func computeHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
