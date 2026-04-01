// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package endpoints

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// AdminInfoResponse wraps admin server info.
type AdminInfoResponse struct {
	Body struct {
		Version   string `json:"version" maxLength:"50" doc:"Server version"`
		Uptime    string `json:"uptime" maxLength:"50" doc:"Server uptime"`
		StartTime string `json:"startTime" maxLength:"50" doc:"Server start time"`
		Providers int    `json:"providers" doc:"Number of registered providers"`
	}
}

// AdminReloadRequest is the request body for config reload.
type AdminReloadRequest struct {
	Body struct{} // empty body
}

// AdminReloadResponse wraps config reload result.
type AdminReloadResponse struct {
	Body struct {
		Success bool   `json:"success" doc:"Whether reload succeeded"`
		Message string `json:"message" maxLength:"500" doc:"Result message"`
	}
}

// AdminClearCacheResponse wraps cache clear result.
type AdminClearCacheResponse struct {
	Body struct {
		Success bool   `json:"success" doc:"Whether cache clear succeeded"`
		Message string `json:"message" maxLength:"500" doc:"Result message"`
	}
}

// RegisterAdminEndpoints registers admin API endpoints.
func RegisterAdminEndpoints(humaAPI huma.API, hctx *api.HandlerContext, prefix string) {
	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID: "admin-info",
		Method:      http.MethodGet,
		Path:        fmt.Sprintf("%s/admin/info", prefix),
		Summary:     "Server info",
		Description: "Returns server version, uptime, config summary, and component status.",
		Tags:        []string{"Admin"},
	}, hctx, http.StatusOK), func(_ context.Context, _ *struct{}) (*AdminInfoResponse, error) {
		providerCount := 0
		if hctx.ProviderRegistry != nil {
			providerCount = hctx.ProviderRegistry.Count()
		}

		resp := &AdminInfoResponse{}
		resp.Body.Version = settings.VersionInformation.BuildVersion
		resp.Body.Uptime = time.Since(hctx.StartTime).Round(time.Second).String()
		resp.Body.StartTime = hctx.StartTime.Format(time.RFC3339)
		resp.Body.Providers = providerCount
		return resp, nil
	})

	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID:  "admin-reload-config",
		Method:       http.MethodPost,
		Path:         fmt.Sprintf("%s/admin/reload-config", prefix),
		Summary:      "Reload configuration",
		Description:  "Hot-reloads configuration without restarting the server.",
		Tags:         []string{"Admin"},
		MaxBodyBytes: settings.DefaultAPIAdminMaxBodyBytes,
		Errors:       []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity, http.StatusTooManyRequests, http.StatusNotImplemented, http.StatusInternalServerError},
	}, hctx, http.StatusOK), func(_ context.Context, _ *struct{}) (*AdminReloadResponse, error) {
		// TODO: implement config hot-reload
		return nil, huma.NewError(http.StatusNotImplemented, "configuration reload is not yet implemented")
	})

	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID:  "admin-clear-cache",
		Method:       http.MethodPost,
		Path:         fmt.Sprintf("%s/admin/clear-cache", prefix),
		Summary:      "Clear caches",
		Description:  "Clears CEL, template, and HTTP caches.",
		Tags:         []string{"Admin"},
		MaxBodyBytes: settings.DefaultAPIAdminMaxBodyBytes,
		Errors:       []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity, http.StatusTooManyRequests, http.StatusNotImplemented, http.StatusInternalServerError},
	}, hctx, http.StatusOK), func(_ context.Context, _ *struct{}) (*AdminClearCacheResponse, error) {
		// TODO: implement cache clearing
		return nil, huma.NewError(http.StatusNotImplemented, "cache clearing is not yet implemented")
	})
}
