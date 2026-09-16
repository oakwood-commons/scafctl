// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"encoding/json"
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

// TestDecodeData_AttestationPreserved verifies that an opaque attestation field
// supplied on an intent document survives decoding verbatim, so a downstream
// verifier can read it back unchanged. scafctl must never interpret it.
func TestDecodeData_AttestationPreserved(t *testing.T) {
	t.Parallel()
	raw := fmt.Appendf(nil,
		`{"schemaVersion":%d,"parameters":{"env":"prod"},"attestation":{"principal":"svc-deployer","issuer":"https://issuer.example.com","digest":"sha256:abc"}}`,
		SchemaVersionCurrent)

	sd, err := DecodeData(raw)
	require.NoError(t, err)
	require.NotEmpty(t, sd.Attestation, "attestation must be preserved on decode")

	var att map[string]any
	require.NoError(t, json.Unmarshal(sd.Attestation, &att))
	assert.Equal(t, "svc-deployer", att["principal"])
	assert.Equal(t, "https://issuer.example.com", att["issuer"])
	assert.Equal(t, "sha256:abc", att["digest"])
}

// TestDecodeData_AttestationAbsentIsEmpty verifies a document without an
// attestation decodes to an empty (not spuriously populated) field.
func TestDecodeData_AttestationAbsentIsEmpty(t *testing.T) {
	t.Parallel()
	raw := fmt.Appendf(nil, `{"schemaVersion":%d,"parameters":{"env":"prod"}}`, SchemaVersionCurrent)

	sd, err := DecodeData(raw)
	require.NoError(t, err)
	assert.Empty(t, sd.Attestation)
}

// TestData_AttestationRoundTrip verifies the attestation survives a full
// marshal -> decode cycle, mirroring the load -> regenerate-full-state path a
// run performs when it is pointed at an intent file.
func TestData_AttestationRoundTrip(t *testing.T) {
	t.Parallel()
	original := NewData()
	original.SchemaVersion = SchemaVersionCurrent
	original.Attestation = json.RawMessage(`{"principal":"svc-deployer"}`)

	encoded, err := json.Marshal(original)
	require.NoError(t, err)

	decoded, err := DecodeData(encoded)
	require.NoError(t, err)
	assert.JSONEq(t, `{"principal":"svc-deployer"}`, string(decoded.Attestation))
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
