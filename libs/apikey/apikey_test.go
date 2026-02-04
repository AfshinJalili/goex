package apikey

import (
	"testing"
	"time"
)

func TestGenerateParseVerify(t *testing.T) {
	key, prefix, hash, err := Generate("dev")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	env, parsedPrefix, secret, err := Parse(key)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if env != "dev" {
		t.Fatalf("expected env dev, got %s", env)
	}
	if parsedPrefix != prefix {
		t.Fatalf("expected prefix %s, got %s", prefix, parsedPrefix)
	}
	if secret == "" {
		t.Fatalf("expected secret")
	}

	record := Record{KeyHash: hash}
	if err := Verify(key, record, "127.0.0.1"); err != nil {
		t.Fatalf("verify: %v", err)
	}

	userID, scopes, err := VerifyAPIKey(key, Record{KeyHash: hash, UserID: "user-1", Scopes: []string{"read"}}, "127.0.0.1")
	if err != nil {
		t.Fatalf("verify api key: %v", err)
	}
	if userID != "user-1" || len(scopes) != 1 || scopes[0] != "read" {
		t.Fatalf("unexpected verification result")
	}
}

func TestVerifyRejectsRevoked(t *testing.T) {
	key, _, hash, _ := Generate("dev")
	now := time.Now()
	record := Record{KeyHash: hash, RevokedAt: &now}
	if err := Verify(key, record, "127.0.0.1"); err != ErrRevokedKey {
		t.Fatalf("expected revoked error")
	}
}

func TestIPAllowlist(t *testing.T) {
	allowed := []string{"10.0.0.0/8", "203.0.113.1"}
	if !IPAllowed("10.1.2.3", allowed) {
		t.Fatalf("expected ip allowed")
	}
	if !IPAllowed("203.0.113.1", allowed) {
		t.Fatalf("expected ip allowed")
	}
	if IPAllowed("192.168.1.1", allowed) {
		t.Fatalf("expected ip denied")
	}
}

func TestValidateIPWhitelist(t *testing.T) {
	if err := ValidateIPWhitelist([]string{"10.0.0.0/8", "203.0.113.1"}); err != nil {
		t.Fatalf("expected valid whitelist")
	}
	if err := ValidateIPWhitelist([]string{"bad"}); err == nil {
		t.Fatalf("expected invalid whitelist")
	}
}
