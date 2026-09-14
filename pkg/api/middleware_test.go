// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apimiddleware "github.com/oakwood-commons/scafctl/pkg/api/middleware"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

func TestSetupMiddleware_Default(t *testing.T) {
	router := chi.NewRouter()
	cfg := &config.APIServerConfig{}
	lgr := logr.Discard()

	apiRouter, err := SetupMiddleware(t.Context(), router, cfg, lgr)
	require.NoError(t, err)
	assert.NotNil(t, apiRouter)
}

func TestSetupMiddleware_WithCORS(t *testing.T) {
	router := chi.NewRouter()
	cfg := &config.APIServerConfig{
		CORS: config.APICORSConfig{
			Enabled:        true,
			AllowedOrigins: []string{"*"},
			AllowedMethods: []string{"GET", "POST"},
			AllowedHeaders: []string{"Content-Type"},
			MaxAge:         3600,
		},
	}
	lgr := logr.Discard()

	apiRouter, err := SetupMiddleware(t.Context(), router, cfg, lgr)
	require.NoError(t, err)
	assert.NotNil(t, apiRouter)
}

func TestSetupMiddleware_AuthMissingConfig(t *testing.T) {
	router := chi.NewRouter()
	cfg := &config.APIServerConfig{
		Auth: config.APIAuthConfig{
			AzureOIDC: config.APIAzureOIDCConfig{
				Enabled:  true,
				TenantID: "",
				ClientID: "",
			},
		},
	}
	lgr := logr.Discard()

	_, err := SetupMiddleware(t.Context(), router, cfg, lgr)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "entra OIDC is enabled but tenantId or clientId is empty")
}

func TestSetupMiddleware_WithRateLimit(t *testing.T) {
	router := chi.NewRouter()
	cfg := &config.APIServerConfig{
		RateLimit: config.APIRateLimitConfig{
			Global: &config.APIRateLimitEntry{
				MaxRequests: 100,
				Window:      "1m",
			},
		},
	}
	lgr := logr.Discard()

	apiRouter, err := SetupMiddleware(t.Context(), router, cfg, lgr)
	require.NoError(t, err)
	assert.NotNil(t, apiRouter)
}

// TestSetupMiddleware_RateLimitsWithZeroValuedConfig proves the "on by default"
// guarantee reaches embedders, not just `scafctl serve`.
//
// The default global limit is installed by Manager.Load through Viper, so any
// caller that builds a config.APIServerConfig directly -- every embedder using
// NewServer or SetupMiddleware -- has RateLimit.Global == nil. That previously
// skipped the limiter entirely, leaving the public server API unbounded while
// the CLI was bounded.
func TestSetupMiddleware_RateLimitsWithZeroValuedConfig(t *testing.T) {
	router := chi.NewRouter()

	apiRouter, err := SetupMiddleware(t.Context(), router, &config.APIServerConfig{}, logr.Discard())
	require.NoError(t, err)
	require.NotNil(t, apiRouter)

	path := "/" + settings.DefaultAPIVersion + "/ping"
	router.Get(path, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	// The limiter keys on client IP and httptest.NewRequest uses a fixed
	// RemoteAddr, so every request below shares a single bucket.
	statuses := make([]int, 0, settings.DefaultAPIRateLimitMaxRequests+1)
	for range settings.DefaultAPIRateLimitMaxRequests + 1 {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
		statuses = append(statuses, rec.Code)
	}

	assert.Equal(t, http.StatusOK, statuses[0], "the first request must be allowed")
	assert.Equal(t, http.StatusOK, statuses[settings.DefaultAPIRateLimitMaxRequests-1],
		"requests up to the limit must be allowed")
	assert.Equal(t, http.StatusTooManyRequests, statuses[settings.DefaultAPIRateLimitMaxRequests],
		"request %d must exceed the default %d-request limit",
		settings.DefaultAPIRateLimitMaxRequests+1, settings.DefaultAPIRateLimitMaxRequests)
}

func TestSetupMiddleware_TokenPassThroughWiring(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.APIServerConfig
		expected map[string]string
	}{
		{
			name: "nil config allows default GitHub header",
			cfg:  &config.APIServerConfig{},
			expected: map[string]string{
				"Github": "ghp_123456",
			},
		},
		{
			name: "configured headers override default",
			cfg: &config.APIServerConfig{
				TokenPassThrough: &config.TokenPassThroughConfig{
					AllowedHeaders: []string{"Azure-Ad"},
				},
			},
			expected: map[string]string{
				"Azure-Ad": "azure-token",
			},
		},
		{
			name: "explicit empty list allows none",
			cfg: &config.APIServerConfig{
				TokenPassThrough: &config.TokenPassThroughConfig{
					AllowedHeaders: []string{},
				},
			},
			expected: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := chi.NewRouter()
			apiRouter, err := SetupMiddleware(t.Context(), router, tt.cfg, logr.Discard())
			require.NoError(t, err)
			apiRouter.Get("/test", func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tt.expected, apimiddleware.TokensFromContext(r.Context()))
			})

			req := httptest.NewRequestWithContext(t.Context(), "GET", "/test", nil)
			req.Header.Set(fmt.Sprintf("%sGithub", apimiddleware.TokenHeaderPrefix), "ghp_123456")
			req.Header.Set(fmt.Sprintf("%sAzure-Ad", apimiddleware.TokenHeaderPrefix), "azure-token")
			apiRouter.ServeHTTP(httptest.NewRecorder(), req)
		})
	}
}

func BenchmarkSetupMiddleware(b *testing.B) {
	cfg := &config.APIServerConfig{}
	lgr := logr.Discard()

	for b.Loop() {
		router := chi.NewRouter()
		_, _ = SetupMiddleware(b.Context(), router, cfg, lgr)
	}
}
