package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"log/slog"

	"github.com/AfshinJalili/goex/services/testutil"
	"github.com/AfshinJalili/goex/services/user/internal/storage"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestMeIntegration(t *testing.T) {
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
	handler := New(store, logger, "dev")

	router := gin.New()
	handler.Register(router, []byte("test-secret"))

	token, _ := testutil.GenerateJWT(testutil.DemoUserID, []byte("test-secret"), 15*time.Minute, time.Now())

	t.Run("success with valid JWT", func(t *testing.T) {
		resp := testutil.MakeAuthRequest(router, http.MethodGet, "/me", nil, token)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
		}

		var out meResponse
		if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if out.ID != testutil.DemoUserID {
			t.Fatalf("expected user id %s, got %s", testutil.DemoUserID, out.ID)
		}
	})

	t.Run("missing Authorization header", func(t *testing.T) {
		resp := testutil.MakeAuthRequest(router, http.MethodGet, "/me", nil, "")

		testutil.AssertErrorCode(t, resp, testutil.ErrorCodeUnauthorized)
	})

	t.Run("invalid JWT format", func(t *testing.T) {
		resp := testutil.MakeAuthRequest(router, http.MethodGet, "/me", nil, "invalid-token")

		testutil.AssertErrorCode(t, resp, testutil.ErrorCodeUnauthorized)
	})

	t.Run("expired JWT", func(t *testing.T) {
		expiredToken, _ := testutil.GenerateJWT(testutil.DemoUserID, []byte("test-secret"), -15*time.Minute, time.Now().Add(-30*time.Minute))
		resp := testutil.MakeAuthRequest(router, http.MethodGet, "/me", nil, expiredToken)

		testutil.AssertErrorCode(t, resp, testutil.ErrorCodeUnauthorized)
	})

	t.Run("JWT with invalid signature", func(t *testing.T) {
		invalidToken, _ := testutil.GenerateJWT(testutil.DemoUserID, []byte("wrong-secret"), 15*time.Minute, time.Now())
		resp := testutil.MakeAuthRequest(router, http.MethodGet, "/me", nil, invalidToken)

		testutil.AssertErrorCode(t, resp, testutil.ErrorCodeUnauthorized)
	})

	t.Run("malformed Bearer token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/me", nil)
		req.Header.Set("Authorization", "InvalidFormat token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		testutil.AssertErrorCode(t, w, testutil.ErrorCodeUnauthorized)
	})

}

