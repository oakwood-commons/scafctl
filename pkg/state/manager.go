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

// ProviderGetter looks up providers by name. This is the minimal registry
// surface the state manager needs — it only calls Get to find the backend
// provider. Both *provider.Registry and provider.ProviderLookup satisfy it.
type ProviderGetter interface {
	Get(name string) (provider.Provider, bool)
}

// Manager orchestrates the state lifecycle: pre-execution loading, parameter
// merging, and post-execution saving. It is called by the CLI command layer
// before and after resolver execution.
type Manager struct {
	registry ProviderGetter
	config   *Config
	runtime  settings.RuntimeProvenance // execution provenance for metadata
}

// NewManager creates a state manager for the given state configuration. The
// provenance records both the engine (scafctl library) and invoking
// CLI/frontend identities; see settings.RuntimeProvenance.
func NewManager(config *Config, registry ProviderGetter, runtime settings.RuntimeProvenance) *Manager {
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

	// FirstRun is true when no prior state was found for this backend: a fresh
	// Data document (zero CreatedAt) with no persisted parameters or resolver
	// entries. All three conditions are required -- CreatedAt alone is not
	// enough because the lean "intent" format deliberately omits it, so a
	// document produced by that format looks first-run by timestamp alone even
	// when it carries replayed parameters. False (and the rest of the
	// enrichment fields below) are meaningless when Skipped is true.
	FirstRun bool

	// LoadedParams is the number of parameters present in the previously saved
	// state, before merging with CLI params.
	LoadedParams int

	// LoadedResolvers is the number of persisted resolver entries (including
	// immutable locks) present in the previously saved state.
	LoadedResolvers int

	// Provider is the backend provider name state was loaded from (e.g.,
	// "file", "http"). Used as a fallback label when Location is empty.
	Provider string

	// Location is a human-readable destination the state was loaded from --
	// the resolved "path" or "url" backend input when the backend uses one,
	// otherwise empty. Callers should fall back to displaying the provider
	// name (Provider) when Location is empty.
	Location string
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
	backendInputs, err := m.resolveBackendInputs(ctx, m.config.Backend, resolverData, params)
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
	backendProvider, err := m.getBackendProvider(m.config.Backend.Provider)
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

	// Capture load-time facts before command/context mutation below: a fresh
	// Data document (extractStateData's NewData() fallback for a first run)
	// has a zero CreatedAt, and its Parameters/Resolvers maps start empty.
	// All three conditions must hold for FirstRun -- CreatedAt.IsZero() alone
	// is not enough, because the lean "intent" format omits CreatedAt, so a
	// replayed intent document would otherwise be misreported as a first run
	// despite carrying loaded parameters.
	loadedParams := len(stateData.Parameters)
	loadedResolvers := len(stateData.Resolvers)
	firstRun := stateData.Metadata.CreatedAt.IsZero() && loadedParams == 0 && loadedResolvers == 0

	// Merge saved parameters with CLI params (CLI wins)
	mergedParams := MergeParameters(stateData.Parameters, params)

	// Capture command info
	stateData.Command = command

	// Inject into context
	enrichedCtx := WithState(ctx, stateData)

	return &LoadResult{
		Ctx:             enrichedCtx,
		Data:            stateData,
		MergedParams:    mergedParams,
		FirstRun:        firstRun,
		LoadedParams:    loadedParams,
		LoadedResolvers: loadedResolvers,
		Provider:        m.config.Backend.Provider,
		Location:        resolveLocation(backendInputs),
	}, nil
}

// BackendWrite reports the outcome of a single state_save call against one
// backend -- which provider received it, where, in what format, and whether it
// was skipped. Command-layer callers use this to render a save confirmation.
type BackendWrite struct {
	// Provider is the backend provider name (e.g., "file", "http").
	Provider string

	// Location is a human-readable destination for this write -- the resolved
	// "path" or "url" backend input when the backend uses one, otherwise
	// empty. Callers should fall back to displaying the provider name when
	// Location is empty.
	Location string

	// Format is the backend's declared Format, normalized ("" reports as
	// FormatFull, matching the zero-value contract in Backend.Format and
	// projectState).
	Format string

	// Skipped is true for an Emit target whose Enabled condition evaluated to
	// false; the write was never attempted. Always false for Primary.
	Skipped bool
}

// SaveResult reports what commit actually wrote, for callers that want to
// surface a save confirmation to the user.
type SaveResult struct {
	// Primary is the primary backend's write outcome.
	Primary BackendWrite

	// Emits reports each configured Emit target, in declaration order,
	// including targets skipped by their Enabled condition.
	Emits []BackendWrite
}

