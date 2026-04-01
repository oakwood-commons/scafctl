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

func TestRegisterHealthEndpoints_Root(t *testing.T) {
	_, testAPI := humatest.New(t)
	var shutting int32
	hctx := &api.HandlerContext{
		Config:         &config.Config{},
		IsShuttingDown: &shutting,
		StartTime:      time.Now(),
	}
	RegisterHealthEndpoints(testAPI, hctx)
	resp := testAPI.Get("/")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "scafctl API")
}

func TestRegisterHealthEndpoints_Health(t *testing.T) {
	_, testAPI := humatest.New(t)
	var shutting int32
	hctx := &api.HandlerContext{
		Config:         &config.Config{},
		IsShuttingDown: &shutting,
		StartTime:      time.Now(),
	}
	RegisterHealthEndpoints(testAPI, hctx)
	resp := testAPI.Get("/health")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "healthy")
}

func TestRegisterHealthEndpoints_Liveness(t *testing.T) {
	_, testAPI := humatest.New(t)
	var shutting int32
	hctx := &api.HandlerContext{
		Config:         &config.Config{},
		IsShuttingDown: &shutting,
		StartTime:      time.Now(),
	}
	RegisterHealthEndpoints(testAPI, hctx)
	resp := testAPI.Get("/health/live")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "ok")
}

func TestRegisterHealthEndpoints_Readiness(t *testing.T) {
	_, testAPI := humatest.New(t)
	var shutting int32
	hctx := &api.HandlerContext{
		Config:         &config.Config{},
		IsShuttingDown: &shutting,
		StartTime:      time.Now(),
	}
	RegisterHealthEndpoints(testAPI, hctx)
	resp := testAPI.Get("/health/ready")
	require.Equal(t, http.StatusOK, resp.Code)
}

func TestRegisterHealthEndpoints_Readiness_ShuttingDown(t *testing.T) {
	_, testAPI := humatest.New(t)
	var shutting int32 = 1
	hctx := &api.HandlerContext{
		Config:         &config.Config{},
		IsShuttingDown: &shutting,
		StartTime:      time.Now(),
	}
	RegisterHealthEndpoints(testAPI, hctx)
	resp := testAPI.Get("/health/ready")
	assert.Equal(t, http.StatusServiceUnavailable, resp.Code)
}

func TestRegisterHealthEndpoints_NilProviderRegistry(t *testing.T) {
	_, testAPI := humatest.New(t)
	var shutting int32
	hctx := &api.HandlerContext{
		Config:         &config.Config{},
		IsShuttingDown: &shutting,
		StartTime:      time.Now(),
	}
	RegisterHealthEndpoints(testAPI, hctx)
	resp := testAPI.Get("/health")
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "unavailable")
}

func TestRegisterAllForExport_NoPanic(t *testing.T) {
	_, testAPI := humatest.New(t)
	assert.NotPanics(t, func() {
		RegisterAllForExport(testAPI, "", nil)
	})
}

func BenchmarkHealthEndpoint(b *testing.B) {
	_, testAPI := humatest.New(b)
	var shutting int32
	hctx := &api.HandlerContext{
		Config:         &config.Config{},
		IsShuttingDown: &shutting,
		StartTime:      time.Now(),
	}
	RegisterHealthEndpoints(testAPI, hctx)
	for b.Loop() {
		testAPI.Get("/health")
	}
}
