package rate

import (
	"context"
	"testing"
	"time"
)

func TestMemoryLimiterAllowAndReset(t *testing.T) {
	lim := NewMemory(2, time.Second)
	now := time.Now()

	allowed, retry, err := lim.Allow(context.Background(), "ip", now)
	if err != nil || !allowed || retry != 0 {
		t.Fatalf("expected allow on first call")
	}

	allowed, retry, err = lim.Allow(context.Background(), "ip", now)
	if err != nil || !allowed || retry != 0 {
		t.Fatalf("expected allow on second call")
	}

	allowed, retry, err = lim.Allow(context.Background(), "ip", now)
	if err != nil || allowed {
		t.Fatalf("expected rate limit on third call")
	}
	if retry <= 0 {
		t.Fatalf("expected retryAfter > 0")
	}

	allowed, _, err = lim.Allow(context.Background(), "ip", now.Add(2*time.Second))
	if err != nil || !allowed {
		t.Fatalf("expected allow after window reset")
	}
}
