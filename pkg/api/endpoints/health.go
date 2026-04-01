// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package endpoints

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// RegisterHealthEndpoints registers health and root endpoints.
// These are registered without a version prefix so they bypass auth middleware.
func RegisterHealthEndpoints(humaAPI huma.API, hctx *api.HandlerContext) {
	// Root endpoint — available endpoints listing
	huma.Register(humaAPI, withPublicDefaults(huma.Operation{
		OperationID: "root",
		Method:      http.MethodGet,
		Path:        "/",
		Summary:     "API root",
		Description: "Returns API name, version, and available endpoints.",
		Tags:        []string{"Health"},
	}, hctx, http.StatusOK), func(_ context.Context, _ *struct{}) (*api.RootResponse, error) {
		version := settings.VersionInformation.BuildVersion
		apiVersion := settings.DefaultAPIVersion
		if hctx.Config != nil && hctx.Config.APIServer.APIVersion != "" {
			apiVersion = hctx.Config.APIServer.APIVersion
		}

		resp := &api.RootResponse{}
		resp.Body.Name = "scafctl API"
		resp.Body.Version = version
		resp.Body.Links = []api.Link{
			{Href: "/health", Rel: "health"},
			{Href: fmt.Sprintf("/%s/solutions/lint", apiVersion), Rel: "lint"},
			{Href: fmt.Sprintf("/%s/providers", apiVersion), Rel: "providers"},
			{Href: fmt.Sprintf("/%s/eval/cel", apiVersion), Rel: "eval-cel"},
			{Href: fmt.Sprintf("/%s/catalogs", apiVersion), Rel: "catalogs"},
			{Href: fmt.Sprintf("/%s/schemas", apiVersion), Rel: "schemas"},
			{Href: fmt.Sprintf("/%s/docs", apiVersion), Rel: "docs"},
			{Href: fmt.Sprintf("/%s/openapi.json", apiVersion), Rel: "openapi"},
		}
		return resp, nil
	})

	// Health check
	huma.Register(humaAPI, withPublicDefaults(huma.Operation{
		OperationID: "health",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Health check",
		Description: "Returns overall health status, version, uptime, and component statuses.",
		Tags:        []string{"Health"},
	}, hctx, http.StatusOK), func(_ context.Context, _ *struct{}) (*api.HealthResponse, error) {
		status := "healthy"
		if hctx.IsShuttingDown != nil && atomic.LoadInt32(hctx.IsShuttingDown) == 1 {
			status = "shutting_down"
		}

		components := []api.ComponentStatus{
			{Name: "providers", Status: providerStatus(hctx)},
		}

		resp := &api.HealthResponse{}
		resp.Body.Status = status
		resp.Body.Version = settings.VersionInformation.BuildVersion
		resp.Body.Uptime = time.Since(hctx.StartTime).Round(time.Second).String()
		resp.Body.Components = components
		return resp, nil
	})

	// Liveness probe
	huma.Register(humaAPI, withPublicDefaults(huma.Operation{
		OperationID: "health-live",
		Method:      http.MethodGet,
		Path:        "/health/live",
		Summary:     "Liveness probe",
		Description: "Returns 200 if the process is alive. Used by orchestrators.",
		Tags:        []string{"Health"},
	}, hctx, http.StatusOK), func(_ context.Context, _ *struct{}) (*api.StatusResponse, error) {
		resp := &api.StatusResponse{}
		resp.Body.Status = "ok"
		return resp, nil
	})

	// Readiness probe
	huma.Register(humaAPI, withPublicDefaults(huma.Operation{
		OperationID: "health-ready",
		Method:      http.MethodGet,
		Path:        "/health/ready",
		Summary:     "Readiness probe",
		Description: "Returns 200 if the server is ready to accept traffic. Returns 503 during shutdown.",
		Tags:        []string{"Health"},
	}, hctx, http.StatusOK), func(_ context.Context, _ *struct{}) (*api.StatusResponse, error) {
		if hctx.IsShuttingDown != nil && atomic.LoadInt32(hctx.IsShuttingDown) == 1 {
			return nil, huma.NewError(http.StatusServiceUnavailable, "server is shutting down")
		}
		resp := &api.StatusResponse{}
		resp.Body.Status = "ok"
		return resp, nil
	})
}

// providerStatus returns the health status of the provider registry.
func providerStatus(hctx *api.HandlerContext) string {
	if hctx.ProviderRegistry == nil {
		return "unavailable"
	}
	if hctx.ProviderRegistry.Count() > 0 {
		return "ok"
	}
	return "degraded"
}