func TestAccountsIntegration(t *testing.T) {
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
	handler := New(store, logger, "dev")

	router := gin.New()
	handler.Register(router, []byte("test-secret"))

	token, _ := testutil.GenerateJWT(testutil.DemoUserID, []byte("test-secret"), 15*time.Minute, time.Now())

	t.Run("success with default pagination", func(t *testing.T) {
		resp := testutil.MakeAuthRequest(router, http.MethodGet, "/accounts", nil, token)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
		}

		var out accountsResponse
		if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(out.Accounts) == 0 {
			t.Fatal("expected accounts")
		}
	})

	t.Run("success with custom limit", func(t *testing.T) {
		for _, limit := range []int{5, 10, 50} {
			resp := testutil.MakeAuthRequest(router, http.MethodGet, "/accounts?limit="+strconv.Itoa(limit), nil, token)
			if resp.Code != http.StatusOK {
				t.Fatalf("expected 200 for limit %d, got %d", limit, resp.Code)
			}
		}
	})

	t.Run("pagination with cursor", func(t *testing.T) {
		resp := testutil.MakeAuthRequest(router, http.MethodGet, "/accounts?limit=1", nil, token)
		var out accountsResponse
		json.Unmarshal(resp.Body.Bytes(), &out)

		if out.NextCursor != "" {
			resp2 := testutil.MakeAuthRequest(router, http.MethodGet, "/accounts?limit=1&cursor="+out.NextCursor, nil, token)
			if resp2.Code != http.StatusOK {
				t.Fatalf("expected 200 with cursor, got %d", resp2.Code)
			}
		}
	})

	t.Run("invalid cursor format", func(t *testing.T) {
		resp := testutil.MakeAuthRequest(router, http.MethodGet, "/accounts?cursor=invalid", nil, token)

		testutil.AssertErrorCode(t, resp, testutil.ErrorCodeInvalidRequest)
	})

	t.Run("empty result set", func(t *testing.T) {
		ctx := context.Background()
		newUserID := uuid.New()
		_, err := pool.Exec(ctx, `
			INSERT INTO users (id, email, password_hash, status, kyc_level, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, newUserID, "empty@example.com", "hash", "active", "basic", time.Now(), time.Now())
		if err != nil {
			t.Fatalf("create user: %v", err)
		}

		emptyToken, _ := testutil.GenerateJWT(newUserID, []byte("test-secret"), 15*time.Minute, time.Now())
		resp := testutil.MakeAuthRequest(router, http.MethodGet, "/accounts", nil, emptyToken)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.Code)
		}

		var out accountsResponse
		json.Unmarshal(resp.Body.Bytes(), &out)
		if len(out.Accounts) != 0 {
			t.Fatal("expected empty accounts")
		}
	})

	t.Run("unauthorized", func(t *testing.T) {
		resp := testutil.MakeAuthRequest(router, http.MethodGet, "/accounts", nil, "")

		testutil.AssertErrorCode(t, resp, testutil.ErrorCodeUnauthorized)
	})

}

func TestBalancesIntegration(t *testing.T) {
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
	handler := New(store, logger, "dev")

	router := gin.New()
	handler.Register(router, []byte("test-secret"))

	token, _ := testutil.GenerateJWT(testutil.DemoUserID, []byte("test-secret"), 15*time.Minute, time.Now())

	t.Run("success with seeded balances", func(t *testing.T) {
		resp := testutil.MakeAuthRequest(router, http.MethodGet, "/balances", nil, token)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
		}

		var out balancesResponse
		if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		assets := make(map[string]bool)
		for _, bal := range out.Balances {
			assets[bal.Asset] = true
		}

		expectedAssets := []string{"BTC", "ETH", "USD", "USDT"}
		for _, asset := range expectedAssets {
			if !assets[asset] {
				t.Errorf("expected asset %s in balances", asset)
			}
		}
	})

	t.Run("pagination with limit and cursor", func(t *testing.T) {
		resp := testutil.MakeAuthRequest(router, http.MethodGet, "/balances?limit=2", nil, token)
		var out balancesResponse
		json.Unmarshal(resp.Body.Bytes(), &out)

		if len(out.Balances) > 2 {
			t.Fatal("expected limit to be respected")
		}

		if out.NextCursor != "" {
			resp2 := testutil.MakeAuthRequest(router, http.MethodGet, "/balances?limit=2&cursor="+out.NextCursor, nil, token)
			if resp2.Code != http.StatusOK {
				t.Fatalf("expected 200 with cursor, got %d", resp2.Code)
			}
		}
	})

	t.Run("invalid cursor", func(t *testing.T) {
		resp := testutil.MakeAuthRequest(router, http.MethodGet, "/balances?cursor=invalid", nil, token)

		testutil.AssertErrorCode(t, resp, testutil.ErrorCodeInvalidRequest)
	})

	t.Run("empty balances", func(t *testing.T) {
		ctx := context.Background()
		newUserID := uuid.New()
		accountID := uuid.New()
		_, err := pool.Exec(ctx, `
			INSERT INTO users (id, email, password_hash, status, kyc_level, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, newUserID, "nobalance@example.com", "hash", "active", "basic", time.Now(), time.Now())
		if err != nil {
			t.Fatalf("create user: %v", err)
		}
		_, err = pool.Exec(ctx, `
			INSERT INTO accounts (id, user_id, type, status, created_at)
			VALUES ($1, $2, $3, $4, $5)
		`, accountID, newUserID, "spot", "active", time.Now())
		if err != nil {
			t.Fatalf("create account: %v", err)
		}

		emptyToken, _ := testutil.GenerateJWT(newUserID, []byte("test-secret"), 15*time.Minute, time.Now())
		resp := testutil.MakeAuthRequest(router, http.MethodGet, "/balances", nil, emptyToken)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.Code)
		}

		var out balancesResponse
		json.Unmarshal(resp.Body.Bytes(), &out)
		if len(out.Balances) != 0 {
			t.Fatal("expected empty balances")
		}
	})

	t.Run("unauthorized", func(t *testing.T) {
		resp := testutil.MakeAuthRequest(router, http.MethodGet, "/balances", nil, "")

		testutil.AssertErrorCode(t, resp, testutil.ErrorCodeUnauthorized)
	})

}

