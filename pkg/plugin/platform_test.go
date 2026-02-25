// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCurrentPlatform(t *testing.T) {
	p := CurrentPlatform()
	assert.Contains(t, p, "/")
	assert.Equal(t, runtime.GOOS+"/"+runtime.GOARCH, p)
}

func TestParsePlatform(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantOS   string
		wantArch string
		wantErr  bool
	}{
		{name: "linux/amd64", input: "linux/amd64", wantOS: "linux", wantArch: "amd64"},
		{name: "darwin/arm64", input: "darwin/arm64", wantOS: "darwin", wantArch: "arm64"},
		{name: "windows/amd64", input: "windows/amd64", wantOS: "windows", wantArch: "amd64"},
		{name: "empty", input: "", wantErr: true},
		{name: "no slash", input: "linux", wantErr: true},
		{name: "trailing slash", input: "linux/", wantErr: true},
		{name: "leading slash", input: "/amd64", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os, arch, err := ParsePlatform(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantOS, os)
			assert.Equal(t, tt.wantArch, arch)
		})
	}
}

func TestPlatformCacheKey(t *testing.T) {
	assert.Equal(t, "linux-amd64", PlatformCacheKey("linux/amd64"))
	assert.Equal(t, "darwin-arm64", PlatformCacheKey("darwin/arm64"))
}
