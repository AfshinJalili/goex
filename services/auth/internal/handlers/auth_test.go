package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AfshinJalili/goex/services/auth/internal/rate"
	"github.com/AfshinJalili/goex/services/auth/internal/security"
	"github.com/AfshinJalili/goex/services/auth/internal/storage"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"log/slog"
)

type fakeClock struct {
	now time.Time
}

func (f fakeClock) Now() time.Time { return f.now }

type fakeTokenGen struct {
	tokens []string
	idx    int
}

func (f *fakeTokenGen) New() (string, string, error) {
	if f.idx >= len(f.tokens) {
		return "", "", errors.New("no tokens")
	}
	tok := f.tokens[f.idx]
	f.idx++
	return tok, computeHash(tok), nil
}

type memStore struct {
	mu     sync.Mutex
	users  map[string]*storage.User
	tokens map[string]*storage.RefreshToken
}

func newMemStore() *memStore {
	return &memStore{
		users:  map[string]*storage.User{},
		tokens: map[string]*storage.RefreshToken{},
	}
}

func (m *memStore) GetUserByEmail(ctx context.Context, email string) (*storage.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	user, ok := m.users[email]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	return user, nil
}

func (m *memStore) GetRefreshTokenByHash(ctx context.Context, hash string) (*storage.RefreshToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	token, ok := m.tokens[hash]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	return token, nil
}

func (m *memStore) CreateRefreshToken(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time, ip string, userAgent string) (uuid.UUID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := uuid.New()
	m.tokens[tokenHash] = &storage.RefreshToken{
		ID:        id,
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
	}
	return id, nil
}

func (m *memStore) RotateToken(ctx context.Context, oldTokenID uuid.UUID, userID uuid.UUID, newHash string, expiresAt time.Time, ip string, userAgent string) (uuid.UUID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var oldToken *storage.RefreshToken
	for _, token := range m.tokens {
		if token.ID == oldTokenID {
			oldToken = token
			break
		}
	}
	if oldToken == nil {
		return uuid.Nil, pgx.ErrNoRows
	}
	now := time.Now()
	oldToken.RevokedAt = &now

	id := uuid.New()
	m.tokens[newHash] = &storage.RefreshToken{
		ID:        id,
		UserID:    userID,
		TokenHash: newHash,
		ExpiresAt: expiresAt,
	}
	return id, nil
}

func (m *memStore) RevokeTokenByHash(ctx context.Context, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if token, ok := m.tokens[hash]; ok {
		now := time.Now()
		token.RevokedAt = &now
		return nil
	}
	return pgx.ErrNoRows
}

func (m *memStore) RevokeAllTokens(ctx context.Context, userID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, token := range m.tokens {
		if token.UserID == userID {
			now := time.Now()
			token.RevokedAt = &now
		}
	}
	return nil
}

func setupHandler(t *testing.T, store *memStore, tokens []string, now time.Time) *AuthHandler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	limiter := rate.New(100, time.Minute)
	h := NewAuthHandler(store, logger, "test-secret", 15*time.Minute, 30*24*time.Hour, limiter)
	h.TokenGen = &fakeTokenGen{tokens: tokens}
	h.Clock = fakeClock{now: now}
	return h
}

func performRequest(router *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestLoginSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newMemStore()
	params := security.Argon2Params{Memory: 64 * 1024, Iterations: 2, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	hash, err := security.HashPassword("s3cret", params)
	if err != nil {
		t.Fatalf("hash error: %v", err)
	}
	user := &storage.User{ID: uuid.New(), Email: "user@example.com", PasswordHash: hash}
	store.users[strings.ToLower(user.Email)] = user

	h := setupHandler(t, store, []string{"refresh-1"}, time.Date(2026, 2, 4, 12, 0, 0, 0, time.UTC))
	router := gin.New()
	h.RegisterRoutes(router)

	resp := performRequest(router, http.MethodPost, "/auth/login", loginRequest{Email: user.Email, Password: "s3cret"})
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}

	var out authResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.RefreshToken != "refresh-1" {
		t.Fatalf("expected refresh token, got %q", out.RefreshToken)
	}
	if out.AccessToken == "" {
		t.Fatalf("expected access token")
	}
	if out.ExpiresIn == 0 {
		t.Fatalf("expected expires_in")
	}

	if _, ok := store.tokens[computeHash("refresh-1")]; !ok {
		t.Fatalf("expected refresh token stored")
	}
}

func TestLoginInvalidPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newMemStore()
	params := security.Argon2Params{Memory: 64 * 1024, Iterations: 2, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	hash, _ := security.HashPassword("s3cret", params)
	user := &storage.User{ID: uuid.New(), Email: "user@example.com", PasswordHash: hash}
	store.users[strings.ToLower(user.Email)] = user

	h := setupHandler(t, store, []string{"refresh-1"}, time.Now())
	router := gin.New()
	h.RegisterRoutes(router)

	resp := performRequest(router, http.MethodPost, "/auth/login", loginRequest{Email: user.Email, Password: "wrong"})
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.Code)
	}
}

func TestRefreshRotationAndReuse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newMemStore()
	userID := uuid.New()
	initialHash := computeHash("refresh-1")
	store.tokens[initialHash] = &storage.RefreshToken{
		ID:        uuid.New(),
		UserID:    userID,
		TokenHash: initialHash,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	h := setupHandler(t, store, []string{"refresh-2"}, time.Now())
	router := gin.New()
	h.RegisterRoutes(router)

	resp := performRequest(router, http.MethodPost, "/auth/refresh", refreshRequest{RefreshToken: "refresh-1"})
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}

	var out authResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.RefreshToken != "refresh-2" {
		t.Fatalf("expected rotated token, got %q", out.RefreshToken)
	}

	oldToken := store.tokens[initialHash]
	if oldToken.RevokedAt == nil {
		t.Fatalf("expected old token revoked")
	}

	// reuse detection
	resp = performRequest(router, http.MethodPost, "/auth/refresh", refreshRequest{RefreshToken: "refresh-1"})
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on reuse, got %d", resp.Code)
	}

	newToken := store.tokens[computeHash("refresh-2")]
	if newToken.RevokedAt == nil {
		t.Fatalf("expected new token revoked after reuse detection")
	}
}

func TestLogoutRevokesToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newMemStore()
	userID := uuid.New()
	initialHash := computeHash("refresh-1")
	store.tokens[initialHash] = &storage.RefreshToken{
		ID:        uuid.New(),
		UserID:    userID,
		TokenHash: initialHash,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	h := setupHandler(t, store, []string{}, time.Now())
	router := gin.New()
	h.RegisterRoutes(router)

	resp := performRequest(router, http.MethodPost, "/auth/logout", refreshRequest{RefreshToken: "refresh-1"})
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}

	if store.tokens[initialHash].RevokedAt == nil {
		t.Fatalf("expected token revoked")
	}
}

func TestLoginRequiresMFAWhenEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newMemStore()
	params := security.Argon2Params{Memory: 64 * 1024, Iterations: 2, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	hash, _ := security.HashPassword("s3cret", params)
	user := &storage.User{ID: uuid.New(), Email: "user@example.com", PasswordHash: hash, MFAEnabled: true}
	store.users[strings.ToLower(user.Email)] = user

	h := setupHandler(t, store, []string{"refresh-1"}, time.Now())
	router := gin.New()
	h.RegisterRoutes(router)

	resp := performRequest(router, http.MethodPost, "/auth/login", loginRequest{Email: user.Email, Password: "s3cret"})
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing mfa, got %d", resp.Code)
	}
}