func TestCreateAPIKeyIntegration(t *testing.T) {
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
	handler := New(store, logger, "dev")

	router := gin.New()
	handler.Register(router, []byte("test-secret"))

	token, _ := testutil.GenerateJWT(testutil.DemoUserID, []byte("test-secret"), 15*time.Minute, time.Now())

	t.Run("success with valid scopes", func(t *testing.T) {
		resp := testutil.MakeAuthRequest(router, http.MethodPost, "/api-keys", createAPIKeyRequest{
			Label:  "test-key",
			Scopes: []string{"read", "trade"},
		}, token)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
		}

		var out createAPIKeyResponse
		if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if out.Secret == "" {
			t.Fatal("expected secret")
		}
		if len(out.Scopes) != 2 {
			t.Fatalf("expected 2 scopes, got %d", len(out.Scopes))
		}
	})

	t.Run("invalid scope", func(t *testing.T) {
		resp := testutil.MakeAuthRequest(router, http.MethodPost, "/api-keys", createAPIKeyRequest{
			Label:  "test-key",
			Scopes: []string{"admin", "invalid"},
		}, token)

		testutil.AssertErrorCode(t, resp, testutil.ErrorCodeInvalidRequest)
	})

	t.Run("empty scopes array", func(t *testing.T) {
		resp := testutil.MakeAuthRequest(router, http.MethodPost, "/api-keys", createAPIKeyRequest{
			Label:  "test-key",
			Scopes: []string{},
		}, token)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200 for empty scopes, got %d", resp.Code)
		}
	})

	t.Run("valid IP whitelist", func(t *testing.T) {
		resp := testutil.MakeAuthRequest(router, http.MethodPost, "/api-keys", createAPIKeyRequest{
			Label:       "test-key",
			Scopes:      []string{"read"},
			IPWhitelist: []string{"1.2.3.4"},
		}, token)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
		}
	})

	t.Run("invalid IP whitelist", func(t *testing.T) {
		resp := testutil.MakeAuthRequest(router, http.MethodPost, "/api-keys", createAPIKeyRequest{
			Label:       "test-key",
			Scopes:      []string{"read"},
			IPWhitelist: []string{"invalid", "999.999.999.999"},
		}, token)

		testutil.AssertErrorCode(t, resp, testutil.ErrorCodeInvalidRequest)
	})

	t.Run("CIDR notation in whitelist", func(t *testing.T) {
		resp := testutil.MakeAuthRequest(router, http.MethodPost, "/api-keys", createAPIKeyRequest{
			Label:       "test-key",
			Scopes:      []string{"read"},
			IPWhitelist: []string{"192.168.1.0/24"},
		}, token)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200 for CIDR, got %d: %s", resp.Code, resp.Body.String())
		}
	})

	t.Run("empty IP whitelist", func(t *testing.T) {
		resp := testutil.MakeAuthRequest(router, http.MethodPost, "/api-keys", createAPIKeyRequest{
			Label:       "test-key",
			Scopes:      []string{"read"},
			IPWhitelist: []string{},
		}, token)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200 for empty whitelist, got %d", resp.Code)
		}
	})

	t.Run("missing label field", func(t *testing.T) {
		resp := testutil.MakeAuthRequest(router, http.MethodPost, "/api-keys", map[string]interface{}{
			"scopes": []string{"read"},
		}, token)

		if resp.Code != http.StatusOK {
			t.Fatalf("label is optional, expected 200, got %d", resp.Code)
		}
	})

	t.Run("unauthorized", func(t *testing.T) {
		resp := testutil.MakeAuthRequest(router, http.MethodPost, "/api-keys", createAPIKeyRequest{
			Label:  "test-key",
			Scopes: []string{"read"},
		}, "")

		testutil.AssertErrorCode(t, resp, testutil.ErrorCodeUnauthorized)
	})

}

