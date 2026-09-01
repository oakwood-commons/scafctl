// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package versionindex

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// versions extracts the string form of the indexed versions for a key, in
// stored (descending) order, for concise assertions.
func versions[T comparable](t *testing.T, vi *Index[T], key T) []string {
	t.Helper()
	entry := vi.entries[key]
	out := make([]string, len(entry))
	for i, v := range entry {
		out[i] = v.String()
	}
	return out
}

// assertDescendingNoDup verifies the core invariant: each key's slice is sorted
// strictly descending with no duplicates.
func assertDescendingNoDup[T comparable](t *testing.T, vi *Index[T], key T) {
	t.Helper()
	entry := vi.entries[key]
	for i := 1; i < len(entry); i++ {
		assert.Truef(t, entry[i-1].GreaterThan(entry[i]),
			"not strictly descending at %d: %s !> %s", i, entry[i-1], entry[i])
	}
}

func TestIndex_DeleteVersion(t *testing.T) {
	t.Parallel()

	t.Run("removes an already-parsed version", func(t *testing.T) {
		t.Parallel()
		vi := New[string]()
		for _, v := range []string{"1.0.0", "2.0.0", "3.0.0"} {
			vi.AddVersion("plugin", semver.MustParse(v))
		}
		vi.DeleteVersion("plugin", semver.MustParse("2.0.0"))
		assert.Equal(t, []string{"3.0.0", "1.0.0"}, versions(t, vi, "plugin"))
		assertDescendingNoDup(t, vi, "plugin")
	})

	t.Run("removing the last version deletes the key", func(t *testing.T) {
		t.Parallel()
		vi := New[string]()
		vi.AddVersion("plugin", semver.MustParse("1.0.0"))
		vi.DeleteVersion("plugin", semver.MustParse("1.0.0"))
		_, ok := vi.entries["plugin"]
		assert.False(t, ok)
	})

	t.Run("absent version and absent key are no-ops", func(t *testing.T) {
		t.Parallel()
		vi := New[string]()
		vi.AddVersion("plugin", semver.MustParse("1.0.0"))
		vi.DeleteVersion("plugin", semver.MustParse("9.9.9"))
		vi.DeleteVersion("missing", semver.MustParse("1.0.0"))
		assert.Equal(t, []string{"1.0.0"}, versions(t, vi, "plugin"))
	})
}

func TestIndex_AddVersion(t *testing.T) {
	t.Parallel()

	t.Run("maintains descending order", func(t *testing.T) {
		t.Parallel()
		vi := New[string]()
		for _, v := range []string{"1.0.0", "2.0.0", "1.5.0", "0.9.0", "2.1.0"} {
			vi.AddVersion("plugin", semver.MustParse(v))
		}
		assert.Equal(t, []string{"2.1.0", "2.0.0", "1.5.0", "1.0.0", "0.9.0"},
			versions(t, vi, "plugin"))
		assertDescendingNoDup(t, vi, "plugin")
	})

	t.Run("ignores duplicates", func(t *testing.T) {
		t.Parallel()
		vi := New[string]()
		vi.AddVersion("plugin", semver.MustParse("1.2.3"))
		vi.AddVersion("plugin", semver.MustParse("1.2.3"))
		assert.Equal(t, []string{"1.2.3"}, versions(t, vi, "plugin"))
	})

	t.Run("keys are isolated", func(t *testing.T) {
		t.Parallel()
		vi := New[string]()
		vi.AddVersion("a", semver.MustParse("1.0.0"))
		vi.AddVersion("b", semver.MustParse("2.0.0"))
		assert.Equal(t, []string{"1.0.0"}, versions(t, vi, "a"))
		assert.Equal(t, []string{"2.0.0"}, versions(t, vi, "b"))
	})
}

