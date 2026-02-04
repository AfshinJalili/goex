package rate

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisLimiterWindow(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer client.Close()

	lim := NewRedisLimiter(client, 2, 500*time.Millisecond, "test:")
	ctx := context.Background()

	allowed, _, err := lim.Allow(ctx, "ip", time.Now())
	if err != nil || !allowed {
		t.Fatalf("expected allow on first call")
	}

	allowed, _, err = lim.Allow(ctx, "ip", time.Now())
	if err != nil || !allowed {
		t.Fatalf("expected allow on second call")
	}

	allowed, retryAfter, err := lim.Allow(ctx, "ip", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatalf("expected rate limited")
	}
	if retryAfter <= 0 {
		t.Fatalf("expected retryAfter > 0")
	}

	s.FastForward(600 * time.Millisecond)
	allowed, _, err = lim.Allow(ctx, "ip", time.Now())
	if err != nil || !allowed {
		t.Fatalf("expected allow after window")
	}
}
