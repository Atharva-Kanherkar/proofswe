package cli

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type submitRateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	buckets map[string]rateBucket
}

type rateBucket struct {
	start time.Time
	count int
}

func newSubmitRateLimiter(limit int, window time.Duration) *submitRateLimiter {
	return &submitRateLimiter{limit: limit, window: window, buckets: make(map[string]rateBucket)}
}

func (l *submitRateLimiter) allow(r *http.Request) bool {
	if l.limit <= 0 {
		return true
	}
	key := clientIP(r)
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	bucket := l.buckets[key]
	if bucket.start.IsZero() || now.Sub(bucket.start) >= l.window {
		l.buckets[key] = rateBucket{start: now, count: 1}
		return true
	}
	if bucket.count >= l.limit {
		return false
	}
	bucket.count++
	l.buckets[key] = bucket
	return true
}

func clientIP(r *http.Request) string {
	for _, h := range []string{"fly-client-ip", "x-real-ip"} {
		if v := strings.TrimSpace(r.Header.Get(h)); v != "" {
			return v
		}
	}
	if forwarded := strings.TrimSpace(r.Header.Get("x-forwarded-for")); forwarded != "" {
		if first, _, ok := strings.Cut(forwarded, ","); ok {
			return strings.TrimSpace(first)
		}
		return forwarded
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}
