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

// DeferredValue represents an input that is preserved for runtime evaluation.
// This is used when a ValueRef references __actions (or an action alias) or
// contains credential references (scrt: / tkn:) that must not be resolved during
// graph construction, render, or dry-run previews.
type DeferredValue struct {
	// OriginalExpr is the original CEL expression string (if expr-based).
	OriginalExpr string `json:"originalExpr,omitempty" yaml:"originalExpr,omitempty" doc:"Original CEL expression"`

	// OriginalTmpl is the original Go template string (if tmpl-based).
	OriginalTmpl string `json:"originalTmpl,omitempty" yaml:"originalTmpl,omitempty" doc:"Original Go template"`

	// OriginalRef is a symbolic summary for credential or nested deferred values
	// (e.g. "scrt:npm-token" or "tkn:github"), safe to serialize in render/dry-run output.
	OriginalRef string `json:"originalRef,omitempty" yaml:"originalRef,omitempty" doc:"Symbolic credential or deferred reference summary" maxLength:"512" example:"scrt:npm-token"`

	// Deferred indicates this value requires runtime evaluation.
	Deferred bool `json:"deferred" yaml:"deferred" doc:"If true, value requires runtime evaluation"`

	// OriginalValueRef retains the parsed ValueRef for runtime evaluation of
	// credential-bearing or nested deferred inputs without serializing raw structures.
	OriginalValueRef *spec.ValueRef `json:"-" yaml:"-"`

	// IterCtx retains the forEach iteration context when a forEach action input is deferred.
	IterCtx *spec.IterationContext `json:"-" yaml:"-"`
}

// IsDeferred returns true if this value requires runtime evaluation.
func (d *DeferredValue) IsDeferred() bool {
	return d != nil && d.Deferred
}

// Summary returns a safe symbolic string describing the deferred input for
// dry-run and preview reports without exposing secret values.
func (d *DeferredValue) Summary() string {
	if d == nil {
		return ""
	}
	if d.OriginalExpr != "" {
		return d.OriginalExpr
	}
	if d.OriginalTmpl != "" {
		return d.OriginalTmpl
	}
	if d.OriginalRef != "" {
		return d.OriginalRef
	}
	if d.OriginalValueRef != nil {
		return d.OriginalValueRef.CredentialSummary()
	}
	if d.Deferred {
		return "(deferred)"
	}
	return ""
}

// Evaluate resolves the deferred value using the provided resolver data and additional variables.
// The additionalVars should contain both the __actions namespace (keyed by celexp.VarActions)
// and any alias top-level variables pointing to individual action result data.
func (d *DeferredValue) Evaluate(ctx context.Context, resolverData, additionalVars map[string]any) (any, error) {
	if d == nil || !d.Deferred {
		return nil, fmt.Errorf("cannot evaluate non-deferred value")
	}

	if d.OriginalValueRef != nil {
		return evaluateValueRefWithActionVars(ctx, d.OriginalValueRef, resolverData, additionalVars, d.IterCtx)
	}

	if d.OriginalExpr != "" {
		result, err := celexp.EvaluateExpression(ctx, d.OriginalExpr, resolverData, additionalVars)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate deferred expression: %w", err)
		}
		return result, nil
	}

	if d.OriginalTmpl != "" {
		templateData := mergeTemplateActionData(resolverData, additionalVars, d.IterCtx)
		result, err := gotmpl.Execute(ctx, gotmpl.TemplateOptions{
			Content:    d.OriginalTmpl,
			Data:       templateData,
			MissingKey: gotmpl.MissingKeyError,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to execute deferred template: %w", err)
		}
		return result.Output, nil
	}

	return nil, fmt.Errorf("deferred value has no expression or template")
}

func mergeCELActionVars(additionalVars map[string]any, iterCtx *spec.IterationContext) map[string]any {
	if iterCtx == nil && len(additionalVars) == 0 {
		return nil
	}
	merged := make(map[string]any, len(additionalVars)+4)
	for k, v := range additionalVars {
		merged[k] = v
	}
	if iterCtx != nil {
		merged[celexp.VarItem] = iterCtx.Item
		merged[celexp.VarIndex] = iterCtx.Index
		if iterCtx.ItemAlias != "" {
			merged[iterCtx.ItemAlias] = iterCtx.Item
		}
		if iterCtx.IndexAlias != "" {
			merged[iterCtx.IndexAlias] = iterCtx.Index
		}
	}
	return merged
}

func mergeTemplateActionData(resolverData, additionalVars map[string]any, iterCtx *spec.IterationContext) map[string]any {
	templateData := make(map[string]any, len(resolverData)+len(additionalVars)+4)
	for k, v := range resolverData {
		templateData[k] = v
	}
	for k, v := range additionalVars {
		templateData[k] = v
	}
	if iterCtx != nil {
		templateData["__item"] = iterCtx.Item
		templateData["__index"] = iterCtx.Index
		if iterCtx.ItemAlias != "" {
			templateData[iterCtx.ItemAlias] = iterCtx.Item
		}
		if iterCtx.IndexAlias != "" {
			templateData[iterCtx.IndexAlias] = iterCtx.Index
		}
	}
	return templateData
}