func TestListAPIKeysIntegration(t *testing.T) {
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
	handler := New(store, logger, "dev")

	router := gin.New()
	handler.Register(router, []byte("test-secret"))

	token, _ := testutil.GenerateJWT(testutil.DemoUserID, []byte("test-secret"), 15*time.Minute, time.Now())

	t.Run("success", func(t *testing.T) {
		resp := testutil.MakeAuthRequest(router, http.MethodGet, "/api-keys", nil, token)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
		}

		var out apiKeysResponse
		if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(out.Keys) == 0 {
			t.Fatal("expected seeded keys")
		}
	})

	t.Run("empty list", func(t *testing.T) {
		ctx := context.Background()
		newUserID := uuid.New()
		_, err := pool.Exec(ctx, `
			INSERT INTO users (id, email, password_hash, status, kyc_level, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, newUserID, "nokeys@example.com", "hash", "active", "basic", time.Now(), time.Now())
		if err != nil {
			t.Fatalf("create user: %v", err)
		}

		emptyToken, _ := testutil.GenerateJWT(newUserID, []byte("test-secret"), 15*time.Minute, time.Now())
		resp := testutil.MakeAuthRequest(router, http.MethodGet, "/api-keys", nil, emptyToken)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.Code)
		}

		var out apiKeysResponse
		json.Unmarshal(resp.Body.Bytes(), &out)
		if len(out.Keys) != 0 {
			t.Fatal("expected empty keys")
		}
	})

	t.Run("unauthorized", func(t *testing.T) {
		resp := testutil.MakeAuthRequest(router, http.MethodGet, "/api-keys", nil, "")

		testutil.AssertErrorCode(t, resp, testutil.ErrorCodeUnauthorized)
	})

}

func TestRevokeAPIKeyIntegration(t *testing.T) {
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
	handler := New(store, logger, "dev")

	router := gin.New()
	handler.Register(router, []byte("test-secret"))

	token, _ := testutil.GenerateJWT(testutil.DemoUserID, []byte("test-secret"), 15*time.Minute, time.Now())

	t.Run("success", func(t *testing.T) {
		createResp := testutil.MakeAuthRequest(router, http.MethodPost, "/api-keys", createAPIKeyRequest{
			Label:  "to-revoke",
			Scopes: []string{"read"},
		}, token)

		var createOut createAPIKeyResponse
		json.Unmarshal(createResp.Body.Bytes(), &createOut)

		resp := testutil.MakeAuthRequest(router, http.MethodDelete, "/api-keys/"+createOut.ID.String(), nil, token)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
		}
	})

	t.Run("key not found", func(t *testing.T) {
		otherUserID := uuid.New()
		otherToken, _ := testutil.GenerateJWT(otherUserID, []byte("test-secret"), 15*time.Minute, time.Now())

		resp := testutil.MakeAuthRequest(router, http.MethodDelete, "/api-keys/"+uuid.New().String(), nil, otherToken)

		testutil.AssertHTTPStatus(t, resp, http.StatusNotFound)
	})

	t.Run("invalid UUID format", func(t *testing.T) {
		resp := testutil.MakeAuthRequest(router, http.MethodDelete, "/api-keys/invalid-uuid", nil, token)

		testutil.AssertErrorCode(t, resp, testutil.ErrorCodeInvalidRequest)
	})

	t.Run("already revoked key", func(t *testing.T) {
		createResp := testutil.MakeAuthRequest(router, http.MethodPost, "/api-keys", createAPIKeyRequest{
			Label:  "to-revoke-twice",
			Scopes: []string{"read"},
		}, token)

		var createOut createAPIKeyResponse
		json.Unmarshal(createResp.Body.Bytes(), &createOut)

		testutil.MakeAuthRequest(router, http.MethodDelete, "/api-keys/"+createOut.ID.String(), nil, token)
		resp := testutil.MakeAuthRequest(router, http.MethodDelete, "/api-keys/"+createOut.ID.String(), nil, token)

		testutil.AssertHTTPStatus(t, resp, http.StatusNotFound)
	})

	t.Run("unauthorized", func(t *testing.T) {
		resp := testutil.MakeAuthRequest(router, http.MethodDelete, "/api-keys/"+uuid.New().String(), nil, "")

		testutil.AssertErrorCode(t, resp, testutil.ErrorCodeUnauthorized)
	})

}
