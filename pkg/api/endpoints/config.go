// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package endpoints

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/oakwood-commons/scafctl/pkg/api"
	apimw "github.com/oakwood-commons/scafctl/pkg/api/middleware"
)

// ConfigResponse wraps configuration data.
type ConfigResponse struct {
	Body struct {
		Settings   any `json:"settings,omitempty" doc:"Application settings"`
		Catalogs   any `json:"catalogs,omitempty" doc:"Catalog configurations"`
		Logging    any `json:"logging,omitempty" doc:"Logging configuration"`
		Telemetry  any `json:"telemetry,omitempty" doc:"Telemetry configuration"`
		HTTPClient any `json:"httpClient,omitempty" doc:"HTTP client configuration"`
		CEL        any `json:"cel,omitempty" doc:"CEL configuration"`
		GoTemplate any `json:"goTemplate,omitempty" doc:"Go template configuration"`
		APIServer  any `json:"apiServer,omitempty" doc:"API server configuration"`
	}
}

// SettingsResponse wraps runtime settings.
type SettingsResponse struct {
	Body struct {
		DefaultCatalog string `json:"defaultCatalog,omitempty" maxLength:"255" doc:"Default catalog name"`
		NoColor        bool   `json:"noColor" doc:"Color output disabled"`
		Quiet          bool   `json:"quiet" doc:"Quiet mode enabled"`
	}
}

// RegisterConfigEndpoints registers config and settings API endpoints.
func RegisterConfigEndpoints(humaAPI huma.API, hctx *api.HandlerContext, prefix string) {
	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID: "get-config",
		Method:      http.MethodGet,
		Path:        fmt.Sprintf("%s/config", prefix),
		Summary:     "Get current configuration",
		Description: "Returns the current application configuration (sensitive fields redacted).",
		Tags:        []string{"Config"},
	}, hctx, http.StatusOK), func(_ context.Context, _ *struct{}) (*ConfigResponse, error) {
		if hctx.Config == nil {
			return nil, huma.NewError(http.StatusNotFound, "no configuration loaded")
		}

		cfg := hctx.Config
		resp := &ConfigResponse{}
		resp.Body.Settings = apimw.RedactJSON(cfg.Settings)
		resp.Body.Catalogs = apimw.RedactJSON(cfg.Catalogs)
		resp.Body.Logging = apimw.RedactJSON(cfg.Logging)
		resp.Body.Telemetry = apimw.RedactJSON(cfg.Telemetry)
		resp.Body.HTTPClient = apimw.RedactJSON(cfg.HTTPClient)
		resp.Body.CEL = apimw.RedactJSON(cfg.CEL)
		resp.Body.GoTemplate = apimw.RedactJSON(cfg.GoTemplate)
		resp.Body.APIServer = apimw.RedactJSON(cfg.APIServer)
		return resp, nil
	})

	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID: "get-settings",
		Method:      http.MethodGet,
		Path:        fmt.Sprintf("%s/settings", prefix),
		Summary:     "Get runtime settings",
		Description: "Returns the current runtime settings.",
		Tags:        []string{"Config"},
	}, hctx, http.StatusOK), func(_ context.Context, _ *struct{}) (*SettingsResponse, error) {
		if hctx.Config == nil {
			return nil, huma.NewError(http.StatusNotFound, "no configuration loaded")
		}

		resp := &SettingsResponse{}
		resp.Body.DefaultCatalog = hctx.Config.Settings.DefaultCatalog
		resp.Body.NoColor = hctx.Config.Settings.NoColor
		resp.Body.Quiet = hctx.Config.Settings.Quiet
		return resp, nil
	})
}
