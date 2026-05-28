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
