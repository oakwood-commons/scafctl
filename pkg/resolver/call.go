// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"context"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/call"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// runStepProvider dispatches a non-iterating step to either an expanded call or
// a direct provider, depending on whether the step invokes a call definition.
func (e *Executor) runStepProvider(ctx context.Context, ref spec.CallRef, providerName string, inputs map[string]*ValueRef, self any) (any, error) {
	if ref.HasCall() {
		return e.expandAndRunCall(ctx, ref, self, nil)
	}
	return e.executeProviderWithSelf(ctx, providerName, inputs, self)
}

// runStepProviderIter dispatches an iterating (forEach) step to either an
// expanded call or a direct provider, threading the iteration context.
func (e *Executor) runStepProviderIter(ctx context.Context, ref spec.CallRef, providerName string, inputs map[string]*ValueRef, self any, iterCtx *IterationContext) (any, error) {
	if ref.HasCall() {
		return e.expandAndRunCall(ctx, ref, self, iterCtx)
	}
	return e.executeProviderWithIterationContext(ctx, providerName, inputs, self, iterCtx)
}

// expandAndRunCall resolves a call site's arguments, binds them against the
// referenced definition, and runs the definition's provider with the arguments
// exposed under the args namespace. When iterCtx is non-nil, arguments and the
// definition inputs are resolved with forEach iteration variables available.
func (e *Executor) expandAndRunCall(ctx context.Context, ref spec.CallRef, self any, iterCtx *IterationContext) (any, error) {
	def, ok := e.calls[ref.Call]
	if !ok || def == nil {
		return nil, fmt.Errorf("call %q not found", ref.Call)
	}

	resolverCtx, _ := FromContext(ctx)
	resolverData := resolverCtx.ToMap()

	// Resolve each call-site argument in the caller's context.
	resolvedArgs := make(map[string]any, len(ref.Args))
	for name, vr := range ref.Args {
		if vr == nil {
			return nil, fmt.Errorf("call %q: argument %q has no value (nil)", ref.Call, name)
		}
		var (
			v   any
			err error
		)
		if iterCtx != nil {
			v, err = vr.ResolveWithIterationContext(ctx, resolverData, self, iterCtx)
		} else {
			v, err = vr.Resolve(ctx, resolverData, self)
		}
		if err != nil {
			return nil, fmt.Errorf("call %q: failed to resolve argument %q: %w", ref.Call, name, err)
		}
		resolvedArgs[name] = v
	}

	bound, err := call.BindArgs(ref.Call, def, resolvedArgs)
	if err != nil {
		return nil, err
	}

	run := func() (any, error) {
		return e.runCallProvider(ctx, def, bound, self, iterCtx, resolverData)
	}

	// Opt-in de-duplication: identical bound args within a single run reuse the
	// first result instead of re-invoking the provider.
	if def.Dedup && e.dedupMemo != nil {
		key, keyErr := call.DedupKey(ref.Call, bound)
		if keyErr == nil {
			return e.dedupMemo.Do(key, run)
		}
	}
	return run()
}

// runCallProvider expands the resolver data with the bound arguments and runs
// the call definition's provider.
func (e *Executor) runCallProvider(ctx context.Context, def *spec.Call, bound map[string]any, self any, iterCtx *IterationContext, resolverData map[string]any) (any, error) {
	enriched := call.ExpandData(resolverData, bound)
	if iterCtx != nil {
		return e.executeProviderWithDataAndIteration(ctx, def.Provider, def.Inputs, self, iterCtx, enriched)
	}
	return e.executeProviderWithData(ctx, def.Provider, def.Inputs, self, enriched)
}

// executeProviderWithData mirrors executeProviderWithSelf but resolves inputs
// against an explicitly enriched data map (carrying the args namespace) instead
// of the resolver context's map.
func (e *Executor) executeProviderWithData(ctx context.Context, providerName string, inputRefs map[string]*ValueRef, self any, enrichedData map[string]any) (any, error) {
	prov, ok := e.registry.Get(providerName)
	if !ok {
		return nil, fmt.Errorf("provider %q not found", providerName)
	}

	inputs := make(map[string]any)
	for key, valueRef := range inputRefs {
		if valueRef == nil {
			return nil, fmt.Errorf("input %q has no value (nil); check for dangling YAML keys with no value", key)
		}
		resolved, err := valueRef.Resolve(ctx, enrichedData, self)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve input %q: %w", key, err)
		}
		inputs[key] = resolved
	}

	if self != nil {
		enrichedData["__self"] = self
	}
	ctxWithResolvers := provider.WithResolverContext(ctx, enrichedData)

	if err := provider.ValidateWriteOperation(ctxWithResolvers, prov, inputs); err != nil {
		return nil, err
	}

	output, err := prov.Execute(ctxWithResolvers, inputs)
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, fmt.Errorf("provider returned nil output")
	}
	return output.Data, nil
}

// executeProviderWithDataAndIteration mirrors executeProviderWithIterationContext
// but resolves inputs against an explicitly enriched data map (carrying the args
// namespace).
func (e *Executor) executeProviderWithDataAndIteration(ctx context.Context, providerName string, inputRefs map[string]*ValueRef, self any, iterCtx *IterationContext, enrichedData map[string]any) (any, error) {
	prov, ok := e.registry.Get(providerName)
	if !ok {
		return nil, fmt.Errorf("provider %q not found", providerName)
	}

	inputs := make(map[string]any)
	for key, valueRef := range inputRefs {
		if valueRef == nil {
			return nil, fmt.Errorf("input %q has no value (nil); check for dangling YAML keys with no value", key)
		}
		resolved, err := valueRef.ResolveWithIterationContext(ctx, enrichedData, self, iterCtx)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve input %q: %w", key, err)
		}
		inputs[key] = resolved
	}

	enrichedData["__self"] = self
	enrichedData["__item"] = iterCtx.Item
	enrichedData["__index"] = iterCtx.Index
	if iterCtx.ItemAlias != "" {
		enrichedData[iterCtx.ItemAlias] = iterCtx.Item
	}
	if iterCtx.IndexAlias != "" {
		enrichedData[iterCtx.IndexAlias] = iterCtx.Index
	}

	ctxWithResolvers := provider.WithResolverContext(ctx, enrichedData)
	if err := provider.ValidateWriteOperation(ctxWithResolvers, prov, inputs); err != nil {
		return nil, err
	}

	provIterCtx := &provider.IterationContext{
		Item:       iterCtx.Item,
		Index:      iterCtx.Index,
		ItemAlias:  iterCtx.ItemAlias,
		IndexAlias: iterCtx.IndexAlias,
	}
	ctxWithResolvers = provider.WithIterationContext(ctxWithResolvers, provIterCtx)

	output, err := prov.Execute(ctxWithResolvers, inputs)
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, fmt.Errorf("provider returned nil output")
	}
	return output.Data, nil
}
