// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectState_FullFormat(t *testing.T) {
	t.Parallel()

	sd := NewMockData("deploy-app", "1.4.2", map[string]any{"env": "prod"})
	sd.Resolvers["cluster_id"] = &PersistedEntry{Value: "abc", Type: "string", Immutable: true}
	sd.Fingerprints["__fingerprint:build:sources"] = &FingerprintEntry{Value: "sha256:xyz"}

	m, err := projectState(sd, Backend{Format: FormatFull})
	require.NoError(t, err)

	assert.Equal(t, "deploy-app", m["metadata"].(map[string]any)["solution"])
	assert.Contains(t, m, "resolvers", "full projection must retain resolvers")
	assert.Contains(t, m, "fingerprints", "full projection must retain fingerprints")
	assert.Contains(t, m, "command", "full projection must retain command")
}

func TestProjectState_EmptyFormatMeansFull(t *testing.T) {
	t.Parallel()

	sd := NewMockData("deploy-app", "1.4.2", nil)
	sd.Resolvers["x"] = &PersistedEntry{Value: "y", Type: "string"}

	m, err := projectState(sd, Backend{})
	require.NoError(t, err)
	assert.Contains(t, m, "resolvers", "an empty format string must behave as FormatFull")
}

func TestProjectState_IntentFormat_OmitsVolatileAndDerivedFields(t *testing.T) {
	t.Parallel()

	sd := NewMockData("deploy-app", "1.4.2", map[string]any{"env": "prod"})
	sd.Resolvers["cluster_id"] = &PersistedEntry{Value: "abc", Type: "string", Immutable: true}
	sd.Fingerprints["__fingerprint:build:sources"] = &FingerprintEntry{Value: "sha256:xyz"}
	sd.Command.Subcommand = "run solution"

	m, err := projectState(sd, Backend{Format: FormatIntent})
	require.NoError(t, err)

	assert.NotContains(t, m, "resolvers", "intent must omit resolver locks")
	assert.NotContains(t, m, "fingerprints", "intent must omit action fingerprints")
	assert.NotContains(t, m, "command", "intent must omit the command block")

	meta, ok := m["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "deploy-app", meta["solution"])
	assert.Equal(t, "1.4.2", meta["version"])
	assert.NotContains(t, meta, "createdAt", "intent metadata must omit volatile timestamps")
	assert.NotContains(t, meta, "lastUpdatedAt", "intent metadata must omit volatile timestamps")
	assert.NotContains(t, meta, "runtime", "intent metadata must omit the runtime provenance block")

	params, ok := m["parameters"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "prod", params["env"])

	assert.Equal(t, float64(SchemaVersionCurrent), m["schemaVersion"])
}

func TestProjectState_IntentFormat_AttestationCarriedWhenPresent(t *testing.T) {
	t.Parallel()

	sd := NewMockData("deploy-app", "1.4.2", nil)
	sd.Attestation = json.RawMessage(`{"principal":"svc-deployer"}`)

	m, err := projectState(sd, Backend{Format: FormatIntent})
	require.NoError(t, err)

	att, ok := m["attestation"].(map[string]any)
	require.True(t, ok, "attestation must be carried through the intent projection")
	assert.Equal(t, "svc-deployer", att["principal"])
}

func TestProjectState_IntentFormat_AttestationOmittedWhenAbsent(t *testing.T) {
	t.Parallel()

	sd := NewMockData("deploy-app", "1.4.2", nil)

	m, err := projectState(sd, Backend{Format: FormatIntent})
	require.NoError(t, err)
	assert.NotContains(t, m, "attestation", "attestation must be omitted from the projection when absent")
}

func TestProjectState_UnknownFormat(t *testing.T) {
	t.Parallel()

	sd := NewMockData("deploy-app", "1.4.2", nil)
	_, err := projectState(sd, Backend{Format: "unknown"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown backend format")
	assert.Contains(t, err.Error(), "unknown")
}

// TestProjectState_IntentFormat_Idempotent verifies the design guarantee that
// underlies signing an intent file: projecting the same parameters twice (as
// happens when a run replays an unchanged intent) produces byte-identical
// JSON, because the intent projection carries no timestamps or resolver/
// fingerprint state that would drift between runs.
func TestProjectState_IntentFormat_Idempotent(t *testing.T) {
	t.Parallel()

	sd1 := NewMockData("deploy-app", "1.4.2", map[string]any{"env": "prod", "region": "us-east-1"})
	m1, err := projectState(sd1, Backend{Format: FormatIntent})
	require.NoError(t, err)
	b1, err := json.Marshal(m1)
	require.NoError(t, err)

	// A second, independently-constructed Data with the same identity and
	// parameters but different volatile fields (a later timestamp, a
	// different persisted resolver value) must project to identical bytes.
	sd2 := NewMockData("deploy-app", "1.4.2", map[string]any{"env": "prod", "region": "us-east-1"})
	sd2.Metadata.CreatedAt = sd2.Metadata.CreatedAt.Add(time.Hour)
	sd2.Resolvers["unrelated"] = &PersistedEntry{Value: "differs", Type: "string"}
	m2, err := projectState(sd2, Backend{Format: FormatIntent})
	require.NoError(t, err)
	b2, err := json.Marshal(m2)
	require.NoError(t, err)

	assert.Equal(t, string(b1), string(b2), "intent projection must be byte-identical across volatile-field drift (Go marshals map[string]any with sorted keys, so this is the strict form of the idempotency guarantee)")
}

func TestProjectState_Narrowing_NilSpecProjectsEverything(t *testing.T) {
	t.Parallel()

	sd := NewMockData("deploy-app", "1.4.2", map[string]any{"appName": "hello", "mode": "publish"})
	m, err := projectState(sd, Backend{Format: FormatIntent})
	require.NoError(t, err)

	params, ok := m["parameters"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "hello", params["appName"])
	assert.Equal(t, "publish", params["mode"], "no Parameters spec means no narrowing")
}

func TestProjectState_Narrowing_Include(t *testing.T) {
	t.Parallel()

	sd := NewMockData("deploy-app", "1.4.2", map[string]any{
		"appName": "hello", "environment": "sandbox", "mode": "publish", "githubOwner": "acme",
	})
	m, err := projectState(sd, Backend{
		Format:     FormatIntent,
		Parameters: &ParameterProjection{Include: []string{"appName", "environment"}},
	})
	require.NoError(t, err)

	params, ok := m["parameters"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"appName": "hello", "environment": "sandbox"}, params)
}

func TestProjectState_Narrowing_Exclude(t *testing.T) {
	t.Parallel()

	sd := NewMockData("deploy-app", "1.4.2", map[string]any{
		"appName": "hello", "environment": "sandbox", "mode": "publish", "githubOwner": "acme",
	})
	m, err := projectState(sd, Backend{
		Format:     FormatIntent,
		Parameters: &ParameterProjection{Exclude: []string{"mode", "githubOwner"}},
	})
	require.NoError(t, err)

	params, ok := m["parameters"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"appName": "hello", "environment": "sandbox"}, params)
}

func TestProjectState_Narrowing_IncludeNameAbsentFromParams_SilentlyOmitted(t *testing.T) {
	t.Parallel()

	sd := NewMockData("deploy-app", "1.4.2", map[string]any{"appName": "hello"})
	m, err := projectState(sd, Backend{
		Format:     FormatIntent,
		Parameters: &ParameterProjection{Include: []string{"appName", "neverPassedThisRun"}},
	})
	require.NoError(t, err)

	params, ok := m["parameters"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"appName": "hello"}, params, "an Include name absent from saved parameters is silently omitted, not an error")
}

