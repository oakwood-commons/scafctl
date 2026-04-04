// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celprovider

import (
	"context"
	"fmt"
	"maps"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/ptrs"
)

const (
	// ProviderName is the name of the cel provider
	ProviderName = "cel"
	// Version is the version of the cel provider
	Version = "1.0.0"
)

// CelProvider provides data transformation using CEL expressions
type CelProvider struct {
	descriptor *provider.Descriptor
}

// NewCelProvider creates a new CEL provider
func NewCelProvider() *CelProvider {
	version, _ := semver.NewVersion(Version)

	return &CelProvider{
		descriptor: &provider.Descriptor{
			Name:        ProviderName,
			DisplayName: "CEL Provider",
			APIVersion:  "v1",
			Description: "Transform and evaluate data using CEL (Common Expression Language) expressions with resolver data from context",
			Version:     version,
			Category:    "data",
			WhatIf: func(_ context.Context, input any) (string, error) {
				inputs, ok := input.(map[string]any)
				if !ok {
					return "", nil
				}
				expr, _ := inputs["expression"].(string)
				if expr != "" {
					const maxDisplay = 80
					if len(expr) > maxDisplay {
						expr = expr[:maxDisplay] + "..."
					}
					return fmt.Sprintf("Would evaluate CEL expression: %s", expr), nil
				}
				return "Would evaluate CEL expression", nil
			},
			Capabilities: []provider.Capability{
				provider.CapabilityTransform,
				provider.CapabilityAction,
			},
			Schema: schemahelper.ObjectSchema([]string{"expression"}, map[string]*jsonschema.Schema{
				"expression": schemahelper.StringProp("CEL expression to evaluate. Resolver data is available under the '_' variable (e.g., _.name).",
					schemahelper.WithExample("_.name.upperAscii()"),
					schemahelper.WithMaxLength(*ptrs.IntPtr(8192))),
				"variables": schemahelper.AnyProp("Additional variables to make available in the CEL expression context",
					schemahelper.WithExample(map[string]any{"prefix": "Mr.", "suffix": "Jr."})),
			}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityTransform: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"result": schemahelper.AnyProp("The evaluation result", schemahelper.WithExample("HELLO WORLD")),
				}),
				provider.CapabilityAction: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"success": schemahelper.BoolProp("Whether the CEL expression evaluated successfully"),
					"result":  schemahelper.AnyProp("The evaluation result", schemahelper.WithExample("HELLO WORLD")),
				}),
			},
			Examples: []provider.Example{
				{
					Name:        "Transform string to uppercase",
					Description: "Use CEL string extension to convert a string to uppercase using resolver data under _",
					YAML: `name: uppercase-transform
provider: cel
inputs:
  expression: "_.name.upperAscii()"`,
				},
				{
					Name:        "Conditional expression with resolver data",
					Description: "Evaluate conditional logic based on resolver context values under _",
					YAML: `name: environment-check
provider: cel
inputs:
  expression: "_.environment == 'prod' ? 'production' : 'non-production'"`,
				},
				{
					Name:        "Using custom variables",
					Description: "Evaluate expressions with resolver data under _ and custom top-level variables",
					YAML: `name: custom-variables
provider: cel
inputs:
  expression: "prefix + ' ' + _.name + ' ' + suffix"
  variables:
    prefix: "Dr."
    suffix: "PhD"`,
				},
			},
			// ExtractDependencies extracts resolver references from the CEL expression input
			ExtractDependencies: extractDependencies,
		},
	}
}

// Descriptor returns the provider's descriptor
func (p *CelProvider) Descriptor() *provider.Descriptor {
	return p.descriptor
}

// Execute performs the CEL expression evaluation
func (p *CelProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	inputs, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected map[string]any, got %T", ProviderName, input)
	}

	lgr.V(1).Info("executing provider", "provider", ProviderName)

	// Check for dry-run mode
	if provider.DryRunFromContext(ctx) {
		return p.executeDryRun(inputs)
	}

	// Extract expression
	exprStr, ok := inputs["expression"].(string)
	if !ok || exprStr == "" {
		return nil, fmt.Errorf("%s: expression is required and must be a string", ProviderName)
	}

	// Get resolver data from context
	resolverData, _ := provider.ResolverContextFromContext(ctx)

	// Get additional variables from inputs
	additionalVars := make(map[string]any)
	if vars, ok := inputs["variables"].(map[string]any); ok {
		maps.Copy(additionalVars, vars)
	}

	// Extract standard special variables from resolver data and make them top-level CEL variables.
	// These include __self, __item, __index, and __plan (pre-execution topology).
	if resolverData != nil {
		for _, key := range []string{celexp.VarSelf, celexp.VarItem, celexp.VarIndex, celexp.VarPlan} {
			if value, ok := resolverData[key]; ok {
				additionalVars[key] = value
				delete(resolverData, key)
			}
		}
	}

	// Extract custom forEach aliases from iteration context if present.
	// The iteration context provides explicit alias information set by the executor.
	if iterCtx, ok := provider.IterationContextFromContext(ctx); ok {
		if iterCtx.ItemAlias != "" {
			additionalVars[iterCtx.ItemAlias] = iterCtx.Item
			delete(resolverData, iterCtx.ItemAlias) // Remove from resolver data to avoid duplication
		}
		if iterCtx.IndexAlias != "" {
			additionalVars[iterCtx.IndexAlias] = iterCtx.Index
			delete(resolverData, iterCtx.IndexAlias) // Remove from resolver data to avoid duplication
		}
	}

	// Use helper to compile and evaluate the expression
	convertedResult, err := celexp.EvaluateExpression(ctx, exprStr, resolverData, additionalVars)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ProviderName, err)
	}

	lgr.V(1).Info("provider completed", "provider", ProviderName)

	// Return result directly - the resolver executor expects output.Data to be the actual value
	return &provider.Output{
		Data: convertedResult,
	}, nil
}

func (p *CelProvider) executeDryRun(inputs map[string]any) (*provider.Output, error) {
	exprStr, _ := inputs["expression"].(string)

	// Return a placeholder - the resolver executor expects output.Data to be the actual value
	return &provider.Output{
		Data: fmt.Sprintf("[DRY-RUN] Expression not evaluated: %s", exprStr),
		Metadata: map[string]any{
			"dryRun": true,
		},
	}, nil
}

// extractDependencies extracts resolver references from the CEL expression input.
// It uses the celexp package to parse the expression and extract variables with the "_." prefix.
func extractDependencies(inputs map[string]any) []string {
	// Get expression content
	exprStr, ok := inputs["expression"].(string)
	if !ok || exprStr == "" {
		return nil
	}

	// Use celexp to extract underscore variables
	expr := celexp.Expression(exprStr)
	vars, err := expr.GetUnderscoreVariables(context.TODO())
	if err != nil {
		// On parse error, fall back to no dependencies - the error will be caught during execution
		return nil
	}

	return vars
}