func TestIndex_GetVersion(t *testing.T) {
	t.Parallel()

	vi := New[string]()
	for _, v := range []string{"1.0.0", "1.2.3", "2.0.0"} {
		vi.AddVersion("plugin", semver.MustParse(v))
	}

	t.Run("returns the exact indexed version", func(t *testing.T) {
		t.Parallel()
		got := vi.GetVersion("plugin", semver.MustParse("1.2.3"))
		require.NotNil(t, got)
		assert.Equal(t, "1.2.3", got.String())
	})

	t.Run("absent version returns nil", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, vi.GetVersion("plugin", semver.MustParse("1.2.4")))
	})

	t.Run("absent key returns nil", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, vi.GetVersion("missing", semver.MustParse("1.0.0")))
	})

	t.Run("does not interpret ranges", func(t *testing.T) {
		t.Parallel()
		// GetVersion is exact-only: a prerelease equal-but-not-identical must
		// only match its exact form.
		exact := New[string]()
		exact.AddVersion("plugin", semver.MustParse("1.2.3-rc1"))
		assert.Nil(t, exact.GetVersion("plugin", semver.MustParse("1.2.3")))
		got := exact.GetVersion("plugin", semver.MustParse("1.2.3-rc1"))
		require.NotNil(t, got)
		assert.Equal(t, "1.2.3-rc1", got.String())
	})
}

func TestIndex_GetConstraints(t *testing.T) {
	t.Parallel()

	vi := New[string]()
	for _, v := range []string{"1.0.0", "1.2.0", "1.5.0", "2.0.0", "2.1.0"} {
		vi.AddVersion("plugin", semver.MustParse(v))
	}

	mustConstraint := func(t *testing.T, s string) *semver.Constraints {
		t.Helper()
		c, err := semver.NewConstraint(s)
		require.NoError(t, err)
		return c
	}

	tests := []struct {
		name       string
		constraint string
		want       string // "" means nil expected
	}{
		{name: "caret picks highest in major", constraint: "^1.0.0", want: "1.5.0"},
		{name: "range picks highest match", constraint: ">=1.2.0, <2.0.0", want: "1.5.0"},
		{name: "tilde picks highest patch", constraint: "~1.2.0", want: "1.2.0"},
		{name: "wildcard minor picks highest", constraint: "2.x", want: "2.1.0"},
		{name: "no match returns nil", constraint: ">=3.0.0", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := vi.GetConstraints("plugin", mustConstraint(t, tt.constraint))
			if tt.want == "" {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tt.want, got.String())
		})
	}

	t.Run("absent key returns nil", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, vi.GetConstraints("missing", mustConstraint(t, "^1.0.0")))
	})
}

func TestIndex_Has(t *testing.T) {
	t.Parallel()

	vi := New[string]()
	vi.AddVersion("plugin", semver.MustParse("1.0.0"))

	assert.True(t, vi.Has("plugin"))
	assert.False(t, vi.Has("missing"))

	vi.DeleteVersion("plugin", semver.MustParse("1.0.0"))
	assert.False(t, vi.Has("plugin"), "key must be absent after its last version is removed")
}

func TestIndex_All(t *testing.T) {
	t.Parallel()

	vi := New[string]()
	vi.AddVersion("a", semver.MustParse("1.0.0"))
	vi.AddVersion("a", semver.MustParse("2.0.0"))
	vi.AddVersion("b", semver.MustParse("3.0.0"))

	got := make(map[string][]string)
	for key, vers := range vi.All() {
		for _, v := range vers {
			got[key] = append(got[key], v.String())
		}
	}

	assert.Equal(t, map[string][]string{
		"a": {"2.0.0", "1.0.0"},
		"b": {"3.0.0"},
	}, got)
}

func TestIndex_All_EarlyStop(t *testing.T) {
	t.Parallel()

	vi := New[string]()
	vi.AddVersion("a", semver.MustParse("1.0.0"))
	vi.AddVersion("b", semver.MustParse("2.0.0"))

	var count int
	for range vi.All() {
		count++
		break
	}
	assert.Equal(t, 1, count, "breaking out of All must stop iteration")
}
