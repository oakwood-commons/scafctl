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
	registry        ProviderGetter
	config          *Config
	runtime         settings.RuntimeProvenance // execution provenance for metadata
	requireExisting bool
	workingDir      string // base for providers' relative locations; empty = context/process default
}

// ManagerOption configures optional Manager behavior.
type ManagerOption func(*Manager)

// WithRequireExisting makes Load fail with a *NotFoundError when the load
// provider reports that no state exists, instead of treating it as a first
// run. Use it when the caller named an exact state location to read (for
// example the CLI's --state-file flag): a missing document there is almost
// certainly a typo, not a first run.
func WithRequireExisting() ManagerOption {
	return func(m *Manager) {
		m.requireExisting = true
	}
}

// WithWorkingDirectory sets the directory state providers resolve relative
// locations against (for the file provider, a relative state path). Use it
// when the process directory may differ from the caller's working directory --
// for example while a bundled solution runs with its extraction directory as
// the process directory. It applies only to state provider calls, never to
// resolver execution. An empty dir keeps the context or process default.
func WithWorkingDirectory(dir string) ManagerOption {
	return func(m *Manager) {
		m.workingDir = dir
	}
}

// NewManager creates a state manager for the given state configuration. The
// provenance records both the engine (scafctl library) and invoking
// CLI/frontend identities; see settings.RuntimeProvenance. extends: load save
// targets are resolved against config's load block here (see ApplyOverrides for
// replacing the load block without redirecting those targets).
func NewManager(config *Config, registry ProviderGetter, runtime settings.RuntimeProvenance, opts ...ManagerOption) *Manager {
	m := &Manager{
		config:   materializeExtends(config),
		registry: registry,
		runtime:  runtime,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
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

	// Loaded is true when a load block was configured and read. It is false
	// when the configuration has save targets only, in which case every run
	// starts from empty state and there is nothing to report as loaded.
	Loaded bool

	// FirstRun is true when no prior state was found: a fresh Data document
	// (zero CreatedAt) with no persisted parameters or resolver entries. All
	// three conditions are required -- CreatedAt alone is not enough because the
	// lean "intent" format deliberately omits it, so a document produced by that
	// format looks first-run by timestamp alone even when it carries replayed
	// parameters. False (and the rest of the enrichment fields below) are
	// meaningless when Skipped is true.
	FirstRun bool

	// LoadedParams is the number of parameters present in the previously saved
	// state, before merging with CLI params.
	LoadedParams int

	// LoadedResolvers is the number of persisted resolver entries (including
	// immutable locks) present in the previously saved state.
	LoadedResolvers int

	// Provider is the load provider name state was read from (e.g., "file",
	// "http"). Used as a fallback label when Location is empty.
	Provider string

	// Location is a human-readable location state was read from -- the resolved
	// "path" or "url" load input when the provider uses one, otherwise empty.
	// Callers should fall back to displaying the provider name (Provider) when
	// Location is empty.
	Location string
}

// Load executes the pre-execution state lifecycle:
//  1. Evaluates the enabled ValueRef using CLI params (available as __params
//     in CEL expressions). No resolver data is available at load time.
//  2. If disabled, returns LoadResult{Skipped: true}.
//  3. Without a load block, starts from empty state.
//  4. Otherwise resolves the load inputs with params as __params and calls the
//     load provider with operation=state_load.
//  5. Merges saved parameters with CLI params (CLI wins on conflict).
//  6. Captures command info.
//  7. Injects state into context via WithState.
//
// Load resolves enabled and load inputs with no resolver data (_ is empty).
// To let those fields reference state-independent resolvers, use LoadTwoPhase,
// which runs the referenced resolvers first and passes their values here.
func (m *Manager) Load(ctx context.Context, params map[string]any, command CommandInfo) (*LoadResult, error) {
	return m.load(ctx, nil, params, command)
}

// load is the shared implementation behind Load and LoadTwoPhase. resolverData
// holds any resolver outputs available at load time (empty for the single-phase
// Load; the Phase-A results for LoadTwoPhase) and is exposed as _ in enabled and
// load-input expressions.
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

	// No load block: every run starts from empty state.
	if m.config.Load == nil {
		stateData := NewData()
		stateData.Command = command
		return &LoadResult{
			Ctx:          WithState(ctx, stateData),
			Data:         stateData,
			MergedParams: MergeParameters(stateData.Parameters, params),
			FirstRun:     true,
		}, nil
	}

	// Resolve load inputs -- resolverData is exposed as _, CLI params as __params.
	loadInputs, err := m.resolveInputs(ctx, m.config.Load.Inputs, resolverData, params)
	if err != nil {
		if missing := MissingParams(ctx, m.config, params); len(missing) > 0 {
			return nil, &MissingParamsError{
				Missing:  missing,
				Original: fmt.Errorf("state: resolve load inputs: %w", err),
			}
		}
		return nil, fmt.Errorf("state: resolve load inputs: %w", err)
	}

	loadProvider, err := m.getStateProvider(m.config.Load.Provider)
	if err != nil {
		return nil, err
	}

	// Capture the location before loadInputs gains the operation key.
	location := resolveLocation(loadInputs)

	loadInputs["operation"] = "state_load"
	execCtx := m.providerContext(ctx)
	result, err := provider.Execute(execCtx, loadProvider, loadInputs)
	if err != nil {
		return nil, fmt.Errorf("state: load: %w", err)
	}

	stateData, found, err := extractStateData(result)
	if err != nil {
		return nil, fmt.Errorf("state: extract loaded data: %w", err)
	}
	if !found && m.requireExisting {
		return nil, &NotFoundError{Location: location, Provider: m.config.Load.Provider}
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

	// Merge saved parameters with CLI params (CLI wins). MergeParameters returns
	// a fresh map, so stateData.Parameters stays as loaded -- which is what a
	// checkpoint write persists.
	mergedParams := MergeParameters(stateData.Parameters, params)

	// Capture command info
	stateData.Command = command

	return &LoadResult{
		Ctx:             WithState(ctx, stateData),
		Data:            stateData,
		MergedParams:    mergedParams,
		Loaded:          true,
		FirstRun:        firstRun,
		LoadedParams:    loadedParams,
		LoadedResolvers: loadedResolvers,
		Provider:        m.config.Load.Provider,
		Location:        location,
	}, nil
}

// TargetWrite reports the outcome of a single save target write -- which
// provider received it, where, in what format, and whether it was skipped.
// Command-layer callers use this to render a save confirmation.
type TargetWrite struct {
	// Provider is the save target's provider name (e.g., "file", "http").
	Provider string

	// Location is a human-readable location for this write -- the resolved
	// "path" or "url" input when the provider uses one, otherwise empty.
	// Callers should fall back to displaying the provider name when Location
	// is empty.
	Location string

	// Format is the target's declared Format, normalized ("" reports as
	// FormatFull, matching the zero-value contract of SaveTarget.Format).
	Format string

	// Skipped is true for a target whose Enabled condition evaluated to false;
	// the write was never attempted.
	Skipped bool
}

// SaveResult reports what Save wrote, for callers that want to surface a save
// confirmation to the user.
type SaveResult struct {
	// Targets reports each configured save target, in declaration order,
	// including targets skipped by their Enabled condition.
	Targets []TargetWrite
}

// Checkpoint writes the save targets marked checkpoint: true. The entrypoints
// that run workflow actions call it after resolvers run and before any action,
// so immutable values locked this run survive a later action failure.
//
// It locks immutable resolver values (except those in skip, whose deferred
// validation failed) and records persisted ones, but deliberately leaves the
// saved parameter set as loaded: new -r values are saved only by Save, once the
// whole run has succeeded. A checkpoint is an interim write, not the run's
// save confirmation, so it reports nothing. It is a no-op when no save target
// sets checkpoint: true.
func (m *Manager) Checkpoint(ctx context.Context, stateData *Data, resolverCtx *resolver.Context, resolvers []*resolver.Resolver, mergedParams, resolverData map[string]any, solMeta SolutionMeta, skip map[string]bool) error {
	if m.config == nil || stateData == nil || !m.hasCheckpointTargets() {
		return nil
	}

	if err := PersistResolvers(stateData, resolverCtx, resolvers, skip); err != nil {
		return err
	}

	_, err := m.write(ctx, stateData, resolverData, mergedParams, solMeta, true)
	return err
}

// Save executes the post-execution state lifecycle, writing every enabled save
// target once the run has succeeded:
//  1. Records the merged parameter set.
//  2. Locks new immutable resolver values and verifies existing locks (except
//     resolvers in skip, whose deferred validation failed), and records
//     persisted resolver values.
//  3. Updates metadata timestamps.
//  4. Writes each target, projected per its own Format, via state_save.
//
// resolverData contains resolver outputs, available as _ in save-target
// expressions. It returns nil when no save target is configured. When a target
// fails, the result still reports the targets written before it.
func (m *Manager) Save(ctx context.Context, stateData *Data, resolverCtx *resolver.Context, resolvers []*resolver.Resolver, mergedParams, resolverData map[string]any, solMeta SolutionMeta, skip map[string]bool) (*SaveResult, error) {
	if m.config == nil || stateData == nil || len(m.config.Save) == 0 {
		return nil, nil
	}

	stateData.Parameters = mergedParams

	if err := PersistResolvers(stateData, resolverCtx, resolvers, skip); err != nil {
		return nil, err
	}

	return m.write(ctx, stateData, resolverData, mergedParams, solMeta, false)
}

// hasCheckpointTargets reports whether any save target sets checkpoint: true.
func (m *Manager) hasCheckpointTargets() bool {
	for _, target := range m.config.Save {
		if target.Checkpoint {
			return true
		}
	}
	return false
}

// write updates state metadata and writes the document to the save targets:
// only the checkpoint: true targets when checkpoint is set, otherwise every
// target. Targets are written in declaration order, each projected per its own
// Format and gated by its own Enabled condition. A failure aborts the remaining
// targets but does not undo the targets already written; the returned result
// then reports those earlier targets alongside the error.
func (m *Manager) write(ctx context.Context, stateData *Data, resolverData, mergedParams map[string]any, solMeta SolutionMeta, checkpoint bool) (*SaveResult, error) {
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

	result := &SaveResult{}
	for i, target := range m.config.Save {
		if checkpoint && !target.Checkpoint {
			continue
		}
		enabled, err := m.evaluateTargetEnabled(ctx, target, resolverData, mergedParams)
		if err != nil {
			return result, fmt.Errorf("state: evaluate save[%d] enabled: %w", i, err)
		}
		if !enabled {
			result.Targets = append(result.Targets, TargetWrite{
				Provider: target.Provider,
				Format:   normalizeFormat(target.Format),
				Skipped:  true,
			})
			continue
		}
		written, err := m.writeTarget(ctx, target, stateData, resolverData, mergedParams)
		if err != nil {
			return result, fmt.Errorf("state: save[%d]: %w", i, err)
		}
		result.Targets = append(result.Targets, written)
	}

	return result, nil
}

// writeTarget projects stateData per target.Format and writes it through the
// target's provider via state_save.
func (m *Manager) writeTarget(ctx context.Context, target SaveTarget, stateData *Data, resolverData, mergedParams map[string]any) (TargetWrite, error) {
	if target.Extends != "" {
		// materializeExtends resolves every extends: load target when a load
		// block exists, so an extends target reaching here has nothing to
		// inherit from (or an unsupported value).
		if target.Extends == ExtendsLoad {
			return TargetWrite{}, fmt.Errorf("extends: %s requires a state.load block", ExtendsLoad)
		}
		return TargetWrite{}, fmt.Errorf("unsupported extends value %q (only %q is supported)", target.Extends, ExtendsLoad)
	}

	// Resolve inputs -- resolver outputs are available as _ and CLI params as
	// __params, since every resolver has run by save time.
	inputs, err := m.resolveInputs(ctx, target.Inputs, resolverData, mergedParams)
	if err != nil {
		return TargetWrite{}, fmt.Errorf("resolve inputs: %w", err)
	}

	saveProvider, err := m.getStateProvider(target.Provider)
	if err != nil {
		return TargetWrite{}, err
	}

	// Project stateData into the shape this target's Format calls for, as a
	// map[string]any so the provider executor's JSON-schema validator can
	// inspect the value (it cannot validate Go structs directly).
	dataMap, err := projectState(stateData, target)
	if err != nil {
		return TargetWrite{}, fmt.Errorf("project state data: %w", err)
	}

	// Capture the write's reporting facts before inputs gains the
	// operation/data keys below.
	written := TargetWrite{
		Provider: target.Provider,
		Location: resolveLocation(inputs),
		Format:   normalizeFormat(target.Format),
	}

	inputs["operation"] = "state_save"
	inputs["data"] = dataMap
	execCtx := m.providerContext(ctx)
	if _, err := provider.Execute(execCtx, saveProvider, inputs); err != nil {
		return TargetWrite{}, fmt.Errorf("write: %w", err)
	}

	return written, nil
}

// resolveLocation extracts a human-readable location from resolved provider
// inputs, for status reporting. It checks the common "path" (file provider)
// and "url" (http provider) input keys; other providers report an empty
// location, and callers fall back to displaying the provider name.
func resolveLocation(inputs map[string]any) string {
	if path, ok := inputs["path"].(string); ok && path != "" {
		return path
	}
	if url, ok := inputs["url"].(string); ok && url != "" {
		return url
	}
	return ""
}

// normalizeFormat returns format if non-empty, otherwise FormatFull -- mirroring
// the zero-value contract of SaveTarget.Format and projectState.
func normalizeFormat(format string) string {
	if format == "" {
		return FormatFull
	}
	return format
}

// evaluateTargetEnabled resolves a save target's Enabled condition, defaulting
// to true (always save) when unset. It is evaluated at save time, so unlike
// Config.Enabled it may reference any resolver -- all resolvers have run by
// save time, so there is no pre-load acyclic constraint to honor.
func (m *Manager) evaluateTargetEnabled(ctx context.Context, target SaveTarget, resolverData, params map[string]any) (bool, error) {
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

// resolveInputs resolves a provider input map's ValueRefs. resolverData becomes
// _ in CEL; params becomes __params. Used for the load block (at load time) and
// every save target (at save time).
func (m *Manager) resolveInputs(ctx context.Context, inputs map[string]*spec.ValueRef, resolverData, params map[string]any) (map[string]any, error) {
	resolved := make(map[string]any, len(inputs))

	for key, vr := range inputs {
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

// providerContext returns the context a state provider operation runs in:
// the state execution mode, plus the configured working directory, if any.
func (m *Manager) providerContext(ctx context.Context) context.Context {
	ctx = provider.WithExecutionMode(ctx, provider.CapabilityState)
	if m.workingDir != "" {
		ctx = provider.WithWorkingDirectory(ctx, m.workingDir)
	}
	return ctx
}

// getStateProvider looks up a state provider by name from the registry.
// Used for the load block and every save target.
func (m *Manager) getStateProvider(name string) (provider.Provider, error) {
	if name == "" {
		return nil, fmt.Errorf("state: provider name is empty")
	}

	prov, exists := m.registry.Get(name)
	if !exists {
		return nil, fmt.Errorf("state: provider %q not found in registry: %w", name, ErrInvalidProvider)
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
		return nil, fmt.Errorf("state: provider %q does not have CapabilityState: %w", name, ErrInvalidProvider)
	}

	return prov, nil
}

// extractStateData extracts *Data from a provider execution result.
// It handles both direct *Data pointers (returned by in-process providers)
// and map[string]any representations (returned after JSON round-trips).
//
// found is false when the provider reports that no state object exists (a
// first run); the returned Data is then fresh empty state.
func extractStateData(result *provider.ExecutionResult) (*Data, bool, error) {
	if result == nil {
		return nil, false, fmt.Errorf("nil execution result")
	}

	dataMap, ok := result.Output.Data.(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("expected map output, got %T", result.Output.Data)
	}

	// A provider reports an absent object (a first run) via found:false. Treat it
	// as fresh empty state without decoding or version-checking the payload, so
	// the misleading "delete the state file" guidance is never emitted before a
	// file exists. Absent found defaults to true (the prior provider contract).
	if found, ok := dataMap[OutputKeyFound]; ok && !isTruthy(found) {
		return NewData(), false, nil
	}

	sd, ok := dataMap[OutputKeyData]
	if !ok {
		return nil, false, fmt.Errorf("missing 'data' field in state provider output")
	}

	// Direct pointer — returned by in-process providers.
	if stateData, ok := sd.(*Data); ok {
		// A zero-value document is the in-process equivalent of a contentless
		// payload: treat it as fresh empty state rather than a version-0 file.
		if isEmptyData(stateData) {
			return NewData(), true, nil
		}
		if err := validateSchemaVersion(stateData.SchemaVersion, true); err != nil {
			return nil, false, err
		}
		normalizeData(stateData)
		return stateData, true, nil
	}

	// Map representation — may occur after JSON serialization round-trips
	// (e.g., plugin providers or test mocks).
	if m, ok := sd.(map[string]any); ok {
		b, err := json.Marshal(m)
		if err != nil {
			return nil, false, fmt.Errorf("marshal state map: %w", err)
		}
		stateData, err := DecodeData(b)
		if err != nil {
			return nil, false, err
		}
		return stateData, true, nil
	}

	return nil, false, fmt.Errorf("expected *Data or map[string]any, got %T", sd)
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

// RequiredParams extracts the __params keys referenced by the load-time state
// configuration. These are the CLI parameters (-r flags) that must be supplied
// for state to resolve its load inputs at load time.
//
// It inspects Enabled and Load.Inputs ValueRefs for:
//   - CEL expressions: uses Expression.GetVariablesWithPrefix("__params.")
//   - Go templates: uses GetGoTemplateReferences() and filters for .__params.*
//
// Literal and resolver-ref ValueRefs are skipped (they don't use __params).
// Save targets are also skipped (they are only evaluated at save time).
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

	// Extract from Load.Inputs
	if config.Load != nil {
		for _, vr := range config.Load.Inputs {
			extractParamRefs(ctx, vr, seen)
		}
	}

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