// Save executes the post-execution state lifecycle:
//  1. Saves the merged parameter set.
//  2. Checks immutable resolvers (saves new, verifies existing).
//  3. Updates metadata timestamps.
//  4. Calls the backend provider with operation=state_save.
//
// params are the merged parameters for this execution. resolverData contains
// resolver outputs, available as _ in CEL expressions for backend inputs.
func (m *Manager) Save(ctx context.Context, stateData *Data, resolverCtx *resolver.Context, resolvers []*resolver.Resolver, mergedParams, resolverData map[string]any, solMeta SolutionMeta) (*SaveResult, error) {
	if m.config == nil || stateData == nil {
		return nil, nil
	}

	// Save the merged parameter set
	stateData.Parameters = mergedParams

	// Record persisted and immutable resolver values
	if err := PersistResolvers(stateData, resolverCtx, resolvers, nil); err != nil {
		return nil, err
	}

	return m.commit(ctx, stateData, resolverData, mergedParams, solMeta, true)
}

// SaveImmutables checks and persists immutable resolver locks without touching
// the saved parameter set. It is used by entrypoints that run side-effecting
// actions: immutable locks are committed after deferred validation passes and
// before actions run, so a value that is now fixed is persisted even if a
// downstream action later fails.
//
// skip contains resolver names whose deferred validation failed; their
// immutable values are not locked.
//
// This is an interim commit ahead of actions, not the run's user-facing save
// confirmation -- so its SaveResult is discarded (not returned), and, more
// importantly, it never writes Emit targets. An Emit target may be a
// side-effecting backend (e.g. a REST endpoint), so it must be written at
// most once per run, from the run's single final commit (SaveParams) --
// never from this interim commit, which runs before an action might still
// fail and would otherwise publish an emit for a run that never completes.
func (m *Manager) SaveImmutables(ctx context.Context, stateData *Data, resolverCtx *resolver.Context, resolvers []*resolver.Resolver, mergedParams, resolverData map[string]any, solMeta SolutionMeta, skip map[string]bool) error {
	if m.config == nil || stateData == nil {
		return nil
	}

	// Check and save immutable resolver values. Parameters are intentionally not
	// updated here -- they are persisted after actions complete via SaveParams.
	if err := PersistResolvers(stateData, resolverCtx, resolvers, skip); err != nil {
		return err
	}

	_, err := m.commit(ctx, stateData, resolverData, mergedParams, solMeta, false)
	return err
}

// SaveParams persists the merged parameter set. It is called after actions
// complete so that ordinary (mutable) parameters are only saved on a fully
// successful run.
func (m *Manager) SaveParams(ctx context.Context, stateData *Data, mergedParams, resolverData map[string]any, solMeta SolutionMeta) (*SaveResult, error) {
	if m.config == nil || stateData == nil {
		return nil, nil
	}

	stateData.Parameters = mergedParams

	return m.commit(ctx, stateData, resolverData, mergedParams, solMeta, true)
}

// commit updates state metadata and saves the current state document to the
// primary backend. When writeEmits is true, it also saves to each configured
// Emit target (each independently projected per its own Format and gated by
// its own Enabled condition).
//
// writeEmits is false for the interim pre-action commit (SaveImmutables) and
// true for the run's single user-facing save (Save for run resolver;
// SaveParams for run solution/run action) -- see SaveImmutables for why an
// Emit target must be written at most once per run, at the final commit.
//
// A failure saving to the primary backend aborts before any Emit target is
// attempted. A failure saving to an Emit target aborts the remaining Emit
// targets but does not undo the primary save, which has already succeeded by
// that point.
func (m *Manager) commit(ctx context.Context, stateData *Data, resolverData, mergedParams map[string]any, solMeta SolutionMeta, writeEmits bool) (*SaveResult, error) {
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

	primaryWrite, err := m.saveToBackend(ctx, m.config.Backend, stateData, resolverData, mergedParams)
	if err != nil {
		return nil, err
	}

	result := &SaveResult{Primary: primaryWrite}

	if !writeEmits {
		return result, nil
	}

	for i, target := range m.config.Emit {
		enabled, err := m.evaluateEmitEnabled(ctx, target, resolverData, mergedParams)
		if err != nil {
			return nil, fmt.Errorf("state: evaluate emit[%d] enabled: %w", i, err)
		}
		if !enabled {
			result.Emits = append(result.Emits, BackendWrite{
				Provider: target.Backend.Provider,
				Format:   normalizeFormat(target.Backend.Format),
				Skipped:  true,
			})
			continue
		}
		emitWrite, err := m.saveToBackend(ctx, target.Backend, stateData, resolverData, mergedParams)
		if err != nil {
			return nil, fmt.Errorf("state: emit[%d]: %w", i, err)
		}
		result.Emits = append(result.Emits, emitWrite)
	}

	return result, nil
}

