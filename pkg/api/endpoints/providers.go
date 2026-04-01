// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package endpoints

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// ProviderListItem represents a provider in API responses.
type ProviderListItem struct {
	Name         string   `json:"name" maxLength:"255" example:"http" doc:"Provider name"`
	Version      string   `json:"version,omitempty" maxLength:"50" example:"1.0.0" doc:"Provider version"`
	Capabilities []string `json:"capabilities,omitempty" maxItems:"10" doc:"Provider capabilities"`
	Category     string   `json:"category,omitempty" maxLength:"50" doc:"Provider category"`
	Description  string   `json:"description,omitempty" maxLength:"1000" doc:"Provider description"`
}

// ProviderListResponse wraps a list of providers.
type ProviderListResponse struct {
	Body struct {
		Items      []ProviderListItem `json:"items" doc:"List of providers"`
		Pagination api.PaginationInfo `json:"pagination" doc:"Pagination metadata"`
	}
}

// ProviderDetailResponse wraps a single provider's details.
type ProviderDetailResponse struct {
	Body struct {
		Name          string   `json:"name" maxLength:"255" example:"http" doc:"Provider name"`
		Version       string   `json:"version,omitempty" maxLength:"50" doc:"Provider version"`
		Capabilities  []string `json:"capabilities,omitempty" maxItems:"10" doc:"Provider capabilities"`
		Category      string   `json:"category,omitempty" maxLength:"50" doc:"Provider category"`
		Description   string   `json:"description,omitempty" maxLength:"1000" doc:"Provider description"`
		InputSchema   any      `json:"inputSchema,omitempty" doc:"Input JSON schema"`
		OutputSchemas any      `json:"outputSchemas,omitempty" doc:"Output schemas by operation"`
	}
}

// ProviderSchemaResponse wraps a provider's JSON schema.
type ProviderSchemaResponse struct {
	Body struct {
		Name       string `json:"name" maxLength:"255" doc:"Provider name"`
		SchemaData any    `json:"schema" doc:"JSON Schema"`
	}
}

// RegisterProviderEndpoints registers provider-related API endpoints.
func RegisterProviderEndpoints(humaAPI huma.API, hctx *api.HandlerContext, prefix string) {
	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID: "list-providers",
		Method:      http.MethodGet,
		Path:        fmt.Sprintf("%s/providers", prefix),
		Summary:     "List registered providers",
		Description: "Returns all registered providers with optional CEL filtering.",
		Tags:        []string{"Providers"},
	}, hctx, http.StatusOK), func(ctx context.Context, input *struct {
		api.PaginationParams
		api.FilterParam
	},
	) (*ProviderListResponse, error) {
		if hctx.ProviderRegistry == nil {
			resp := &ProviderListResponse{}
			resp.Body.Items = []ProviderListItem{}
			resp.Body.Pagination = api.NewPaginationInfo(0, input.Page, input.PerPage)
			return resp, nil
		}

		items := buildProviderList(hctx.ProviderRegistry)

		// Apply CEL filter
		if input.Filter != "" {
			filtered, err := api.FilterItems(ctx, items, input.Filter)
			if err != nil {
				return nil, huma.NewError(http.StatusBadRequest, fmt.Sprintf("invalid filter: %v", err))
			}
			items = filtered
		}

		total := len(items)
		paged := api.Paginate(items, input.Page, input.PerPage)
		pagination := api.NewPaginationInfo(total, input.Page, input.PerPage)

		resp := &ProviderListResponse{}
		resp.Body.Items = paged
		resp.Body.Pagination = pagination
		return resp, nil
	})

	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID: "get-provider",
		Method:      http.MethodGet,
		Path:        fmt.Sprintf("%s/providers/{name}", prefix),
		Summary:     "Get provider details",
		Description: "Returns detailed information about a specific provider.",
		Tags:        []string{"Providers"},
	}, hctx, http.StatusOK), func(_ context.Context, input *struct {
		Name string `path:"name" maxLength:"255" doc:"Provider name"`
	},
	) (*ProviderDetailResponse, error) {
		if hctx.ProviderRegistry == nil {
			return nil, api.NotFoundError("provider", input.Name)
		}

		p, ok := hctx.ProviderRegistry.Get(input.Name)
		if !ok {
			return nil, api.NotFoundError("provider", input.Name)
		}

		desc := p.Descriptor()
		resp := &ProviderDetailResponse{}
		resp.Body.Name = desc.Name
		resp.Body.Version = versionString(desc.Version)
		resp.Body.Description = desc.Description
		resp.Body.Capabilities = capabilityStrings(desc.Capabilities)
		resp.Body.Category = desc.Category
		resp.Body.InputSchema = desc.Schema
		resp.Body.OutputSchemas = desc.OutputSchemas
		return resp, nil
	})

	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID: "get-provider-schema",
		Method:      http.MethodGet,
		Path:        fmt.Sprintf("%s/providers/{name}/schema", prefix),
		Summary:     "Get provider JSON schema",
		Description: "Returns the input JSON schema for a specific provider.",
		Tags:        []string{"Providers"},
	}, hctx, http.StatusOK), func(_ context.Context, input *struct {
		Name string `path:"name" maxLength:"255" doc:"Provider name"`
	},
	) (*ProviderSchemaResponse, error) {
		if hctx.ProviderRegistry == nil {
			return nil, api.NotFoundError("provider", input.Name)
		}

		p, ok := hctx.ProviderRegistry.Get(input.Name)
		if !ok {
			return nil, api.NotFoundError("provider", input.Name)
		}

		desc := p.Descriptor()
		resp := &ProviderSchemaResponse{}
		resp.Body.Name = desc.Name
		resp.Body.SchemaData = desc.Schema
		return resp, nil
	})
}

func buildProviderList(reg *provider.Registry) []ProviderListItem {
	providers := reg.ListProviders()
	items := make([]ProviderListItem, 0, len(providers))
	for _, p := range providers {
		desc := p.Descriptor()
		items = append(items, ProviderListItem{
			Name:         desc.Name,
			Version:      versionString(desc.Version),
			Capabilities: capabilityStrings(desc.Capabilities),
			Category:     desc.Category,
			Description:  desc.Description,
		})
	}
	return items
}

func capabilityStrings(caps []provider.Capability) []string {
	if len(caps) == 0 {
		return nil
	}
	result := make([]string, len(caps))
	for i, c := range caps {
		result[i] = string(c)
	}
	return result
}

// versionString safely converts a *semver.Version to string.
func versionString(v fmt.Stringer) string {
	if v == nil {
		return ""
	}
	return v.String()
}
