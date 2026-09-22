// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"encoding/json"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/spec"
)

const (
	// SchemaVersionCurrent is the state file schema version written by this build.
	// v3 replaced the single-purpose metadata.scafctlVersion field with a
	// metadata.runtime block that separates the execution engine identity
	// (runtime.engine) from the invoking CLI/frontend identity (runtime.cli).
	// Legacy v2 files still load (SchemaVersionMinimum stays 2) but their old
	// scafctlVersion value is intentionally NOT migrated into runtime -- the
	// runtime block is re-stamped from the current invocation on the next save.
	SchemaVersionCurrent = 3

	// SchemaVersionMinimum is the oldest state file schema version this build can
	// safely load. Bump this ONLY when a breaking change makes older files unsafe
	// to read -- e.g. schema v1 stored immutable locks under a legacy `immutables`
	// field that is now dropped on unmarshal, so loading a v1 file would silently
	// discard immutable enforcement. Additive, backward-compatible bumps should
	// raise SchemaVersionCurrent WITHOUT raising this floor so older files still load.
	SchemaVersionMinimum = 2
)

// ReadProviderName is the canonical name of the state *read* provider (the
// provider resolvers use to read the loaded state snapshot). A resolver using it
// is state-dependent and cannot participate in the two-phase pre-load. This is
// the single source of truth for the name; the provider implementation and all
// partition logic reference it to avoid drift.
const ReadProviderName = "state"

// Backend format identifiers. Format controls what a backend's state_save
// receives: the complete state document, or a lean, human-readable "intent"
// projection of it. See Backend.Format and projectState.
const (
	// FormatFull is the default: the complete state document (schemaVersion,
	// metadata, command, parameters, resolvers, fingerprints, attestation).
	FormatFull = "full"

	// FormatIntent projects only the replay-relevant subset: schemaVersion,
	// metadata.solution/metadata.version, parameters, and attestation (when
	// present). It deliberately omits command, resolvers, fingerprints, and the
	// volatile metadata fields (createdAt/lastUpdatedAt/runtime), producing a
	// deterministic, reviewable document suitable for committing and signing.
	//
	// Using FormatIntent as the PRIMARY backend's format is lossy: immutable
	// resolver locks live in the omitted "resolvers" section, so they will not
	// persist across runs against that backend alone. Pair it with a full-
	// fidelity primary backend and an "emit" entry (see Config.Emit) to keep
	// both a complete local state and a published intent; see
	// lintStateFormatLossy for the guardrail.
	FormatIntent = "intent"
)

const (
	// OutputKeyData is the state_load output field that carries the decoded or
	// serialized state document a backend read from its storage.
	OutputKeyData = "data"

	// OutputKeyFound is the state_load output field a backend sets to false to
	// report that no state object exists yet (a first run). When absent it
	// defaults to true, which preserves the prior backend contract. The core
	// loader treats found:false as fresh empty state without decoding the
	// payload or applying the schema-version guard, so the "delete the state
	// file and recreate it" guidance is never emitted before a file exists.
	OutputKeyFound = "found"
)

