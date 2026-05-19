// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"context"
	"net/http"
	"sync/atomic"
)

var flightCounter atomic.Uint64

const flightIDContextKey contextKey = "flightID"

// FlightID is a middleware that assigns a unique monotonic uint64 to each request
// and stores it in the request context for go-flight singleflight tagging.
func FlightID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := flightCounter.Add(1)
		ctx := context.WithValue(r.Context(), flightIDContextKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// FlightIDFromContext extracts the flight ID from context.
// Returns 0 if no ID was set.
func FlightIDFromContext(ctx context.Context) uint64 {
	id, _ := ctx.Value(flightIDContextKey).(uint64)
	return id
}

// FlightIDExtractor returns a function compatible with go-flight's
// cache.RequestIDExtractor type for singleflight tagging.
func FlightIDExtractor() func(ctx context.Context) uint64 {
	return FlightIDFromContext
}
