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

// projectState converts a state Data document into the shape a save target's
// state_save should receive, according to target.Format. It is the single
// place that defines what each SaveTarget.Format value means; every save target
// routes its payload through it.
//
// The return value is always a map[string]any (via a JSON round-trip through
// structToMap), because the provider executor's JSON-schema validator can only
// inspect map/JSON values, never a Go struct pointer directly.
//
// An empty format string is treated as FormatFull, matching the zero value of
// SaveTarget.Format.
func projectState(d *Data, target SaveTarget) (map[string]any, error) {
	switch target.Format {
	case "", FormatFull:
		return structToMap(d)
	case FormatIntent:
		if p := target.Parameters; p != nil && len(p.Include) > 0 && len(p.Exclude) > 0 {
			// Defense in depth: lint already rejects this combination for any
			// solution loaded from YAML, but projectState is also reachable
			// from a hand-constructed SaveTarget (an embedder, or future
			// internal caller) that never went through lint. Enforce the
			// invariant here too, at the one place that actually executes it,
			// rather than silently letting Include win.
			return nil, fmt.Errorf("state: save target parameters narrowing cannot set both include and exclude")
		}
		intent := Intent{
			SchemaVersion: d.SchemaVersion,
			Metadata: IntentMetadata{
				Solution: d.Metadata.Solution,
				Version:  d.Metadata.Version,
			},
			Parameters:  narrowParameters(d.Parameters, target.Parameters),
			Attestation: d.Attestation,
		}
		return structToMap(intent)
	default:
		return nil, fmt.Errorf("state: unknown save target format %q (valid: %q, %q)", target.Format, FormatFull, FormatIntent)
	}
}

// narrowParameters applies an intent-format save target's Parameters projection
// spec to a saved parameter set. A nil spec is a no-op (every parameter is
// projected, matching behavior before this field existed). Include is an
// allowlist (intersection); Exclude is a denylist (difference). Callers
// (projectState) guarantee Include and Exclude are not both set -- lint
// rejects that combination for any YAML-authored solution, and projectState
// itself rejects it as a defense-in-depth check before calling this helper --
// so this function does not need to arbitrate between them.
//
// The result is always a fresh map, even when params is empty or the spec
// selects nothing, so callers never observe the original map's identity.
func narrowParameters(params map[string]any, projection *ParameterProjection) map[string]any {
	if projection == nil {
		return params
	}

	result := make(map[string]any, len(params))

	if len(projection.Include) > 0 {
		for _, name := range projection.Include {
			if v, ok := params[name]; ok {
				result[name] = v
			}
		}
		return result
	}

	if len(projection.Exclude) > 0 {
		excluded := make(map[string]bool, len(projection.Exclude))
		for _, name := range projection.Exclude {
			excluded[name] = true
		}
		for k, v := range params {
			if !excluded[k] {
				result[k] = v
			}
		}
		return result
	}

	// Spec present but both lists empty: no-op, matching a nil spec.
	for k, v := range params {
		result[k] = v
	}
	return result
}