// evaluateValueRefWithActionVars resolves vr with full access to resolverData,
// action-level variables (__actions, aliases, __cwd, __execution), forEach
// iteration variables, and CredentialResolver (scrt:/tkn:) at any nesting depth.
func evaluateValueRefWithActionVars(
	ctx context.Context,
	vr *spec.ValueRef,
	resolverData, additionalVars map[string]any,
	iterCtx *spec.IterationContext,
) (any, error) {
	if vr == nil {
		return nil, nil
	}

	if vr.Expr != nil {
		mergedVars := mergeCELActionVars(additionalVars, iterCtx)
		result, err := celexp.EvaluateExpression(ctx, string(*vr.Expr), resolverData, mergedVars)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate deferred expression: %w", err)
		}
		return result, nil
	}

	if vr.Tmpl != nil {
		templateData := mergeTemplateActionData(resolverData, additionalVars, iterCtx)
		result, err := gotmpl.Execute(ctx, gotmpl.TemplateOptions{
			Content:    string(*vr.Tmpl),
			Data:       templateData,
			MissingKey: gotmpl.MissingKeyError,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to execute deferred template: %w", err)
		}
		return result.Output, nil
	}

	if vr.Literal != nil {
		return evaluateNestedActionLiteral(ctx, vr.Literal, resolverData, additionalVars, iterCtx, 0)
	}

	return vr.ResolveWithIterationContext(ctx, resolverData, nil, iterCtx)
}

const maxDeferredLiteralDepth = 32

func evaluateNestedActionLiteral(
	ctx context.Context,
	val any,
	resolverData, additionalVars map[string]any,
	iterCtx *spec.IterationContext,
	depth int,
) (any, error) {
	if depth > maxDeferredLiteralDepth {
		return nil, fmt.Errorf("nested value ref exceeds maximum depth of %d", maxDeferredLiteralDepth)
	}
	switch typed := val.(type) {
	case map[string]any:
		if spec.IsValueRefMap(typed) {
			if exprVal, ok := typed["expr"]; ok {
				s, isStr := exprVal.(string)
				if !isStr {
					return nil, fmt.Errorf("invalid nested value ref: expr value must be a string, got %T", exprVal)
				}
				e := celexp.Expression(s)
				return evaluateValueRefWithActionVars(ctx, &spec.ValueRef{Expr: &e}, resolverData, additionalVars, iterCtx)
			}
			if tmplVal, ok := typed["tmpl"]; ok {
				s, isStr := tmplVal.(string)
				if !isStr {
					return nil, fmt.Errorf("invalid nested value ref: tmpl value must be a string, got %T", tmplVal)
				}
				t := gotmpl.GoTemplatingContent(s)
				return evaluateValueRefWithActionVars(ctx, &spec.ValueRef{Tmpl: &t}, resolverData, additionalVars, iterCtx)
			}
			// Delegate rslvr / scrt / tkn single-key maps to spec.ValueRef
			return (&spec.ValueRef{Literal: typed}).ResolveWithIterationContext(ctx, resolverData, nil, iterCtx)
		}
		out := make(map[string]any, len(typed))
		for k, elem := range typed {
			resolved, err := evaluateNestedActionLiteral(ctx, elem, resolverData, additionalVars, iterCtx, depth+1)
			if err != nil {
				return nil, fmt.Errorf("key %q: %w", k, err)
			}
			out[k] = resolved
		}
		return out, nil

	case []any:
		out := make([]any, len(typed))
		for i, elem := range typed {
			resolved, err := evaluateNestedActionLiteral(ctx, elem, resolverData, additionalVars, iterCtx, depth+1)
			if err != nil {
				return nil, fmt.Errorf("index %d: %w", i, err)
			}
			out[i] = resolved
		}
		return out, nil

	default:
		return val, nil
	}
}

// Materialize evaluates a ValueRef, returning either a concrete value
// or a DeferredValue if it references __actions or contains credential refs (scrt:/tkn:).
// This is used during the render phase to prepare action inputs.
func Materialize(ctx context.Context, v *spec.ValueRef, resolverData map[string]any) (any, error) {
	if v == nil {
		return nil, nil
	}

	// Defer if the value references __actions or contains scrt:/tkn: credentials.
	if v.ReferencesVariable(celexp.VarActions) || v.ContainsCredentialRef() {
		return materializeDeferredWithIter(v, nil)
	}

	// Evaluate immediately with resolver data
	return v.Resolve(ctx, resolverData, nil)
}

// materializeDeferredWithIter creates a DeferredValue from a ValueRef while
// retaining optional forEach iteration context for runtime evaluation.
func materializeDeferredWithIter(v *spec.ValueRef, iterCtx *spec.IterationContext) (*DeferredValue, error) {
	if v == nil {
		return nil, fmt.Errorf("cannot defer nil value")
	}

	if v.Expr != nil {
		return &DeferredValue{
			OriginalExpr:     string(*v.Expr),
			Deferred:         true,
			OriginalValueRef: v,
			IterCtx:          iterCtx,
		}, nil
	}

	if v.Tmpl != nil {
		return &DeferredValue{
			OriginalTmpl:     string(*v.Tmpl),
			Deferred:         true,
			OriginalValueRef: v,
			IterCtx:          iterCtx,
		}, nil
	}

	if v.Secret != nil || v.Token != nil {
		return &DeferredValue{
			OriginalRef:      v.CredentialSummary(),
			Deferred:         true,
			OriginalValueRef: v,
			IterCtx:          iterCtx,
		}, nil
	}

	if v.Literal != nil && (v.ContainsCredentialRef() || len(v.ReferencedVariables()) > 0) {
		summary := v.CredentialSummary()
		return &DeferredValue{
			OriginalRef:      summary,
			Deferred:         true,
			OriginalValueRef: v,
			IterCtx:          iterCtx,
		}, nil
	}

	return nil, fmt.Errorf("cannot defer non-expression value")
}

// MaterializeInputs processes all inputs for an action, materializing immediate values
// and preserving deferred values that reference __actions or credentials.
// Returns a map where concrete values are resolved and deferred references are DeferredValues.
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
