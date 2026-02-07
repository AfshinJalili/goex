package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"log/slog"

	"github.com/AfshinJalili/goex/services/auth/internal/rate"
	"github.com/AfshinJalili/goex/services/auth/internal/security"
	"github.com/AfshinJalili/goex/services/auth/internal/storage"
	"github.com/AfshinJalili/goex/services/testutil"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestLoginIntegration(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION") == "" {
		t.Skip("set RUN_DB_INTEGRATION=1 to run")
	}

	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Skipf("db connection failed: %v", err)
	}
	defer pool.Close()
	ctx := context.Background()
	defer testutil.CleanupTestData(ctx, pool)

	store := storage.New(pool)
	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	limiter := rate.NewMemory(100, time.Minute)
	handler := NewAuthHandler(store, logger, "test-secret", 15*time.Minute, 30*24*time.Hour, limiter, "cex-auth")

	router := gin.New()
	handler.RegisterRoutes(router)

	t.Run("success with valid credentials", func(t *testing.T) {
		resp := testutil.MakeAPIRequest(router, http.MethodPost, "/auth/login", loginRequest{
			Email:    "demo@example.com",
			Password: "demo123",
		})

		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
		}

		var out authResponse
		if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if out.AccessToken == "" {
			t.Fatal("expected access token")
		}
		if out.RefreshToken == "" {
			t.Fatal("expected refresh token")
		}
	})

	t.Run("invalid email", func(t *testing.T) {
		resp := testutil.MakeAPIRequest(router, http.MethodPost, "/auth/login", loginRequest{
			Email:    "nonexistent@example.com",
			Password: "demo123",
		})

		testutil.AssertErrorCode(t, resp, testutil.ErrorCodeUnauthorized)
	})

	t.Run("invalid password", func(t *testing.T) {
		resp := testutil.MakeAPIRequest(router, http.MethodPost, "/auth/login", loginRequest{
			Email:    "demo@example.com",
			Password: "wrongpassword",
		})

		testutil.AssertErrorCode(t, resp, testutil.ErrorCodeUnauthorized)
	})

	t.Run("mfa required but not provided", func(t *testing.T) {
		ctx := context.Background()
		mfaUserID := uuid.New()
		hash, _ := security.HashPassword("mfa123", security.Argon2Params{
			Memory: 64 * 1024, Iterations: 3, Parallelism: 2, SaltLength: 16, KeyLength: 32,
		})
		_, err := pool.Exec(ctx, `
			INSERT INTO users (id, email, password_hash, status, kyc_level, mfa_enabled, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (email) DO UPDATE SET mfa_enabled = $6
		`, mfaUserID, "mfa@example.com", hash, "active", "basic", true, time.Now(), time.Now())
		if err != nil {
			t.Fatalf("create mfa user: %v", err)
		}

		resp := testutil.MakeAPIRequest(router, http.MethodPost, "/auth/login", loginRequest{
			Email:    "mfa@example.com",
			Password: "mfa123",
		})

		testutil.AssertErrorCode(t, resp, testutil.ErrorCodeUnauthorized)
	})

	t.Run("rate limiting", func(t *testing.T) {
		limiter := rate.NewMemory(10, time.Minute)
		handler := NewAuthHandler(store, logger, "test-secret", 15*time.Minute, 30*24*time.Hour, limiter, "cex-auth")
		router := gin.New()
		handler.RegisterRoutes(router)

		for i := 0; i < 10; i++ {
			testutil.MakeAPIRequest(router, http.MethodPost, "/auth/login", loginRequest{
				Email:    "demo@example.com",
				Password: "wrong",
			})
		}

		resp := testutil.MakeAPIRequest(router, http.MethodPost, "/auth/login", loginRequest{
			Email:    "demo@example.com",
			Password: "wrong",
		})

		testutil.AssertErrorCode(t, resp, testutil.ErrorCodeRateLimited)
	})

	t.Run("malformed JSON payload", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader("invalid json"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		testutil.AssertErrorCode(t, w, testutil.ErrorCodeInvalidRequest)
	})

	t.Run("missing email field", func(t *testing.T) {
		resp := testutil.MakeAPIRequest(router, http.MethodPost, "/auth/login", map[string]string{
			"password": "demo123",
		})

		testutil.AssertErrorCode(t, resp, testutil.ErrorCodeInvalidRequest)
	})

	t.Run("missing password field", func(t *testing.T) {
		resp := testutil.MakeAPIRequest(router, http.MethodPost, "/auth/login", map[string]string{
			"email": "demo@example.com",
		})

		testutil.AssertErrorCode(t, resp, testutil.ErrorCodeInvalidRequest)
	})

	t.Run("SQL injection attempt", func(t *testing.T) {
		resp := testutil.MakeAPIRequest(router, http.MethodPost, "/auth/login", loginRequest{
			Email:    "demo@example.com' OR '1'='1",
			Password: "demo123",
		})

		testutil.AssertErrorCode(t, resp, testutil.ErrorCodeUnauthorized)
	})

}

