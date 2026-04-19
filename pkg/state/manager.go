// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// Manager orchestrates the state lifecycle: pre-execution loading, context
// injection, and post-execution saving. It is called by the CLI command
// layer before and after resolver execution.
type Manager struct {
	registry *provider.Registry
	config   *Config
	version  string // scafctl build version for metadata
}

// NewManager creates a state manager for the given state configuration.
func NewManager(config *Config, registry *provider.Registry, version string) *Manager {
	return &Manager{
		config:   config,
		registry: registry,
		version:  version,
	}
}

// LoadResult is returned by Load with the loaded state and enriched context.
type LoadResult struct {
	// Ctx is the context enriched with state.WithState.
	Ctx context.Context

	// Data is the loaded (or empty) state data.
	Data *Data

	// Skipped is true when state is disabled or the enabled ValueRef is falsy.
	Skipped bool
}

// Load executes the pre-execution state lifecycle:
//  1. Evaluates the enabled ValueRef using CLI params (available as __params
//     in CEL expressions). No resolver data is available at load time.
//  2. If disabled, returns LoadResult{Skipped: true}.
//  3. Resolves backend inputs with params as __params.
//  4. Calls the backend provider with operation=state_load.
//  5. Captures command info.
//  6. Injects state into context via WithState.
func (m *Manager) Load(ctx context.Context, params map[string]any, command CommandInfo) (*LoadResult, error) {
	if m.config == nil {
		return &LoadResult{Ctx: ctx, Skipped: true}, nil
	}

	// Evaluate enabled — no resolver data at load time, only CLI params
	enabled, err := m.evaluateEnabled(ctx, nil, params)
	if err != nil {
		return nil, fmt.Errorf("state: evaluate enabled: %w", err)
	}
	if !enabled {
		return &LoadResult{Ctx: ctx, Skipped: true}, nil
	}

	// Resolve backend inputs — no resolver data at load time, only CLI params
	backendInputs, err := m.resolveBackendInputs(ctx, nil, params)
	if err != nil {
		return nil, fmt.Errorf("state: resolve backend inputs: %w", err)
	}

	// Look up backend provider
	backendProvider, err := m.getBackendProvider()
	if err != nil {
		return nil, err
	}

	// Execute load
	backendInputs["operation"] = "state_load"
	execCtx := provider.WithExecutionMode(ctx, provider.CapabilityState)
	result, err := provider.Execute(execCtx, backendProvider, backendInputs)
	if err != nil {
		return nil, fmt.Errorf("state: backend load: %w", err)
	}

	// Extract state data from result
	stateData, err := extractStateData(result)
	if err != nil {
		return nil, fmt.Errorf("state: extract loaded data: %w", err)
	}

	// Capture command info
	stateData.Command = command

	// Inject into context
	enrichedCtx := WithState(ctx, stateData)

	return &LoadResult{
		Ctx:  enrichedCtx,
		Data: stateData,
	}, nil
}

