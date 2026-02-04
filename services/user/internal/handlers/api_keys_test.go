package handlers

import "testing"

func TestValidateScopes(t *testing.T) {
	if err := validateScopes([]string{"read", "trade"}); err != nil {
		t.Fatalf("expected scopes valid")
	}
	if err := validateScopes([]string{"admin"}); err == nil {
		t.Fatalf("expected invalid scopes")
	}
}
