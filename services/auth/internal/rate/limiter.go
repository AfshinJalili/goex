package rate

import (
	"sync"
	"time"
)

type Limiter struct {
	mu          sync.Mutex
	limit       int
	window      time.Duration
	entries     map[string]*entry
	lastCleanup time.Time
}

type entry struct {
	count int
	reset time.Time
}

func New(limit int, window time.Duration) *Limiter {
	return &Limiter{
		limit:       limit,
		window:      window,
		entries:     map[string]*entry{},
		lastCleanup: time.Now(),
	}
}

func (l *Limiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastCleanup) >= l.window {
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
		return true
	}

	if e.count >= l.limit {
		return false
	}
	e.count++
	return true
}
