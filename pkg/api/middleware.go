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
// Global middleware (logging, recovery, request ID, strip-slashes) runs for
// every request, including health probes and the /metrics endpoint.
//
// API-specific middleware (authentication, rate limiting, security headers,
// compression, metrics instrumentation, etc.) is scoped to versioned paths
// (e.g. /v1/*) via makeVersionedOnly so that health probes and /metrics are
// never blocked or instrumented by the per-request Metrics() middleware.
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

	// makePrefixOnly wraps mw so it only activates for requests whose path
	// starts with prefix, or equals it with the trailing slash removed. The
	// bare-path case matters because chi's StripSlashes rewrites the routing
	// path but leaves r.URL.Path intact, so a route registered at "/v1/admin"
	// would route successfully while a strict HasPrefix("/v1/admin/") check
	// missed it. The inner handler is built once at setup time (not
	// per-request) so there is no allocation overhead.
	//
	// NOTE: this gate reads r.URL.Path (decoded) while chi routes on
	// RouteContext.RoutePath/RawPath. Adding path-rewriting middleware such as
	// chimiddleware.CleanPath above this point could desync the two; revisit
	// this matcher if that changes.
	makePrefixOnly := func(prefix string, mw func(http.Handler) http.Handler) func(http.Handler) http.Handler {
		bare := strings.TrimSuffix(prefix, "/")
		return func(next http.Handler) http.Handler {
			wrapped := mw(next)
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == bare || strings.HasPrefix(r.URL.Path, prefix) {
					wrapped.ServeHTTP(w, r)
					return
				}
				next.ServeHTTP(w, r)
			})
		}
	}

	// makeVersionedOnly restricts mw to versioned business endpoints, so health
	// probes and /metrics are never blocked or instrumented by it.
	makeVersionedOnly := func(mw func(http.Handler) http.Handler) func(http.Handler) http.Handler {
		return makePrefixOnly(versionedPrefix, mw)
	}

	// ── Global middleware (all routes including health probes) ──
	router.Use(chimiddleware.Recoverer)
	router.Use(chimiddleware.RequestID)
	router.Use(middleware.FlightID)
	router.Use(chimiddleware.StripSlashes)
	router.Use(middleware.RequestLogging(lgr))
	if cfg.TokenPassThrough != nil {
		if err := cfg.TokenPassThrough.Validate(); err != nil {
			return nil, fmt.Errorf("invalid token pass-through configuration: %w", err)
		}
	}
	router.Use(middleware.TokenPassthrough(cfg.TokenPassThroughAllowedHeaders()))

	// ── API middleware (versioned paths only) ──

	// 0. Host allowlist.
	//
	// Runs first so a request for a host this server does not serve is rejected
	// before any other middleware does work. Guards against DNS rebinding,
	// where an attacker-controlled name is pointed at this server so a victim's
	// browser will talk to it while keeping the attacker's page origin.
	//
	// Scoped to versioned paths, matching the rest of this chain, so health and
	// readiness probes (which commonly send a pod IP as the Host) keep working
	// when an allowlist is configured. Empty config accepts every host.
	router.Use(makeVersionedOnly(middleware.HostAllowlist(cfg.AllowedHosts, lgr)))

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

	// 3. Max concurrent in-flight requests (chi Throttle)
	maxConns := cfg.MaxConcurrent
	if maxConns <= 0 {
		maxConns = settings.DefaultAPIMaxConcurrentRequests
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

	// 4b. Admin authorization.
	//
	// Registered after authentication so validated claims are in the request
	// context. Enforces the policy documented in docs/design/api-surface.md:
	// an "admin" role claim when auth is enabled, loopback-only when it is not.
	// Without this the admin routes are reachable by any caller that reaches
	// the port, despite the documentation promising otherwise.
	adminPrefix := versionedPrefix + "admin/"
	router.Use(makePrefixOnly(adminPrefix, middleware.AdminAuthorization(cfg.Auth.AzureOIDC.Enabled, lgr)))

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
