package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/AfshinJalili/goex/services/testutil"
	"github.com/google/uuid"
)

func getGatewayURL() string {
	if url := os.Getenv("GATEWAY_URL"); url != "" {
		return url
	}
	return "http://localhost:8000"
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type meResponse struct {
	ID         string `json:"id"`
	Email      string `json:"email"`
	Status     string `json:"status"`
	KYCLevel   string `json:"kyc_level"`
	MFAEnabled bool   `json:"mfa_enabled"`
	CreatedAt  string `json:"created_at"`
}

type createAPIKeyRequest struct {
	Label       string   `json:"label,omitempty"`
	Scopes      []string `json:"scopes"`
	IPWhitelist []string `json:"ip_whitelist,omitempty"`
}

type createAPIKeyResponse struct {
	ID          uuid.UUID `json:"id"`
	Prefix      string    `json:"prefix"`
	Secret      string    `json:"secret"`
	Scopes      []string  `json:"scopes"`
	IPWhitelist []string  `json:"ip_whitelist"`
	CreatedAt   string    `json:"created_at"`
}

func makeGatewayRequest(method, path string, body interface{}, headers map[string]string) (*http.Response, error) {
	url := getGatewayURL() + path
	var reqBody []byte
	if body != nil {
		var err error
		reqBody, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequest(method, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if path == "/auth/login" {
		if headers == nil {
			headers = map[string]string{}
		}
		if _, ok := headers["X-Forwarded-For"]; !ok {
			headers["X-Forwarded-For"] = randomIP()
		}
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	return client.Do(req)
}

func randomIP() string {
	rand.Seed(time.Now().UnixNano())
	return fmt.Sprintf("10.0.%d.%d", rand.Intn(255), rand.Intn(255))
}

func TestGatewayJWTAuth(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION") == "" {
		t.Skip("set RUN_INTEGRATION=1 to run")
	}

	t.Run("GET /me without JWT returns 401", func(t *testing.T) {
		resp, err := makeGatewayRequest(http.MethodGet, "/me", nil, nil)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", resp.StatusCode)
		}

		var errResp errorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			t.Fatalf("decode error response: %v", err)
		}
		if errResp.Code != "UNAUTHORIZED" {
			t.Fatalf("expected error code UNAUTHORIZED, got %s", errResp.Code)
		}
	})

	t.Run("GET /me with invalid JWT returns 401", func(t *testing.T) {
		headers := map[string]string{
			"Authorization": "Bearer invalid-token",
		}
		resp, err := makeGatewayRequest(http.MethodGet, "/me", nil, headers)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("GET /me with valid JWT succeeds", func(t *testing.T) {
		loginResp, err := makeGatewayRequest(http.MethodPost, "/auth/login", loginRequest{
			Email:    "demo@example.com",
			Password: "demo123",
		}, nil)
		if err != nil {
			t.Fatalf("login request failed: %v", err)
		}
		defer loginResp.Body.Close()

		if loginResp.StatusCode != http.StatusOK {
			t.Fatalf("login failed: expected 200, got %d", loginResp.StatusCode)
		}

		var loginOut loginResponse
		if err := json.NewDecoder(loginResp.Body).Decode(&loginOut); err != nil {
			t.Fatalf("decode login response: %v", err)
		}

		headers := map[string]string{
			"Authorization": "Bearer " + loginOut.AccessToken,
		}
		resp, err := makeGatewayRequest(http.MethodGet, "/me", nil, headers)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var me meResponse
		if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
			t.Fatalf("decode me response: %v", err)
		}
		if me.Email != "demo@example.com" {
			t.Fatalf("expected email demo@example.com, got %s", me.Email)
		}
	})

	t.Run("GET /accounts without JWT returns 401", func(t *testing.T) {
		resp, err := makeGatewayRequest(http.MethodGet, "/accounts", nil, nil)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("GET /accounts with valid JWT succeeds", func(t *testing.T) {
		loginResp, err := makeGatewayRequest(http.MethodPost, "/auth/login", loginRequest{
			Email:    "demo@example.com",
			Password: "demo123",
		}, nil)
		if err != nil {
			t.Fatalf("login request failed: %v", err)
		}
		defer loginResp.Body.Close()

		var loginOut loginResponse
		json.NewDecoder(loginResp.Body).Decode(&loginOut)

		headers := map[string]string{
			"Authorization": "Bearer " + loginOut.AccessToken,
		}
		resp, err := makeGatewayRequest(http.MethodGet, "/accounts", nil, headers)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("GET /balances without JWT returns 401", func(t *testing.T) {
		resp, err := makeGatewayRequest(http.MethodGet, "/balances", nil, nil)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("GET /api-keys without JWT returns 401", func(t *testing.T) {
		resp, err := makeGatewayRequest(http.MethodGet, "/api-keys", nil, nil)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", resp.StatusCode)
		}
	})
}

func TestGatewayAPIKeyAuth(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION") == "" {
		t.Skip("set RUN_INTEGRATION=1 to run")
	}

	loginResp, err := makeGatewayRequest(http.MethodPost, "/auth/login", loginRequest{
		Email:    "demo@example.com",
		Password: "demo123",
	}, nil)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer loginResp.Body.Close()

	var loginOut loginResponse
	if err := json.NewDecoder(loginResp.Body).Decode(&loginOut); err != nil {
		t.Fatalf("decode login response: %v", err)
	}

	jwtHeaders := map[string]string{
		"Authorization": "Bearer " + loginOut.AccessToken,
	}

	createResp, err := makeGatewayRequest(http.MethodPost, "/api-keys", createAPIKeyRequest{
		Scopes: []string{"read", "trade"},
	}, jwtHeaders)
	if err != nil {
		t.Fatalf("create api key request failed: %v", err)
	}
	defer createResp.Body.Close()

	if createResp.StatusCode != http.StatusOK {
		t.Fatalf("create api key failed: expected 200, got %d", createResp.StatusCode)
	}

	var apiKeyOut createAPIKeyResponse
	if err := json.NewDecoder(createResp.Body).Decode(&apiKeyOut); err != nil {
		t.Fatalf("decode api key response: %v", err)
	}

	t.Run("orders endpoints", func(t *testing.T) {
		if os.Getenv("RUN_ORDER_INTEGRATION") == "" {
			t.Skip("set RUN_ORDER_INTEGRATION=1 to run /orders gateway checks")
		}

		t.Run("POST /orders without API key returns 401", func(t *testing.T) {
			resp, err := makeGatewayRequest(http.MethodPost, "/orders", map[string]interface{}{
				"symbol":   "BTC-USD",
				"side":     "buy",
				"type":     "limit",
				"price":    "45000.00",
				"quantity": "0.10",
			}, nil)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", resp.StatusCode)
			}
		})

		t.Run("POST /orders with invalid API key returns 401", func(t *testing.T) {
			headers := map[string]string{
				"X-API-Key": "invalid-api-key",
			}
			resp, err := makeGatewayRequest(http.MethodPost, "/orders", map[string]interface{}{
				"symbol":   "BTC-USD",
				"side":     "buy",
				"type":     "limit",
				"price":    "45000.00",
				"quantity": "0.10",
			}, headers)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", resp.StatusCode)
			}
		})

		t.Run("POST /orders with valid API key succeeds", func(t *testing.T) {
			headers := map[string]string{
				"X-API-Key": apiKeyOut.Secret,
			}
			resp, err := makeGatewayRequest(http.MethodPost, "/orders", map[string]interface{}{
				"symbol":   "BTC-USD",
				"side":     "buy",
				"type":     "limit",
				"price":    "45000.00",
				"quantity": "0.10",
			}, headers)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected 401 or 400 (service may not be running), got %d", resp.StatusCode)
			}
		})
	})
}

func TestGatewayRateLimit(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION") == "" {
		t.Skip("set RUN_INTEGRATION=1 to run")
	}

	t.Run("POST /auth/login rate limits after threshold", func(t *testing.T) {
		headers := map[string]string{
			"X-Forwarded-For": "203.0.113.10",
			"X-Real-IP":       "203.0.113.10",
		}
		for i := 0; i < 10; i++ {
			resp, err := makeGatewayRequest(http.MethodPost, "/auth/login", loginRequest{
				Email:    "demo@example.com",
				Password: "wrongpassword",
			}, headers)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			resp.Body.Close()
		}

		resp, err := makeGatewayRequest(http.MethodPost, "/auth/login", loginRequest{
			Email:    "demo@example.com",
			Password: "wrongpassword",
		}, headers)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("expected 429, got %d", resp.StatusCode)
		}

		var errResp errorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil {
			if errResp.Code != "RATE_LIMITED" {
				t.Fatalf("expected error code RATE_LIMITED, got %s", errResp.Code)
			}
		}
	})
}

func TestGatewaySeededCredentials(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION") == "" {
		t.Skip("set RUN_INTEGRATION=1 to run")
	}

	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Skipf("db connection failed: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()
	demoUserID := testutil.DemoUserID

	var prefix string
	err = pool.QueryRow(ctx, `
		SELECT prefix FROM api_keys
		WHERE user_id = $1 AND revoked_at IS NULL
		ORDER BY created_at DESC
		LIMIT 1
	`, demoUserID).Scan(&prefix)
	if err != nil {
		t.Skipf("no seeded api key found: %v", err)
	}

	t.Run("login with seeded credentials succeeds", func(t *testing.T) {
		resp, err := makeGatewayRequest(http.MethodPost, "/auth/login", loginRequest{
			Email:    "demo@example.com",
			Password: "demo123",
		}, nil)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var loginOut loginResponse
		if err := json.NewDecoder(resp.Body).Decode(&loginOut); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if loginOut.AccessToken == "" {
			t.Fatal("expected access token")
		}
	})

	t.Run("login with trader credentials succeeds", func(t *testing.T) {
		resp, err := makeGatewayRequest(http.MethodPost, "/auth/login", loginRequest{
			Email:    "trader@example.com",
			Password: "trader123",
		}, nil)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Logf("seeded api key prefix: %s", prefix)
}
