// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
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
// All state load paths (file store, backend providers, and provider result
// extraction) route through this helper so the version guard is enforced
// consistently across every backend.
func DecodeData(raw []byte) (*Data, error) {
	var probe struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("read state schema version: %w", err)
	}

	if err := validateSchemaVersion(probe.SchemaVersion); err != nil {
		return nil, err
	}

	var sd Data
	if err := json.Unmarshal(raw, &sd); err != nil {
		return nil, fmt.Errorf("unmarshal state data: %w", err)
	}

	normalizeData(&sd)
	return &sd, nil
}

// validateSchemaVersion enforces the supported schema-version bounds, returning
// an actionable error for out-of-range versions. It is applied both when
// decoding raw JSON (DecodeData) and when a backend provider returns an
// already-decoded *Data, so the version floor is enforced at every trust
// boundary.
func validateSchemaVersion(version int) error {
	if version > SchemaVersionCurrent {
		return fmt.Errorf("%w: file version %d is newer than supported version %d; upgrade scafctl",
			ErrUnsupportedSchemaVersion, version, SchemaVersionCurrent)
	}
	if version < SchemaVersionMinimum {
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
