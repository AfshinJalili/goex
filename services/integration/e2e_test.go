package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

type accountsResponse struct {
	Accounts []accountInfo `json:"accounts"`
}

type accountInfo struct {
	ID        uuid.UUID `json:"id"`
	Type      string    `json:"type"`
	Status    string    `json:"status"`
	CreatedAt string    `json:"created_at"`
}

type balancesResponse struct {
	Balances []balanceInfo `json:"balances"`
}

type balanceInfo struct {
	AccountID uuid.UUID `json:"account_id"`
	Asset     string    `json:"asset"`
	Available string    `json:"available"`
	Locked    string    `json:"locked"`
	UpdatedAt string    `json:"updated_at"`
}

type listAPIKeysResponse struct {
	Keys []apiKeyInfo `json:"keys"`
}

type apiKeyInfo struct {
	ID          uuid.UUID `json:"id"`
	Prefix      string    `json:"prefix"`
	Scopes      []string  `json:"scopes"`
	IPWhitelist []string  `json:"ip_whitelist"`
	LastUsedAt  *string   `json:"last_used_at"`
	RevokedAt   *string   `json:"revoked_at"`
	CreatedAt   string    `json:"created_at"`
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func TestE2EFlow(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION") == "" {
		t.Skip("set RUN_INTEGRATION=1 to run")
	}

	waitForGatewayRoutes(t)

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

	if loginOut.AccessToken == "" {
		t.Fatal("expected access token")
	}
	if loginOut.RefreshToken == "" {
		t.Fatal("expected refresh token")
	}

	headers := map[string]string{
		"Authorization": "Bearer " + loginOut.AccessToken,
	}

	t.Run("GET /me", func(t *testing.T) {
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

	t.Run("GET /accounts", func(t *testing.T) {
		resp, err := makeGatewayRequest(http.MethodGet, "/accounts", nil, headers)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var accounts accountsResponse
		if err := json.NewDecoder(resp.Body).Decode(&accounts); err != nil {
			t.Fatalf("decode accounts response: %v", err)
		}
		if len(accounts.Accounts) == 0 {
			t.Fatal("expected at least one account")
		}
	})

	t.Run("GET /balances", func(t *testing.T) {
		resp, err := makeGatewayRequest(http.MethodGet, "/balances", nil, headers)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var balances balancesResponse
		if err := json.NewDecoder(resp.Body).Decode(&balances); err != nil {
			t.Fatalf("decode balances response: %v", err)
		}
		if len(balances.Balances) == 0 {
			t.Fatal("expected at least one balance")
		}
	})

	var createdKeyID uuid.UUID
	t.Run("POST /api-keys", func(t *testing.T) {
		resp, err := makeGatewayRequest(http.MethodPost, "/api-keys", createAPIKeyRequest{
			Label:  "e2e-test-key",
			Scopes: []string{"read", "trade"},
		}, headers)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var apiKey createAPIKeyResponse
		if err := json.NewDecoder(resp.Body).Decode(&apiKey); err != nil {
			t.Fatalf("decode api key response: %v", err)
		}
		if apiKey.Secret == "" {
			t.Fatal("expected secret in response")
		}
		createdKeyID = apiKey.ID
	})

	t.Run("GET /api-keys", func(t *testing.T) {
		resp, err := makeGatewayRequest(http.MethodGet, "/api-keys", nil, headers)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var keys listAPIKeysResponse
		if err := json.NewDecoder(resp.Body).Decode(&keys); err != nil {
			t.Fatalf("decode api keys response: %v", err)
		}
		if len(keys.Keys) == 0 {
			t.Fatal("expected at least one API key")
		}

		found := false
		for _, key := range keys.Keys {
			if key.ID == createdKeyID {
				found = true
				if key.RevokedAt != nil {
					t.Fatal("created key should not be revoked")
				}
				break
			}
		}
		if !found {
			t.Fatal("created API key not found in list")
		}
	})

	t.Run("DELETE /api-keys/{id}", func(t *testing.T) {
		resp, err := makeGatewayRequest(http.MethodDelete, "/api-keys/"+createdKeyID.String(), nil, headers)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("POST /auth/logout", func(t *testing.T) {
		resp, err := makeGatewayRequest(http.MethodPost, "/auth/logout", logoutRequest{
			RefreshToken: loginOut.RefreshToken,
		}, nil)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})
}

func waitForGatewayRoutes(t *testing.T) {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := makeGatewayRequest(http.MethodGet, "/me", nil, nil)
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	t.Fatal("gateway routes not ready within timeout")
}