func TestRefreshIntegration(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION") == "" {
		t.Skip("set RUN_DB_INTEGRATION=1 to run")
	}

	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Skipf("db connection failed: %v", err)
	}
	defer pool.Close()
	ctx := context.Background()
	defer testutil.CleanupTestData(ctx, pool)

	store := storage.New(pool)
	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	limiter := rate.NewMemory(100, time.Minute)
	handler := NewAuthHandler(store, logger, "test-secret", 15*time.Minute, 30*24*time.Hour, limiter, "cex-auth")

	router := gin.New()
	handler.RegisterRoutes(router)

	loginResp := testutil.MakeAPIRequest(router, http.MethodPost, "/auth/login", loginRequest{
		Email:    "demo@example.com",
		Password: "demo123",
	})
	var loginOut authResponse
	json.Unmarshal(loginResp.Body.Bytes(), &loginOut)

	t.Run("success with valid refresh token", func(t *testing.T) {
		resp := testutil.MakeAPIRequest(router, http.MethodPost, "/auth/refresh", refreshRequest{
			RefreshToken: loginOut.RefreshToken,
		})

		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
		}

		var out authResponse
		if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if out.AccessToken == "" {
			t.Fatal("expected access token")
		}
		if out.RefreshToken == "" {
			t.Fatal("expected refresh token")
		}
		if out.RefreshToken == loginOut.RefreshToken {
			t.Fatal("expected token rotation")
		}
	})

	t.Run("token rotation", func(t *testing.T) {
		loginResp := testutil.MakeAPIRequest(router, http.MethodPost, "/auth/login", loginRequest{
			Email:    "demo@example.com",
			Password: "demo123",
		})
		var loginOut authResponse
		json.Unmarshal(loginResp.Body.Bytes(), &loginOut)

		oldToken := loginOut.RefreshToken

		resp := testutil.MakeAPIRequest(router, http.MethodPost, "/auth/refresh", refreshRequest{
			RefreshToken: oldToken,
		})

		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.Code)
		}

		var out authResponse
		json.Unmarshal(resp.Body.Bytes(), &out)

		reuseResp := testutil.MakeAPIRequest(router, http.MethodPost, "/auth/refresh", refreshRequest{
			RefreshToken: oldToken,
		})

		testutil.AssertErrorCode(t, reuseResp, testutil.ErrorCodeUnauthorized)
	})

	t.Run("reuse detection", func(t *testing.T) {
		loginResp := testutil.MakeAPIRequest(router, http.MethodPost, "/auth/login", loginRequest{
			Email:    "demo@example.com",
			Password: "demo123",
		})
		var loginOut authResponse
		json.Unmarshal(loginResp.Body.Bytes(), &loginOut)

		token := loginOut.RefreshToken

		testutil.MakeAPIRequest(router, http.MethodPost, "/auth/refresh", refreshRequest{
			RefreshToken: token,
		})

		resp := testutil.MakeAPIRequest(router, http.MethodPost, "/auth/refresh", refreshRequest{
			RefreshToken: token,
		})

		testutil.AssertErrorCode(t, resp, testutil.ErrorCodeUnauthorized)
	})

	t.Run("expired token", func(t *testing.T) {
		ctx := context.Background()
		userID := testutil.DemoUserID
		hash := computeHash("expired-token")
		expiredTime := time.Now().Add(-1 * time.Hour)

		tokenID := uuid.New()
		_, err := pool.Exec(ctx, `
			INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at, created_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT DO NOTHING
		`, tokenID, userID, hash, expiredTime, expiredTime)
		if err != nil {
			t.Fatalf("insert expired token: %v", err)
		}

		resp := testutil.MakeAPIRequest(router, http.MethodPost, "/auth/refresh", refreshRequest{
			RefreshToken: "expired-token",
		})

		testutil.AssertErrorCode(t, resp, testutil.ErrorCodeUnauthorized)
	})

	t.Run("invalid token format", func(t *testing.T) {
		resp := testutil.MakeAPIRequest(router, http.MethodPost, "/auth/refresh", refreshRequest{
			RefreshToken: "invalid-token-format",
		})

		testutil.AssertErrorCode(t, resp, testutil.ErrorCodeUnauthorized)
	})

	t.Run("revoked token", func(t *testing.T) {
		loginResp := testutil.MakeAPIRequest(router, http.MethodPost, "/auth/login", loginRequest{
			Email:    "demo@example.com",
			Password: "demo123",
		})
		var loginOut authResponse
		json.Unmarshal(loginResp.Body.Bytes(), &loginOut)

		testutil.MakeAPIRequest(router, http.MethodPost, "/auth/logout", refreshRequest{
			RefreshToken: loginOut.RefreshToken,
		})

		resp := testutil.MakeAPIRequest(router, http.MethodPost, "/auth/refresh", refreshRequest{
			RefreshToken: loginOut.RefreshToken,
		})

		testutil.AssertErrorCode(t, resp, testutil.ErrorCodeUnauthorized)
	})

	t.Run("missing refresh_token field", func(t *testing.T) {
		resp := testutil.MakeAPIRequest(router, http.MethodPost, "/auth/refresh", map[string]string{})

		testutil.AssertErrorCode(t, resp, testutil.ErrorCodeInvalidRequest)
	})

}

