// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package endpoints

import (
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

func TestRegisterAdminEndpoints_Info(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterAdminEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Get("/v1/admin/info")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "uptime")
	assert.Contains(t, resp.Body.String(), "startTime")
}

func TestRegisterAdminEndpoints_Info_NilProviderRegistry(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterAdminEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Get("/v1/admin/info")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), `"providers":0`)
}

func TestRegisterAdminEndpoints_ReloadConfig(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterAdminEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/admin/reload-config")
	require.Equal(t, http.StatusNotImplemented, resp.Code)
}

func TestRegisterAdminEndpoints_ClearCache(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterAdminEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/admin/clear-cache")
	require.Equal(t, http.StatusNotImplemented, resp.Code)
}

func BenchmarkAdminInfoEndpoint(b *testing.B) {
	_, testAPI := humatest.New(b)
	var shutting int32
	hctx := &api.HandlerContext{
		Config:         &config.Config{},
		IsShuttingDown: &shutting,
		StartTime:      time.Now(),
	}
	RegisterAdminEndpoints(testAPI, hctx, "/v1")
	for b.Loop() {
		testAPI.Get("/v1/admin/info")
	}
}