// Config is the solution-level state configuration.
// It is a top-level peer to Spec, Catalog, Bundle, and Compose on the Solution struct.
//
// CLI parameters passed via -r flags are available as __params in CEL expressions
// and Go templates used in Enabled and Backend.Inputs. This allows dynamic backend
// configuration from user input.
//
// Enabled and Backend.Inputs may also reference resolvers (via rslvr:, expr:, or
// tmpl:), as long as those resolvers are state-INDEPENDENT -- i.e. they do not
// read state (via the state provider) and do not transitively depend on one that
// does. The engine runs the referenced resolvers first (a minimal pre-load pass),
// then loads state with their values exposed as _. Referencing a state-dependent
// resolver is a circular dependency and is rejected (see CycleError).
//
// Example:
//
//	state:
//	  enabled:
//	    expr: "_.stateEnabled"   # stateEnabled is a state-independent resolver
//	  backend:
//	    provider: file
//	    inputs:
//	      path:
//	        expr: "'gcp/' + __params.project + '/state.json'"
//
// A solution can additionally publish one or more projected copies of the same
// state document via Emit -- for example, keeping a full-fidelity primary state
// file locally while also emitting a lean "intent" file intended to be committed
// and signed:
//
//	state:
//	  enabled: true
//	  backend:                                # full-fidelity primary
//	    provider: file
//	    inputs: { path: ".scafctl/state.json" }
//	  emit:
//	    - provider: file
//	      format: intent
//	      inputs: { path: "intent/sandbox.json" }
type Config struct {
	// Enabled controls whether state persistence is active. Supports a literal bool,
	// CEL expression, Go template, or resolver reference.
	// References to state-independent resolvers are resolved via a pre-load pass;
	// references to state-dependent resolvers are rejected (circular dependency).
	// Use __params to reference CLI parameters (e.g. expr: "__params.enable_state == true").
	Enabled *spec.ValueRef `json:"enabled" yaml:"enabled" doc:"Dynamic activation of state persistence"`

	// Backend configures which provider handles state persistence. This is the
	// PRIMARY backend: the only one used for load, and always saved to.
	Backend Backend `json:"backend" yaml:"backend" doc:"Backend provider configuration"`

	// Emit configures additional save-only projections of the same state
	// document, each written through its own backend. Emit targets are never
	// used for load -- only Backend is. Each target may set its own Format
	// (e.g. "intent") independent of the primary backend's format, and its own
	// Enabled condition to skip that emission on some runs. See FormatIntent.
	Emit []EmitTarget `json:"emit,omitempty" yaml:"emit,omitempty" doc:"Additional save-only projected state emissions" maxItems:"20"`
}

// Backend configures a state persistence backend -- used both for the primary
// Config.Backend (load + save) and for each Config.Emit target (save only).
type Backend struct {
	// Provider is the name of a registered provider with CapabilityState (e.g., "file").
	Provider string `json:"provider" yaml:"provider" doc:"Provider name with CapabilityState" maxLength:"253" example:"file"`

	// Format controls what shape of the state document this backend receives at
	// save time. "full" (the default) saves the complete state document; "intent"
	// saves the lean, replay-relevant projection (see FormatIntent). Load is
	// unaffected by Format -- decoding already tolerates a lean document.
	Format string `json:"format,omitempty" yaml:"format,omitempty" doc:"Save-time projection: full (default) or intent" enum:"full,intent" example:"intent"`

	// Parameters narrows which parameters an "intent"-format projection carries
	// (see FormatIntent). Only meaningful when Format is "intent" -- lint rejects
	// it on a "full" (or unset) backend, since narrowing the authoritative state
	// document would silently break replay. Nil means no narrowing: every saved
	// parameter is projected, as before this field existed.
	Parameters *ParameterProjection `json:"parameters,omitempty" yaml:"parameters,omitempty" doc:"Narrow which parameters an intent-format projection carries (intent format only)"`

	// Inputs are provider-specific inputs. Each value is a ValueRef for dynamic resolution.
	//
	// CEL expressions use __params for CLI parameters (e.g. __params.project) and _
	// for resolver outputs. A referenced resolver must be state-independent (it must
	// not read state or depend on one that does); the engine runs such resolvers in a
	// pre-load pass before loading state. Referencing a state-dependent resolver is a
	// circular dependency and is rejected.
	//
	// Go templates spread resolver data at top level (e.g. {{ .name }}) and expose CLI
	// parameters under __params (e.g. {{ .__params.project }}).
	//
	// For an Emit target, Inputs are resolved at save time only (resolver data _
	// is always available), so this constraint applies only to the primary backend.
	Inputs map[string]*spec.ValueRef `json:"inputs" yaml:"inputs" doc:"Provider-specific inputs (ValueRef for dynamic resolution)"`

	// SaveOverrides are provider-specific inputs resolved only at save time when
	// resolver data (_) is available. Keys that overlap with Inputs override them
	// at save time. This enables patterns like loading state from a fixed branch
	// (via Inputs) and saving to a resolver-derived branch (via SaveOverrides).
	//
	// At load time, SaveOverrides are completely ignored -- no errors are raised
	// for resolver-dependent expressions. Not applicable to Emit targets, which
	// are save-only already -- an Emit target's Inputs may reference resolvers
	// directly.
	SaveOverrides map[string]*spec.ValueRef `json:"saveOverrides,omitempty" yaml:"saveOverrides,omitempty" doc:"Save-time-only inputs that override Inputs keys"`
}

