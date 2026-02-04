package handlers

import (
	"testing"

	"github.com/AfshinJalili/goex/libs/auth"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestValidateScopes(t *testing.T) {
	tests := []struct {
		name    string
		scopes  []string
		wantErr bool
	}{
		{"valid scopes", []string{"read", "trade"}, false},
		{"invalid scope", []string{"admin"}, true},
		{"invalid scope", []string{"invalid"}, true},
		{"empty scopes", []string{}, false},
		{"read only", []string{"read"}, false},
		{"trade only", []string{"trade"}, false},
		{"withdraw only", []string{"withdraw"}, false},
		{"mixed valid", []string{"read", "trade", "withdraw"}, false},
		{"mixed invalid", []string{"read", "admin"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateScopes(tt.scopes)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateScopes() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseLimit(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"empty string", "", 0},
		{"valid number", "10", 10},
		{"zero", "0", 0},
		{"negative", "-5", 0},
		{"very large", "999999", 999999},
		{"invalid", "abc", 0},
		{"float", "10.5", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseLimit(tt.input)
			if result != tt.expected {
				t.Fatalf("parseLimit(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestUserIDFromContext(t *testing.T) {
	c := &gin.Context{}

	_, ok := userIDFromContext(c)
	if ok {
		t.Fatal("expected false for empty context")
	}

	c.Set(auth.ContextUserIDKey, "invalid-uuid")
	_, ok = userIDFromContext(c)
	if ok {
		t.Fatal("expected false for invalid UUID")
	}

	c.Set(auth.ContextUserIDKey, uuid.New().String())
	_, ok = userIDFromContext(c)
	if !ok {
		t.Fatal("expected true for valid UUID")
	}
}
