// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimit returns middleware that limits requests per IP using a sliding window.
// ctx controls the lifetime of the background cleanup goroutine; cancel it (e.g.
// on server shutdown) to stop the goroutine and prevent leaks.
// Set trustProxy true only when a trusted reverse proxy sanitizes X-Forwarded-For
// and X-Real-IP; otherwise leave false to use RemoteAddr and prevent IP spoofing.
func RateLimit(ctx context.Context, maxRequests int, window time.Duration, trustProxy bool) func(http.Handler) http.Handler {
	limiter := newIPRateLimiter(maxRequests, window, trustProxy)
	// Background cleanup goroutine to evict stale entries; stops on ctx cancellation.
	go limiter.cleanup(ctx)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractIP(r, limiter.trustProxy)
			remaining, resetAt, allowed := limiter.allowWithInfo(ip)

			// Always set rate limit headers
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", maxRequests))
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", resetAt.Unix()))

			if !allowed {
				// RFC 7231 §7.1.3 requires Retry-After to be an integer number of seconds.
				// Use the remaining time until resetAt (when the earliest request expires)
				// rather than the full window, so clients wait only as long as necessary.
				retryAfterSec := int(math.Ceil(time.Until(resetAt).Seconds()))
				if retryAfterSec < 1 {
					retryAfterSec = 1
				}
				w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfterSec))
				writeJSONError(w, `{"title":"Too Many Requests","status":429,"detail":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ipRateLimiter implements a simple sliding window rate limiter per IP.
type ipRateLimiter struct {
	mu          sync.Mutex
	clients     map[string]*clientWindow
	maxRequests int
	window      time.Duration
	trustProxy  bool
}

// clientWindow tracks request timestamps for a single client.
type clientWindow struct {
	timestamps []time.Time
	lastSeen   time.Time
}

func newIPRateLimiter(maxRequests int, window time.Duration, trustProxy bool) *ipRateLimiter {
	return &ipRateLimiter{
		clients:     make(map[string]*clientWindow),
		maxRequests: maxRequests,
		window:      window,
		trustProxy:  trustProxy,
	}
}

// allowWithInfo checks if the request from the given IP is within the rate limit
// and returns the remaining request count, window reset time, and whether the request is allowed.
func (l *ipRateLimiter) allowWithInfo(ip string) (remaining int, resetAt time.Time, allowed bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cw, ok := l.clients[ip]
	if !ok {
		cw = &clientWindow{}
		l.clients[ip] = cw
	}

	cw.lastSeen = now

	// Remove expired timestamps
	cutoff := now.Add(-l.window)
	valid := cw.timestamps[:0]
	for _, ts := range cw.timestamps {
		if ts.After(cutoff) {
			valid = append(valid, ts)
		}
	}
	cw.timestamps = valid

	resetAt = now.Add(l.window)
	if len(cw.timestamps) > 0 {
		resetAt = cw.timestamps[0].Add(l.window)
	}

	if len(cw.timestamps) >= l.maxRequests {
		return 0, resetAt, false
	}
	cw.timestamps = append(cw.timestamps, now)
	remaining = l.maxRequests - len(cw.timestamps)
	return remaining, resetAt, true
}

// cleanup removes stale client entries periodically. It exits when ctx is cancelled.
func (l *ipRateLimiter) cleanup(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.mu.Lock()
			cutoff := time.Now().Add(-2 * l.window)
			for ip, cw := range l.clients {
				if cw.lastSeen.Before(cutoff) {
					delete(l.clients, ip)
				}
			}
			l.mu.Unlock()
		}
	}
}

// extractIP returns the client's IP address from the request.
// When trustProxy is true, X-Forwarded-For and X-Real-IP headers are consulted
// (safe only when a trusted reverse proxy sanitizes these headers).
// When trustProxy is false (default for rate limiting), r.RemoteAddr is always
// used to prevent clients from spoofing their IP to bypass rate limits.
func extractIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// Take the left-most IP (client IP); trim spaces from proxy-added padding.
			// Safe only when a sanitizing proxy sets this header.
			parts := strings.SplitN(xff, ",", 2)
			return strings.TrimSpace(parts[0])
		}
		if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" {
			return xrip
		}
	}
	// Strip port from RemoteAddr using net.SplitHostPort to correctly handle
	// both IPv4 ("1.2.3.4:port") and IPv6 ("[::1]:port") formats.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// No port present (unusual); use address as-is.
		return r.RemoteAddr
	}
	return host
}