// ParameterProjection narrows which parameters an "intent"-format backend
// projects (see Backend.Parameters and FormatIntent). Include and Exclude are
// mutually exclusive -- lint rejects setting both.
//
// Include is the safer default for a new solution: an unlisted parameter is
// silently dropped from the committed intent, so a newly-added control
// parameter can never leak by omission. Exclude is more practical for a
// solution with a large, evolving parameter surface, where hand-maintaining an
// allowlist would be a constant maintenance burden -- there, denying the small,
// stable set of non-domain parameters (e.g. a run-mode switch) is the
// tractable list to keep current.
//
// A name in Include that never appears in the saved parameters is silently
// ignored (not an error): which parameters are actually present legitimately
// varies run to run (a solution may accept optional parameters), so an
// allowlist naming one that happens to be absent this run is not a mistake.
type ParameterProjection struct {
	// Include, when set, is an allowlist: only these parameter names are
	// projected. All other saved parameters are dropped.
	Include []string `json:"include,omitempty" yaml:"include,omitempty" doc:"Allowlist: only these parameter names are projected" maxItems:"200"`

	// Exclude, when set, is a denylist: every saved parameter is projected
	// except these names.
	Exclude []string `json:"exclude,omitempty" yaml:"exclude,omitempty" doc:"Denylist: every saved parameter is projected except these names" maxItems:"200"`
}

// EmitTarget is one additional, save-only projected state emission. It embeds
// Backend (provider, format, inputs, saveOverrides) and adds a per-target
// enable condition.
type EmitTarget struct {
	Backend `json:",inline" yaml:",inline"`

	// Enabled gates this emission independent of the primary Config.Enabled.
	// Resolved at save time, so it may reference ANY resolver (unlike the
	// primary Config.Enabled, which is load-time and restricted to
	// state-independent resolvers): all resolvers have run by save time. CEL
	// expressions have __params (CLI parameters) and _ (resolver outputs)
	// available; Go templates spread resolver data at top level and expose
	// __params. Coerced to bool; absent defaults to true (always emit).
	Enabled *spec.ValueRef `json:"enabled,omitempty" yaml:"enabled,omitempty" doc:"Per-emit condition, resolved at save time (default: always emit)"`
}

// Data is the complete persisted state structure.
// It is serialized as JSON to the backend storage.
type Data struct {
	// SchemaVersion enables forward-compatible format migrations.
	SchemaVersion int `json:"schemaVersion" doc:"Format version for migrations"`

	// Metadata identifies the solution and tracks timestamps.
	Metadata Metadata `json:"metadata" doc:"Solution identity and timestamps"`

	// Command captures the most recent invocation for validation replay.
	Command CommandInfo `json:"command" doc:"Most recent invocation"`

	// Parameters stores the merged parameter set used for replay. On each run,
	// CLI parameters are merged into this map (CLI wins on conflict, new keys added).
	// When no CLI params are provided, these saved parameters drive replay.
	Parameters map[string]any `json:"parameters" doc:"Merged parameter set for replay"`

	// Resolvers maps resolver names to their persisted outputs. Resolvers marked
	// persist: true have their value recorded here after each successful run for
	// later retrieval via the state provider. Resolvers marked immutable: true are
	// also stored here (with Immutable set) and verified on subsequent runs.
	Resolvers map[string]*PersistedEntry `json:"resolvers" doc:"Persisted resolver values"`

	// Fingerprints stores file and input hashes for action up-to-date checks.
	// Keys use the format "__fingerprint:<actionName>:<type>".
	Fingerprints map[string]*FingerprintEntry `json:"fingerprints" doc:"Action fingerprint hashes"`

	// Attestation carries an optional producer attestation supplied alongside an
	// intent document -- for example the authenticated principal, its issuing
	// identity provider, and a digest of the bytes that were signed.
	//
	// scafctl never interprets, validates, or verifies this value. It is stored
	// verbatim and round-tripped across load, projection (see FormatIntent), and
	// save so that a downstream verifier (a pipeline, a policy gate) can read it
	// back unchanged. Keeping it opaque means the attestation schema is owned by
	// whoever produces it and can evolve without a scafctl change.
	//
	// Any cryptographic signature over the state document is expected to be
	// detached (stored beside the file, not embedded here): a signature cannot
	// cover bytes that contain the signature itself.
	Attestation json.RawMessage `json:"attestation,omitempty" doc:"Opaque producer attestation, stored verbatim and never interpreted by scafctl"`
}

