// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"encoding/json"
	"fmt"
)

// Intent is the lean, replay-relevant projection of a state Data document: just
// enough to reproduce a run (solution identity + parameters), plus any opaque
// attestation riding along. It is the shape produced by projectState when
// format is FormatIntent, and it is what a solution author commits and signs.
//
// Intent deliberately omits Command, Resolvers, and Fingerprints (derived /
// volatile), and the volatile Metadata sub-fields (CreatedAt, LastUpdatedAt,
// Runtime) -- keeping only the solution identity. This is what makes a
// re-projected intent byte-identical run over run when the underlying
// parameters are unchanged: the document carries no timestamps or build
// version stamps for a signature to trip over.
type Intent struct {
	// SchemaVersion mirrors the state schema version this intent is compatible
	// with, so an intent document loads cleanly as state input (--state-file)
	// without a version mismatch.
	SchemaVersion int `json:"schemaVersion" doc:"State schema version this intent is compatible with"`

	// Metadata carries only the solution identity (name/version) -- the
	// volatile timestamp and runtime fields on the full Metadata are omitted.
	Metadata IntentMetadata `json:"metadata" doc:"Solution identity"`

	// Parameters is the merged parameter set for replay, identical in shape to
	// Data.Parameters.
	Parameters map[string]any `json:"parameters" doc:"Merged parameter set for replay"`

	// Attestation is Data.Attestation, carried through unchanged. Omitted from
	// the projection when absent.
	Attestation json.RawMessage `json:"attestation,omitempty" doc:"Opaque producer attestation, carried through unchanged"`
}

// IntentMetadata is the solution-identity subset of Metadata carried in an
// Intent projection.
type IntentMetadata struct {
	// Solution is the solution name from metadata.name.
	Solution string `json:"solution" doc:"Solution name from metadata.name"`

	// Version is the solution semver string.
	Version string `json:"version" doc:"Solution semver"`
}

// projectState converts a state Data document into the shape a backend's
// state_save should receive, according to format. It is the single place that
// defines what each Backend.Format value means; both the primary backend and
// every Config.Emit target route their save payload through it.
//
// The return value is always a map[string]any (via a JSON round-trip through
// structToMap), because the provider executor's JSON-schema validator can only
// inspect map/JSON values, never a Go struct pointer directly.
//
// An empty format string is treated as FormatFull, matching the zero value of
// Backend.Format (so an unset Format behaves exactly as state behaved before
// Format existed).
func projectState(d *Data, format string) (map[string]any, error) {
	switch format {
	case "", FormatFull:
		return structToMap(d)
	case FormatIntent:
		intent := Intent{
			SchemaVersion: d.SchemaVersion,
			Metadata: IntentMetadata{
				Solution: d.Metadata.Solution,
				Version:  d.Metadata.Version,
			},
			Parameters:  d.Parameters,
			Attestation: d.Attestation,
		}
		return structToMap(intent)
	default:
		return nil, fmt.Errorf("state: unknown backend format %q (valid: %q, %q)", format, FormatFull, FormatIntent)
	}
}
