// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package endpoints

import (
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// RegisterAll registers all API endpoints on the Huma API and chi router.
func RegisterAll(humaAPI huma.API, router *chi.Mux, hctx *api.HandlerContext) {
	apiVersion := hctx.Config.APIServer.APIVersion
	if apiVersion == "" {
		apiVersion = settings.DefaultAPIVersion
	}
	prefix := fmt.Sprintf("/%s", apiVersion)

	// Health and operational endpoints (on root router, bypass auth)
	RegisterHealthEndpoints(humaAPI, hctx)

	// Prometheus metrics endpoint (on root router, bypass auth)
	router.Handle("/metrics", promhttp.Handler())

	// Business endpoints (on API router with auth middleware)
	RegisterSolutionEndpoints(humaAPI, hctx, prefix)
	RegisterProviderEndpoints(humaAPI, hctx, prefix)
	RegisterEvalEndpoints(humaAPI, hctx, prefix)
	RegisterCatalogEndpoints(humaAPI, hctx, prefix)
	RegisterSchemaEndpoints(humaAPI, hctx, prefix)
	RegisterConfigEndpoints(humaAPI, hctx, prefix)
	RegisterSnapshotEndpoints(humaAPI, hctx, prefix)
	RegisterExplainEndpoints(humaAPI, hctx, prefix)
	RegisterAdminEndpoints(humaAPI, hctx, prefix)

	// Custom 404/405 handlers
	router.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"title":"Not Found","status":404,"detail":"the requested resource was not found"}`))
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = w.Write([]byte(`{"title":"Method Not Allowed","status":405,"detail":"the HTTP method is not allowed for this resource"}`))
	})
}

// RegisterAllForExport registers all endpoints on a Huma API for
// OpenAPI spec generation without starting the server.
// It accepts an apiVersion so the exported spec matches the configured version,
// and an optional config so operation Security reflects auth settings.
func RegisterAllForExport(humaAPI huma.API, apiVersion string, cfg *config.Config) {
	if apiVersion == "" {
		apiVersion = settings.DefaultAPIVersion
	}
	prefix := fmt.Sprintf("/%s", apiVersion)
	hctx := &api.HandlerContext{Config: cfg}

	RegisterHealthEndpoints(humaAPI, hctx)
	RegisterSolutionEndpoints(humaAPI, hctx, prefix)
	RegisterProviderEndpoints(humaAPI, hctx, prefix)
	RegisterEvalEndpoints(humaAPI, hctx, prefix)
	RegisterCatalogEndpoints(humaAPI, hctx, prefix)
	RegisterSchemaEndpoints(humaAPI, hctx, prefix)
	RegisterConfigEndpoints(humaAPI, hctx, prefix)
	RegisterSnapshotEndpoints(humaAPI, hctx, prefix)
	RegisterExplainEndpoints(humaAPI, hctx, prefix)
	RegisterAdminEndpoints(humaAPI, hctx, prefix)
}
