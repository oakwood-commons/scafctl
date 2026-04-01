// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package endpoints

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/soldiff"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// ExplainRequest is the request body for solution explanation.
type ExplainRequest struct {
	Body struct {
		Solution string `json:"solution" maxLength:"100000" doc:"Solution YAML content"`
	}
}

// ExplainResponse wraps explanation results.
type ExplainResponse struct {
	Body struct {
		Name          string   `json:"name" maxLength:"255" doc:"Solution name"`
		Description   string   `json:"description,omitempty" maxLength:"2000" doc:"Solution description"`
		ResolverCount int      `json:"resolverCount" doc:"Number of resolvers"`
		HasWorkflow   bool     `json:"hasWorkflow" doc:"Whether the solution has a workflow"`
		HasActions    bool     `json:"hasActions" doc:"Whether the solution has actions"`
		Resolvers     []string `json:"resolvers,omitempty" maxItems:"500" doc:"Resolver names"`
		Tags          []string `json:"tags,omitempty" maxItems:"50" doc:"Solution tags"`
	}
}

// DiffRequest is the request body for solution diffing.
type DiffRequest struct {
	Body struct {
		SolutionA string `json:"solutionA" maxLength:"100000" doc:"First solution YAML"`
		SolutionB string `json:"solutionB" maxLength:"100000" doc:"Second solution YAML"`
	}
}

// DiffChange represents a single change between two solutions.
type DiffChange struct {
	Field    string `json:"field" maxLength:"500" doc:"Changed field path"`
	Type     string `json:"type" maxLength:"20" doc:"Change type (added, removed, changed)"`
	OldValue any    `json:"oldValue,omitempty" doc:"Original value"`
	NewValue any    `json:"newValue,omitempty" doc:"New value"`
}

// DiffResponse wraps diff results.
type DiffResponse struct {
	Body struct {
		Changes []DiffChange `json:"changes" doc:"List of changes"`
		Summary struct {
			Total   int `json:"total" doc:"Total changes"`
			Added   int `json:"added" doc:"Fields added"`
			Removed int `json:"removed" doc:"Fields removed"`
			Changed int `json:"changed" doc:"Fields changed"`
		} `json:"summary" doc:"Change summary"`
	}
}

// RegisterExplainEndpoints registers explain and diff API endpoints.
func RegisterExplainEndpoints(humaAPI huma.API, hctx *api.HandlerContext, prefix string) {
	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID: "explain-solution",
		Method:      http.MethodPost,
		Path:        fmt.Sprintf("%s/explain", prefix),
		Summary:     "Explain a solution",
		Description: "Returns detailed analysis of a solution's structure and components.",
		Tags:        []string{"Explain"},
	}, hctx, http.StatusOK), func(_ context.Context, input *ExplainRequest) (*ExplainResponse, error) {
		if input.Body.Solution == "" {
			return nil, huma.NewError(http.StatusBadRequest, "solution YAML is required")
		}

		sol := &solution.Solution{}
		if err := sol.FromYAML([]byte(input.Body.Solution)); err != nil {
			return nil, huma.NewError(http.StatusBadRequest, fmt.Sprintf("invalid solution YAML: %v", err))
		}

		resolverNames := make([]string, 0, len(sol.Spec.Resolvers))
		for name := range sol.Spec.Resolvers {
			resolverNames = append(resolverNames, name)
		}

		resp := &ExplainResponse{}
		resp.Body.Name = sol.Metadata.Name
		resp.Body.Description = sol.Metadata.Description
		resp.Body.ResolverCount = len(sol.Spec.Resolvers)
		resp.Body.HasWorkflow = sol.Spec.HasWorkflow()
		resp.Body.HasActions = sol.Spec.HasActions()
		resp.Body.Resolvers = resolverNames
		resp.Body.Tags = sol.Metadata.Tags
		return resp, nil
	})

	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID: "diff-solutions",
		Method:      http.MethodPost,
		Path:        fmt.Sprintf("%s/diff", prefix),
		Summary:     "Diff two solutions",
		Description: "Compares two solution YAML documents and returns the differences.",
		Tags:        []string{"Explain"},
	}, hctx, http.StatusOK), func(_ context.Context, input *DiffRequest) (*DiffResponse, error) {
		if input.Body.SolutionA == "" || input.Body.SolutionB == "" {
			return nil, huma.NewError(http.StatusBadRequest, "both solutionA and solutionB are required")
		}

		solA := &solution.Solution{}
		if err := solA.FromYAML([]byte(input.Body.SolutionA)); err != nil {
			return nil, huma.NewError(http.StatusBadRequest, fmt.Sprintf("invalid solutionA YAML: %v", err))
		}

		solB := &solution.Solution{}
		if err := solB.FromYAML([]byte(input.Body.SolutionB)); err != nil {
			return nil, huma.NewError(http.StatusBadRequest, fmt.Sprintf("invalid solutionB YAML: %v", err))
		}

		result := soldiff.Compare(solA, solB)

		changes := make([]DiffChange, 0, len(result.Changes))
		for _, c := range result.Changes {
			changes = append(changes, DiffChange{
				Field:    c.Field,
				Type:     string(c.Type),
				OldValue: c.OldValue,
				NewValue: c.NewValue,
			})
		}

		resp := &DiffResponse{}
		resp.Body.Changes = changes
		resp.Body.Summary.Total = result.Summary.Total
		resp.Body.Summary.Added = result.Summary.Added
		resp.Body.Summary.Removed = result.Summary.Removed
		resp.Body.Summary.Changed = result.Summary.Changed
		return resp, nil
	})
}
