// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"time"

	"github.com/oakwood-commons/scafctl/pkg/spec"
)

const (
	// SchemaVersionCurrent is the current state file schema version.
	SchemaVersionCurrent = 1
)

// Config is the solution-level state configuration.
// It is a top-level peer to Spec, Catalog, Bundle, and Compose on the Solution struct.
type Config struct {
	// Enabled controls whether state persistence is active. Supports literal bool, CEL, resolver ref, or template.
	Enabled *spec.ValueRef `json:"enabled" yaml:"enabled" doc:"Dynamic activation of state persistence"`

	// Backend configures which provider handles state persistence.
	Backend Backend `json:"backend" yaml:"backend" doc:"Backend provider configuration"`
}

// Backend configures the state persistence backend.
type Backend struct {
	// Provider is the name of a registered provider with CapabilityState (e.g., "file").
	Provider string `json:"provider" yaml:"provider" doc:"Provider name with CapabilityState" maxLength:"253" example:"file"`

	// Inputs are provider-specific inputs. Each value is a ValueRef for dynamic resolution.
	Inputs map[string]*spec.ValueRef `json:"inputs" yaml:"inputs" doc:"Provider-specific inputs (ValueRef for dynamic resolution)"`
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

	// Values maps resolver names to persisted entries.
	Values map[string]*Entry `json:"values" doc:"Resolver name to persisted entry"`
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

	// Parameters are the key-value pairs from --parameter flags.
	Parameters map[string]string `json:"parameters" doc:"Key-value pairs from --parameter flags"`
}

// Entry is a single persisted resolver value.
type Entry struct {
	// Value is the stored resolver value.
	Value any `json:"value" doc:"Stored resolver value"`

	// Type is the resolver's declared type (string, int, float, bool, array, object, any).
	Type string `json:"type" doc:"Resolver declared type" maxLength:"30" example:"string"`

	// UpdatedAt is when this entry was last written.
	UpdatedAt time.Time `json:"updatedAt" doc:"When this entry was last written"`

	// Immutable indicates whether this entry is locked permanently (future enhancement).
	Immutable bool `json:"immutable" doc:"Locked permanently (future enhancement)"`
}

// NewData returns an initialized empty StateData with the current schema version.
func NewData() *Data {
	return &Data{
		SchemaVersion: SchemaVersionCurrent,
		Values:        make(map[string]*Entry),
		Command: CommandInfo{
			Parameters: make(map[string]string),
		},
	}
}
