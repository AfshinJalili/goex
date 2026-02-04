package rate

import (
	"context"
	"sync"
	"time"
)

type MemoryLimiter struct {
	mu           sync.Mutex
	limit        int
	window       time.Duration
	entries      map[string]*entry
	lastCleanup  time.Time
	cleanupEvery time.Duration
}

type entry struct {
	count int
	reset time.Time
}

func NewMemory(limit int, window time.Duration) *MemoryLimiter {
	return &MemoryLimiter{
		limit:        limit,
		window:       window,
		entries:      map[string]*entry{},
		lastCleanup:  time.Now(),
		cleanupEvery: window,
	}
}

func (l *MemoryLimiter) Allow(_ context.Context, key string, now time.Time) (bool, time.Duration, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastCleanup) >= l.cleanupEvery {
		for k, v := range l.entries {
			if now.After(v.reset) {
				delete(l.entries, k)
			}
		}
		l.lastCleanup = now
	}

	e, ok := l.entries[key]
	if !ok || now.After(e.reset) {
		l.entries[key] = &entry{count: 1, reset: now.Add(l.window)}
		return true, 0, nil
	}

	if e.count >= l.limit {
		retryAfter := time.Until(e.reset)
		if retryAfter < 0 {
			retryAfter = 0
		}
		return false, retryAfter, nil
	}

	e.count++
	return true, 0, nil
}
