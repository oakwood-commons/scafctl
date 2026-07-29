// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"encoding/json"
	"text/template"

	sdkprovider "github.com/oakwood-commons/scafctl-plugin-sdk/provider"
)

// --- SDK type aliases ---

// SolutionMeta holds solution metadata fields made available to providers via context.
type SolutionMeta = sdkprovider.SolutionMeta

// IOStreams holds terminal IO writers for providers that support streaming output.
type IOStreams = sdkprovider.IOStreams

// IterationContext holds information about the current forEach iteration.
type IterationContext = sdkprovider.IterationContext

// --- SDK function wrappers ---

// WithSolutionMetadata returns a new context with the solution metadata attached.
func WithSolutionMetadata(ctx context.Context, meta *SolutionMeta) context.Context {
	return sdkprovider.WithSolutionMetadata(ctx, meta)
}

// SolutionMetadataFromContext retrieves the solution metadata from the context.
func SolutionMetadataFromContext(ctx context.Context) (*SolutionMeta, bool) {
	return sdkprovider.SolutionMetadataFromContext(ctx)
}

// WithOutputDirectory returns a new context with the output directory path attached.
func WithOutputDirectory(ctx context.Context, dir string) context.Context {
	return sdkprovider.WithOutputDirectory(ctx, dir)
}

// OutputDirectoryFromContext retrieves the output directory from the context.
func OutputDirectoryFromContext(ctx context.Context) (string, bool) {
	return sdkprovider.OutputDirectoryFromContext(ctx)
}

// WithWorkingDirectory returns a new context with the logical working directory attached.
func WithWorkingDirectory(ctx context.Context, dir string) context.Context {
	return sdkprovider.WithWorkingDirectory(ctx, dir)
}

// WorkingDirectoryFromContext retrieves the logical working directory from the context.
func WorkingDirectoryFromContext(ctx context.Context) (string, bool) {
	return sdkprovider.WorkingDirectoryFromContext(ctx)
}

// WithIOStreams returns a new context with IO streams for provider terminal output.
func WithIOStreams(ctx context.Context, streams *IOStreams) context.Context {
	return sdkprovider.WithIOStreams(ctx, streams)
}

// IOStreamsFromContext retrieves the IO streams from the context.
func IOStreamsFromContext(ctx context.Context) (*IOStreams, bool) {
	return sdkprovider.IOStreamsFromContext(ctx)
}

// WithExecutionMode returns a new context with the specified execution mode (capability).
func WithExecutionMode(ctx context.Context, mode Capability) context.Context {
	return sdkprovider.WithExecutionMode(ctx, mode)
}

// ExecutionModeFromContext retrieves the execution mode from the context.
func ExecutionModeFromContext(ctx context.Context) (Capability, bool) {
	return sdkprovider.ExecutionModeFromContext(ctx)
}

// WithDryRun returns a new context with the dry-run flag set.
func WithDryRun(ctx context.Context, dryRun bool) context.Context {
	return sdkprovider.WithDryRun(ctx, dryRun)
}

// DryRunFromContext retrieves the dry-run flag from the context. Defaults to false if not set.
func DryRunFromContext(ctx context.Context) bool {
	return sdkprovider.DryRunFromContext(ctx)
}

// WithResolverContext returns a new context with the resolver context map.
func WithResolverContext(ctx context.Context, resolverContext map[string]any) context.Context {
	return sdkprovider.WithResolverContext(ctx, resolverContext)
}

// ResolverContextFromContext retrieves the resolver context map from the context.
func ResolverContextFromContext(ctx context.Context) (map[string]any, bool) {
	return sdkprovider.ResolverContextFromContext(ctx)
}

// WithParameters returns a new context with the CLI parameters map.
func WithParameters(ctx context.Context, parameters map[string]any) context.Context {
	return sdkprovider.WithParameters(ctx, parameters)
}

// ParametersFromContext retrieves the CLI parameters map from the context.
func ParametersFromContext(ctx context.Context) (map[string]any, bool) {
	return sdkprovider.ParametersFromContext(ctx)
}

// WithConflictStrategy returns a new context with the conflict strategy attached.
func WithConflictStrategy(ctx context.Context, strategy string) context.Context {
	return sdkprovider.WithConflictStrategy(ctx, strategy)
}

// ConflictStrategyFromContext retrieves the conflict strategy from the context.
func ConflictStrategyFromContext(ctx context.Context) (string, bool) {
	return sdkprovider.ConflictStrategyFromContext(ctx)
}

