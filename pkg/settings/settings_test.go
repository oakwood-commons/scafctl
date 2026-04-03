// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewCliParams(t *testing.T) {
	tests := []struct {
		name string
		want *Run
	}{
		{
			name: "default CLI params",
			want: &Run{
				MinLogLevel: "none",
				EntryPointSettings: EntryPointSettings{
					FromAPI: false,
					FromCli: true,
					Path:    "",
				},
				IsQuiet:     false,
				NoColor:     false,
				ExitOnError: true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewCliParams()
			if *got != *tt.want {
				t.Errorf("NewCliParams() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDefaultHTTPCacheDir(t *testing.T) {
	dir := DefaultHTTPCacheDir()
	assert.Contains(t, dir, "scafctl")
	assert.Contains(t, dir, "http-cache")
}

func TestDefaultBuildCacheDir(t *testing.T) {
	dir := DefaultBuildCacheDir()
	assert.Contains(t, dir, "scafctl")
	assert.Contains(t, dir, "build-cache")
}

func TestDefaultPluginCacheDir(t *testing.T) {
	dir := DefaultPluginCacheDir()
	assert.Contains(t, dir, "scafctl")
	assert.Contains(t, dir, "plugins")
}

func TestSolutionFoldersFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		binaryName string
		want       []string
	}{
		{
			name:       "default binary name",
			binaryName: "scafctl",
			want:       []string{"scafctl", ".scafctl", ""},
		},
		{
			name:       "custom binary name",
			binaryName: "mycli",
			want:       []string{"mycli", ".mycli", ""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := SolutionFoldersFor(tt.binaryName)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSolutionFileNamesFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		binaryName  string
		wantLen     int
		mustContain []string
	}{
		{
			name:        "default binary name",
			binaryName:  "scafctl",
			wantLen:     6,
			mustContain: []string{"solution.yaml", "scafctl.yaml", "scafctl.json"},
		},
		{
			name:        "custom binary name",
			binaryName:  "mycli",
			wantLen:     6,
			mustContain: []string{"solution.yaml", "mycli.yaml", "mycli.json"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := SolutionFileNamesFor(tt.binaryName)
			assert.Len(t, got, tt.wantLen)
			for _, want := range tt.mustContain {
				assert.Contains(t, got, want)
			}
		})
	}
}

func TestHTTPCacheKeyPrefixFor(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "scafctl:", HTTPCacheKeyPrefixFor("scafctl"))
	assert.Equal(t, "mycli:", HTTPCacheKeyPrefixFor("mycli"))
}

func TestHTTPCacheDirFor(t *testing.T) {
	t.Parallel()
	dir := HTTPCacheDirFor("mycli")
	assert.Contains(t, dir, "mycli")
	assert.Contains(t, dir, "http-cache")
	assert.NotContains(t, dir, "scafctl")
}

func TestBuildCacheDirFor(t *testing.T) {
	t.Parallel()
	dir := BuildCacheDirFor("mycli")
	assert.Contains(t, dir, "mycli")
	assert.Contains(t, dir, "build-cache")
	assert.NotContains(t, dir, "scafctl")
}

func TestPluginCacheDirFor(t *testing.T) {
	t.Parallel()
	dir := PluginCacheDirFor("mycli")
	assert.Contains(t, dir, "mycli")
	assert.Contains(t, dir, "plugins")
	assert.NotContains(t, dir, "scafctl")
}

func TestRootSolutionFolders_MatchesDefault(t *testing.T) {
	t.Parallel()
	assert.Equal(t, SolutionFoldersFor(CliBinaryName), RootSolutionFolders)
}

func TestSolutionFileNames_MatchesDefault(t *testing.T) {
	t.Parallel()
	assert.Equal(t, SolutionFileNamesFor(CliBinaryName), SolutionFileNames)
}
