// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"time"

	"github.com/oakwood-commons/scafctl/pkg/spec"
)

const (
	// SchemaVersionCurrent is the state file schema version written by this build.
	SchemaVersionCurrent = 2

	// SchemaVersionMinimum is the oldest state file schema version this build can
	// safely load. Bump this ONLY when a breaking change makes older files unsafe
	// to read -- e.g. schema v1 stored immutable locks under a legacy `immutables`
	// field that is now dropped on unmarshal, so loading a v1 file would silently
	// discard immutable enforcement. Additive, backward-compatible bumps should
	// raise SchemaVersionCurrent WITHOUT raising this floor so older files still load.
	SchemaVersionMinimum = 2
)

// Config is the solution-level state configuration.
// It is a top-level peer to Spec, Catalog, Bundle, and Compose on the Solution struct.
//
// CLI parameters passed via -r flags are available as __params in CEL expressions
// and Go templates used in Enabled and Backend.Inputs. This allows dynamic backend
// configuration without requiring resolver execution (which happens after state load).
//
// Example:
//
//	state:
//	  enabled:
//	    expr: "__params.state_enabled == true"
//	  backend:
//	    provider: file
//	    inputs:
//	      path:
//	        expr: "'gcp/' + __params.project + '/state.json'"
type Config struct {
	// Enabled controls whether state persistence is active. Supports literal bool, CEL expression, or template.
	// Resolver references (rslvr:) are not supported because state is loaded before resolver execution.
	// Use __params to reference CLI parameters (e.g. expr: "__params.enable_state == true").
	Enabled *spec.ValueRef `json:"enabled" yaml:"enabled" doc:"Dynamic activation of state persistence"`

	// Backend configures which provider handles state persistence.
	Backend Backend `json:"backend" yaml:"backend" doc:"Backend provider configuration"`
}

// Backend configures the state persistence backend.
type Backend struct {
	// Provider is the name of a registered provider with CapabilityState (e.g., "file").
	Provider string `json:"provider" yaml:"provider" doc:"Provider name with CapabilityState" maxLength:"253" example:"file"`

	// Inputs are provider-specific inputs. Each value is a ValueRef for dynamic resolution.
	//
	// CEL expressions use __params for CLI parameters (e.g. __params.project) and _ for
	// resolver outputs (available at save time only, not load time).
	//
	// Go templates spread resolver data at top level (e.g. {{ .name }}) and expose CLI
	// parameters under __params (e.g. {{ .__params.project }}).
	Inputs map[string]*spec.ValueRef `json:"inputs" yaml:"inputs" doc:"Provider-specific inputs (ValueRef for dynamic resolution)"`

	// SaveOverrides are provider-specific inputs resolved only at save time when
	// resolver data (_) is available. Keys that overlap with Inputs override them
	// at save time. This enables patterns like loading state from a fixed branch
	// (via Inputs) and saving to a resolver-derived branch (via SaveOverrides).
	//
	// At load time, SaveOverrides are completely ignored -- no errors are raised
	// for resolver-dependent expressions.
	SaveOverrides map[string]*spec.ValueRef `json:"saveOverrides,omitempty" yaml:"saveOverrides,omitempty" doc:"Save-time-only inputs that override Inputs keys"`
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

	// ScafctlVersion is the version of scafctl that last wrote the state.
	ScafctlVersion string `json:"scafctlVersion" doc:"Version of scafctl that last wrote" maxLength:"30"`
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
