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

func TestRegisterConfigEndpoints_GetConfig(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterConfigEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Get("/v1/config")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "settings")
}

func TestRegisterConfigEndpoints_GetConfig_NilConfig(t *testing.T) {
	_, testAPI := humatest.New(t)
	var shutting int32
	hctx := &api.HandlerContext{
		Config:         nil,
		IsShuttingDown: &shutting,
		StartTime:      time.Now(),
	}
	RegisterConfigEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Get("/v1/config")
	assert.Equal(t, http.StatusNotFound, resp.Code)
}

func TestRegisterConfigEndpoints_GetSettings(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterConfigEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Get("/v1/settings")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "noColor")
}

func TestRegisterConfigEndpoints_GetSettings_NilConfig(t *testing.T) {
	_, testAPI := humatest.New(t)
	var shutting int32
	hctx := &api.HandlerContext{
		Config:         nil,
		IsShuttingDown: &shutting,
		StartTime:      time.Now(),
	}
	RegisterConfigEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Get("/v1/settings")
	assert.Equal(t, http.StatusNotFound, resp.Code)
}

func BenchmarkConfigEndpoint(b *testing.B) {
	_, testAPI := humatest.New(b)
	var shutting int32
	hctx := &api.HandlerContext{
		Config:         &config.Config{},
		IsShuttingDown: &shutting,
		StartTime:      time.Now(),
	}
	RegisterConfigEndpoints(testAPI, hctx, "/v1")
	for b.Loop() {
		testAPI.Get("/v1/config")
	}
}
