// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package endpoints

import (
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"

	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

func TestRegisterCatalogEndpoints_ListEmpty(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterCatalogEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Get("/v1/catalogs")
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "items")
}

func TestRegisterCatalogEndpoints_ListWithCatalogs(t *testing.T) {
	_, testAPI := humatest.New(t)
	var shutting int32
	hctx := &api.HandlerContext{
		Config: &config.Config{
			Catalogs: []config.CatalogConfig{
				{Name: "test-cat", Type: "filesystem", Path: "/tmp/test"},
			},
		},
		IsShuttingDown: &shutting,
		StartTime:      time.Now(),
	}
	RegisterCatalogEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Get("/v1/catalogs")
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "test-cat")
}

func TestRegisterCatalogEndpoints_DetailNotFound(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterCatalogEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Get("/v1/catalogs/nonexistent")
	assert.Equal(t, http.StatusNotFound, resp.Code)
}

func TestRegisterCatalogEndpoints_CatalogSolutionsList(t *testing.T) {
	_, testAPI := humatest.New(t)
	var shutting int32
	hctx := &api.HandlerContext{
		Config: &config.Config{
			Catalogs: []config.CatalogConfig{
				{Name: "test-cat", Type: "filesystem", Path: "/tmp/test"},
			},
		},
		IsShuttingDown: &shutting,
		StartTime:      time.Now(),
	}
	RegisterCatalogEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Get("/v1/catalogs/test-cat/solutions")
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "items")
}

func TestRegisterCatalogEndpoints_CatalogSolutionsNotFound(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterCatalogEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Get("/v1/catalogs/nonexistent/solutions")
	assert.Equal(t, http.StatusNotFound, resp.Code)
}

func TestRegisterCatalogEndpoints_CatalogSync(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterCatalogEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/catalogs/sync")
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "success")
}

func BenchmarkCatalogListEndpoint(b *testing.B) {
	_, testAPI := humatest.New(b)
	var shutting int32
	hctx := &api.HandlerContext{
		Config:         &config.Config{},
		IsShuttingDown: &shutting,
		StartTime:      time.Now(),
	}
	RegisterCatalogEndpoints(testAPI, hctx, "/v1")
	for b.Loop() {
		testAPI.Get("/v1/catalogs")
	}
}

func BenchmarkCatalogSyncEndpoint(b *testing.B) {
	_, testAPI := humatest.New(b)
	var shutting int32
	hctx := &api.HandlerContext{
		Config:         &config.Config{},
		IsShuttingDown: &shutting,
		StartTime:      time.Now(),
	}
	RegisterCatalogEndpoints(testAPI, hctx, "/v1")
	for b.Loop() {
		testAPI.Post("/v1/catalogs/sync")
	}
}