// saveToBackend projects stateData per backend.Format and writes it through
// backend's provider via state_save. It is shared by the primary backend save
// and every Emit target save in commit, so format handling and input/override
// resolution behave identically regardless of which backend is being written.
func (m *Manager) saveToBackend(ctx context.Context, backend Backend, stateData *Data, resolverData, mergedParams map[string]any) (BackendWrite, error) {
	// Resolve backend inputs for save -- resolver outputs are available as _
	// and CLI params as __params in backend input expressions.
	backendInputs, err := m.resolveBackendInputs(ctx, backend, resolverData, mergedParams)
	if err != nil {
		return BackendWrite{}, fmt.Errorf("state: resolve backend inputs for save: %w", err)
	}

	// Resolve save-only overrides (can use rslvr: and _ since resolvers have run)
	if len(backend.SaveOverrides) > 0 {
		overrides, err := m.resolveSaveOverrides(ctx, backend, resolverData, mergedParams)
		if err != nil {
			return BackendWrite{}, fmt.Errorf("state: resolve save overrides: %w", err)
		}
		// Merge: saveOverrides keys override inputs keys
		for k, v := range overrides {
			backendInputs[k] = v
		}
	}

	// Look up backend provider
	backendProvider, err := m.getBackendProvider(backend.Provider)
	if err != nil {
		return BackendWrite{}, err
	}

	// Project stateData into the shape this backend's Format calls for, then
	// convert to map[string]any so the provider executor's JSON-schema
	// validator can inspect the value (it cannot validate Go structs directly).
	dataMap, err := projectState(stateData, backend.Format)
	if err != nil {
		return BackendWrite{}, fmt.Errorf("state: project state data: %w", err)
	}

	// Capture the write's reporting facts before backendInputs is mutated with
	// the operation/data keys below.
	write := BackendWrite{
		Provider: backend.Provider,
		Location: resolveLocation(backendInputs),
		Format:   normalizeFormat(backend.Format),
	}

	backendInputs["operation"] = "state_save"
	backendInputs["data"] = dataMap
	execCtx := provider.WithExecutionMode(ctx, provider.CapabilityState)
	if _, err := provider.Execute(execCtx, backendProvider, backendInputs); err != nil {
		return BackendWrite{}, fmt.Errorf("state: backend save: %w", err)
	}

	return write, nil
}

// resolveLocation extracts a human-readable location from resolved backend
// inputs, for status reporting. It checks the common "path" (file backend)
// and "url" (http backend) input keys; other backend providers report an
// empty location, and callers fall back to displaying the provider name.
func resolveLocation(backendInputs map[string]any) string {
	if path, ok := backendInputs["path"].(string); ok && path != "" {
		return path
	}
	if url, ok := backendInputs["url"].(string); ok && url != "" {
		return url
	}
	return ""
}

// normalizeFormat returns format if non-empty, otherwise FormatFull -- mirroring
// the zero-value contract in Backend.Format and projectState (an unset Format
// behaves exactly as state behaved before Format existed).
func normalizeFormat(format string) string {
	if format == "" {
		return FormatFull
	}
	return format
}

// evaluateEmitEnabled resolves an Emit target's Enabled condition, defaulting
// to true (always emit) when unset. It is evaluated at save time, so unlike
// the primary Config.Enabled it may reference any resolver -- all resolvers
// have run by save time, so there is no pre-load acyclic constraint to honor.
func (m *Manager) evaluateEmitEnabled(ctx context.Context, target EmitTarget, resolverData, params map[string]any) (bool, error) {
	if target.Enabled == nil {
		return true, nil
	}
	val, err := resolveWithParams(ctx, target.Enabled, resolverData, params)
	if err != nil {
		return false, err
	}
	return isTruthy(val), nil
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

// resolveBackendInputs resolves all of backend's input ValueRefs.
// resolverData becomes _ in CEL; params becomes __params. Used for both the
// primary Config.Backend (at load and save) and each Config.Emit target's
// Backend (at save only).
func (m *Manager) resolveBackendInputs(ctx context.Context, backend Backend, resolverData, params map[string]any) (map[string]any, error) {
	resolved := make(map[string]any, len(backend.Inputs))

	for key, vr := range backend.Inputs {
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

// resolveSaveOverrides resolves all of backend's SaveOverrides ValueRefs.
// These are only called at save time when resolver data (_) is available.
func (m *Manager) resolveSaveOverrides(ctx context.Context, backend Backend, resolverData, params map[string]any) (map[string]any, error) {
	resolved := make(map[string]any, len(backend.SaveOverrides))

	for key, vr := range backend.SaveOverrides {
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

// getBackendProvider looks up a backend provider by name from the registry.
// Used for both the primary Config.Backend and each Config.Emit target.
func (m *Manager) getBackendProvider(name string) (provider.Provider, error) {
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

	// A backend reports an absent object (a first run) via found:false. Treat it
	// as fresh empty state without decoding or version-checking the payload, so
	// the misleading "delete the state file" guidance is never emitted before a
	// file exists. Absent found defaults to true (the prior backend contract).
	if found, ok := dataMap[OutputKeyFound]; ok && !isTruthy(found) {
		return NewData(), nil
	}

	sd, ok := dataMap[OutputKeyData]
	if !ok {
		return nil, fmt.Errorf("missing 'data' field in backend output")
	}

	// Direct pointer — returned by in-process providers.
	if stateData, ok := sd.(*Data); ok {
		// A zero-value document is the in-process equivalent of a contentless
		// payload: treat it as fresh empty state rather than a version-0 file.
		if isEmptyData(stateData) {
			return NewData(), nil
		}
		if err := validateSchemaVersion(stateData.SchemaVersion, true); err != nil {
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
