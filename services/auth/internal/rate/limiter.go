package rate

import (
	"context"
	"time"
)

// Limiter is a generic interface for rate limiting implementations.
type Limiter interface {
	Allow(ctx context.Context, key string, now time.Time) (allowed bool, retryAfter time.Duration, err error)
}
