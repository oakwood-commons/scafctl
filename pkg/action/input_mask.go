// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"context"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/redact"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// WithSensitiveResolvers configures the set of resolver names whose outputs are
// marked sensitive: true or backed by credentials so action input snapshots,
// graph renders, and coercion diagnostics structurally mask them regardless of
// value length or coerced Go type.
func WithSensitiveResolvers(names map[string]bool) ExecutorOption {
	return func(e *Executor) {
		if len(names) == 0 {
			return
		}
		cp := make(map[string]bool, len(names))
		for k, v := range names {
			if v {
				cp[k] = true
			}
		}
		e.sensitiveResolvers = cp
	}
}

// RedactGraphInputs scrubs all materialized action inputs and forEach item
// metadata in graph in place using both structural ValueRef analysis (covering
// sensitive/credential-backed resolvers even when <4 bytes or coerced to
// non-string types) and tracker substring redaction. Intended for render,
// dry-run, and preview commands that inspect graph.Actions without executing.
func RedactGraphInputs(graph *Graph, tracker *redact.Tracker, sensitiveResolvers map[string]bool) {
	if graph == nil || len(graph.Actions) == 0 {
		return
	}
	for _, ea := range graph.Actions {
		if ea == nil {
			continue
		}

		effectiveSensitive, itemTainted := effectiveSensitiveVarsForAction(ea, sensitiveResolvers)

		if ea.ForEachMetadata != nil {
			metaCopy := *ea.ForEachMetadata
			if itemTainted {
				metaCopy.Item = redact.RedactedPlaceholder
			} else {
				var inVR *spec.ValueRef
				if ea.Action != nil && ea.ForEach != nil {
					inVR = ea.ForEach.In
				}
				metaCopy.Item = maskForEachItemBySpec(inVR, metaCopy.Index, metaCopy.Item, sensitiveResolvers)
				if tracker != nil {
					metaCopy.Item = tracker.RedactAny(metaCopy.Item)
				}
			}
			ea.ForEachMetadata = &metaCopy
		}

		if len(ea.MaterializedInputs) == 0 {
			continue
		}
		masked := make(map[string]any, len(ea.MaterializedInputs))
		for k, val := range ea.MaterializedInputs {
			var vr *spec.ValueRef
			if ea.Action != nil && ea.Inputs != nil {
				vr = ea.Inputs[k]
			}
			mVal, _ := maskValueByRef(vr, val, effectiveSensitive, nil, 0)
			masked[k] = mVal
		}
		if tracker != nil {
			masked = tracker.RedactMap(masked)
		}
		ea.MaterializedInputs = masked
	}
}

func effectiveSensitiveVarsForAction(ea *ExpandedAction, sensitiveResolvers map[string]bool) (map[string]bool, bool) {
	if ea == nil || ea.Action == nil || ea.ForEach == nil || ea.ForEach.In == nil {
		return sensitiveResolvers, false
	}
	inVR := ea.ForEach.In
	itemTainted := false
	if inVR.Literal != nil {
		if slice, ok := inVR.Literal.([]any); ok && ea.ForEachMetadata != nil &&
			ea.ForEachMetadata.Index >= 0 && ea.ForEachMetadata.Index < len(slice) {
			itemTainted = literalSpecIsTainted(slice[ea.ForEachMetadata.Index], sensitiveResolvers, nil, 0)
		} else {
			itemTainted = literalSpecIsTainted(inVR.Literal, sensitiveResolvers, nil, 0)
		}
	} else {
		itemTainted = valueRefIsSecret(inVR, sensitiveResolvers, nil)
	}
	if !itemTainted {
		return sensitiveResolvers, false
	}
	cp := make(map[string]bool, len(sensitiveResolvers)+2)
	for k, v := range sensitiveResolvers {
		if v {
			cp[k] = true
		}
	}
	cp["__item"] = true
	if ea.ForEach.Item != "" {
		cp[ea.ForEach.Item] = true
	}
	return cp, true
}

func maskForEachItemBySpec(inVR *spec.ValueRef, index int, item any, sensitiveResolvers map[string]bool) any {
	if inVR == nil || inVR.Literal == nil {
		return item
	}
	slice, ok := inVR.Literal.([]any)
	if !ok || index < 0 || index >= len(slice) {
		return item
	}
	masked, _ := maskLiteralBySpec(slice[index], item, sensitiveResolvers, nil, 0)
	return masked
}

