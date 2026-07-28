// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeData_ValidRoundTrip(t *testing.T) {
	t.Parallel()
	raw := fmt.Appendf(nil, `{"schemaVersion":%d,"parameters":{"env":"prod"}}`, SchemaVersionCurrent)

	sd, err := DecodeData(raw)
	require.NoError(t, err)
	assert.Equal(t, SchemaVersionCurrent, sd.SchemaVersion)
	assert.Equal(t, "prod", sd.Parameters["env"])
	// Nil maps are normalized so callers can write without panicking.
	assert.NotNil(t, sd.Resolvers)
	assert.NotNil(t, sd.Fingerprints)
	assert.NotNil(t, sd.Command.Parameters)
}

func TestDecodeData_ContentlessIsFreshState(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"empty":         ``,
		"whitespace":    "  \n\t ",
		"json null":     `null`,
		"empty object":  `{}`,
		"padded object": "  { }  ",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sd, err := DecodeData([]byte(raw))
			require.NoError(t, err)
			assert.Equal(t, SchemaVersionCurrent, sd.SchemaVersion)
			assert.NotNil(t, sd.Parameters)
			assert.NotNil(t, sd.Resolvers)
			assert.NotNil(t, sd.Fingerprints)
			assert.NotNil(t, sd.Command.Parameters)
		})
	}
}

// TestDecodeData_StructuredEmptyStillGuarded verifies that a document with keys
// but no schemaVersion is NOT treated as contentless -- it must still be
// rejected so a genuine old file is not silently accepted.
func TestDecodeData_StructuredEmptyStillGuarded(t *testing.T) {
	t.Parallel()
	_, err := DecodeData([]byte(`{"parameters":{}}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrIncompatibleSchemaVersion)
}

func TestDecodeData_NewerVersionUnsupported(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"schemaVersion":999,"parameters":{}}`)

	_, err := DecodeData(raw)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedSchemaVersion)
	assert.Contains(t, err.Error(), "999")
	assert.Contains(t, err.Error(), "upgrade scafctl")
}

func TestDecodeData_OlderVersionIncompatible(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"schemaVersion":1,"parameters":{}}`)

	_, err := DecodeData(raw)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrIncompatibleSchemaVersion)
	assert.Contains(t, err.Error(), "delete the state file")
}

func TestDecodeData_MissingVersionIncompatible(t *testing.T) {
	t.Parallel()
	// A file predating the schemaVersion field decodes to 0, below the minimum.
	raw := []byte(`{"parameters":{}}`)

	_, err := DecodeData(raw)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrIncompatibleSchemaVersion)
}

// TestDecodeData_OutOfRangeWinsOverTypeMismatch is the crux of the fix: an
// out-of-range file whose layout is also structurally incompatible with the
// current struct must surface the actionable version error, NOT a raw Go
// reflection error from the strict full-struct decode.
func TestDecodeData_OutOfRangeWinsOverTypeMismatch(t *testing.T) {
	t.Parallel()
	// schemaVersion below the floor AND metadata.solution is an object where the
	// current struct expects a string.
	raw := []byte(`{"schemaVersion":1,"metadata":{"solution":{"nested":"object"}}}`)

	_, err := DecodeData(raw)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrIncompatibleSchemaVersion)
	assert.NotContains(t, err.Error(), "cannot unmarshal")
}

func TestDecodeData_InvalidJSON(t *testing.T) {
	t.Parallel()
	_, err := DecodeData([]byte(`{not valid json`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read state schema version")
}

func TestDecodeData_InRangeTypeMismatchStillRawError(t *testing.T) {
	t.Parallel()
	// A file claiming a supported version but malformed still fails at the
	// strict decode -- the version guard only rescues out-of-range files.
	raw := fmt.Appendf(nil, `{"schemaVersion":%d,"metadata":{"solution":{"nested":"object"}}}`, SchemaVersionCurrent)

	_, err := DecodeData(raw)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrIncompatibleSchemaVersion)
	assert.NotErrorIs(t, err, ErrUnsupportedSchemaVersion)
	assert.Contains(t, err.Error(), "unmarshal state data")
}

func TestValidateSchemaVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		version  int
		explicit bool
		wantErr  error
	}{
		{name: "current", version: SchemaVersionCurrent, explicit: true, wantErr: nil},
		{name: "minimum", version: SchemaVersionMinimum, explicit: true, wantErr: nil},
		{name: "too new", version: SchemaVersionCurrent + 1, explicit: true, wantErr: ErrUnsupportedSchemaVersion},
		{name: "too old", version: SchemaVersionMinimum - 1, explicit: true, wantErr: ErrIncompatibleSchemaVersion},
		{name: "zero explicit", version: 0, explicit: true, wantErr: ErrIncompatibleSchemaVersion},
		{name: "zero implicit", version: 0, explicit: false, wantErr: ErrIncompatibleSchemaVersion},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateSchemaVersion(tt.version, tt.explicit)
			if tt.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

// TestValidateSchemaVersion_MessageDistinguishesMissingField verifies the
// message differs for a document that carries an explicit older version versus
// one that predates the schemaVersion field entirely.
func TestValidateSchemaVersion_MessageDistinguishesMissingField(t *testing.T) {
	t.Parallel()

	explicitErr := validateSchemaVersion(1, true)
	require.Error(t, explicitErr)
	assert.Contains(t, explicitErr.Error(), "file version 1")

	implicitErr := validateSchemaVersion(0, false)
	require.Error(t, implicitErr)
	assert.Contains(t, implicitErr.Error(), "no schemaVersion field")
}

func TestIsEmptyData(t *testing.T) {
	t.Parallel()

	assert.True(t, isEmptyData(&Data{}), "zero-value document is empty")

	assert.False(t, isEmptyData(nil), "nil is not a fresh document")
	assert.False(t, isEmptyData(NewData()), "NewData has a nonzero schema version")

	withParam := &Data{Parameters: map[string]any{"env": "prod"}}
	assert.False(t, isEmptyData(withParam), "content makes it non-empty")

	withMeta := &Data{Metadata: Metadata{Solution: "app"}}
	assert.False(t, isEmptyData(withMeta), "metadata makes it non-empty")
}
