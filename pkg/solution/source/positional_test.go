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

func TestValidateFileFlagPositional(t *testing.T) {
	t.Parallel()

	const usage = "mycli run solution"
	tests := []struct {
		name     string
		fileFlag string
		wantKind Kind
		wantErr  string
		wantIs   error
	}{
		{name: "empty is noop", fileFlag: "", wantKind: KindUnknown},
		{name: "local relative path", fileFlag: "./solution.yaml", wantKind: KindLocalFile},
		{name: "local yaml filename", fileFlag: "solution.yaml", wantKind: KindLocalFile},
		{name: "local absolute path", fileFlag: "/tmp/solution.yaml", wantKind: KindLocalFile},
		{name: "stdin", fileFlag: "-", wantKind: KindStdin},
		{name: "catalog bare name rejected", fileFlag: "my-app", wantErr: "pass references positionally: " + usage + " my-app"},
		{name: "versioned name rejected", fileFlag: "my-app@1.0.0", wantErr: "pass references positionally: " + usage + " my-app@1.0.0"},
		{name: "registry ref rejected", fileFlag: "ghcr.io/org/sol:v1", wantErr: "-f/--file accepts only local file paths or \"-\" for stdin"},
		{name: "url rejected", fileFlag: "https://example.com/sol.yaml", wantErr: "pass references positionally: " + usage + " https://example.com/sol.yaml"},
		{name: "malformed ref", fileFlag: "my-app@", wantIs: ErrInvalidRef},
		{name: "unsupported scheme", fileFlag: "ftp://example.com/sol", wantIs: ErrInvalidRef},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, err := ValidateFileFlagPositional(tt.fileFlag, usage)
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
			}
		})
	}
}

func TestValidateFileFlagSolution(t *testing.T) {
	t.Parallel()

	const usage = "mycli run action"
	tests := []struct {
		name     string
		fileFlag string
		wantKind Kind
		wantErr  string
		wantIs   error
	}{
		{name: "empty is noop", fileFlag: "", wantKind: KindUnknown},
		{name: "local file", fileFlag: "./actions.yaml", wantKind: KindLocalFile},
		{name: "stdin", fileFlag: "-", wantKind: KindStdin},
		{name: "catalog bare name suggests -s", fileFlag: "my-app", wantErr: "-s/--solution: " + usage + " -s my-app"},
		{name: "versioned name suggests -s", fileFlag: "my-app@1.2.3", wantErr: "-s/--solution: " + usage + " -s my-app@1.2.3"},
		{name: "url suggests -s", fileFlag: "https://example.com/sol.yaml", wantErr: "-s/--solution: " + usage + " -s https://example.com/sol.yaml"},
		{name: "malformed ref", fileFlag: "oci://", wantIs: ErrInvalidRef},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, err := ValidateFileFlagSolution(tt.fileFlag, usage)
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
			}
		})
	}
}

func TestValidateSolutionFlag(t *testing.T) {
	t.Parallel()

	const usage = "mycli run resolver"
	tests := []struct {
		name         string
		solutionFlag string
		fileFlag     string
		wantKind     Kind
		wantErr      string
		wantIs       error
	}{
		{name: "catalog bare name", solutionFlag: "my-app", wantKind: KindBareName},
		{name: "versioned name", solutionFlag: "my-app@1.0.0", wantKind: KindBareName},
		{name: "registry ref", solutionFlag: "ghcr.io/org/sol:v1", wantKind: KindRemoteRef},
		{name: "url", solutionFlag: "https://example.com/sol.yaml", wantKind: KindURL},
		{name: "file and solution conflict", solutionFlag: "my-app", fileFlag: "./solution.yaml", wantIs: ErrFileAndSolution},
		{name: "local path suggests -f", solutionFlag: "./solution.yaml", wantErr: "-s/--solution accepts catalog names, registry references, or URLs; for local files use -f/--file: " + usage + " -f ./solution.yaml"},
		{name: "yaml filename suggests -f", solutionFlag: "solution.yaml", wantErr: usage + " -f solution.yaml"},
		{name: "stdin suggests -f", solutionFlag: "-", wantErr: usage + " -f -"},
		{name: "empty rejected", solutionFlag: "", wantErr: "-s/--solution accepts catalog names"},
		{name: "malformed ref", solutionFlag: "my-app@", wantIs: ErrInvalidRef},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, err := ValidateSolutionFlag(tt.solutionFlag, tt.fileFlag, usage)
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
				assert.Equal(t, tt.solutionFlag, c.Ref)
			}
		})
	}
}

func BenchmarkValidateSourceFlags(b *testing.B) {
	inputs := []string{"", "./solution.yaml", "-", "my-app", "my-app@1.0.0", "ghcr.io/org/sol:v1"}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for _, in := range inputs {
			_, _ = ValidateFileFlagPositional(in, "mycli run solution")
			_, _ = ValidateFileFlagSolution(in, "mycli run action")
			_, _ = ValidateSolutionFlag(in, "", "mycli run action")
		}
	}
}
