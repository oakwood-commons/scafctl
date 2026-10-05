// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package source

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKind_IsReference(t *testing.T) {
	t.Parallel()

	want := map[Kind]bool{
		KindUnknown:          false,
		KindAutoDiscover:     false,
		KindStdin:            false,
		KindURL:              true,
		KindRemoteRef:        true,
		KindCatalogQualified: true,
		KindBareName:         true,
		KindLocalFile:        false,
		Kind(99):             false,
	}
	for k, expected := range want {
		assert.Equal(t, expected, k.IsReference(), "Kind(%s).IsReference()", k)
	}
}

func TestClassification_IsUnversionedName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  bool
	}{
		{"my-app", true},
		{"deploy-to-k8s", true},
		{"my_app", true},
		{"my-app@1.0.0", false},
		{"ghcr.io/myorg/solutions/app:2.0", false},
		{"ghcr.io/myorg/app", false},
		{"localhost:5000/sol", false},
		{"https://example.com/solution.yaml", false},
		{"oci://registry.example.com/sol:v1", false},
		{"@params.yaml", false},
		{"/tmp/solution.yaml", false},
		{"./solution.yaml", false},
		{"configs/solution", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			c, err := Classify(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.want, c.IsUnversionedName())
		})
	}
}

func TestValidatePositionalRef(t *testing.T) {
	t.Parallel()

	const usage = "mycli test cmd"
	tests := []struct {
		name     string
		arg      string
		fileFlag string
		wantKind Kind
		wantErr  string
		wantIs   error
	}{
		{name: "catalog name", arg: "my-app", wantKind: KindBareName},
		{name: "versioned name", arg: "my-app@1.0.0", wantKind: KindBareName},
		{name: "registry ref", arg: "ghcr.io/org/sol:v1", wantKind: KindRemoteRef},
		{name: "oci ref", arg: "oci://ghcr.io/org/sol", wantKind: KindRemoteRef},
		{name: "url", arg: "https://example.com/sol.yaml", wantKind: KindURL},
		{name: "file flag conflict", arg: "my-app", fileFlag: "existing.yaml", wantIs: ErrFileAndPositional},
		{name: "relative path", arg: "./solution.yaml", wantErr: "local file paths must use -f/--file flag"},
		{name: "yaml extension", arg: "solution.yaml", wantErr: "local file paths must use -f/--file flag"},
		{name: "relative dir", arg: "configs/solution", wantErr: "local file paths must use -f/--file flag"},
		{name: "windows path", arg: `C:\dir\sol`, wantErr: "local file paths must use -f/--file flag"},
		{name: "stdin", arg: "-", wantErr: usage + " -f -"},
		{name: "error includes usage", arg: "../foo.yaml", wantErr: usage + " -f ../foo.yaml"},
		{name: "malformed ref", arg: "my-app@", wantIs: ErrInvalidRef},
		{name: "unsupported scheme", arg: "ftp://example.com/sol", wantIs: ErrInvalidRef},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, err := ValidatePositionalRef(tt.arg, tt.fileFlag, usage)
			switch {
			case tt.wantIs != nil:
				require.ErrorIs(t, err, tt.wantIs)
				assert.Equal(t, Classification{}, c)
			case tt.wantErr != "":
				require.ErrorContains(t, err, tt.wantErr)
				assert.Equal(t, Classification{}, c)
			default:
				require.NoError(t, err)
				assert.Equal(t, tt.wantKind, c.Kind)
				assert.Equal(t, tt.arg, c.Ref)
			}
		})
	}
}

func BenchmarkValidatePositionalRef(b *testing.B) {
	args := []string{"my-app", "./solution.yaml", "solution.yaml", "my-app@1.0.0", "ghcr.io/org/sol:v1"}

	b.ReportAllocs()
	for b.Loop() {
		for _, arg := range args {
			_, _ = ValidatePositionalRef(arg, "", "mycli run solution")
		}
	}
}
