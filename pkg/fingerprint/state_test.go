// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fingerprint

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/stretchr/testify/assert"
)

func TestStateKeys(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "__fingerprint:build:sources", sourcesKey("build"))
	assert.Equal(t, "__fingerprint:build:generates", generatesKey("build"))
	assert.Equal(t, "__fingerprint:deploy-service:sources", sourcesKey("deploy-service"))
}

func TestLoadSourcesHash(t *testing.T) {
	t.Parallel()

	t.Run("nil data returns empty", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, LoadSourcesHash(nil, "build"))
	})

	t.Run("missing key returns empty", func(t *testing.T) {
		t.Parallel()
		data := state.NewData()
		assert.Empty(t, LoadSourcesHash(data, "build"))
	})

	t.Run("existing key returns value", func(t *testing.T) {
		t.Parallel()
		data := state.NewData()
		data.Values["__fingerprint:build:sources"] = &state.Entry{Value: "abc123"}
		assert.Equal(t, "abc123", LoadSourcesHash(data, "build"))
	})
}

func TestLoadGeneratesHash(t *testing.T) {
	t.Parallel()

	t.Run("nil data returns empty", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, LoadGeneratesHash(nil, "build"))
	})

	t.Run("existing key returns value", func(t *testing.T) {
		t.Parallel()
		data := state.NewData()
		data.Values["__fingerprint:build:generates"] = &state.Entry{Value: "def456"}
		assert.Equal(t, "def456", LoadGeneratesHash(data, "build"))
	})
}

func TestSaveHashes(t *testing.T) {
	t.Parallel()

	t.Run("saves sources only when generates empty", func(t *testing.T) {
		t.Parallel()
		data := state.NewData()
		SaveHashes(data, "build", "src-hash", "")

		assert.Equal(t, "src-hash", LoadSourcesHash(data, "build"))
		assert.Empty(t, LoadGeneratesHash(data, "build"))
	})

	t.Run("saves both when generates provided", func(t *testing.T) {
		t.Parallel()
		data := state.NewData()
		SaveHashes(data, "build", "src-hash", "gen-hash")

		assert.Equal(t, "src-hash", LoadSourcesHash(data, "build"))
		assert.Equal(t, "gen-hash", LoadGeneratesHash(data, "build"))
	})

	t.Run("nil data is safe", func(t *testing.T) {
		t.Parallel()
		assert.NotPanics(t, func() {
			SaveHashes(nil, "build", "hash", "hash")
		})
	})
}
