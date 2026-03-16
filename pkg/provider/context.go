// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"io"
)

// Context keys for provider execution control (unexported for safety).
type contextKey string

const (
	executionModeKey    contextKey = "scafctl.provider.executionMode"
	dryRunKey           contextKey = "scafctl.provider.dryRun"
	resolverContextKey  contextKey = "scafctl.provider.resolverContext"
	parametersKey       contextKey = "scafctl.provider.parameters"
	iterationContextKey contextKey = "scafctl.provider.iterationContext"
	ioStreamsKey        contextKey = "scafctl.provider.ioStreams"
	solutionMetadataKey contextKey = "scafctl.provider.solutionMetadata"
	outputDirectoryKey  contextKey = "scafctl.provider.outputDirectory"
	workingDirectoryKey contextKey = "scafctl.provider.workingDirectory"

	conflictStrategyKey contextKey = "scafctl.provider.conflictStrategy"
	backupKey           contextKey = "scafctl.provider.backup"
)

// SolutionMeta holds solution metadata fields made available to providers via context.
// This is a provider-package type to avoid circular imports with pkg/solution.
type SolutionMeta struct {
	// Name is the unique identifier for the solution.
	Name string `json:"name" yaml:"name" doc:"The unique name of the solution" maxLength:"256" example:"my-solution"`
	// Version is the semantic version string of the solution.
	Version string `json:"version" yaml:"version" doc:"The version of the solution" maxLength:"64" example:"1.0.0"`
	// DisplayName is the human-readable name of the solution.
	DisplayName string `json:"displayName,omitempty" yaml:"displayName,omitempty" doc:"The display name of the solution" maxLength:"256" example:"My Solution"`
	// Description provides details about the solution's purpose.
	Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"The description of the solution" maxLength:"2048" example:"Deploys and configures services"`
	// Category classifies the solution.
	Category string `json:"category,omitempty" yaml:"category,omitempty" doc:"The category of the solution" maxLength:"128" example:"infrastructure"`
	// Tags are searchable keywords associated with the solution.
	Tags []string `json:"tags,omitempty" yaml:"tags,omitempty" doc:"A list of tags for the solution" maxItems:"50"`
}

// WithSolutionMetadata returns a new context with the solution metadata attached.
func WithSolutionMetadata(ctx context.Context, meta *SolutionMeta) context.Context {
	return context.WithValue(ctx, solutionMetadataKey, meta)
}

// SolutionMetadataFromContext retrieves the solution metadata from the context.
// Returns the solution metadata and true if found, nil and false otherwise.
func SolutionMetadataFromContext(ctx context.Context) (*SolutionMeta, bool) {
	meta, ok := ctx.Value(solutionMetadataKey).(*SolutionMeta)
	return meta, ok
}

// WithOutputDirectory returns a new context with the output directory path attached.
// When set, providers executing in action mode resolve relative paths against this directory
// instead of the current working directory.
func WithOutputDirectory(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, outputDirectoryKey, dir)
}

// OutputDirectoryFromContext retrieves the output directory from the context.
// Returns the directory path and true if found, empty string and false otherwise.
func OutputDirectoryFromContext(ctx context.Context) (string, bool) {
	dir, ok := ctx.Value(outputDirectoryKey).(string)
	return dir, ok
}

// WithWorkingDirectory returns a new context with the logical working directory attached.
// When set, path resolution helpers use this directory instead of the process CWD
// (os.Getwd). This allows callers—such as the MCP server or a --cwd CLI flag—to
// control path resolution without mutating global process state.
func WithWorkingDirectory(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, workingDirectoryKey, dir)
}

// WorkingDirectoryFromContext retrieves the logical working directory from the context.
// Returns the directory path and true if found, empty string and false otherwise.
// When not set, callers should fall back to os.Getwd().
func WorkingDirectoryFromContext(ctx context.Context) (string, bool) {
	dir, ok := ctx.Value(workingDirectoryKey).(string)
	return dir, ok
}

// IOStreams holds terminal IO writers for providers that support streaming output.
// Providers can use these to write output directly to the terminal during execution,
// while still capturing data for inter-action dependencies.
type IOStreams struct {
	// Out is the writer for standard output (typically os.Stdout).
	Out io.Writer `json:"-" yaml:"-" doc:"Writer for standard output"`
	// ErrOut is the writer for standard error output (typically os.Stderr).
	ErrOut io.Writer `json:"-" yaml:"-" doc:"Writer for standard error output"`
}

// WithIOStreams returns a new context with IO streams for provider terminal output.
func WithIOStreams(ctx context.Context, streams *IOStreams) context.Context {
	return context.WithValue(ctx, ioStreamsKey, streams)
}

// IOStreamsFromContext retrieves the IO streams from the context.
// Returns the IO streams and true if found, nil and false otherwise.
// Providers should check this to determine if they can stream output to the terminal.
func IOStreamsFromContext(ctx context.Context) (*IOStreams, bool) {
	streams, ok := ctx.Value(ioStreamsKey).(*IOStreams)
	return streams, ok
}

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

// WithConflictStrategy returns a new context with the conflict strategy attached.
func WithConflictStrategy(ctx context.Context, strategy string) context.Context {
	return context.WithValue(ctx, conflictStrategyKey, strategy)
}

// ConflictStrategyFromContext retrieves the conflict strategy from the context.
// Returns the strategy string and true if found, empty string and false otherwise.
func ConflictStrategyFromContext(ctx context.Context) (string, bool) {
	s, ok := ctx.Value(conflictStrategyKey).(string)
	return s, ok
}

// WithBackup returns a new context with the backup flag attached.
func WithBackup(ctx context.Context, backup bool) context.Context {
	return context.WithValue(ctx, backupKey, backup)
}

// BackupFromContext retrieves the backup flag from the context.
// Returns the backup flag and true if found, false and false otherwise.
func BackupFromContext(ctx context.Context) (bool, bool) {
	b, ok := ctx.Value(backupKey).(bool)
	return b, ok
}

// IterationContext holds information about the current forEach iteration.
// This is passed to providers to enable them to access iteration variables as top-level CEL variables.
type IterationContext struct {
	// Item is the current element being iterated over.
	Item any `json:"item" yaml:"item" doc:"Current element in the iteration."`
	// Index is the current index in the iteration.
	Index int `json:"index" yaml:"index" doc:"Current zero-based index in the iteration." maximum:"10000" example:"0"`
	// ItemAlias is the custom variable name for the current item (empty if using default __item).
	ItemAlias string `json:"itemAlias,omitempty" yaml:"itemAlias,omitempty" doc:"Custom variable name for current item." maxLength:"128" example:"server"`
	// IndexAlias is the custom variable name for the current index (empty if using default __index).
	IndexAlias string `json:"indexAlias,omitempty" yaml:"indexAlias,omitempty" doc:"Custom variable name for current index." maxLength:"128" example:"i"`
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
