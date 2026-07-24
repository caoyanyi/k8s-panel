package httpapi

import (
	"net"
	"net/http"
	"sync"
	"time"
)

type failureWindow struct {
	count   int
	resetAt time.Time
}

type failureLimiter struct {
	mu         sync.Mutex
	entries    map[string]failureWindow
	maxFailure int
	window     time.Duration
	maxEntries int
	now        func() time.Time
}

func newFailureLimiter(maxFailure int, window time.Duration, maxEntries int, now func() time.Time) *failureLimiter {
	return &failureLimiter{
		entries:    make(map[string]failureWindow),
		maxFailure: maxFailure,
		window:     window,
		maxEntries: maxEntries,
		now:        now,
	}
}

func (l *failureLimiter) blocked(key string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	entry, exists := l.entries[key]
	if !exists {
		return 0, false
	}
	if !now.Before(entry.resetAt) {
		delete(l.entries, key)
		return 0, false
	}
	if entry.count < l.maxFailure {
		return 0, false
	}
	return entry.resetAt.Sub(now), true
}

func (l *failureLimiter) failed(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	entry, exists := l.entries[key]
	if exists && now.Before(entry.resetAt) {
		entry.count++
		l.entries[key] = entry
		return
	}
	if !exists && len(l.entries) >= l.maxEntries {
		l.evictOldest()
	}
	l.entries[key] = failureWindow{count: 1, resetAt: now.Add(l.window)}
}

func (l *failureLimiter) reset(key string) {
	l.mu.Lock()
	delete(l.entries, key)
	l.mu.Unlock()
}

func (l *failureLimiter) evictOldest() {
	var oldestKey string
	var oldestReset time.Time
	for key, entry := range l.entries {
		if oldestKey == "" || entry.resetAt.Before(oldestReset) {
			oldestKey = key
			oldestReset = entry.resetAt
		}
	}
	delete(l.entries, oldestKey)
}

func loginClientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
