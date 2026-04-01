// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func BenchmarkSetupMiddleware(b *testing.B) {
	cfg := &config.APIServerConfig{}
	lgr := logr.Discard()

	for b.Loop() {
		router := chi.NewRouter()
		_, _ = SetupMiddleware(b.Context(), router, cfg, lgr)
	}
}
