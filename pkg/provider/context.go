// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import "context"

// Context keys for provider execution control (unexported for safety).
type contextKey string

const (
	executionModeKey    contextKey = "scafctl.provider.executionMode"
	dryRunKey           contextKey = "scafctl.provider.dryRun"
	resolverContextKey  contextKey = "scafctl.provider.resolverContext"
	parametersKey       contextKey = "scafctl.provider.parameters"
	iterationContextKey contextKey = "scafctl.provider.iterationContext"
)

// WithExecutionMode returns a new context with the specified execution mode (capability).
func WithExecutionMode(ctx context.Context, mode Capability) context.Context {
	return context.WithValue(ctx, executionModeKey, mode)
}

// ExecutionModeFromContext retrieves the execution mode from the context.
func ExecutionModeFromContext(ctx context.Context) (Capability, bool) {
	mode, ok := ctx.Value(executionModeKey).(Capability)
	return mode, ok
}

// WithDryRun returns a new context with the dry-run flag set.
func WithDryRun(ctx context.Context, dryRun bool) context.Context {
	return context.WithValue(ctx, dryRunKey, dryRun)
}

// DryRunFromContext retrieves the dry-run flag from the context.
// Defaults to false if not set.
func DryRunFromContext(ctx context.Context) bool {
	dryRun, ok := ctx.Value(dryRunKey).(bool)
	if !ok {
		return false
	}
	return dryRun
}

// WithResolverContext returns a new context with the resolver context map.
func WithResolverContext(ctx context.Context, resolverContext map[string]any) context.Context {
	return context.WithValue(ctx, resolverContextKey, resolverContext)
}

// ResolverContextFromContext retrieves the resolver context map from the context.
func ResolverContextFromContext(ctx context.Context) (map[string]any, bool) {
	resolverCtx, ok := ctx.Value(resolverContextKey).(map[string]any)
	return resolverCtx, ok
}

// WithParameters returns a new context with the CLI parameters map.
// Parameters are parsed from -r/--resolver flags and stored for retrieval by the parameter provider.
func WithParameters(ctx context.Context, parameters map[string]any) context.Context {
	return context.WithValue(ctx, parametersKey, parameters)
}

// ParametersFromContext retrieves the CLI parameters map from the context.
// Returns the parameters map and true if found, nil and false otherwise.
func ParametersFromContext(ctx context.Context) (map[string]any, bool) {
	params, ok := ctx.Value(parametersKey).(map[string]any)
	return params, ok
}

// IterationContext holds information about the current forEach iteration.
// This is passed to providers to enable them to access iteration variables as top-level CEL variables.
type IterationContext struct {
	// Item is the current element being iterated over.
	Item any `json:"item" yaml:"item" doc:"Current element in the iteration."`
	// Index is the current index in the iteration.
	Index int `json:"index" yaml:"index" doc:"Current zero-based index in the iteration."`
	// ItemAlias is the custom variable name for the current item (empty if using default __item).
	ItemAlias string `json:"itemAlias,omitempty" yaml:"itemAlias,omitempty" doc:"Custom variable name for current item."`
	// IndexAlias is the custom variable name for the current index (empty if using default __index).
	IndexAlias string `json:"indexAlias,omitempty" yaml:"indexAlias,omitempty" doc:"Custom variable name for current index."`
}

// WithIterationContext returns a new context with the iteration context.
func WithIterationContext(ctx context.Context, iterCtx *IterationContext) context.Context {
	return context.WithValue(ctx, iterationContextKey, iterCtx)
}

// IterationContextFromContext retrieves the iteration context from the context.
// Returns the iteration context and true if found, nil and false otherwise.
func IterationContextFromContext(ctx context.Context) (*IterationContext, bool) {
	iterCtx, ok := ctx.Value(iterationContextKey).(*IterationContext)
	return iterCtx, ok
}
