// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"context"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/call"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// actionCallPlan holds the resolved provider name, inputs, and de-duplication
// metadata for an action that invokes a reusable call definition.
type actionCallPlan struct {
	// ProviderName is the definition's provider.
	ProviderName string
	// Inputs are the definition's inputs resolved with the args namespace in scope.
	Inputs map[string]any
	// ResolverData is the resolver data enriched with the args namespace. It is
	// installed as the provider resolver context so providers that evaluate
	// against it (e.g. cel) can read _.args.* just like resolver call sites.
	ResolverData map[string]any
	// DedupKey is the canonical de-duplication key (empty when dedup is disabled).
	DedupKey string
	// Dedup indicates whether the definition opted into de-duplication.
	Dedup bool
}

// expandActionCall resolves a call-based action's arguments, binds them against
// the referenced definition, exposes them under the args namespace, and resolves
// the definition's inputs. It returns the effective provider and inputs to run.
func (e *Executor) expandActionCall(ctx context.Context, action *ExpandedAction, aliasMap map[string]string) (*actionCallPlan, error) {
	def, ok := e.calls[action.Call]
	if !ok || def == nil {
		return nil, fmt.Errorf("call %q not found", action.Call)
	}

	additionalVars := e.buildAdditionalVars(aliasMap)

	// Resolve each call-site argument in the action's context (resolver data plus
	// the __actions namespace for args that read prior action results).
	resolvedArgs := make(map[string]any, len(action.Args))
	for name, vr := range action.Args {
		if vr == nil {
			return nil, fmt.Errorf("call %q: argument %q has no value (nil)", action.Call, name)
		}
		v, err := e.resolveArgRef(ctx, vr, additionalVars)
		if err != nil {
			return nil, fmt.Errorf("call %q: failed to resolve argument %q: %w", action.Call, name, err)
		}
		resolvedArgs[name] = v
	}

	bound, err := call.BindArgs(action.Call, def, resolvedArgs)
	if err != nil {
		return nil, err
	}

	// Expose bound arguments under the args namespace and resolve definition inputs.
	enriched := call.ExpandData(e.resolverData, bound)
	inputs := make(map[string]any, len(def.Inputs))
	for name, vr := range def.Inputs {
		if vr == nil {
			return nil, fmt.Errorf("call %q: definition input %q has no value (nil)", action.Call, name)
		}
		resolved, err := vr.Resolve(ctx, enriched, nil)
		if err != nil {
			return nil, fmt.Errorf("call %q: failed to resolve definition input %q: %w", action.Call, name, err)
		}
		inputs[name] = resolved
	}

	plan := &actionCallPlan{
		ProviderName: def.Provider,
		Inputs:       inputs,
		ResolverData: enriched,
	}
	if def.Dedup {
		key, keyErr := call.DedupKey(action.Call, bound)
		if keyErr == nil {
			plan.Dedup = true
			plan.DedupKey = key
		}
	}
	return plan, nil
}

// resolveArgRef resolves a single call-site argument ValueRef. Arguments that
// reference the __actions namespace are evaluated against the merged action data
// (mirroring deferred input evaluation); all others resolve against resolver data.
func (e *Executor) resolveArgRef(ctx context.Context, vr *spec.ValueRef, additionalVars map[string]any) (any, error) {
	if vr.ReferencesVariable(celexp.VarActions) {
		if vr.Expr != nil {
			return celexp.EvaluateExpression(ctx, string(*vr.Expr), e.resolverData, additionalVars)
		}
		if vr.Tmpl != nil {
			templateData := make(map[string]any, len(e.resolverData)+len(additionalVars))
			for k, v := range e.resolverData {
				templateData[k] = v
			}
			for k, v := range additionalVars {
				templateData[k] = v
			}
			result, err := gotmpl.Execute(ctx, gotmpl.TemplateOptions{
				Content:    string(*vr.Tmpl),
				Data:       templateData,
				MissingKey: gotmpl.MissingKeyError,
			})
			if err != nil {
				return nil, err
			}
			return result.Output, nil
		}
		return nil, fmt.Errorf("argument references %s but is not an expression or template", celexp.VarActions)
	}
	return vr.Resolve(ctx, e.resolverData, nil)
}
