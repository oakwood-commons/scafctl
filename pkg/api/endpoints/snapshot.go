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

// SnapshotListItem represents a snapshot in API responses.
type SnapshotListItem struct {
	ID        string `json:"id" maxLength:"255" doc:"Snapshot identifier"`
	Solution  string `json:"solution,omitempty" maxLength:"255" doc:"Source solution name"`
	CreatedAt string `json:"createdAt,omitempty" maxLength:"50" doc:"Creation timestamp"`
}

// SnapshotListResponse wraps a list of snapshots.
type SnapshotListResponse struct {
	Body struct {
		Items      []SnapshotListItem `json:"items" doc:"List of snapshots"`
		Pagination api.PaginationInfo `json:"pagination" doc:"Pagination metadata"`
	}
}

// RegisterSnapshotEndpoints registers snapshot-related API endpoints.
func RegisterSnapshotEndpoints(humaAPI huma.API, hctx *api.HandlerContext, prefix string) {
	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID: "list-snapshots",
		Method:      http.MethodGet,
		Path:        fmt.Sprintf("%s/snapshots", prefix),
		Summary:     "List snapshots",
		Description: "Returns all available execution snapshots.",
		Tags:        []string{"Snapshots"},
	}, hctx, http.StatusOK), func(_ context.Context, input *struct {
		api.PaginationParams
	},
	) (*SnapshotListResponse, error) {
		// TODO: integrate with snapshot storage
		items := []SnapshotListItem{}

		total := len(items)
		paged := api.Paginate(items, input.Page, input.PerPage)
		pagination := api.NewPaginationInfo(total, input.Page, input.PerPage)

		resp := &SnapshotListResponse{}
		resp.Body.Items = paged
		resp.Body.Pagination = pagination
		return resp, nil
	})

	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID: "get-snapshot",
		Method:      http.MethodGet,
		Path:        fmt.Sprintf("%s/snapshots/{id}", prefix),
		Summary:     "Get snapshot details",
		Description: "Returns detailed information about a specific snapshot.",
		Tags:        []string{"Snapshots"},
	}, hctx, http.StatusOK), func(_ context.Context, input *struct {
		ID string `path:"id" maxLength:"255" doc:"Snapshot ID"`
	}) (*struct {
		Body struct {
			ID       string `json:"id" maxLength:"255" doc:"Snapshot identifier"`
			Solution string `json:"solution,omitempty" maxLength:"255" doc:"Source solution name"`
			Detail   string `json:"detail,omitempty" maxLength:"10000" doc:"Snapshot detail"`
		}
	}, error,
	) {
		// TODO: integrate with snapshot storage
		return nil, api.NotFoundError("snapshot", input.ID)
	})
}
