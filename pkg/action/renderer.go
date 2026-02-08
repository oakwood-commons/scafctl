// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/spec"
	"gopkg.in/yaml.v3"
)

const (
	// APIVersion is the version of the rendered graph format.
	APIVersion = "scafctl.oakwood-commons.github.io/v1alpha1"

	// KindActionGraph is the kind for rendered action graphs.
	KindActionGraph = "ActionGraph"

	// FormatJSON indicates JSON output format.
	FormatJSON = "json"

	// FormatYAML indicates YAML output format.
	FormatYAML = "yaml"
)

// RenderedGraph is the executor-ready action graph output structure.
// This is the serializable form that can be consumed by external executors.
type RenderedGraph struct {
	// APIVersion identifies the schema version.
	APIVersion string `json:"apiVersion" yaml:"apiVersion" doc:"Schema version" example:"scafctl.oakwood-commons.github.io/v1alpha1"`

	// Kind identifies the resource type.
	Kind string `json:"kind" yaml:"kind" doc:"Resource kind" example:"ActionGraph"`

	// Metadata contains graph-level metadata.
	Metadata *RenderedMetadata `json:"metadata,omitempty" yaml:"metadata,omitempty" doc:"Graph metadata"`

	// ExecutionOrder contains phases of action names that can run in parallel.
	ExecutionOrder [][]string `json:"executionOrder" yaml:"executionOrder" doc:"Parallel execution phases for main actions"`

	// FinallyOrder contains phases for the finally section.
	FinallyOrder [][]string `json:"finallyOrder,omitempty" yaml:"finallyOrder,omitempty" doc:"Parallel execution phases for finally actions"`

	// Actions is a map of all rendered actions keyed by their name.
	Actions map[string]*RenderedAction `json:"actions" yaml:"actions" doc:"All actions in the graph"`
}

// RenderedMetadata contains graph-level metadata.
type RenderedMetadata struct {
	// GeneratedAt is the timestamp when the graph was rendered.
	GeneratedAt string `json:"generatedAt,omitempty" yaml:"generatedAt,omitempty" doc:"Render timestamp" example:"2026-01-29T12:00:00Z"`

	// TotalActions is the total number of actions in the graph.
	TotalActions int `json:"totalActions" yaml:"totalActions" doc:"Total action count" example:"5"`

	// TotalPhases is the total number of execution phases (main + finally).
	TotalPhases int `json:"totalPhases" yaml:"totalPhases" doc:"Total phase count" example:"3"`

	// HasFinally indicates if the graph has finally actions.
	HasFinally bool `json:"hasFinally" yaml:"hasFinally" doc:"Whether finally section exists"`

	// ForEachExpansions maps original action names to their expanded names.
	ForEachExpansions map[string][]string `json:"forEachExpansions,omitempty" yaml:"forEachExpansions,omitempty" doc:"ForEach expansion mapping"`
}

// RenderedAction is a fully rendered action ready for execution.
type RenderedAction struct {
	// Name is the action name (may be expanded with index for forEach).
	Name string `json:"name" yaml:"name" doc:"Action name" maxLength:"150" example:"deploy[0]"`

	// OriginalName is the action name before forEach expansion (if applicable).
	OriginalName string `json:"originalName,omitempty" yaml:"originalName,omitempty" doc:"Original action name before forEach expansion" maxLength:"100"`

	// Description provides documentation for the action.
	Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"Human-readable description" maxLength:"500"`

	// DisplayName is a human-friendly name for UI display.
	DisplayName string `json:"displayName,omitempty" yaml:"displayName,omitempty" doc:"Display name for UI" maxLength:"100"`

	// Sensitive indicates the action handles sensitive data.
	Sensitive bool `json:"sensitive,omitempty" yaml:"sensitive,omitempty" doc:"If true, mask in logs"`

	// Provider specifies which action provider to use.
	Provider string `json:"provider" yaml:"provider" doc:"Provider name" maxLength:"100" example:"shell"`

	// DependsOn lists action names that must complete first.
	DependsOn []string `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty" doc:"Dependency list" maxItems:"100"`

	// When contains the condition for execution.
	// Can be a boolean (already evaluated) or a DeferredValue (requires runtime evaluation).
	When any `json:"when,omitempty" yaml:"when,omitempty" doc:"Execution condition (bool or deferred)"`

	// OnError defines behavior when this action fails.
	OnError string `json:"onError,omitempty" yaml:"onError,omitempty" doc:"Error handling behavior" example:"fail"`

	// Timeout is the maximum execution duration as a string.
	Timeout string `json:"timeout,omitempty" yaml:"timeout,omitempty" doc:"Max duration" example:"30s"`

	// Retry contains retry configuration.
	Retry *RenderedRetryConfig `json:"retry,omitempty" yaml:"retry,omitempty" doc:"Retry settings"`

	// Inputs contains the action inputs.
	// Values are either concrete (materialized) or DeferredValue structs.
	Inputs map[string]any `json:"inputs" yaml:"inputs" doc:"Action inputs (may contain deferred values)"`

	// Section indicates which workflow section this action belongs to.
	Section string `json:"section" yaml:"section" doc:"Workflow section" example:"actions"`

	// ForEach contains forEach metadata if this is an expanded iteration.
	ForEach *RenderedForEachMetadata `json:"forEach,omitempty" yaml:"forEach,omitempty" doc:"ForEach expansion info"`
}

