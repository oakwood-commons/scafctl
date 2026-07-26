// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// Manager orchestrates the state lifecycle: pre-execution loading, parameter
// merging, and post-execution saving. It is called by the CLI command layer
// before and after resolver execution.
type Manager struct {
	registry *provider.Registry
	config   *Config
	runtime  settings.RuntimeProvenance // execution provenance for metadata
}

// NewManager creates a state manager for the given state configuration. The
// provenance records both the engine (scafctl library) and invoking
// CLI/frontend identities; see settings.RuntimeProvenance.
func NewManager(config *Config, registry *provider.Registry, runtime settings.RuntimeProvenance) *Manager {
	return &Manager{
		config:   config,
		registry: registry,
		runtime:  runtime,
	}
}

// runtimeMetadata adapts the shared provenance primitive into the state
// Runtime metadata block, applying the CLI-mirrors-engine fallback.
func runtimeMetadata(p settings.RuntimeProvenance) Runtime {
	return Runtime{
		Engine: RuntimeComponent{Name: p.EngineName, Version: p.EngineVersion},
		CLI:    RuntimeComponent{Name: p.ResolvedCLIName(), Version: p.ResolvedCLIVersion()},
	}
}

// RuntimeProvenanceFromContext builds execution provenance from the ambient CLI
// settings. It is a thin wrapper over settings.RuntimeProvenanceFromContext so
// callers in the command layer need not import settings directly.
func RuntimeProvenanceFromContext(ctx context.Context) settings.RuntimeProvenance {
	return settings.RuntimeProvenanceFromContext(ctx, "")
}

// LoadResult is returned by Load with the loaded state and merged parameters.
type LoadResult struct {
	// Ctx is the context enriched with state.WithState.
	Ctx context.Context

	// Data is the loaded (or empty) state data.
	Data *Data

	// MergedParams is the effective parameter set (saved params merged with CLI params).
	// CLI params override saved params; new CLI keys are added.
	MergedParams map[string]any

	// Skipped is true when state is disabled or the enabled ValueRef is falsy.
	Skipped bool
}

// Load executes the pre-execution state lifecycle:
//  1. Evaluates the enabled ValueRef using CLI params (available as __params
//     in CEL expressions). No resolver data is available at load time.
//  2. If disabled, returns LoadResult{Skipped: true}.
//  3. Resolves backend inputs with params as __params.
//  4. Calls the backend provider with operation=state_load.
//  5. Merges saved parameters with CLI params (CLI wins on conflict).
//  6. Captures command info.
//  7. Injects state into context via WithState.
//
// Load resolves enabled and backend inputs with no resolver data (_ is empty).
// To let those fields reference state-independent resolvers, use LoadTwoPhase,
// which runs the referenced resolvers first and passes their values here.
func (m *Manager) Load(ctx context.Context, params map[string]any, command CommandInfo) (*LoadResult, error) {
	return m.load(ctx, nil, params, command)
}

