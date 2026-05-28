// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package authdelegation

import "net/http"

// Middleware returns an HTTP middleware that injects the given
// DelegatorRegistry into every request's context. If reg is nil the
// middleware is a no-op passthrough.
func Middleware(reg *DelegatorRegistry) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if reg == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := WithRegistry(r.Context(), reg)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