// Save executes the post-execution state lifecycle:
//  1. Collects saveToState resolver results from resolverCtx.
//  2. Updates state data with collected values.
//  3. Updates metadata timestamps.
//  4. Calls the backend provider with operation=state_save.
//
// params are the original CLI parameters, available as __params in backend
// input CEL expressions. resolverData contains resolver outputs, available
// as _ in CEL expressions.
func (m *Manager) Save(ctx context.Context, stateData *Data, resolverCtx *resolver.Context, resolvers []*resolver.Resolver, params, resolverData map[string]any, solMeta SolutionMeta) error {
	if m.config == nil || stateData == nil {
		return nil
	}

	// Ensure maps are initialized before assigning entries
	if stateData.Values == nil {
		stateData.Values = make(map[string]*Entry)
	}

	// Collect saveToState values
	now := time.Now().UTC()
	for _, r := range resolvers {
		if !r.SaveToState {
			continue
		}
		result, ok := resolverCtx.GetResult(r.Name)
		if !ok || result.Status != resolver.ExecutionStatusSuccess {
			continue
		}

		stateData.Values[r.Name] = &Entry{
			Value:     result.Value,
			Type:      string(r.Type),
			UpdatedAt: now,
		}
	}

	// Update metadata
	if stateData.Metadata.CreatedAt.IsZero() {
		stateData.Metadata.CreatedAt = now
	}
	stateData.Metadata.LastUpdatedAt = now
	stateData.Metadata.Solution = solMeta.Name
	stateData.Metadata.Version = solMeta.Version
	stateData.Metadata.ScafctlVersion = m.version

	// Resolve backend inputs for save — resolver outputs are available as _
	// and CLI params as __params in backend input expressions.
	backendInputs, err := m.resolveBackendInputs(ctx, resolverData, params)
	if err != nil {
		return fmt.Errorf("state: resolve backend inputs for save: %w", err)
	}

	// Look up backend provider
	backendProvider, err := m.getBackendProvider()
	if err != nil {
		return err
	}

	// Execute save — convert stateData to map[string]any so that the provider
	// executor's JSON-schema validator can inspect the value (it cannot
	// validate Go structs directly).
	backendInputs["operation"] = "state_save"
	dataMap, err := structToMap(stateData)
	if err != nil {
		return fmt.Errorf("state: marshal state data: %w", err)
	}
	backendInputs["data"] = dataMap
	execCtx := provider.WithExecutionMode(ctx, provider.CapabilityState)
	if _, err := provider.Execute(execCtx, backendProvider, backendInputs); err != nil {
		return fmt.Errorf("state: backend save: %w", err)
	}

	return nil
}

// SolutionMeta contains solution identity for state metadata.
type SolutionMeta struct {
	Name    string
	Version string
}

