// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"context"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// DeferredValue represents an expression that is preserved for runtime evaluation.
// This is used when a ValueRef references __actions, which cannot be resolved until
// the referenced action has completed during workflow execution.
type DeferredValue struct {
	// OriginalExpr is the original CEL expression string (if expr-based).
	OriginalExpr string `json:"originalExpr,omitempty" yaml:"originalExpr,omitempty" doc:"Original CEL expression"`

	// OriginalTmpl is the original Go template string (if tmpl-based).
	OriginalTmpl string `json:"originalTmpl,omitempty" yaml:"originalTmpl,omitempty" doc:"Original Go template"`

	// Deferred indicates this value requires runtime evaluation.
	Deferred bool `json:"deferred" yaml:"deferred" doc:"If true, value requires runtime evaluation"`
}

// IsDeferred returns true if this value requires runtime evaluation.
func (d *DeferredValue) IsDeferred() bool {
	return d != nil && d.Deferred
}

// Evaluate resolves the deferred value using the provided resolver data and additional variables.
// The additionalVars should contain both the __actions namespace (keyed by celexp.VarActions)
// and any alias top-level variables pointing to individual action result data.
func (d *DeferredValue) Evaluate(ctx context.Context, resolverData, additionalVars map[string]any) (any, error) {
	if d == nil || !d.Deferred {
		return nil, fmt.Errorf("cannot evaluate non-deferred value")
	}

	if d.OriginalExpr != "" {
		result, err := celexp.EvaluateExpression(ctx, d.OriginalExpr, resolverData, additionalVars)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate deferred expression: %w", err)
		}
		return result, nil
	}

	if d.OriginalTmpl != "" {
		// For templates, merge resolver data with additional vars at the template data level
		templateData := map[string]any{
			"_": resolverData,
		}
		for k, v := range additionalVars {
			templateData[k] = v
		}
		result, err := gotmpl.Execute(ctx, gotmpl.TemplateOptions{
			Content: d.OriginalTmpl,
			Data:    templateData,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to execute deferred template: %w", err)
		}
		return result.Output, nil
	}

	return nil, fmt.Errorf("deferred value has no expression or template")
}

// Materialize evaluates a ValueRef, returning either a concrete value
// or a DeferredValue if it references __actions.
// This is used during the render phase to prepare action inputs.
func Materialize(ctx context.Context, v *spec.ValueRef, resolverData map[string]any) (any, error) {
	if v == nil {
		return nil, nil
	}

	// Check if the value references __actions
	if v.ReferencesVariable(celexp.VarActions) {
		return materializeDeferred(v)
	}

	// Evaluate immediately with resolver data
	return v.Resolve(ctx, resolverData, nil)
}

// materializeDeferred creates a DeferredValue from a ValueRef that references __actions.
func materializeDeferred(v *spec.ValueRef) (*DeferredValue, error) {
	if v.Expr != nil {
		return &DeferredValue{
			OriginalExpr: string(*v.Expr),
			Deferred:     true,
		}, nil
	}

	if v.Tmpl != nil {
		return &DeferredValue{
			OriginalTmpl: string(*v.Tmpl),
			Deferred:     true,
		}, nil
	}

	// This shouldn't happen - ReferencesVariable should only return true for expr/tmpl
	return nil, fmt.Errorf("cannot defer non-expression value")
}

// MaterializeInputs processes all inputs for an action, materializing immediate values
// and preserving deferred values that reference __actions.
// Returns a map where concrete values are resolved and __actions references are DeferredValues.
func MaterializeInputs(ctx context.Context, inputs map[string]*spec.ValueRef, resolverData map[string]any) (map[string]any, error) {
	if inputs == nil {
		return nil, nil
	}

	result := make(map[string]any, len(inputs))

	for name, valueRef := range inputs {
		if valueRef == nil {
			result[name] = nil
			continue
		}

		materialized, err := Materialize(ctx, valueRef, resolverData)
		if err != nil {
			return nil, fmt.Errorf("failed to materialize input %q: %w", name, err)
		}
		result[name] = materialized
	}

	return result, nil
}

// HasDeferredValues checks if any values in the map are deferred.
func HasDeferredValues(values map[string]any) bool {
	for _, v := range values {
		if dv, ok := v.(*DeferredValue); ok && dv.IsDeferred() {
			return true
		}
	}
	return false
}

// ResolveDeferredValues evaluates all deferred values in the map using the provided additional variables.
// additionalVars should contain __actions namespace and any alias variables.
// Non-deferred values are passed through unchanged.
func ResolveDeferredValues(ctx context.Context, values, resolverData, additionalVars map[string]any) (map[string]any, error) {
	if values == nil {
		return nil, nil
	}

	result := make(map[string]any, len(values))

	for name, v := range values {
		if dv, ok := v.(*DeferredValue); ok && dv.IsDeferred() {
			resolved, err := dv.Evaluate(ctx, resolverData, additionalVars)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve deferred value %q: %w", name, err)
			}
			result[name] = resolved
		} else {
			result[name] = v
		}
	}

	return result, nil
}

// GetDeferredInputNames returns the names of inputs that contain deferred values.
func GetDeferredInputNames(values map[string]any) []string {
	var names []string
	for name, v := range values {
		if dv, ok := v.(*DeferredValue); ok && dv.IsDeferred() {
			names = append(names, name)
		}
	}
	return names
}
