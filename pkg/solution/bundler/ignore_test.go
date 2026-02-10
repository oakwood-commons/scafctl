// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScafctlIgnore_BasicPatterns(t *testing.T) {
	ig := ParseIgnorePatterns([]string{
		"*.bak",
		"testdata/",
		".env",
	})

	tests := []struct {
		path     string
		expected bool
	}{
		{"file.bak", true},
		{"dir/file.bak", true},
		{"testdata/fixture.yaml", true},
		{".env", true},
		{"config/.env", true},
		{"main.go", false},
		{"templates/main.tf.tmpl", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.expected, ig.IsIgnored(tt.path), "path: %s", tt.path)
		})
	}
}

func TestScafctlIgnore_Negation(t *testing.T) {
	ig := ParseIgnorePatterns([]string{
		"*.bak",
		"!important.bak",
	})

	assert.True(t, ig.IsIgnored("random.bak"))
	assert.False(t, ig.IsIgnored("important.bak"))
}

func TestScafctlIgnore_AnchoredPattern(t *testing.T) {
	ig := ParseIgnorePatterns([]string{
		"/build",
	})

	assert.True(t, ig.IsIgnored("build"))
	assert.False(t, ig.IsIgnored("src/build"))
}

func TestScafctlIgnore_Comments(t *testing.T) {
	ig := ParseIgnorePatterns([]string{
		"# This is a comment",
		"*.bak",
		"",
		"# Another comment",
	})

	assert.True(t, ig.IsIgnored("test.bak"))
	assert.False(t, ig.IsIgnored("test.go"))
}

func TestScafctlIgnore_EmptyPatterns(t *testing.T) {
	ig := ParseIgnorePatterns([]string{})
	assert.False(t, ig.IsIgnored("anything.go"))
}

func TestScafctlIgnore_DoublestarPattern(t *testing.T) {
	ig := ParseIgnorePatterns([]string{
		"vendor/**",
	})

	assert.True(t, ig.IsIgnored("vendor/mod.go"))
	assert.True(t, ig.IsIgnored("vendor/sub/file.go"))
	assert.False(t, ig.IsIgnored("mod.go"))
}

func TestNoopIgnoreChecker(t *testing.T) {
	noop := &noopIgnoreChecker{}
	assert.False(t, noop.IsIgnored("anything"))
	assert.False(t, noop.IsIgnored("vendor/mod.go"))
}
