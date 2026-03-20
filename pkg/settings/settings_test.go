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