// WithBackup returns a new context with the backup flag attached.
func WithBackup(ctx context.Context, backup bool) context.Context {
	return sdkprovider.WithBackup(ctx, backup)
}

// BackupFromContext retrieves the backup flag from the context.
func BackupFromContext(ctx context.Context) (bool, bool) {
	return sdkprovider.BackupFromContext(ctx)
}

// WithIterationContext returns a new context with the iteration context.
func WithIterationContext(ctx context.Context, iterCtx *IterationContext) context.Context {
	return sdkprovider.WithIterationContext(ctx, iterCtx)
}

// IterationContextFromContext retrieves the iteration context from the context.
func IterationContextFromContext(ctx context.Context) (*IterationContext, bool) {
	return sdkprovider.IterationContextFromContext(ctx)
}

// --- scafctl-only context functions ---

// solutionDirectoryKey is a scafctl-only context key for solution directory.
type scafctlContextKey string

const solutionDirectoryKey scafctlContextKey = "scafctl.provider.solutionDirectory"

// WithSolutionDirectory returns a new context with the solution file's parent directory.
// When set, resolver-phase path resolution uses this as the base for relative paths
// instead of the process CWD, ensuring consistent behaviour between local and bundled execution.
func WithSolutionDirectory(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, solutionDirectoryKey, dir)
}

// SolutionDirectoryFromContext retrieves the solution file's parent directory.
// Returns the directory path and true if found, empty string and false otherwise.
func SolutionDirectoryFromContext(ctx context.Context) (string, bool) {
	dir, ok := ctx.Value(solutionDirectoryKey).(string)
	return dir, ok
}

// executionSettingsKey is a scafctl-only context key carrying the per-execution
// host settings that the host serializes into ExecuteProviderRequest.settings.
const executionSettingsKey scafctlContextKey = "scafctl.provider.executionSettings"

// WithExecutionSettings returns a new context carrying the per-execution host
// settings map that the host serializes into ExecuteProviderRequest.settings on
// each provider execution. This is the host-side (producer) counterpart to the
// SDK's provider.WithSettings, which the plugin server uses to expose the merged
// effective settings to the plugin (consumer). Pooled hosts use this channel to
// deliver per-solution values that the one-time ConfigureProvider call cannot.
// The map and its values are treated as read-only.
func WithExecutionSettings(ctx context.Context, settings map[string]json.RawMessage) context.Context {
	return context.WithValue(ctx, executionSettingsKey, settings)
}

// ExecutionSettingsFromContext retrieves the per-execution host settings map.
// Returns the settings map and true if present, nil and false otherwise.
func ExecutionSettingsFromContext(ctx context.Context) (map[string]json.RawMessage, bool) {
	settings, ok := ctx.Value(executionSettingsKey).(map[string]json.RawMessage)
	return settings, ok
}

// TemplateFuncBinder supplies solution-author-defined Go template helper
// functions to the go-template provider. It is implemented by the authorfuncs
// library compiled from a solution's spec.functions. Keeping the provider layer
// dependent only on this interface (rather than the concrete library) avoids an
// import cycle between the provider/executor packages and the higher-level
// solution/authorfuncs packages.
type TemplateFuncBinder interface {
	// Bind returns the author-defined functions as a template.FuncMap whose
	// closures capture ctx (for CEL evaluation and nested rendering). It returns
	// nil when there are no functions to bind.
	Bind(ctx context.Context) template.FuncMap

	// Fingerprint returns a stable identifier for the function *implementations*
	// so template caches can distinguish same-named functions with different
	// bodies. It returns an empty string when there are no functions.
	Fingerprint() string
}

// templateFuncBinderKey is a scafctl-only context key carrying the author
// function binder into provider execution.
const templateFuncBinderKey scafctlContextKey = "scafctl.provider.templateFuncBinder"

// WithTemplateFuncBinder returns a new context carrying the author-defined
// template function binder. A nil binder is not attached, so downstream lookups
// behave as if no functions were declared.
func WithTemplateFuncBinder(ctx context.Context, binder TemplateFuncBinder) context.Context {
	if binder == nil {
		return ctx
	}
	return context.WithValue(ctx, templateFuncBinderKey, binder)
}

// TemplateFuncBinderFromContext retrieves the author-defined template function
// binder. Returns the binder and true if present, nil and false otherwise.
func TemplateFuncBinderFromContext(ctx context.Context) (TemplateFuncBinder, bool) {
	binder, ok := ctx.Value(templateFuncBinderKey).(TemplateFuncBinder)
	return binder, ok
}