// maskResolvedActionInputs builds a copy of resolvedInputs suitable for
// ActionResult.Inputs (and __actions.<name>.inputs) by structurally masking
// inputs derived from scrt:, tkn:, or sensitive/credential-backed resolvers
// before applying tracker.RedactMap. It also returns the set of top-level
// property names carrying secret material so provider coercion errors are masked.
func (e *Executor) maskResolvedActionInputs(
	action *ExpandedAction,
	resolvedInputs map[string]any,
	tracker *redact.Tracker,
) (map[string]any, map[string]bool) {
	if resolvedInputs == nil {
		return nil, nil
	}

	effectiveSensitive, _ := effectiveSensitiveVarsForAction(action, e.sensitiveResolvers)
	masked := make(map[string]any, len(resolvedInputs))
	secretProps := make(map[string]bool)

	if action != nil && action.HasCall() {
		def := e.calls[action.Call]
		taintedArgs := make(map[string]bool)
		for argName, argVR := range action.Args {
			if valueRefIsSecret(argVR, effectiveSensitive, nil) {
				taintedArgs[argName] = true
			}
		}
		if def != nil {
			for argName, paramDef := range def.Args {
				if taintedArgs[argName] || paramDef == nil || paramDef.Default == nil {
					continue
				}
				defVR := spec.ValueRef{Literal: paramDef.Default}
				if valueRefIsSecret(&defVR, effectiveSensitive, nil) {
					taintedArgs[argName] = true
				}
			}
		}
		for k, val := range resolvedInputs {
			var defVR *spec.ValueRef
			if def != nil && def.Inputs != nil {
				defVR = def.Inputs[k]
			}
			mVal, isSec := maskValueByRef(defVR, val, effectiveSensitive, taintedArgs, 0)
			masked[k] = mVal
			if isSec {
				secretProps[k] = true
			}
		}
	} else {
		for k, val := range resolvedInputs {
			var vr *spec.ValueRef
			if action != nil && action.Action != nil && action.Inputs != nil {
				vr = action.Inputs[k]
			}
			mVal, isSec := maskValueByRef(vr, val, effectiveSensitive, nil, 0)
			masked[k] = mVal
			if isSec {
				secretProps[k] = true
			}
		}
	}

	if tracker != nil {
		masked = tracker.RedactMap(masked)
	}
	return masked, secretProps
}

func maskValueByRef(
	vr *spec.ValueRef,
	val any,
	sensitiveResolvers map[string]bool,
	taintedArgs map[string]bool,
	depth int,
) (any, bool) {
	if vr == nil || depth > 32 {
		return val, false
	}

	if vr.Secret != nil || vr.Token != nil {
		return redact.RedactedPlaceholder, true
	}

	if vr.Resolver != nil {
		if bindingIsTainted(*vr.Resolver, sensitiveResolvers, taintedArgs) {
			return redact.RedactedPlaceholder, true
		}
		return val, false
	}

	if vr.Expr != nil || vr.Tmpl != nil {
		if exprOrTmplIsTainted(vr, sensitiveResolvers, taintedArgs) {
			return redact.RedactedPlaceholder, true
		}
		return val, false
	}

	if vr.Literal != nil {
		return maskLiteralBySpec(vr.Literal, val, sensitiveResolvers, taintedArgs, depth+1)
	}

	return val, false
}

func maskLiteralBySpec(
	specNode any,
	val any,
	sensitiveResolvers map[string]bool,
	taintedArgs map[string]bool,
	depth int,
) (any, bool) {
	if depth > 32 || specNode == nil || val == nil {
		return val, false
	}

	switch typedSpec := specNode.(type) {
	case map[string]any:
		if spec.IsValueRefMap(typedSpec) {
			nestedVR, err := spec.ParseNestedValueRef(typedSpec)
			if err != nil {
				return val, false
			}
			return maskValueByRef(nestedVR, val, sensitiveResolvers, taintedArgs, depth+1)
		}
		valMap, ok := val.(map[string]any)
		if !ok {
			return val, false
		}
		out := make(map[string]any, len(valMap))
		anySecret := false
		for k, elemVal := range valMap {
			specChild, hasSpec := typedSpec[k]
			if !hasSpec {
				out[k] = elemVal
				continue
			}
			mChild, childSec := maskLiteralBySpec(specChild, elemVal, sensitiveResolvers, taintedArgs, depth+1)
			out[k] = mChild
			if childSec {
				anySecret = true
			}
		}
		return out, anySecret

	case []any:
		valSlice, ok := val.([]any)
		if !ok {
			return val, false
		}
		out := make([]any, len(valSlice))
		anySecret := false
		for i, elemVal := range valSlice {
			if i >= len(typedSpec) {
				out[i] = elemVal
				continue
			}
			mChild, childSec := maskLiteralBySpec(typedSpec[i], elemVal, sensitiveResolvers, taintedArgs, depth+1)
			out[i] = mChild
			if childSec {
				anySecret = true
			}
		}
		return out, anySecret

	default:
		return val, false
	}
}

