// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package endpoints

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/schema"
)

// SchemaListItem represents a schema in API responses.
type SchemaListItem struct {
	Name string `json:"name" maxLength:"255" doc:"Schema name"`
}

// SchemaListResponse wraps a list of schemas.
type SchemaListResponse struct {
	Body struct {
		Items      []SchemaListItem   `json:"items" doc:"List of schemas"`
		Pagination api.PaginationInfo `json:"pagination" doc:"Pagination metadata"`
	}
}

// SchemaDetailResponse wraps a single schema.
type SchemaDetailResponse struct {
	Body struct {
		Name       string `json:"name" maxLength:"255" doc:"Schema name"`
		SchemaData any    `json:"schema" doc:"JSON Schema"`
	}
}

// SchemaValidateRequest is the request body for schema validation.
type SchemaValidateRequest struct {
	Body struct {
		Schema string `json:"schema,omitempty" maxLength:"255" doc:"Schema name (currently only 'solution' supported)" example:"solution"`
		Data   any    `json:"data" doc:"Data to validate against the schema"`
	}
}

// SchemaValidateResponse wraps schema validation results.
type SchemaValidateResponse struct {
	Body struct {
		Valid      bool              `json:"valid" doc:"Whether the data is valid"`
		Violations []SchemaViolation `json:"violations,omitempty" doc:"List of violations"`
	}
}

// SchemaViolation represents a single schema violation.
type SchemaViolation struct {
	Path    string `json:"path" maxLength:"500" doc:"JSON path of the violation"`
	Message string `json:"message" maxLength:"2000" doc:"Violation message"`
}

// RegisterSchemaEndpoints registers schema-related API endpoints.
func RegisterSchemaEndpoints(humaAPI huma.API, hctx *api.HandlerContext, prefix string) {
	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID: "list-schemas",
		Method:      http.MethodGet,
		Path:        fmt.Sprintf("%s/schemas", prefix),
		Summary:     "List available schemas",
		Description: "Returns all available JSON schemas.",
		Tags:        []string{"Schemas"},
	}, hctx, http.StatusOK), func(_ context.Context, input *struct {
		api.PaginationParams
	},
	) (*SchemaListResponse, error) {
		// Currently only "solution" schema is available
		items := []SchemaListItem{
			{Name: "solution"},
		}

		total := len(items)
		paged := api.Paginate(items, input.Page, input.PerPage)
		pagination := api.NewPaginationInfo(total, input.Page, input.PerPage)

		resp := &SchemaListResponse{}
		resp.Body.Items = paged
		resp.Body.Pagination = pagination
		return resp, nil
	})

	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID: "get-schema",
		Method:      http.MethodGet,
		Path:        fmt.Sprintf("%s/schemas/{name}", prefix),
		Summary:     "Get a schema",
		Description: "Returns a JSON schema by name.",
		Tags:        []string{"Schemas"},
	}, hctx, http.StatusOK), func(ctx context.Context, input *struct {
		Name string `path:"name" maxLength:"255" doc:"Schema name"`
	},
	) (*SchemaDetailResponse, error) {
		if input.Name != "solution" {
			return nil, api.NotFoundError("schema", input.Name)
		}

		schemaBytes, err := schema.GenerateSolutionSchema()
		if err != nil {
			return nil, api.InternalError(ctx, err, "get-schema")
		}

		var schemaObj any
		if err := json.Unmarshal(schemaBytes, &schemaObj); err != nil {
			return nil, api.InternalError(ctx, err, "get-schema")
		}

		resp := &SchemaDetailResponse{}
		resp.Body.Name = input.Name
		resp.Body.SchemaData = schemaObj
		return resp, nil
	})

	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID: "validate-schema",
		Method:      http.MethodPost,
		Path:        fmt.Sprintf("%s/schemas/validate", prefix),
		Summary:     "Validate data against a schema",
		Description: "Validates data against a JSON schema and returns any violations.",
		Tags:        []string{"Schemas"},
	}, hctx, http.StatusOK), func(_ context.Context, input *SchemaValidateRequest) (*SchemaValidateResponse, error) {
		if input.Body.Data == nil {
			return nil, huma.NewError(http.StatusBadRequest, "data is required")
		}

		// Default to "solution" when schema is not specified; reject unknown schemas.
		schemaName := input.Body.Schema
		if schemaName == "" {
			schemaName = "solution"
		}
		if schemaName != "solution" {
			return nil, api.NotFoundError("schema", schemaName)
		}

		violations, err := schema.ValidateSolutionAgainstSchema(input.Body.Data)
		if err != nil {
			return nil, huma.NewError(http.StatusBadRequest, fmt.Sprintf("validation failed: %v", err))
		}

		apiViolations := make([]SchemaViolation, 0, len(violations))
		for _, v := range violations {
			apiViolations = append(apiViolations, SchemaViolation{
				Path:    v.Path,
				Message: v.Message,
			})
		}

		resp := &SchemaValidateResponse{}
		resp.Body.Valid = len(violations) == 0
		resp.Body.Violations = apiViolations
		return resp, nil
	})
}
