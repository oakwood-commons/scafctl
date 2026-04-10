// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package build

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/solution/builder"
	"github.com/oakwood-commons/scafctl/pkg/terminal/format"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseByteSize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		{"100B", 100, false},
		{"1KB", 1024, false},
		{"50MB", 50 * 1024 * 1024, false},
		{"1GB", 1024 * 1024 * 1024, false},
		{"100", 100, false},
		{"50mb", 50 * 1024 * 1024, false},
		{"invalid", 0, true},
		{"MB", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := builder.ParseByteSize(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestFormatByteSize(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{100, "100 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{50 * 1024 * 1024, "50.0 MB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, format.Bytes(tt.input))
		})
	}
}

func TestTagFlagParsing(t *testing.T) {
	tests := []struct {
		name        string
		tag         string
		wantName    string
		wantVersion string
		wantErr     bool
	}{
		{
			name:        "name and version",
			tag:         "hello-world@1.0.0",
			wantName:    "hello-world",
			wantVersion: "1.0.0",
		},
		{
			name:        "name and prerelease version",
			tag:         "my-solution@0.1.0-beta.1",
			wantName:    "my-solution",
			wantVersion: "0.1.0-beta.1",
		},
		{
			name:     "name only",
			tag:      "hello-world",
			wantName: "hello-world",
		},
		{
			name:    "empty version after @",
			tag:     "hello-world@",
			wantErr: true,
		},
		{
			name:    "invalid version",
			tag:     "hello-world@notaversion",
			wantErr: true,
		},
		{
			name:    "empty tag",
			tag:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := catalog.ParseReference(catalog.ArtifactKindSolution, tt.tag)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, ref.Name)
			if tt.wantVersion != "" {
				require.NotNil(t, ref.Version)
				assert.Equal(t, tt.wantVersion, ref.Version.String())
			} else {
				assert.Nil(t, ref.Version)
			}
		})
	}
}

func TestTagFlagRemoteRefParsing(t *testing.T) {
	tests := []struct {
		name        string
		tag         string
		wantName    string
		wantVersion string
		wantErr     bool
	}{
		{
			name:        "full remote ref with kind",
			tag:         "ghcr.io/myorg/solutions/my-solution@1.0.0",
			wantName:    "my-solution",
			wantVersion: "1.0.0",
		},
		{
			name:        "full remote ref without kind",
			tag:         "ghcr.io/myorg/my-solution@2.0.0",
			wantName:    "my-solution",
			wantVersion: "2.0.0",
		},
		{
			name:        "remote ref with deep repository path",
			tag:         "registry.example.com/org/team/solutions/hello-world@0.1.0",
			wantName:    "hello-world",
			wantVersion: "0.1.0",
		},
		{
			name:     "remote ref without version",
			tag:      "ghcr.io/myorg/solutions/my-solution",
			wantName: "my-solution",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remoteRef, err := catalog.ParseRemoteReference(tt.tag)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, remoteRef.Name)
			if tt.wantVersion != "" {
				assert.Equal(t, tt.wantVersion, remoteRef.Tag)
			} else {
				assert.Empty(t, remoteRef.Tag)
			}
		})
	}
}
