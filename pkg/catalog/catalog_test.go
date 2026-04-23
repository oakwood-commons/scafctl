// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
)

func TestIsPreRelease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version *semver.Version
		want    bool
	}{
		{"nil version", nil, false},
		{"stable", semver.MustParse("1.0.0"), false},
		{"beta", semver.MustParse("1.0.0-beta.1"), true},
		{"alpha", semver.MustParse("2.0.0-alpha"), true},
		{"rc", semver.MustParse("3.0.0-rc.1"), true},
		{"dev build", semver.MustParse("0.0.99-beta.1"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsPreRelease(tt.version))
		})
	}
}

func TestIncludePreReleaseContext(t *testing.T) {
	t.Parallel()

	t.Run("false by default", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		assert.False(t, IncludePreReleaseFromContext(ctx))
	})

	t.Run("true when set", func(t *testing.T) {
		t.Parallel()
		ctx := WithIncludePreRelease(context.Background())
		assert.True(t, IncludePreReleaseFromContext(ctx))
	})
}

func TestSearchPatternContext(t *testing.T) {
	t.Parallel()

	t.Run("empty by default", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		assert.Empty(t, SearchPatternFromContext(ctx))
	})

	t.Run("returns pattern when set", func(t *testing.T) {
		t.Parallel()
		ctx := WithSearchPattern(context.Background(), "starter*")
		assert.Equal(t, "starter*", SearchPatternFromContext(ctx))
	})
}

func TestMatchesSearchPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
		input string
		want  bool
	}{
		{"empty query matches everything", "", "anything", true},
		{"exact match", "starter-kit", "starter-kit", true},
		{"substring match", "starter", "starter-kit", true},
		{"suffix match", "kit", "starter-kit", true},
		{"middle match", "arter", "starter-kit", true},
		{"no match", "terraform", "hello-world", false},
		{"case insensitive query", "Starter", "starter-kit", true},
		{"case insensitive name", "starter", "Starter-Kit", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, matchesSearchPattern(tc.query, tc.input))
		})
	}
}