// RequiredResolvers returns the resolver names that must be resolved before
// state loading (referenced by enabled and backend inputs ValueRefs).
func (m *Manager) RequiredResolvers() []string {
	if m.config == nil {
		return nil
	}

	seen := make(map[string]struct{})
	var names []string

	collect := func(vr *spec.ValueRef) {
		if vr == nil {
			return
		}
		// Direct resolver reference
		if vr.Resolver != nil {
			name := *vr.Resolver
			if _, ok := seen[name]; !ok {
				seen[name] = struct{}{}
				names = append(names, name)
			}
		}
		// CEL or template references
		for name := range vr.ReferencedVariables() {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}

	collect(m.config.Enabled)
	for _, input := range m.config.Backend.Inputs {
		collect(input)
	}

	return names
}

// evaluateEnabled resolves the enabled ValueRef and coerces to bool.
// CLI params are available as __params in CEL expressions.
func (m *Manager) evaluateEnabled(ctx context.Context, resolverData, params map[string]any) (bool, error) {
	if m.config.Enabled == nil {
		// No enabled field means enabled by default when state block is present
		return true, nil
	}

	val, err := resolveWithParams(ctx, m.config.Enabled, resolverData, params)
	if err != nil {
		return false, fmt.Errorf("resolve enabled: %w", err)
	}

	return isTruthy(val), nil
}

// resolveBackendInputs resolves all backend input ValueRefs.
// resolverData becomes _ in CEL; params becomes __params.
func (m *Manager) resolveBackendInputs(ctx context.Context, resolverData, params map[string]any) (map[string]any, error) {
	resolved := make(map[string]any, len(m.config.Backend.Inputs))

	for key, vr := range m.config.Backend.Inputs {
		if vr == nil {
			continue
		}
		val, err := resolveWithParams(ctx, vr, resolverData, params)
		if err != nil {
			return nil, fmt.Errorf("resolve input %q: %w", key, err)
		}
		resolved[key] = val
	}

	return resolved, nil
}

// resolveWithParams resolves a ValueRef with CLI params available as __params.
// resolverData is passed as the standard _ variable. For literal and resolver-ref
// ValueRefs, params are not used (they are only relevant for CEL and templates).
func resolveWithParams(ctx context.Context, vr *spec.ValueRef, resolverData, params map[string]any) (any, error) {
	if vr == nil {
		return nil, nil
	}

	// Literal and resolver references don't need params
	if vr.Literal != nil || vr.Resolver != nil {
		return vr.Resolve(ctx, resolverData, nil)
	}

	// CEL expression — inject __params as an additional variable
	if vr.Expr != nil {
		additionalVars := map[string]any{celexp.VarParams: params}
		result, err := celexp.EvaluateExpression(ctx, string(*vr.Expr), resolverData, additionalVars)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate expression: %w", err)
		}
		return result, nil
	}

	// Go template — add __params to template data
	if vr.Tmpl != nil {
		templateData := make(map[string]any, len(resolverData)+2)
		for k, val := range resolverData {
			templateData[k] = val
		}
		templateData[celexp.VarParams] = params
		result, err := gotmpl.Execute(ctx, gotmpl.TemplateOptions{
			Content:    string(*vr.Tmpl),
			Data:       templateData,
			MissingKey: gotmpl.MissingKeyError,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to execute template: %w", err)
		}
		return result.Output, nil
	}

	return nil, fmt.Errorf("empty value reference")
}

// getBackendProvider looks up the backend provider from the registry.
func (m *Manager) getBackendProvider() (provider.Provider, error) {
	name := m.config.Backend.Provider
	if name == "" {
		return nil, fmt.Errorf("state: backend provider name is empty")
	}

	prov, exists := m.registry.Get(name)
	if !exists {
		return nil, fmt.Errorf("state: backend provider %q not found in registry: %w", name, ErrInvalidBackend)
	}

	// Verify it has CapabilityState
	desc := prov.Descriptor()
	hasState := false
	for _, cap := range desc.Capabilities {
		if cap == provider.CapabilityState {
			hasState = true
			break
		}
	}
	if !hasState {
		return nil, fmt.Errorf("state: provider %q does not have CapabilityState: %w", name, ErrInvalidBackend)
	}

	return prov, nil
}

// extractStateData extracts *Data from a provider execution result.
// It handles both direct *Data pointers (returned by in-process providers)
// and map[string]any representations (returned after JSON round-trips).
func extractStateData(result *provider.ExecutionResult) (*Data, error) {
	if result == nil {
		return nil, fmt.Errorf("nil execution result")
	}

	dataMap, ok := result.Output.Data.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected map output, got %T", result.Output.Data)
	}

	sd, ok := dataMap["data"]
	if !ok {
		return nil, fmt.Errorf("missing 'data' field in backend output")
	}

	// Direct pointer — returned by in-process providers.
	if stateData, ok := sd.(*Data); ok {
		return stateData, nil
	}

	// Map representation — may occur after JSON serialization round-trips
	// (e.g., plugin providers or test mocks).
	if m, ok := sd.(map[string]any); ok {
		b, err := json.Marshal(m)
		if err != nil {
			return nil, fmt.Errorf("marshal state map: %w", err)
		}
		var stateData Data
		if err := json.Unmarshal(b, &stateData); err != nil {
			return nil, fmt.Errorf("unmarshal state map: %w", err)
		}
		return &stateData, nil
	}

	return nil, fmt.Errorf("expected *Data or map[string]any, got %T", sd)
}

// isTruthy coerces a value to bool.
func isTruthy(v any) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val != "" && val != "false" && val != "0"
	case int:
		return val != 0
	case int64:
		return val != 0
	case float64:
		return val != 0
	default:
		return true
	}
}

// structToMap converts a Go struct to a map[string]any via JSON round-trip.
// This is necessary because the provider executor's schema validator cannot
// validate Go structs directly.
func structToMap(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}