func valueRefIsSecret(vr *spec.ValueRef, sensitiveResolvers, taintedArgs map[string]bool) bool {
	if vr == nil {
		return false
	}
	if vr.ContainsCredentialRef() {
		return true
	}
	if vr.Resolver != nil && bindingIsTainted(*vr.Resolver, sensitiveResolvers, taintedArgs) {
		return true
	}
	if (vr.Expr != nil || vr.Tmpl != nil) && exprOrTmplIsTainted(vr, sensitiveResolvers, taintedArgs) {
		return true
	}
	if vr.Literal != nil {
		return literalSpecIsTainted(vr.Literal, sensitiveResolvers, taintedArgs, 0)
	}
	return false
}

func literalSpecIsTainted(specNode any, sensitiveResolvers, taintedArgs map[string]bool, depth int) bool {
	if depth > 32 || specNode == nil {
		return false
	}
	switch typed := specNode.(type) {
	case map[string]any:
		if spec.IsValueRefMap(typed) {
			nestedVR, err := spec.ParseNestedValueRef(typed)
			if err != nil {
				return false
			}
			return valueRefIsSecret(nestedVR, sensitiveResolvers, taintedArgs)
		}
		for _, elem := range typed {
			if literalSpecIsTainted(elem, sensitiveResolvers, taintedArgs, depth+1) {
				return true
			}
		}
	case []any:
		for _, elem := range typed {
			if literalSpecIsTainted(elem, sensitiveResolvers, taintedArgs, depth+1) {
				return true
			}
		}
	}
	return false
}

func bindingIsTainted(binding string, sensitiveResolvers, taintedArgs map[string]bool) bool {
	if binding == "" {
		return false
	}
	root, rest, hasDot := strings.Cut(binding, ".")
	if root == "_" && hasDot {
		root, rest, hasDot = strings.Cut(rest, ".")
	}
	if root == "args" && hasDot && len(taintedArgs) > 0 {
		argName, _, _ := strings.Cut(rest, ".")
		return taintedArgs[argName]
	}
	return len(sensitiveResolvers) > 0 && sensitiveResolvers[root]
}

func exprOrTmplIsTainted(vr *spec.ValueRef, sensitiveResolvers, taintedArgs map[string]bool) bool {
	if vr == nil {
		return false
	}
	if vr.ContainsCredentialRef() {
		return true
	}
	if len(sensitiveResolvers) > 0 {
		for varName := range vr.ReferencedVariables() {
			if sensitiveResolvers[varName] {
				return true
			}
		}
	}
	if len(taintedArgs) == 0 {
		return false
	}
	if vr.Expr != nil {
		for _, prefix := range []string{"args.", "_.args."} {
			if argVars, err := vr.Expr.GetVariablesWithPrefix(context.Background(), prefix); err == nil {
				for _, argName := range argVars {
					rootArg, _, _ := strings.Cut(argName, ".")
					if taintedArgs[rootArg] {
						return true
					}
				}
			}
		}
	}
	if vr.Tmpl != nil {
		if refs, err := gotmpl.GetGoTemplateReferences(string(*vr.Tmpl), "", ""); err == nil {
			for _, ref := range refs {
				if ref.Scoped {
					continue
				}
				p := strings.TrimPrefix(ref.Path, ".")
				if after, ok := strings.CutPrefix(p, "args."); ok {
					rootArg, _, _ := strings.Cut(after, ".")
					if taintedArgs[rootArg] {
						return true
					}
				}
			}
		}
	}
	return false
}