// Metadata identifies the solution and tracks state lifecycle timestamps.
type Metadata struct {
	// Solution is the solution name from metadata.name.
	Solution string `json:"solution" doc:"Solution name from metadata.name" maxLength:"253"`

	// Version is the solution semver string.
	Version string `json:"version" doc:"Solution semver" maxLength:"30"`

	// CreatedAt is when the state file was first created.
	CreatedAt time.Time `json:"createdAt" doc:"First state file creation"`

	// LastUpdatedAt is when the state file was most recently saved.
	LastUpdatedAt time.Time `json:"lastUpdatedAt" doc:"Most recent save"`

	// Runtime records the execution provenance that last wrote the state,
	// separating the engine (library) identity from the invoking CLI/frontend.
	Runtime Runtime `json:"runtime" doc:"Execution provenance that last wrote the state"`
}

// Runtime records execution provenance: which engine performed the run and
// which CLI/frontend invoked it. When scafctl is run directly (not embedded),
// CLI mirrors Engine.
type Runtime struct {
	// Engine identifies the execution engine (the scafctl library) that
	// performed the run.
	Engine RuntimeComponent `json:"engine" doc:"Execution engine (scafctl library) identity"`

	// CLI identifies the CLI/frontend that invoked execution. For embedded
	// runners this is the wrapper binary; for direct scafctl use it mirrors
	// Engine.
	CLI RuntimeComponent `json:"cli" doc:"Invoking CLI/frontend identity"`
}

// RuntimeComponent identifies a named, versioned runtime participant.
type RuntimeComponent struct {
	// Name is the component's binary/identifier name.
	Name string `json:"name" doc:"Component name" maxLength:"64" example:"scafctl"`

	// Version is the component's version string. May be empty when unknown.
	Version string `json:"version" doc:"Component version" maxLength:"64" example:"0.5.0"`
}

// CommandInfo captures the most recent invocation for validation replay.
// Only the latest invocation is stored -- no history.
type CommandInfo struct {
	// Subcommand is the CLI subcommand used (e.g., "run solution").
	Subcommand string `json:"subcommand" doc:"CLI subcommand used" maxLength:"100" example:"run solution"`

	// Parameters are the key-value pairs from --parameter flags for the most recent run.
	Parameters map[string]string `json:"parameters" doc:"Key-value pairs from --parameter flags"`
}

// PersistedEntry is a resolver value persisted in state. Persist-only entries
// are overwritten on each run; immutable entries (Immutable set) are locked on
// first write and verified thereafter.
type PersistedEntry struct {
	// Value is the persisted resolver value.
	Value any `json:"value" doc:"Persisted resolver value"`

	// Type is the resolver's declared type.
	Type string `json:"type" doc:"Resolver declared type" maxLength:"30" example:"string"`

	// Immutable discriminates immutable entries (locked and verified) from
	// persist-only entries (overwritten each run).
	Immutable bool `json:"immutable,omitempty" doc:"Whether the entry is locked and verified across runs"`

	// CreatedAt is when the entry was first stored and never changes thereafter.
	// For entries created immutable this is the lock time. For an entry promoted
	// from persist-only to immutable it is the original persist time, not the
	// lock time.
	CreatedAt time.Time `json:"createdAt" doc:"When the value was first stored"`

	// UpdatedAt is when the entry was last written. For persist-only entries it
	// advances each run. For entries created immutable it equals CreatedAt. For
	// an entry promoted from persist-only to immutable it is the promotion (lock)
	// time and is later than CreatedAt.
	UpdatedAt time.Time `json:"updatedAt" doc:"When the value was last written"`
}

// FingerprintEntry stores a fingerprint hash for action up-to-date checks.
type FingerprintEntry struct {
	// Value is the stored hash string.
	Value string `json:"value" doc:"Stored hash value"`

	// UpdatedAt is when this fingerprint was last written.
	UpdatedAt time.Time `json:"updatedAt" doc:"When this fingerprint was last written"`
}

// NewData returns an initialized empty StateData with the current schema version.
func NewData() *Data {
	return &Data{
		SchemaVersion: SchemaVersionCurrent,
		Parameters:    make(map[string]any),
		Resolvers:     make(map[string]*PersistedEntry),
		Fingerprints:  make(map[string]*FingerprintEntry),
		Command: CommandInfo{
			Parameters: make(map[string]string),
		},
	}
}
