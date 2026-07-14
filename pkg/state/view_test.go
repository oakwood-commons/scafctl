// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildListView_Empty(t *testing.T) {
	t.Parallel()
	view := BuildListView(NewData())

	assert.Equal(t, 0, view.EntryCount())

	// The overview section always carries the schema version.
	overview := view.SectionByName("overview")
	require.NotNil(t, overview)
	assert.Equal(t, SchemaVersionCurrent, overview.Fields["schemaVersion"])

	// Metadata and command are always present as key/value sections.
	meta := view.SectionByName("metadata")
	require.NotNil(t, meta)
	assert.Equal(t, SectionKindKeyValue, meta.Kind)
	require.NotNil(t, view.SectionByName("command"))

	// Collection sections exist but hold no rows.
	params := view.SectionByName("parameters")
	require.NotNil(t, params)
	assert.Equal(t, SectionKindRows, params.Kind)
	assert.Empty(t, params.Rows)
	assert.Empty(t, view.SectionByName("resolvers").Rows)
	assert.Empty(t, view.SectionByName("fingerprints").Rows)
}

func TestBuildListView_GroupsSections(t *testing.T) {
	t.Parallel()
	created := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	updated := time.Date(2025, 6, 7, 8, 9, 10, 0, time.UTC)

	sd := NewData()
	sd.Metadata = Metadata{
		Solution:       "demo",
		Version:        "1.2.3",
		ScafctlVersion: "dev",
		CreatedAt:      created,
		LastUpdatedAt:  updated,
	}
	sd.Command = CommandInfo{
		Subcommand: "run resolver",
		Parameters: map[string]string{"token": "alpha"},
	}
	sd.Parameters["env"] = "prod"
	sd.Parameters["count"] = float64(42)
	sd.Resolvers["current_token"] = &PersistedEntry{
		Value:     "alpha",
		Type:      "string",
		CreatedAt: created,
		UpdatedAt: updated,
	}
	sd.Resolvers["locked"] = &PersistedEntry{
		Value:     "secret",
		Type:      "string",
		Immutable: true,
		CreatedAt: created,
		UpdatedAt: created,
	}
	sd.Fingerprints["__fingerprint:build:inputs"] = &FingerprintEntry{
		Value:     "deadbeef",
		UpdatedAt: updated,
	}

	view := BuildListView(sd)

	// Metadata section (key/value, timestamps formatted).
	meta := view.SectionByName("metadata").Fields
	assert.Equal(t, "demo", meta["solution"])
	assert.Equal(t, "1.2.3", meta["version"])
	assert.Equal(t, "dev", meta["scafctlVersion"])
	assert.Equal(t, "2025-01-02T03:04:05Z", meta["createdAt"])
	assert.Equal(t, "2025-06-07T08:09:10Z", meta["lastUpdatedAt"])

	// Command section (nested parameters map passes through as-is).
	cmd := view.SectionByName("command").Fields
	assert.Equal(t, "run resolver", cmd["subcommand"])
	assert.Equal(t, map[string]string{"token": "alpha"}, cmd["parameters"])

	// Parameters are sorted by key.
	params := view.SectionByName("parameters").Rows
	require.Len(t, params, 2)
	assert.Equal(t, "count", params[0]["key"])
	assert.Equal(t, "env", params[1]["key"])

	// Resolvers are sorted by key with entry fields expanded into columns.
	resolvers := view.SectionByName("resolvers").Rows
	require.Len(t, resolvers, 2)
	assert.Equal(t, "current_token", resolvers[0]["key"])
	assert.Equal(t, false, resolvers[0]["immutable"])
	assert.Equal(t, "alpha", resolvers[0]["value"])
	assert.Equal(t, "string", resolvers[0]["type"])
	assert.Equal(t, "2025-06-07T08:09:10Z", resolvers[0]["updatedAt"])
	assert.Equal(t, "locked", resolvers[1]["key"])
	assert.Equal(t, true, resolvers[1]["immutable"])

	// Fingerprints section.
	fingerprints := view.SectionByName("fingerprints").Rows
	require.Len(t, fingerprints, 1)
	assert.Equal(t, "__fingerprint:build:inputs", fingerprints[0]["key"])
	assert.Equal(t, "deadbeef", fingerprints[0]["value"])

	// EntryCount counts collection rows only (params + resolvers + fingerprints).
	assert.Equal(t, 5, view.EntryCount())
}

func TestBuildListView_ZeroTimestampsRenderEmpty(t *testing.T) {
	t.Parallel()
	sd := NewData()
	sd.Resolvers["k"] = &PersistedEntry{Value: "v", Type: "string"}

	view := BuildListView(sd)

	resolvers := view.SectionByName("resolvers").Rows
	require.Len(t, resolvers, 1)
	assert.Equal(t, "", resolvers[0]["createdAt"])
	assert.Equal(t, "", resolvers[0]["updatedAt"])
	assert.Equal(t, "", view.SectionByName("metadata").Fields["createdAt"])
}

func TestSummarize(t *testing.T) {
	t.Parallel()

	t.Run("empty state", func(t *testing.T) {
		t.Parallel()
		s := NewData().Summarize()
		assert.Equal(t, SchemaVersionCurrent, s.SchemaVersion)
		assert.Empty(t, s.Solution)
		assert.Empty(t, s.Version)
		assert.True(t, s.LastUpdated.IsZero())
		assert.Zero(t, s.ParameterCount)
		assert.Zero(t, s.ResolverCount)
		assert.Zero(t, s.FingerprintCount)
	})

	t.Run("populated state", func(t *testing.T) {
		t.Parallel()
		updated := time.Date(2025, 6, 7, 8, 9, 10, 0, time.UTC)
		sd := NewData()
		sd.Metadata = Metadata{Solution: "demo", Version: "1.2.3", LastUpdatedAt: updated}
		sd.Parameters["a"] = 1
		sd.Parameters["b"] = 2
		sd.Resolvers["r"] = &PersistedEntry{Value: "v"}
		sd.Fingerprints["f"] = &FingerprintEntry{Value: "h"}

		s := sd.Summarize()
		assert.Equal(t, "demo", s.Solution)
		assert.Equal(t, "1.2.3", s.Version)
		assert.Equal(t, updated, s.LastUpdated)
		assert.Equal(t, 2, s.ParameterCount)
		assert.Equal(t, 1, s.ResolverCount)
		assert.Equal(t, 1, s.FingerprintCount)
	})
}
