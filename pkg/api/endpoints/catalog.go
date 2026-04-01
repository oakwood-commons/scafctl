// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package endpoints

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/oakwood-commons/scafctl/pkg/api"
)

// CatalogListItem represents a catalog in API responses.
type CatalogListItem struct {
	Name string `json:"name" maxLength:"255" doc:"Catalog name"`
	Type string `json:"type" maxLength:"50" doc:"Catalog type"`
	Path string `json:"path,omitempty" maxLength:"4096" doc:"Catalog path"`
	URL  string `json:"url,omitempty" maxLength:"2048" doc:"Catalog URL"`
}

// CatalogListResponse wraps a list of catalogs.
type CatalogListResponse struct {
	Body struct {
		Items      []CatalogListItem  `json:"items" doc:"List of catalogs"`
		Pagination api.PaginationInfo `json:"pagination" doc:"Pagination metadata"`
	}
}

// CatalogDetailResponse wraps a single catalog's details.
type CatalogDetailResponse struct {
	Body struct {
		Name     string            `json:"name" maxLength:"255" doc:"Catalog name"`
		Type     string            `json:"type" maxLength:"50" doc:"Catalog type"`
		Path     string            `json:"path,omitempty" maxLength:"4096" doc:"Catalog path"`
		URL      string            `json:"url,omitempty" maxLength:"2048" doc:"Catalog URL"`
		Metadata map[string]string `json:"metadata,omitempty" doc:"Additional metadata"`
	}
}

// CatalogSolutionItem represents a solution within a catalog in API responses.
type CatalogSolutionItem struct {
	Name    string `json:"name" maxLength:"255" doc:"Solution name"`
	Version string `json:"version,omitempty" maxLength:"50" doc:"Solution version"`
	Digest  string `json:"digest,omitempty" maxLength:"255" doc:"Content digest"`
	Catalog string `json:"catalog,omitempty" maxLength:"255" doc:"Source catalog name"`
}

// CatalogSolutionListResponse wraps a list of solutions within a catalog.
type CatalogSolutionListResponse struct {
	Body struct {
		Items      []CatalogSolutionItem `json:"items" doc:"List of solutions"`
		Pagination api.PaginationInfo    `json:"pagination" doc:"Pagination metadata"`
	}
}

// CatalogSyncResponse wraps the catalog sync result.
type CatalogSyncResponse struct {
	Body struct {
		Success  bool   `json:"success" doc:"Whether sync succeeded"`
		Message  string `json:"message" maxLength:"500" doc:"Result message"`
		Catalogs int    `json:"catalogs" doc:"Number of catalogs synced"`
	}
}

// RegisterCatalogEndpoints registers catalog-related API endpoints.
func RegisterCatalogEndpoints(humaAPI huma.API, hctx *api.HandlerContext, prefix string) {
	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID: "list-catalogs",
		Method:      http.MethodGet,
		Path:        fmt.Sprintf("%s/catalogs", prefix),
		Summary:     "List catalogs",
		Description: "Returns all configured catalogs with optional CEL filtering.",
		Tags:        []string{"Catalogs"},
	}, hctx, http.StatusOK), func(ctx context.Context, input *struct {
		api.PaginationParams
		api.FilterParam
	},
	) (*CatalogListResponse, error) {
		if hctx.Config == nil {
			return &CatalogListResponse{}, nil
		}

		items := make([]CatalogListItem, 0, len(hctx.Config.Catalogs))
		for _, cat := range hctx.Config.Catalogs {
			items = append(items, CatalogListItem{
				Name: cat.Name,
				Type: cat.Type,
				Path: cat.Path,
				URL:  cat.URL,
			})
		}

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

		resp := &CatalogListResponse{}
		resp.Body.Items = paged
		resp.Body.Pagination = pagination
		return resp, nil
	})

	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID: "get-catalog",
		Method:      http.MethodGet,
		Path:        fmt.Sprintf("%s/catalogs/{name}", prefix),
		Summary:     "Get catalog details",
		Description: "Returns detailed information about a specific catalog.",
		Tags:        []string{"Catalogs"},
	}, hctx, http.StatusOK), func(_ context.Context, input *struct {
		Name string `path:"name" maxLength:"255" doc:"Catalog name"`
	},
	) (*CatalogDetailResponse, error) {
		if hctx.Config == nil {
			return nil, api.NotFoundError("catalog", input.Name)
		}

		cat, ok := hctx.Config.GetCatalog(input.Name)
		if !ok {
			return nil, api.NotFoundError("catalog", input.Name)
		}

		resp := &CatalogDetailResponse{}
		resp.Body.Name = cat.Name
		resp.Body.Type = cat.Type
		resp.Body.Path = cat.Path
		resp.Body.URL = cat.URL
		resp.Body.Metadata = cat.Metadata
		return resp, nil
	})

	// GET /catalogs/{name}/solutions
	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID: "list-catalog-solutions",
		Method:      http.MethodGet,
		Path:        fmt.Sprintf("%s/catalogs/{name}/solutions", prefix),
		Summary:     "List catalog solutions",
		Description: "Returns all solutions available in a specific catalog.",
		Tags:        []string{"Catalogs"},
	}, hctx, http.StatusOK), func(_ context.Context, input *struct {
		Name string `path:"name" maxLength:"255" doc:"Catalog name"`
		api.PaginationParams
	},
	) (*CatalogSolutionListResponse, error) {
		if hctx.Config == nil {
			return nil, api.NotFoundError("catalog", input.Name)
		}

		_, ok := hctx.Config.GetCatalog(input.Name)
		if !ok {
			return nil, api.NotFoundError("catalog", input.Name)
		}

		// TODO: integrate with catalog.Catalog.List() to fetch solutions
		items := []CatalogSolutionItem{}

		total := len(items)
		paged := api.Paginate(items, input.Page, input.PerPage)
		pagination := api.NewPaginationInfo(total, input.Page, input.PerPage)

		resp := &CatalogSolutionListResponse{}
		resp.Body.Items = paged
		resp.Body.Pagination = pagination
		return resp, nil
	})

	// POST /catalogs/sync
	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID: "sync-catalogs",
		Method:      http.MethodPost,
		Path:        fmt.Sprintf("%s/catalogs/sync", prefix),
		Summary:     "Sync catalogs",
		Description: "Triggers a refresh of all configured catalogs.",
		Tags:        []string{"Catalogs"},
	}, hctx, http.StatusOK), func(_ context.Context, _ *struct{}) (*CatalogSyncResponse, error) {
		catalogCount := 0
		if hctx.Config != nil {
			catalogCount = len(hctx.Config.Catalogs)
		}

		// TODO: integrate with catalog sync/refresh logic
		resp := &CatalogSyncResponse{}
		resp.Body.Success = true
		resp.Body.Message = "catalog sync is not yet implemented"
		resp.Body.Catalogs = catalogCount
		return resp, nil
	})
}