func TestProjectState_Narrowing_EmptySpecIsNoOp(t *testing.T) {
	t.Parallel()

	sd := NewMockData("deploy-app", "1.4.2", map[string]any{"appName": "hello", "mode": "publish"})
	m, err := projectState(sd, Backend{Format: FormatIntent, Parameters: &ParameterProjection{}})
	require.NoError(t, err)

	params, ok := m["parameters"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"appName": "hello", "mode": "publish"}, params, "a Parameters spec with neither Include nor Exclude set is a no-op")
}

func TestProjectState_Narrowing_FullFormatIgnoresParameters(t *testing.T) {
	t.Parallel()

	// Backend.Parameters only has an effect under FormatIntent; a Full-format
	// backend must not be affected by it even if somehow set (lint rejects
	// this combination at the solution level -- this test verifies the
	// runtime behavior independent of that lint gate).
	sd := NewMockData("deploy-app", "1.4.2", map[string]any{"appName": "hello", "mode": "publish"})
	sd.Resolvers["x"] = &PersistedEntry{Value: "y", Type: "string"}

	m, err := projectState(sd, Backend{
		Format:     FormatFull,
		Parameters: &ParameterProjection{Include: []string{"appName"}},
	})
	require.NoError(t, err)

	assert.Contains(t, m, "resolvers", "full format must still project resolvers regardless of a Parameters spec")
	params, ok := m["parameters"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "publish", params["mode"], "full format must not apply narrowing")
}

// TestProjectState_Narrowing_BothIncludeAndExcludeIsRejected verifies
// projectState enforces the include/exclude mutual-exclusivity invariant
// itself (not only lint): a hand-constructed Backend that never went through
// lint -- an embedder, or any future internal caller -- must not silently get
// Include-wins-over-Exclude behavior.
func TestProjectState_Narrowing_BothIncludeAndExcludeIsRejected(t *testing.T) {
	t.Parallel()

	sd := NewMockData("deploy-app", "1.4.2", map[string]any{"appName": "hello", "mode": "publish"})
	_, err := projectState(sd, Backend{
		Format: FormatIntent,
		Parameters: &ParameterProjection{
			Include: []string{"appName"},
			Exclude: []string{"mode"},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot set both include and exclude")
}

func TestNarrowParameters(t *testing.T) {
	t.Parallel()

	params := map[string]any{"a": 1, "b": 2, "c": 3}

	t.Run("nil spec returns the original map unchanged", func(t *testing.T) {
		t.Parallel()
		got := narrowParameters(params, nil)
		assert.Equal(t, params, got)
	})

	t.Run("include intersects", func(t *testing.T) {
		t.Parallel()
		got := narrowParameters(params, &ParameterProjection{Include: []string{"a", "c", "missing"}})
		assert.Equal(t, map[string]any{"a": 1, "c": 3}, got)
	})

	t.Run("exclude differences", func(t *testing.T) {
		t.Parallel()
		got := narrowParameters(params, &ParameterProjection{Exclude: []string{"b"}})
		assert.Equal(t, map[string]any{"a": 1, "c": 3}, got)
	})

	t.Run("empty params with an include spec returns an empty map, not nil", func(t *testing.T) {
		t.Parallel()
		got := narrowParameters(map[string]any{}, &ParameterProjection{Include: []string{"a"}})
		assert.NotNil(t, got)
		assert.Empty(t, got)
	})
}
