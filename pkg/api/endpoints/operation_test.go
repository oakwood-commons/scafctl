// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package endpoints

import (
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"

	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

func TestWithDefaults_SetsAllFieldsWhenZero(t *testing.T) {
	// Auth is disabled (zero value config), so Security should be noSecurity.
	hctx := &api.HandlerContext{}

	op := withDefaults(huma.Operation{
		OperationID: "test-op",
		Method:      http.MethodGet,
		Path:        "/test",
	}, hctx, http.StatusOK)

	assert.Equal(t, http.StatusOK, op.DefaultStatus)
	assert.Equal(t, settings.DefaultAPIOperationMaxBodyBytes, op.MaxBodyBytes)
	assert.NotZero(t, op.BodyReadTimeout)
	assert.Equal(t, noSecurity, op.Security)
	assert.Equal(t, authenticatedGETErrors, op.Errors)
}

func TestWithDefaults_PreservesExplicitValues(t *testing.T) {
	hctx := &api.HandlerContext{}

	customSecurity := []map[string][]string{{"apiKey": {}}}
	customErrors := []int{http.StatusTeapot}
	customTimeout := 5 * time.Second

	op := withDefaults(huma.Operation{
		OperationID:     "test-op",
		Method:          http.MethodPost,
		Path:            "/test",
		DefaultStatus:   http.StatusCreated,
		MaxBodyBytes:    2048,
		BodyReadTimeout: customTimeout,
		Security:        customSecurity,
		Errors:          customErrors,
	}, hctx, http.StatusOK)

	assert.Equal(t, http.StatusCreated, op.DefaultStatus)
	assert.Equal(t, int64(2048), op.MaxBodyBytes)
	assert.Equal(t, customTimeout, op.BodyReadTimeout)
	assert.Equal(t, customSecurity, op.Security)
	assert.Equal(t, customErrors, op.Errors)
}

func TestWithDefaults_POSTGetsPostErrors(t *testing.T) {
	hctx := &api.HandlerContext{}

	op := withDefaults(huma.Operation{
		OperationID: "test-post",
		Method:      http.MethodPost,
		Path:        "/test",
	}, hctx, http.StatusOK)

	assert.Equal(t, authenticatedPOSTErrors, op.Errors)
}

func TestWithDefaults_GETGetsGetErrors(t *testing.T) {
	hctx := &api.HandlerContext{}

	op := withDefaults(huma.Operation{
		OperationID: "test-get",
		Method:      http.MethodGet,
		Path:        "/test",
	}, hctx, http.StatusOK)

	assert.Equal(t, authenticatedGETErrors, op.Errors)
}

func TestWithPublicDefaults_SetsNoAuthSecurity(t *testing.T) {
	hctx := &api.HandlerContext{}

	op := withPublicDefaults(huma.Operation{
		OperationID: "health",
		Method:      http.MethodGet,
		Path:        "/health",
	}, hctx, http.StatusOK)

	assert.Equal(t, noSecurity, op.Security)
	assert.Equal(t, publicErrors, op.Errors)
}

func TestWithDefaults_UsesDefaultMaxBodyBytes(t *testing.T) {
	// MaxBodyBytes should always use DefaultAPIOperationMaxBodyBytes regardless
	// of MaxRequestSize (which is the outer chi limit, not the per-op Huma limit).
	hctx := &api.HandlerContext{
		Config: &config.Config{
			APIServer: config.APIServerConfig{
				MaxRequestSize: 5 * 1024 * 1024, // 5 MiB — outer chi limit, must not bleed into Huma
			},
		},
	}

	op := withDefaults(huma.Operation{
		OperationID: "test-op",
		Method:      http.MethodGet,
		Path:        "/test",
	}, hctx, http.StatusOK)

	assert.Equal(t, settings.DefaultAPIOperationMaxBodyBytes, op.MaxBodyBytes)
}

func TestWithDefaults_AuthEnabledSetsOAuthSecurity(t *testing.T) {
	// When Entra OIDC auth is enabled, withDefaults should use oauthSecurity
	// so that the OpenAPI spec accurately reflects the runtime auth requirement.
	hctx := &api.HandlerContext{
		Config: &config.Config{
			APIServer: config.APIServerConfig{
				Auth: config.APIAuthConfig{
					AzureOIDC: config.APIAzureOIDCConfig{
						Enabled:  true,
						TenantID: "tenant",
						ClientID: "client",
					},
				},
			},
		},
	}

	op := withDefaults(huma.Operation{
		OperationID: "test-op",
		Method:      http.MethodGet,
		Path:        "/test",
	}, hctx, http.StatusOK)

	assert.Equal(t, oauthSecurity, op.Security)
}

func TestWithDefaults_UsesConfigBodyReadTimeout(t *testing.T) {
	hctx := &api.HandlerContext{
		Config: &config.Config{
			APIServer: config.APIServerConfig{
				BodyReadTimeout: "30s",
			},
		},
	}

	op := withDefaults(huma.Operation{
		OperationID: "test-op",
		Method:      http.MethodGet,
		Path:        "/test",
	}, hctx, http.StatusOK)

	assert.Equal(t, 30*time.Second, op.BodyReadTimeout)
}

func TestWithDefaults_FallsBackOnInvalidBodyReadTimeout(t *testing.T) {
	hctx := &api.HandlerContext{
		Config: &config.Config{
			APIServer: config.APIServerConfig{
				BodyReadTimeout: "not-a-duration",
			},
		},
	}

	op := withDefaults(huma.Operation{
		OperationID: "test-op",
		Method:      http.MethodGet,
		Path:        "/test",
	}, hctx, http.StatusOK)

	expected, _ := time.ParseDuration(settings.DefaultAPIBodyReadTimeout)
	assert.Equal(t, expected, op.BodyReadTimeout)
}

func TestWithDefaults_NilHandlerContext(t *testing.T) {
	// nil hctx means auth state is unknown — default to noSecurity so the spec
	// doesn't claim auth is required when no auth middleware is installed.
	op := withDefaults(huma.Operation{
		OperationID: "test-op",
		Method:      http.MethodGet,
		Path:        "/test",
	}, nil, http.StatusOK)

	assert.Equal(t, http.StatusOK, op.DefaultStatus)
	assert.Equal(t, settings.DefaultAPIOperationMaxBodyBytes, op.MaxBodyBytes)
	assert.NotZero(t, op.BodyReadTimeout)
	assert.Equal(t, noSecurity, op.Security)
}

func TestWithDefaults_AdminMaxBodyOverride(t *testing.T) {
	hctx := &api.HandlerContext{}

	op := withDefaults(huma.Operation{
		OperationID:  "admin-reload",
		Method:       http.MethodPost,
		Path:         "/admin/reload",
		MaxBodyBytes: settings.DefaultAPIAdminMaxBodyBytes,
	}, hctx, http.StatusOK)

	assert.Equal(t, settings.DefaultAPIAdminMaxBodyBytes, op.MaxBodyBytes)
}

func TestParseBodyReadTimeout_EmptyConfig(t *testing.T) {
	expected, _ := time.ParseDuration(settings.DefaultAPIBodyReadTimeout)

	tests := []struct {
		name string
		hctx *api.HandlerContext
	}{
		{name: "nil_hctx", hctx: nil},
		{name: "nil_config", hctx: &api.HandlerContext{}},
		{name: "empty_timeout", hctx: &api.HandlerContext{Config: &config.Config{}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseBodyReadTimeout(tt.hctx)
			assert.Equal(t, expected, got)
		})
	}
}

func BenchmarkWithDefaults(b *testing.B) {
	hctx := &api.HandlerContext{
		Config: &config.Config{
			APIServer: config.APIServerConfig{
				MaxRequestSize:  10 * 1024 * 1024,
				BodyReadTimeout: "15s",
			},
		},
	}

	op := huma.Operation{
		OperationID: "bench-op",
		Method:      http.MethodPost,
		Path:        "/bench",
		Summary:     "Benchmark operation",
		Tags:        []string{"Bench"},
	}

	b.ResetTimer()
	for range b.N {
		_ = withDefaults(op, hctx, http.StatusOK)
	}
}

func BenchmarkWithPublicDefaults(b *testing.B) {
	hctx := &api.HandlerContext{}

	op := huma.Operation{
		OperationID: "bench-health",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Benchmark health",
		Tags:        []string{"Health"},
	}

	b.ResetTimer()
	for range b.N {
		_ = withPublicDefaults(op, hctx, http.StatusOK)
	}
}
