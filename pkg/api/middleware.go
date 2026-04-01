// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-logr/logr"

	"github.com/oakwood-commons/scafctl/pkg/api/middleware"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// SetupMiddleware configures the middleware stack on the root router.
//
// Global middleware (logging, recovery, request ID) runs for every request,
// including health probes and Prometheus metrics.
//
// API-specific middleware (authentication, rate limiting, security headers,
// compression, etc.) is scoped to versioned paths (e.g. /v1/*) via
// makeVersionedOnly so that health probes and /metrics are never blocked.
//
// Because Huma is backed by the same root router, every route registered with
// huma.Register runs through the full middleware chain assembled here.
// Returns the root router for API-router compatibility with callers.
func SetupMiddleware(ctx context.Context, router *chi.Mux, cfg *config.APIServerConfig, lgr logr.Logger) (chi.Router, error) {
	// Validate auth configuration: refuse to start unauthenticated when auth is expected.
	if cfg.Auth.AzureOIDC.Enabled {
		if cfg.Auth.AzureOIDC.TenantID == "" || cfg.Auth.AzureOIDC.ClientID == "" {
			return nil, fmt.Errorf("entra OIDC is enabled but tenantId or clientId is empty")
		}
	}

	version := cfg.APIVersion
	if version == "" {
		version = settings.DefaultAPIVersion
	}
	// All versioned business endpoints share this prefix.
	versionedPrefix := "/" + version + "/"

	// makeVersionedOnly wraps mw so it only activates for requests whose path
	// starts with versionedPrefix. The inner handler is built once at setup
	// time (not per-request) so there is no allocation overhead.
	makeVersionedOnly := func(mw func(http.Handler) http.Handler) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			wrapped := mw(next)
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, versionedPrefix) {
					wrapped.ServeHTTP(w, r)
					return
				}
				next.ServeHTTP(w, r)
			})
		}
	}

	// ── Global middleware (all routes including health probes) ──
	router.Use(chimiddleware.Recoverer)
	router.Use(chimiddleware.RequestID)
	router.Use(chimiddleware.StripSlashes)
	router.Use(middleware.RequestLogging(lgr))

	// ── API middleware (versioned paths only) ──

	// 1. CORS
	if cfg.CORS.Enabled {
		router.Use(makeVersionedOnly(cors.Handler(cors.Options{
			AllowedOrigins: cfg.CORS.AllowedOrigins,
			AllowedMethods: cfg.CORS.AllowedMethods,
			AllowedHeaders: cfg.CORS.AllowedHeaders,
			MaxAge:         cfg.CORS.MaxAge,
		})))
	}

	// 2. Request timeout
	reqTimeout := parseTimeoutOrDefault(cfg.RequestTimeout, settings.DefaultAPIRequestTimeout)
	router.Use(makeVersionedOnly(chimiddleware.Timeout(reqTimeout)))

	// 3. Max concurrent connections
	maxConns := cfg.MaxConcurrent
	if maxConns <= 0 {
		maxConns = settings.DefaultAPIMaxConcurrentConns
	}
	router.Use(makeVersionedOnly(chimiddleware.Throttle(maxConns)))

	// 4. Authentication
	if cfg.Auth.AzureOIDC.Enabled {
		authMW, err := middleware.NewAzureOIDCAuth(
			cfg.Auth.AzureOIDC.TenantID,
			cfg.Auth.AzureOIDC.ClientID,
			lgr,
		)
		if err != nil {
			return nil, fmt.Errorf("initializing OIDC auth middleware: %w", err)
		}
		router.Use(makeVersionedOnly(authMW))
	}

	// 5. Rate limiting
	if cfg.RateLimit.Global != nil {
		window := parseTimeoutOrDefault(cfg.RateLimit.Global.Window, settings.DefaultAPIRateLimitWindow)
		router.Use(makeVersionedOnly(middleware.RateLimit(ctx, cfg.RateLimit.Global.MaxRequests, window, cfg.RateLimit.Global.TrustProxy)))
	}

	// 6. Request size limits
	maxReqSize := cfg.MaxRequestSize
	if maxReqSize <= 0 {
		maxReqSize = settings.DefaultAPIMaxRequestSize
	}
	router.Use(makeVersionedOnly(middleware.MaxBodySize(maxReqSize)))

	// 7. Compression — Level 0 means disabled per config doc "(0-9, 0=disabled)".
	compLevel := cfg.Compression.Level
	if compLevel > 0 {
		compressor := chimiddleware.NewCompressor(compLevel, "application/json")
		compressor.SetEncoder("gzip", middleware.GzipEncoderFunc)
		router.Use(makeVersionedOnly(compressor.Handler))
	}

	// 8. Security headers
	router.Use(makeVersionedOnly(middleware.SecurityHeaders(cfg.TLS.Enabled)))

	// 9. Metrics
	router.Use(makeVersionedOnly(middleware.Metrics()))

	// 10. Audit logging
	if cfg.Audit.Enabled {
		router.Use(makeVersionedOnly(middleware.AuditLog(lgr, cfg.Audit.TrustProxy)))
	}

	// 11. Tracing
	if cfg.Tracing.Enabled {
		router.Use(makeVersionedOnly(middleware.Tracing()))
	}

	// Return the root router. SetupMiddleware previously returned a sub-router
	// mounted at /v1; with the makeVersionedOnly approach the root router carries
	// all middleware and serves as the API router for callers.
	return router, nil
}
