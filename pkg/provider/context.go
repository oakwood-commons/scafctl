package provider

import "context"

// Context keys for provider execution control (unexported for safety).
type contextKey string

const (
	executionModeKey   contextKey = "scafctl.provider.executionMode"
	dryRunKey          contextKey = "scafctl.provider.dryRun"
	resolverContextKey contextKey = "scafctl.provider.resolverContext"
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
