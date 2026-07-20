// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderRef_Parse(t *testing.T) {
	tests := []struct {
		name string
		ref  ProviderRef
		want ProviderRefParts
	}{
		{
			name: "bare name",
			ref:  "echo",
			want: ProviderRefParts{Name: "echo"},
		},
		{
			name: "name with hyphen",
			ref:  "echo-provider",
			want: ProviderRefParts{Name: "echo-provider"},
		},
		{
			name: "name with version constraint",
			ref:  "echo@^1.0.0",
			want: ProviderRefParts{Name: "echo", Version: "^1.0.0"},
		},
		{
			name: "name with exact version",
			ref:  "echo@1.2.3",
			want: ProviderRefParts{Name: "echo", Version: "1.2.3"},
		},
		{
			name: "name with latest",
			ref:  "echo@latest",
			want: ProviderRefParts{Name: "echo", Version: "latest"},
		},
		{
			name: "name with digest",
			ref:  "echo@sha256:abc123",
			want: ProviderRefParts{Name: "echo", Digest: "sha256:abc123"},
		},
		{
			name: "qualified without version",
			ref:  "ghcr.io/myorg/echo",
			want: ProviderRefParts{Registry: "ghcr.io/myorg", Name: "echo"},
		},
		{
			name: "qualified with version",
			ref:  "registry.example.com/myorg/echo@^1.0.0",
			want: ProviderRefParts{Registry: "registry.example.com/myorg", Name: "echo", Version: "^1.0.0"},
		},
		{
			name: "qualified with port and version",
			ref:  "registry.example.com:5000/myorg/echo@^1.0.0",
			want: ProviderRefParts{Registry: "registry.example.com:5000/myorg", Name: "echo", Version: "^1.0.0"},
		},
		{
			name: "qualified with port no namespace",
			ref:  "localhost:5000/echo@1.0.0",
			want: ProviderRefParts{Registry: "localhost:5000", Name: "echo", Version: "1.0.0"},
		},
		{
			name: "qualified localhost no port",
			ref:  "localhost/echo",
			want: ProviderRefParts{Registry: "localhost", Name: "echo"},
		},
		{
			name: "qualified with digest",
			ref:  "ghcr.io/myorg/echo@sha256:deadbeef",
			want: ProviderRefParts{Registry: "ghcr.io/myorg", Name: "echo", Digest: "sha256:deadbeef"},
		},
		{
			name: "qualified deep namespace",
			ref:  "ghcr.io/org/team/echo@2.0.0",
			want: ProviderRefParts{Registry: "ghcr.io/org/team", Name: "echo", Version: "2.0.0"},
		},
		{
			name: "leading and trailing whitespace trimmed",
			ref:  "  echo@1.0.0  ",
			want: ProviderRefParts{Name: "echo", Version: "1.0.0"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.ref.Parse()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestProviderRef_Parse_Errors(t *testing.T) {
	tests := []struct {
		name string
		ref  ProviderRef
	}{
		{name: "empty", ref: ""},
		{name: "whitespace only", ref: "   "},
		{name: "empty version after at", ref: "echo@"},
		{name: "empty name before at", ref: "@1.0.0"},
		{name: "non-host path segment", ref: "myorg/echo"},
		{name: "non-host path with version", ref: "myorg/echo@1.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.ref.Parse()
			assert.Error(t, err)
		})
	}
}

func TestProviderRefParts_IsQualified(t *testing.T) {
	tests := []struct {
		name  string
		parts ProviderRefParts
		want  bool
	}{
		{name: "bare", parts: ProviderRefParts{Name: "echo"}, want: false},
		{name: "versioned", parts: ProviderRefParts{Name: "echo", Version: "1.0.0"}, want: false},
		{name: "qualified", parts: ProviderRefParts{Registry: "ghcr.io/myorg", Name: "echo"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.parts.IsQualified())
		})
	}
}

func TestProviderRefParts_String(t *testing.T) {
	tests := []struct {
		name  string
		parts ProviderRefParts
		want  string
	}{
		{name: "bare", parts: ProviderRefParts{Name: "echo"}, want: "echo"},
		{name: "versioned", parts: ProviderRefParts{Name: "echo", Version: "^1.0.0"}, want: "echo@^1.0.0"},
		{name: "digest", parts: ProviderRefParts{Name: "echo", Digest: "sha256:abc"}, want: "echo@sha256:abc"},
		{
			name:  "qualified versioned",
			parts: ProviderRefParts{Registry: "ghcr.io/myorg", Name: "echo", Version: "1.0.0"},
			want:  "ghcr.io/myorg/echo@1.0.0",
		},
		{
			name:  "qualified no version",
			parts: ProviderRefParts{Registry: "ghcr.io/myorg", Name: "echo"},
			want:  "ghcr.io/myorg/echo",
		},
		{
			name:  "digest takes precedence over version",
			parts: ProviderRefParts{Name: "echo", Version: "1.0.0", Digest: "sha256:abc"},
			want:  "echo@sha256:abc",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.parts.String())
		})
	}
}

