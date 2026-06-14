// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fingerprint

import (
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/stretchr/testify/assert"
)

func TestStateKeys(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "__fingerprint:build:sources", sourcesKey("build"))
	assert.Equal(t, "__fingerprint:build:generates", generatesKey("build"))
	assert.Equal(t, "__fingerprint:build:inputs", inputsKey("build"))
	assert.Equal(t, "__fingerprint:deploy-service:sources", sourcesKey("deploy-service"))
	assert.Equal(t, "__fingerprint:deploy-service:inputs", inputsKey("deploy-service"))
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
		data.Fingerprints["__fingerprint:build:sources"] = &state.FingerprintEntry{Value: "abc123", UpdatedAt: time.Now().UTC()}
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
		data.Fingerprints["__fingerprint:build:generates"] = &state.FingerprintEntry{Value: "def456", UpdatedAt: time.Now().UTC()}
		assert.Equal(t, "def456", LoadGeneratesHash(data, "build"))
	})
}

func TestSaveHashes(t *testing.T) {
	t.Parallel()

	t.Run("saves sources only when generates and inputs empty", func(t *testing.T) {
		t.Parallel()
		data := state.NewData()
		SaveHashes(data, "build", "src-hash", "", "")

		assert.Equal(t, "src-hash", LoadSourcesHash(data, "build"))
		assert.Empty(t, LoadGeneratesHash(data, "build"))
		assert.Empty(t, LoadInputsHash(data, "build"))
	})

	t.Run("saves sources and generates when inputs empty", func(t *testing.T) {
		t.Parallel()
		data := state.NewData()
		SaveHashes(data, "build", "src-hash", "gen-hash", "")

		assert.Equal(t, "src-hash", LoadSourcesHash(data, "build"))
		assert.Equal(t, "gen-hash", LoadGeneratesHash(data, "build"))
		assert.Empty(t, LoadInputsHash(data, "build"))
	})

	t.Run("saves all three hashes", func(t *testing.T) {
		t.Parallel()
		data := state.NewData()
		SaveHashes(data, "build", "src-hash", "gen-hash", "inp-hash")

		assert.Equal(t, "src-hash", LoadSourcesHash(data, "build"))
		assert.Equal(t, "gen-hash", LoadGeneratesHash(data, "build"))
		assert.Equal(t, "inp-hash", LoadInputsHash(data, "build"))
	})

	t.Run("nil data is safe", func(t *testing.T) {
		t.Parallel()
		assert.NotPanics(t, func() {
			SaveHashes(nil, "build", "hash", "hash", "hash")
		})
	})

	t.Run("clears generates and inputs when new hashes are empty", func(t *testing.T) {
		t.Parallel()
		data := state.NewData()
		// Seed with all three hashes
		SaveHashes(data, "build", "src-hash", "gen-hash", "inp-hash")
		assert.Equal(t, "gen-hash", LoadGeneratesHash(data, "build"))
		assert.Equal(t, "inp-hash", LoadInputsHash(data, "build"))

		// Save again with empty generates and inputs -- should clear old entries
		SaveHashes(data, "build", "new-src", "", "")
		assert.Equal(t, "new-src", LoadSourcesHash(data, "build"))
		assert.Empty(t, LoadGeneratesHash(data, "build"))
		assert.Empty(t, LoadInputsHash(data, "build"))
	})
}

func TestLoadInputsHash(t *testing.T) {
	t.Parallel()

	t.Run("nil data returns empty", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, LoadInputsHash(nil, "build"))
	})

	t.Run("missing key returns empty", func(t *testing.T) {
		t.Parallel()
		data := state.NewData()
		assert.Empty(t, LoadInputsHash(data, "build"))
	})

	t.Run("existing key returns value", func(t *testing.T) {
		t.Parallel()
		data := state.NewData()
		data.Fingerprints["__fingerprint:build:inputs"] = &state.FingerprintEntry{Value: "inp789", UpdatedAt: time.Now().UTC()}
		assert.Equal(t, "inp789", LoadInputsHash(data, "build"))
	})
}