// load is the shared implementation behind Load and LoadTwoPhase. resolverData
// holds any resolver outputs available at load time (empty for the single-phase
// Load; the Phase-A results for LoadTwoPhase) and is exposed as _ in enabled and
// backend-input expressions.
func (m *Manager) load(ctx context.Context, resolverData, params map[string]any, command CommandInfo) (*LoadResult, error) {
	if m.config == nil {
		return &LoadResult{Ctx: ctx, Skipped: true}, nil
	}

	// Evaluate enabled -- resolverData is exposed as _, CLI params as __params.
	enabled, err := m.evaluateEnabled(ctx, resolverData, params)
	if err != nil {
		if missing := MissingParams(ctx, m.config, params); len(missing) > 0 {
			return nil, &MissingParamsError{
				Missing:  missing,
				Original: fmt.Errorf("state: evaluate enabled: %w", err),
			}
		}
		return nil, fmt.Errorf("state: evaluate enabled: %w", err)
	}
	if !enabled {
		return &LoadResult{Ctx: ctx, Skipped: true}, nil
	}

	// Resolve backend inputs -- resolverData is exposed as _, CLI params as __params.
	backendInputs, err := m.resolveBackendInputs(ctx, resolverData, params)
	if err != nil {
		if missing := MissingParams(ctx, m.config, params); len(missing) > 0 {
			return nil, &MissingParamsError{
				Missing:  missing,
				Original: fmt.Errorf("state: resolve backend inputs: %w", err),
			}
		}
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

	// Merge saved parameters with CLI params (CLI wins)
	mergedParams := MergeParameters(stateData.Parameters, params)

	// Capture command info
	stateData.Command = command

	// Inject into context
	enrichedCtx := WithState(ctx, stateData)

	return &LoadResult{
		Ctx:          enrichedCtx,
		Data:         stateData,
		MergedParams: mergedParams,
	}, nil
}

// Save executes the post-execution state lifecycle:
//  1. Saves the merged parameter set.
//  2. Checks immutable resolvers (saves new, verifies existing).
//  3. Updates metadata timestamps.
//  4. Calls the backend provider with operation=state_save.
//
// params are the merged parameters for this execution. resolverData contains
// resolver outputs, available as _ in CEL expressions for backend inputs.
func (m *Manager) Save(ctx context.Context, stateData *Data, resolverCtx *resolver.Context, resolvers []*resolver.Resolver, mergedParams, resolverData map[string]any, solMeta SolutionMeta) error {
	if m.config == nil || stateData == nil {
		return nil
	}

	// Save the merged parameter set
	stateData.Parameters = mergedParams

	// Record persisted and immutable resolver values
	if err := PersistResolvers(stateData, resolverCtx, resolvers, nil); err != nil {
		return err
	}

	return m.commit(ctx, stateData, resolverData, mergedParams, solMeta)
}

// SaveImmutables checks and persists immutable resolver locks without touching
// the saved parameter set. It is used by entrypoints that run side-effecting
// actions: immutable locks are committed after deferred validation passes and
// before actions run, so a value that is now fixed is persisted even if a
// downstream action later fails.
//
// skip contains resolver names whose deferred validation failed; their
// immutable values are not locked.
func (m *Manager) SaveImmutables(ctx context.Context, stateData *Data, resolverCtx *resolver.Context, resolvers []*resolver.Resolver, mergedParams, resolverData map[string]any, solMeta SolutionMeta, skip map[string]bool) error {
	if m.config == nil || stateData == nil {
		return nil
	}

	// Check and save immutable resolver values. Parameters are intentionally not
	// updated here -- they are persisted after actions complete via SaveParams.
	if err := PersistResolvers(stateData, resolverCtx, resolvers, skip); err != nil {
		return err
	}

	return m.commit(ctx, stateData, resolverData, mergedParams, solMeta)
}

// SaveParams persists the merged parameter set. It is called after actions
// complete so that ordinary (mutable) parameters are only saved on a fully
// successful run.
func (m *Manager) SaveParams(ctx context.Context, stateData *Data, mergedParams, resolverData map[string]any, solMeta SolutionMeta) error {
	if m.config == nil || stateData == nil {
		return nil
	}

	stateData.Parameters = mergedParams

	return m.commit(ctx, stateData, resolverData, mergedParams, solMeta)
}

// commit updates state metadata and writes the current state document to the
// configured backend.
func (m *Manager) commit(ctx context.Context, stateData *Data, resolverData, mergedParams map[string]any, solMeta SolutionMeta) error {
	// Update metadata
	now := time.Now().UTC()
	// Stamp the current schema version so a state file loaded under an older
	// (still-supported) schema is re-persisted under the format this build
	// actually writes -- otherwise the on-disk schemaVersion would understate
	// the content (e.g. a v2 file re-saved with the v3 runtime block).
	stateData.SchemaVersion = SchemaVersionCurrent
	if stateData.Metadata.CreatedAt.IsZero() {
		stateData.Metadata.CreatedAt = now
	}
	stateData.Metadata.LastUpdatedAt = now
	stateData.Metadata.Solution = solMeta.Name
	stateData.Metadata.Version = solMeta.Version
	stateData.Metadata.Runtime = runtimeMetadata(m.runtime)

	// Resolve backend inputs for save -- resolver outputs are available as _
	// and CLI params as __params in backend input expressions.
	backendInputs, err := m.resolveBackendInputs(ctx, resolverData, mergedParams)
	if err != nil {
		return fmt.Errorf("state: resolve backend inputs for save: %w", err)
	}

	// Resolve save-only overrides (can use rslvr: and _ since resolvers have run)
	if len(m.config.Backend.SaveOverrides) > 0 {
		overrides, err := m.resolveSaveOverrides(ctx, resolverData, mergedParams)
		if err != nil {
			return fmt.Errorf("state: resolve save overrides: %w", err)
		}
		// Merge: saveOverrides keys override inputs keys
		for k, v := range overrides {
			backendInputs[k] = v
		}
	}

	// Look up backend provider
	backendProvider, err := m.getBackendProvider()
	if err != nil {
		return err
	}

	// Execute save -- convert stateData to map[string]any so that the provider
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

// VerifyImmutables checks that resolved immutable values have not changed
// relative to previously locked state, WITHOUT mutating state or locking new
// values. Call it after resolver execution but BEFORE action execution so that
// a locked-value violation aborts the run before any side effects occur.
//
// It is a no-op when state is disabled or no state data is present.
func (m *Manager) VerifyImmutables(stateData *Data, resolverCtx *resolver.Context, resolvers []*resolver.Resolver) error {
	if m.config == nil || stateData == nil {
		return nil
	}
	return VerifyImmutables(stateData, resolverCtx, resolvers)
}

// SolutionMeta contains solution identity for state metadata.
type SolutionMeta struct {
	Name    string
	Version string
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

// resolveSaveOverrides resolves all SaveOverrides ValueRefs.
// These are only called at save time when resolver data (_) is available.
func (m *Manager) resolveSaveOverrides(ctx context.Context, resolverData, params map[string]any) (map[string]any, error) {
	resolved := make(map[string]any, len(m.config.Backend.SaveOverrides))

	for key, vr := range m.config.Backend.SaveOverrides {
		if vr == nil {
			continue
		}
		val, err := resolveWithParams(ctx, vr, resolverData, params)
		if err != nil {
			return nil, fmt.Errorf("resolve save override %q: %w", key, err)
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
		if err := validateSchemaVersion(stateData.SchemaVersion); err != nil {
			return nil, err
		}
		normalizeData(stateData)
		return stateData, nil
	}

	// Map representation — may occur after JSON serialization round-trips
	// (e.g., plugin providers or test mocks).
	if m, ok := sd.(map[string]any); ok {
		b, err := json.Marshal(m)
		if err != nil {
			return nil, fmt.Errorf("marshal state map: %w", err)
		}
		stateData, err := DecodeData(b)
		if err != nil {
			return nil, err
		}
		return stateData, nil
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

// RequiredParams extracts the __params keys referenced by the state
// configuration. These are the CLI parameters (-r flags) that must be supplied
// for the state backend to resolve its inputs at load time.
//
// It inspects Enabled and Backend.Inputs ValueRefs for:
//   - CEL expressions: uses Expression.GetVariablesWithPrefix("__params.")
//   - Go templates: uses GetGoTemplateReferences() and filters for .__params.*
//
// Literal and resolver-ref ValueRefs are skipped (they don't use __params).
// SaveOverrides are also skipped (they are only evaluated at save time).
//
// Returns a deduplicated, sorted list of parameter names (e.g., ["app_name", "project"]).
// Errors during parsing are silently ignored (best-effort extraction).
func RequiredParams(ctx context.Context, config *Config) []string {
	if config == nil {
		return nil
	}

	seen := make(map[string]struct{})

	// Extract from Enabled
	extractParamRefs(ctx, config.Enabled, seen)

	// Extract from Backend.Inputs
	for _, vr := range config.Backend.Inputs {
		extractParamRefs(ctx, vr, seen)
	}

	// SaveOverrides are skipped: they are evaluated at save time only

	if len(seen) == 0 {
		return nil
	}

	result := make([]string, 0, len(seen))
	for k := range seen {
		result = append(result, k)
	}
	sort.Strings(result)
	return result
}

// MissingParams returns the subset of RequiredParams that are absent from the
// supplied params map. Returns nil if all required params are present.
func MissingParams(ctx context.Context, config *Config, params map[string]any) []string {
	required := RequiredParams(ctx, config)
	if len(required) == 0 {
		return nil
	}

	var missing []string
	for _, key := range required {
		if _, ok := params[key]; !ok {
			missing = append(missing, key)
		}
	}
	return missing
}

// extractParamRefs extracts __params references from a single ValueRef.
func extractParamRefs(ctx context.Context, vr *spec.ValueRef, seen map[string]struct{}) {
	if vr == nil {
		return
	}

	// CEL expression: use GetVariablesWithPrefix("__params.")
	if vr.Expr != nil {
		vars, err := vr.Expr.GetVariablesWithPrefix(ctx, "__params.")
		if err == nil {
			for _, v := range vars {
				seen[v] = struct{}{}
			}
		}
	}

	// Go template: extract references and filter for .__params.*
	if vr.Tmpl != nil {
		refs, err := gotmpl.GetGoTemplateReferences(string(*vr.Tmpl), "", "")
		if err == nil {
			for _, ref := range refs {
				// Paths look like ".__params.project" — strip the ".__params." prefix
				const prefix = ".__params."
				if len(ref.Path) > len(prefix) && ref.Path[:len(prefix)] == prefix {
					key := ref.Path[len(prefix):]
					// Handle nested access: take only the first segment
					if idx := indexOf(key, '.'); idx >= 0 {
						key = key[:idx]
					}
					seen[key] = struct{}{}
				}
			}
		}
	}
}

// indexOf returns the index of the first occurrence of sep in s, or -1.
func indexOf(s string, sep byte) int {
	for i := range s {
		if s[i] == sep {
			return i
		}
	}
	return -1
}