// RenderedRetryConfig is the serializable retry configuration.
type RenderedRetryConfig struct {
	// MaxAttempts is the total number of execution attempts.
	MaxAttempts int `json:"maxAttempts" yaml:"maxAttempts" doc:"Total attempts" minimum:"1" example:"3"`

	// Backoff defines the delay strategy between retries.
	Backoff string `json:"backoff,omitempty" yaml:"backoff,omitempty" doc:"Backoff strategy" example:"exponential"`

	// InitialDelay is the delay before the first retry.
	InitialDelay string `json:"initialDelay,omitempty" yaml:"initialDelay,omitempty" doc:"Initial delay" example:"1s"`

	// MaxDelay caps the maximum delay between retries.
	MaxDelay string `json:"maxDelay,omitempty" yaml:"maxDelay,omitempty" doc:"Max delay cap" example:"30s"`
}

// RenderedForEachMetadata tracks forEach expansion information in rendered output.
type RenderedForEachMetadata struct {
	// ExpandedFrom is the original action name before expansion.
	ExpandedFrom string `json:"expandedFrom" yaml:"expandedFrom" doc:"Original action name" example:"deploy"`

	// Index is the iteration index (0-based).
	Index int `json:"index" yaml:"index" doc:"Iteration index" example:"0"`

	// Item is the current iteration item value.
	Item any `json:"item,omitempty" yaml:"item,omitempty" doc:"Iteration item value"`

	// Concurrency is the concurrency limit from the original forEach clause.
	Concurrency int `json:"concurrency,omitempty" yaml:"concurrency,omitempty" doc:"Concurrency limit (0=unlimited)"`

	// OnError is the error handling behavior for forEach iterations.
	OnError string `json:"onError,omitempty" yaml:"onError,omitempty" doc:"ForEach error handling" example:"fail"`
}

// RenderOptions configures rendering behavior.
type RenderOptions struct {
	// Format specifies the output format: "json" (default) or "yaml".
	Format string

	// IncludeTimestamp adds a generation timestamp to metadata.
	IncludeTimestamp bool

	// PrettyPrint enables indented output for readability.
	PrettyPrint bool
}

// DefaultRenderOptions returns the default rendering options.
func DefaultRenderOptions() *RenderOptions {
	return &RenderOptions{
		Format:           FormatJSON,
		IncludeTimestamp: true,
		PrettyPrint:      true,
	}
}

// Render produces an executor-ready action graph artifact from the built graph.
// The output can be serialized to JSON or YAML based on options.
func Render(graph *Graph, opts *RenderOptions) ([]byte, error) {
	if graph == nil {
		return nil, fmt.Errorf("graph cannot be nil")
	}

	if opts == nil {
		opts = DefaultRenderOptions()
	}

	rendered := renderGraph(graph, opts)

	return serialize(rendered, opts)
}

// RenderToStruct produces a RenderedGraph struct without serializing to bytes.
// This is useful for programmatic access to the rendered graph.
func RenderToStruct(graph *Graph, opts *RenderOptions) (*RenderedGraph, error) {
	if graph == nil {
		return nil, fmt.Errorf("graph cannot be nil")
	}

	if opts == nil {
		opts = DefaultRenderOptions()
	}

	return renderGraph(graph, opts), nil
}

// renderGraph converts the internal Graph to a RenderedGraph.
func renderGraph(graph *Graph, opts *RenderOptions) *RenderedGraph {
	rendered := &RenderedGraph{
		APIVersion:     APIVersion,
		Kind:           KindActionGraph,
		ExecutionOrder: graph.ExecutionOrder,
		FinallyOrder:   graph.FinallyOrder,
		Actions:        make(map[string]*RenderedAction, len(graph.Actions)),
		Metadata:       buildMetadata(graph, opts),
	}

	// Render all actions
	for name, action := range graph.Actions {
		rendered.Actions[name] = renderAction(action)
	}

	return rendered
}

// buildMetadata creates the graph metadata.
func buildMetadata(graph *Graph, opts *RenderOptions) *RenderedMetadata {
	metadata := &RenderedMetadata{
		TotalActions: len(graph.Actions),
		TotalPhases:  len(graph.ExecutionOrder) + len(graph.FinallyOrder),
		HasFinally:   len(graph.FinallyOrder) > 0,
	}

	if opts.IncludeTimestamp {
		metadata.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	}

	// Build forEach expansions map
	expansions := make(map[string][]string)
	for name, action := range graph.Actions {
		if action != nil && action.ForEachMetadata != nil {
			original := action.ForEachMetadata.ExpandedFrom
			expansions[original] = append(expansions[original], name)
		}
	}

	// Sort expansion names for deterministic output
	for original := range expansions {
		sort.Strings(expansions[original])
	}

	if len(expansions) > 0 {
		metadata.ForEachExpansions = expansions
	}

	return metadata
}

