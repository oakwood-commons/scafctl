// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package endpoints

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// CELEvalRequest is the request body for CEL expression evaluation.
type CELEvalRequest struct {
	Body struct {
		Expression string         `json:"expression" maxLength:"10000" doc:"CEL expression to evaluate"`
		Data       map[string]any `json:"data,omitempty" doc:"Data context for expression evaluation"`
	}
}

// CELEvalResponse wraps a CEL evaluation result.
type CELEvalResponse struct {
	Body struct {
		Result any    `json:"result" doc:"Evaluation result"`
		Type   string `json:"type,omitempty" maxLength:"100" doc:"Result type"`
	}
}

// TemplateEvalRequest is the request body for Go template evaluation.
type TemplateEvalRequest struct {
	Body struct {
		Template string         `json:"template" maxLength:"100000" doc:"Go template content"`
		Data     map[string]any `json:"data,omitempty" doc:"Data context for template rendering"`
		Name     string         `json:"name,omitempty" maxLength:"255" doc:"Template name" example:"my-template"`
	}
}

// TemplateEvalResponse wraps a Go template evaluation result.
type TemplateEvalResponse struct {
	Body struct {
		Output string `json:"output" doc:"Rendered template output"`
	}
}

// RegisterEvalEndpoints registers CEL and template evaluation API endpoints.
func RegisterEvalEndpoints(humaAPI huma.API, hctx *api.HandlerContext, prefix string) {
	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID: "eval-cel",
		Method:      http.MethodPost,
		Path:        fmt.Sprintf("%s/eval/cel", prefix),
		Summary:     "Evaluate a CEL expression",
		Description: "Evaluates a CEL expression with optional data context.",
		Tags:        []string{"Eval"},
	}, hctx, http.StatusOK), func(ctx context.Context, input *CELEvalRequest) (*CELEvalResponse, error) {
		if input.Body.Expression == "" {
			return nil, huma.NewError(http.StatusBadRequest, "expression is required")
		}

		result, err := celexp.EvaluateExpression(ctx, input.Body.Expression, input.Body.Data, nil,
			celexp.WithCostLimit(settings.DefaultAPIFilterCostLimit))
		if err != nil {
			return nil, huma.NewError(http.StatusBadRequest, fmt.Sprintf("CEL evaluation failed: %v", err))
		}

		resp := &CELEvalResponse{}
		resp.Body.Result = result
		resp.Body.Type = fmt.Sprintf("%T", result)
		return resp, nil
	})

	huma.Register(humaAPI, withDefaults(huma.Operation{
		OperationID: "eval-template",
		Method:      http.MethodPost,
		Path:        fmt.Sprintf("%s/eval/template", prefix),
		Summary:     "Evaluate a Go template",
		Description: "Renders a Go template with optional data context.",
		Tags:        []string{"Eval"},
	}, hctx, http.StatusOK), func(ctx context.Context, input *TemplateEvalRequest) (*TemplateEvalResponse, error) {
		if input.Body.Template == "" {
			return nil, huma.NewError(http.StatusBadRequest, "template is required")
		}

		name := input.Body.Name
		if name == "" {
			name = "api-eval"
		}

		evalCtx, cancel := context.WithTimeout(ctx, settings.DefaultAPIEvalTimeout)
		defer cancel()

		result, err := gotmpl.Execute(evalCtx, gotmpl.TemplateOptions{
			Content: input.Body.Template,
			Name:    name,
			Data:    input.Body.Data,
		})
		if err != nil {
			return nil, huma.NewError(http.StatusBadRequest, fmt.Sprintf("template evaluation failed: %v", err))
		}

		resp := &TemplateEvalResponse{}
		resp.Body.Output = result.Output
		return resp, nil
	})
}