func TestProviderRefParts_Validate(t *testing.T) {
	valid := []struct {
		name  string
		parts ProviderRefParts
	}{
		{name: "bare name", parts: ProviderRefParts{Name: "echo"}},
		{name: "hyphenated name", parts: ProviderRefParts{Name: "echo-provider"}},
		{name: "exact version", parts: ProviderRefParts{Name: "echo", Version: "1.2.3"}},
		{name: "caret constraint", parts: ProviderRefParts{Name: "echo", Version: "^1.0.0"}},
		{name: "gte constraint", parts: ProviderRefParts{Name: "echo", Version: ">=2.0.0"}},
		{name: "latest", parts: ProviderRefParts{Name: "echo", Version: "latest"}},
		{name: "latest case-insensitive", parts: ProviderRefParts{Name: "echo", Version: "LATEST"}},
		{name: "valid digest", parts: ProviderRefParts{Name: "echo", Digest: "sha256:" + strings.Repeat("a", 64)}},
		{name: "qualified", parts: ProviderRefParts{Registry: "ghcr.io/myorg", Name: "echo", Version: "^1.0.0"}},
		{name: "qualified with port", parts: ProviderRefParts{Registry: "localhost:5000", Name: "echo"}},
	}
	for _, tt := range valid {
		t.Run("valid/"+tt.name, func(t *testing.T) {
			assert.NoError(t, tt.parts.Validate())
		})
	}

	invalid := []struct {
		name  string
		parts ProviderRefParts
	}{
		{name: "empty name", parts: ProviderRefParts{Name: ""}},
		{name: "uppercase name", parts: ProviderRefParts{Name: "Echo"}},
		{name: "underscore name", parts: ProviderRefParts{Name: "my_provider"}},
		{name: "leading hyphen", parts: ProviderRefParts{Name: "-echo"}},
		{name: "trailing hyphen", parts: ProviderRefParts{Name: "echo-"}},
		{name: "double hyphen", parts: ProviderRefParts{Name: "ec--ho"}},
		{name: "leading digit", parts: ProviderRefParts{Name: "1echo"}},
		{name: "bad version constraint", parts: ProviderRefParts{Name: "echo", Version: "not_a_version"}},
		{name: "bad digest algorithm", parts: ProviderRefParts{Name: "echo", Digest: "md5:abc"}},
		{name: "short digest", parts: ProviderRefParts{Name: "echo", Digest: "sha256:abc"}},
		{name: "non-hex digest", parts: ProviderRefParts{Name: "echo", Digest: "sha256:" + strings.Repeat("g", 64)}},
		{name: "non-host registry", parts: ProviderRefParts{Registry: "myorg", Name: "echo"}},
	}
	for _, tt := range invalid {
		t.Run("invalid/"+tt.name, func(t *testing.T) {
			assert.Error(t, tt.parts.Validate())
		})
	}
}

func TestProviderRef_ParseThenValidate(t *testing.T) {
	valid := []ProviderRef{
		"echo",
		"echo@^1.0.0",
		"echo@latest",
		"ghcr.io/myorg/echo@1.0.0",
		"registry.example.com:5000/myorg/echo@^1.0.0",
		ProviderRef("ghcr.io/myorg/echo@sha256:" + strings.Repeat("a", 64)),
	}
	for _, ref := range valid {
		t.Run(string(ref), func(t *testing.T) {
			parts, err := ref.Parse()
			require.NoError(t, err)
			assert.NoError(t, parts.Validate())
		})
	}
}

func TestProviderRef_Parse_RoundTrip(t *testing.T) {
	refs := []ProviderRef{
		"echo",
		"echo@^1.0.0",
		"echo@sha256:abc123",
		"ghcr.io/myorg/echo",
		"registry.example.com:5000/myorg/echo@^1.0.0",
		"localhost:5000/echo@1.0.0",
	}
	for _, ref := range refs {
		t.Run(string(ref), func(t *testing.T) {
			parts, err := ref.Parse()
			require.NoError(t, err)
			assert.Equal(t, string(ref), parts.String())
		})
	}
}