func TestParseActionName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key      string
		wantName string
		wantOK   bool
	}{
		{"__fingerprint:build:sources", "build", true},
		{"__fingerprint:deploy:generates", "deploy", true},
		{"__fingerprint:my-action:inputs", "my-action", true},
		{"__fingerprint:deploy[0]:sources", "deploy[0]", true},
		{"not-a-fingerprint-key", "", false},
		{"__fingerprint:", "", false},
		{"__fingerprint:nocolon", "", false},
		{"__fingerprint:build:", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()
			name, ok := ParseActionName(tt.key)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantName, name)
		})
	}
}

func TestClearAction(t *testing.T) {
	t.Parallel()

	t.Run("removes all keys for action", func(t *testing.T) {
		t.Parallel()
		data := state.NewData()
		now := time.Now().UTC()
		data.Fingerprints["__fingerprint:build:sources"] = &state.FingerprintEntry{Value: "abc", UpdatedAt: now}
		data.Fingerprints["__fingerprint:build:generates"] = &state.FingerprintEntry{Value: "def", UpdatedAt: now}
		data.Fingerprints["__fingerprint:build:inputs"] = &state.FingerprintEntry{Value: "ghi", UpdatedAt: now}
		data.Fingerprints["__fingerprint:deploy:sources"] = &state.FingerprintEntry{Value: "xyz", UpdatedAt: now}

		removed := ClearAction(data, "build")
		assert.Equal(t, 3, removed)
		assert.Len(t, data.Fingerprints, 1)
		assert.Contains(t, data.Fingerprints, "__fingerprint:deploy:sources")
	})

	t.Run("nil data returns zero", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 0, ClearAction(nil, "build"))
	})

	t.Run("missing action returns zero", func(t *testing.T) {
		t.Parallel()
		data := state.NewData()
		assert.Equal(t, 0, ClearAction(data, "nonexistent"))
	})
}

func TestListActions(t *testing.T) {
	t.Parallel()

	t.Run("returns sorted unique action names", func(t *testing.T) {
		t.Parallel()
		data := state.NewData()
		now := time.Now().UTC()
		data.Fingerprints["__fingerprint:deploy:sources"] = &state.FingerprintEntry{Value: "a", UpdatedAt: now}
		data.Fingerprints["__fingerprint:build:sources"] = &state.FingerprintEntry{Value: "b", UpdatedAt: now}
		data.Fingerprints["__fingerprint:build:generates"] = &state.FingerprintEntry{Value: "c", UpdatedAt: now}
		data.Fingerprints["__fingerprint:test:inputs"] = &state.FingerprintEntry{Value: "d", UpdatedAt: now}

		names := ListActions(data)
		assert.Equal(t, []string{"build", "deploy", "test"}, names)
	})

	t.Run("nil data returns nil", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, ListActions(nil))
	})

	t.Run("empty fingerprints returns nil", func(t *testing.T) {
		t.Parallel()
		data := state.NewData()
		assert.Nil(t, ListActions(data))
	})
}

func TestSplitKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key      string
		wantName string
		wantType string
		wantOK   bool
	}{
		{"__fingerprint:build:sources", "build", "sources", true},
		{"__fingerprint:deploy:generates", "deploy", "generates", true},
		{"__fingerprint:my-action:inputs", "my-action", "inputs", true},
		{"__fingerprint:deploy[0]:sources", "deploy[0]", "sources", true},
		{"not-a-fingerprint-key", "", "", false},
		{"__fingerprint:", "", "", false},
		{"__fingerprint:nocolon", "", "", false},
		{"__fingerprint:build:", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()
			name, typ, ok := SplitKey(tt.key)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantName, name)
			assert.Equal(t, tt.wantType, typ)
		})
	}
}
