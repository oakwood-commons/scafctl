// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// DecodeData validates the schema version of raw state JSON before unmarshalling
// it into a *Data. The schema version is peeked from the raw bytes and bounds-
// checked first, so a state file whose schemaVersion is outside the supported
// range produces the actionable ErrUnsupportedSchemaVersion /
// ErrIncompatibleSchemaVersion error rather than a cryptic Go reflection error
// from a type-incompatible field aborting the strict full-struct decode.
//
// A contentless payload -- empty bytes, whitespace only, JSON null, or an empty
// object -- means "no state written yet" (a first run), not a version-0 file.
// Such a payload is returned as fresh empty state so a backend that reports an
// absent object with an empty document does not trip the schema-version floor.
// A document that carries any structure still goes through the full version
// guard, so a genuine v0/v1 file is rejected rather than silently accepted
// (which would discard immutable locks).
//
// All state load paths (file store, backend providers, and provider result
// extraction) route through this helper so the version guard is enforced
// consistently across every backend.
func DecodeData(raw []byte) (*Data, error) {
	if isContentless(raw) {
		return NewData(), nil
	}

	var probe struct {
		SchemaVersion *int `json:"schemaVersion"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("read state schema version: %w", err)
	}

	version := 0
	explicit := probe.SchemaVersion != nil
	if explicit {
		version = *probe.SchemaVersion
	}

	if err := validateSchemaVersion(version, explicit); err != nil {
		return nil, err
	}

	var sd Data
	if err := json.Unmarshal(raw, &sd); err != nil {
		return nil, fmt.Errorf("unmarshal state data: %w", err)
	}

	normalizeData(&sd)
	return &sd, nil
}

// isContentless reports whether raw is a "no state written yet" payload: empty
// bytes, whitespace only, JSON null, or an empty JSON object. Those payloads
// decode to a zero-value document whose schemaVersion defaults to 0; treating
// them as fresh empty state (rather than a version-0 file) lets a first run
// proceed when a backend reports an absent object with an empty document.
//
// A payload with any JSON keys is NOT contentless even when those keys are
// empty (e.g. {"parameters":{}}): it is a structured document and must go
// through the schema-version guard so a genuine old file is rejected rather
// than silently accepted.
func isContentless(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return true
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err == nil && len(obj) == 0 {
		// Covers both JSON null (nil map) and an empty object {}.
		return true
	}
	return false
}

// validateSchemaVersion enforces the supported schema-version bounds, returning
// an actionable error for out-of-range versions. It is applied both when
// decoding raw JSON (DecodeData) and when a backend provider returns an
// already-decoded *Data, so the version floor is enforced at every trust
// boundary. explicit reports whether the document carried an explicit
// schemaVersion field, which sharpens the message for older files.
func validateSchemaVersion(version int, explicit bool) error {
	if version > SchemaVersionCurrent {
		return fmt.Errorf("%w: file version %d is newer than supported version %d; upgrade scafctl",
			ErrUnsupportedSchemaVersion, version, SchemaVersionCurrent)
	}
	if version < SchemaVersionMinimum {
		if !explicit {
			return fmt.Errorf("%w: state document has content but no schemaVersion field (minimum supported version is %d); delete the state file and recreate it",
				ErrIncompatibleSchemaVersion, SchemaVersionMinimum)
		}
		return fmt.Errorf("%w: file version %d is older than the minimum supported version %d; delete the state file and recreate it",
			ErrIncompatibleSchemaVersion, version, SchemaVersionMinimum)
	}
	return nil
}

// normalizeData initializes the nil maps on a decoded *Data so callers can read
// and write entries without nil-map panics.
func normalizeData(sd *Data) {
	if sd.Parameters == nil {
		sd.Parameters = make(map[string]any)
	}
	if sd.Resolvers == nil {
		sd.Resolvers = make(map[string]*PersistedEntry)
	}
	if sd.Fingerprints == nil {
		sd.Fingerprints = make(map[string]*FingerprintEntry)
	}
	if sd.Command.Parameters == nil {
		sd.Command.Parameters = make(map[string]string)
	}
}

// isEmptyData reports whether a decoded *Data carries no persisted content and
// no explicit schema version -- the zero-value document a backend may return
// in-process to signal "no state yet". It mirrors isContentless for the
// direct-pointer path so an absent object is treated as fresh state rather than
// a version-0 file. A document with any content (or a nonzero schema version)
// is not empty and goes through the schema-version guard.
func isEmptyData(sd *Data) bool {
	return sd != nil &&
		sd.SchemaVersion == 0 &&
		len(sd.Parameters) == 0 &&
		len(sd.Resolvers) == 0 &&
		len(sd.Fingerprints) == 0 &&
		sd.Metadata == (Metadata{}) &&
		sd.Command.Subcommand == "" &&
		len(sd.Command.Parameters) == 0
}