// renderAction converts an ExpandedAction to a RenderedAction.
func renderAction(action *ExpandedAction) *RenderedAction {
	if action == nil || action.Action == nil {
		return nil
	}

	rendered := &RenderedAction{
		Name:        action.ExpandedName,
		Description: action.Description,
		DisplayName: action.DisplayName,
		Sensitive:   action.Sensitive,
		Provider:    action.Provider,
		DependsOn:   action.Dependencies,
		Section:     action.Section,
		Inputs:      buildRenderedInputs(action),
	}

	// Set original name if this is a forEach iteration
	if action.ForEachMetadata != nil {
		rendered.OriginalName = action.ForEachMetadata.ExpandedFrom
	}

	// Render condition
	if action.When != nil {
		rendered.When = renderCondition(action.When)
	}

	// Render onError
	if action.OnError != "" {
		rendered.OnError = string(action.OnError)
	}

	// Render timeout
	if action.Timeout != nil {
		rendered.Timeout = action.Timeout.String()
	}

	// Render retry config
	if action.Retry != nil {
		rendered.Retry = renderRetryConfig(action.Retry)
	}

	// Render forEach metadata
	if action.ForEachMetadata != nil {
		rendered.ForEach = renderForEachMetadata(action)
	}

	return rendered
}

// buildRenderedInputs merges materialized and deferred inputs for rendering.
func buildRenderedInputs(action *ExpandedAction) map[string]any {
	inputs := make(map[string]any)

	// Add materialized inputs
	for name, value := range action.MaterializedInputs {
		inputs[name] = value
	}

	// Add deferred inputs (they will serialize with their deferred structure)
	for name, value := range action.DeferredInputs {
		inputs[name] = value
	}

	return inputs
}

// renderCondition renders the When condition.
// If the condition only references resolver data, it could be pre-evaluated,
// but we preserve the expression for executor flexibility.
func renderCondition(condition *spec.Condition) any {
	if condition == nil || condition.Expr == nil {
		return nil
	}

	// Return a DeferredValue for the condition expression.
	// Conditions typically reference __actions or resolver data,
	// so we preserve them as deferred for the executor to evaluate.
	return &DeferredValue{
		OriginalExpr: string(*condition.Expr),
		Deferred:     true,
	}
}

// renderRetryConfig converts RetryConfig to RenderedRetryConfig.
func renderRetryConfig(retry *RetryConfig) *RenderedRetryConfig {
	if retry == nil {
		return nil
	}

	rendered := &RenderedRetryConfig{
		MaxAttempts: retry.MaxAttempts,
	}

	if retry.Backoff != "" {
		rendered.Backoff = string(retry.Backoff)
	}

	if retry.InitialDelay != nil {
		rendered.InitialDelay = retry.InitialDelay.String()
	}

	if retry.MaxDelay != nil {
		rendered.MaxDelay = retry.MaxDelay.String()
	}

	return rendered
}

// renderForEachMetadata converts ForEachExpansionMetadata to RenderedForEachMetadata.
func renderForEachMetadata(action *ExpandedAction) *RenderedForEachMetadata {
	if action.ForEachMetadata == nil {
		return nil
	}

	rendered := &RenderedForEachMetadata{
		ExpandedFrom: action.ForEachMetadata.ExpandedFrom,
		Index:        action.ForEachMetadata.Index,
		Item:         action.ForEachMetadata.Item,
	}

	// Include forEach clause settings if available from the original action
	if action.ForEach != nil {
		rendered.Concurrency = action.ForEach.Concurrency
		if action.ForEach.OnError != "" {
			rendered.OnError = string(action.ForEach.OnError)
		}
	}

	return rendered
}

// serialize converts the RenderedGraph to bytes in the specified format.
func serialize(rendered *RenderedGraph, opts *RenderOptions) ([]byte, error) {
	format := opts.Format
	if format == "" {
		format = FormatJSON
	}

	switch format {
	case FormatJSON:
		if opts.PrettyPrint {
			return json.MarshalIndent(rendered, "", "  ")
		}
		return json.Marshal(rendered)

	case FormatYAML:
		// sigs.k8s.io/yaml marshals to YAML via JSON, handles struct tags correctly
		return yaml.Marshal(rendered)

	default:
		return nil, fmt.Errorf("unsupported output format: %q (supported: json, yaml)", format)
	}
}

// GetFormat normalizes and validates the output format string.
func GetFormat(format string) (string, error) {
	switch format {
	case "", FormatJSON, "JSON":
		return FormatJSON, nil
	case FormatYAML, "YAML", "yml", "YML":
		return FormatYAML, nil
	default:
		return "", fmt.Errorf("unsupported format: %q (supported: json, yaml)", format)
	}
}
