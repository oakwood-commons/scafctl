// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package endpoints

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

func TestRegisterAll_NoPanic(t *testing.T) {
	_, testAPI := humatest.New(t)
	router := chi.NewRouter()
	hctx := newTestHandlerContext(t)
	// RegisterAll must not panic and must register all route groups.
	require.NotPanics(t, func() {
		RegisterAll(testAPI, router, hctx)
	})
}

func TestRegisterAll_HealthEndpointAccessible(t *testing.T) {
	_, testAPI := humatest.New(t)
	router := chi.NewRouter()
	hctx := newTestHandlerContext(t)
	RegisterAll(testAPI, router, hctx)

	// /health is registered by RegisterHealthEndpoints called from RegisterAll.
	resp := testAPI.Get("/health")
	assert.Equal(t, http.StatusOK, resp.Code)
}

func TestRegisterAll_MetricsRouteRegistered(t *testing.T) {
	_, testAPI := humatest.New(t)
	router := chi.NewRouter()
	hctx := newTestHandlerContext(t)
	RegisterAll(testAPI, router, hctx)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	// Prometheus handler returns 200 for /metrics.
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRegisterAll_NotFoundHandler(t *testing.T) {
	_, testAPI := humatest.New(t)
	router := chi.NewRouter()
	hctx := newTestHandlerContext(t)
	RegisterAll(testAPI, router, hctx)

	req := httptest.NewRequest(http.MethodGet, "/this-does-not-exist-anywhere", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "Not Found")
}

func TestRegisterAll_MethodNotAllowedHandler(t *testing.T) {
	_, testAPI := humatest.New(t)
	router := chi.NewRouter()
	hctx := newTestHandlerContext(t)
	RegisterAll(testAPI, router, hctx)

	// Add a GET route and then send a DELETE which should return 405.
	router.Get("/probe", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest(http.MethodDelete, "/probe", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Contains(t, rec.Body.String(), "Method Not Allowed")
}

func TestRegisterAllForExport_RegistersRoutes(t *testing.T) {
	_, testAPI := humatest.New(t)
	require.NotPanics(t, func() {
		RegisterAllForExport(testAPI, "", nil)
	})
}

func BenchmarkRegisterAll(b *testing.B) {
	var shutting int32
	hctx := &api.HandlerContext{
		Config:         &config.Config{},
		IsShuttingDown: &shutting,
		StartTime:      time.Now(),
	}
	for b.Loop() {
		_, testAPI := humatest.New(b)
		router := chi.NewRouter()
		RegisterAll(testAPI, router, hctx)
	}
}
