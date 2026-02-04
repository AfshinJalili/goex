package rate

import (
	"testing"
	"time"
)

func TestLimiterCleanup(t *testing.T) {
	limiter := New(1, 1*time.Second)
	now := time.Now()

	if !limiter.Allow("1.1.1.1", now) {
		t.Fatalf("expected allow")
	}
	if len(limiter.entries) != 1 {
		t.Fatalf("expected entry")
	}

	later := now.Add(2 * time.Second)
	limiter.Allow("2.2.2.2", later)
	if len(limiter.entries) != 1 {
		t.Fatalf("expected cleanup to remove expired entries")
	}
}