func TestLogoutIntegration(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION") == "" {
		t.Skip("set RUN_DB_INTEGRATION=1 to run")
	}

	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Skipf("db connection failed: %v", err)
	}
	defer pool.Close()
	ctx := context.Background()
	defer testutil.CleanupTestData(ctx, pool)

	store := storage.New(pool)
	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	limiter := rate.NewMemory(100, time.Minute)
	handler := NewAuthHandler(store, logger, "test-secret", 15*time.Minute, 30*24*time.Hour, limiter, "cex-auth")

	router := gin.New()
	handler.RegisterRoutes(router)

	t.Run("success with valid refresh token", func(t *testing.T) {
		loginResp := testutil.MakeAPIRequest(router, http.MethodPost, "/auth/login", loginRequest{
			Email:    "demo@example.com",
			Password: "demo123",
		})
		var loginOut authResponse
		json.Unmarshal(loginResp.Body.Bytes(), &loginOut)

		resp := testutil.MakeAPIRequest(router, http.MethodPost, "/auth/logout", refreshRequest{
			RefreshToken: loginOut.RefreshToken,
		})

		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		resp := testutil.MakeAPIRequest(router, http.MethodPost, "/auth/logout", refreshRequest{
			RefreshToken: "invalid-token",
		})

		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
		}
	})

	t.Run("already revoked token", func(t *testing.T) {
		loginResp := testutil.MakeAPIRequest(router, http.MethodPost, "/auth/login", loginRequest{
			Email:    "demo@example.com",
			Password: "demo123",
		})
		var loginOut authResponse
		json.Unmarshal(loginResp.Body.Bytes(), &loginOut)

		testutil.MakeAPIRequest(router, http.MethodPost, "/auth/logout", refreshRequest{
			RefreshToken: loginOut.RefreshToken,
		})

		resp := testutil.MakeAPIRequest(router, http.MethodPost, "/auth/logout", refreshRequest{
			RefreshToken: loginOut.RefreshToken,
		})

		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
		}
	})

}
