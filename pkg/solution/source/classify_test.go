// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package source

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  Kind
	}{
		// auto-discover / stdin
		{"empty", "", KindAutoDiscover},
		{"stdin", "-", KindStdin},

		// URL
		{"https url", "https://example.com/sol.yaml", KindURL},
		{"http localhost url", "http://localhost:8080/sol", KindURL},

		// remote refs
		{"oci with version", "oci://ghcr.io/x/y@1", KindRemoteRef},
		{"oci non-host first segment", "oci://foo/bar", KindRemoteRef},
		{"oci with tag", "oci://ghcr.io/x/y:1", KindRemoteRef},
		{"registry with version", "ghcr.io/x/y@1", KindRemoteRef},
		{"registry with tag", "ghcr.io/x/y:1", KindRemoteRef},
		{"registry with digest", "ghcr.io/x/y@sha256:abc", KindRemoteRef},
		{"registry no version", "ghcr.io/org/sol", KindRemoteRef},
		{"localhost", "localhost/foo", KindRemoteRef},
		{"localhost with port", "localhost:5000/foo@1", KindRemoteRef},
		{"localhost with port no version", "localhost:5000/foo", KindRemoteRef},

		// bare names
		{"bare", "my-app", KindBareName},
		{"bare with version", "my-app@1.0.0", KindBareName},
		{"bare with dot", "my.sol", KindBareName},

		// local files
		{"one slash no ext", "configs/solution", KindLocalFile},
		{"former catalog syntax", "official/foo@1.2.3", KindLocalFile},
		{"dot slash", "./sol.yaml", KindLocalFile},
		{"parent", "../sol", KindLocalFile},
		{"absolute", "/abs/sol", KindLocalFile},
		{"hidden dir", ".scafctl/solution.yaml", KindLocalFile},
		{"yaml file", "sol.yaml", KindLocalFile},
		{"uppercase yml", "SOL.YML", KindLocalFile},
		{"json file", "sol.json", KindLocalFile},
		{"multi segment", "configs/a/b", KindLocalFile},
		{"windows backslash drive", `C:\sol`, KindLocalFile},
		{"windows forward drive", "C:/dir/sol", KindLocalFile},
		{"backslash", `dir\sol`, KindLocalFile},
		{"registry-looking yaml", "ghcr.io/org/sol.yaml", KindLocalFile},
		{"home dir", "~/sol", KindLocalFile},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Classify(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Kind)
			assert.Equal(t, tt.input, got.Ref)
			assert.Empty(t, got.Catalog)
			assert.Empty(t, got.Version)
		})
	}
}

func TestClassify_EmptyOptionsAreNoOps(t *testing.T) {
	t.Parallel()

	got, err := Classify("./sol.yaml", WithCatalog(""), WithVersion(""))
	require.NoError(t, err)
	assert.Equal(t, Classification{Kind: KindLocalFile, Ref: "./sol.yaml"}, got)
}

func TestClassify_WithCatalog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		catalog string
	}{
		{"name", "foo", "official"},
		{"name with version", "foo@1.2.3", "official"},
		{"local catalog", "foo", "local"},
		{"name with dot", "my.sol", "corp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Classify(tt.input, WithCatalog(tt.catalog))
			require.NoError(t, err)
			assert.Equal(t, Classification{Kind: KindCatalogQualified, Ref: tt.input, Catalog: tt.catalog}, got)
		})
	}
}

func TestClassify_CatalogOnlyForBareNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantKind string
	}{
		{"auto-discover", "", "auto-discover"},
		{"stdin", "-", "stdin"},
		{"url", "https://example.com/sol.yaml", "url"},
		{"oci", "oci://ghcr.io/x/y@1", "remote-ref"},
		{"registry", "ghcr.io/x/y@1", "remote-ref"},
		{"local file", "./sol.yaml", "local-file"},
		{"slash path", "official/foo", "local-file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Classify(tt.input, WithCatalog("official"))
			require.ErrorIs(t, err, ErrInvalidRef)
			assert.Contains(t, err.Error(), `catalog "official" applies only to a name[@version]`)
			assert.Contains(t, err.Error(), tt.wantKind)
			assert.Equal(t, Classification{}, got)
		})
	}
}

func TestClassify_WithVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      string
		catalog    string
		constraint string
		want       Classification
	}{
		{
			name: "bare name", input: "foo", constraint: "^1.2.0",
			want: Classification{Kind: KindBareName, Ref: "foo", Version: "^1.2.0"},
		},
		{
			name: "catalog-qualified", input: "foo", catalog: "official", constraint: "~2.1",
			want: Classification{Kind: KindCatalogQualified, Ref: "foo", Catalog: "official", Version: "~2.1"},
		},
		{
			name: "range", input: "foo", constraint: ">=1.0 <3.0",
			want: Classification{Kind: KindBareName, Ref: "foo", Version: ">=1.0 <3.0"},
		},
		{
			name: "exact version as constraint", input: "foo", constraint: "1.2.3",
			want: Classification{Kind: KindBareName, Ref: "foo", Version: "1.2.3"},
		},
		{
			name: "registry", input: "ghcr.io/org/sol", constraint: "^1",
			want: Classification{Kind: KindRemoteRef, Ref: "ghcr.io/org/sol", Version: "^1"},
		},
		{
			name: "oci registry", input: "oci://ghcr.io/org/sol", constraint: "^1",
			want: Classification{Kind: KindRemoteRef, Ref: "oci://ghcr.io/org/sol", Version: "^1"},
		},
		{
			name: "registry port is not a tag", input: "localhost:5000/foo", constraint: "^1",
			want: Classification{Kind: KindRemoteRef, Ref: "localhost:5000/foo", Version: "^1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Classify(tt.input, WithCatalog(tt.catalog), WithVersion(tt.constraint))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestClassify_VersionErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      string
		catalog    string
		constraint string
		wantMsg    string
	}{
		{name: "auto-discover", input: "", constraint: "^1", wantMsg: "not a auto-discover"},
		{name: "stdin", input: "-", constraint: "^1", wantMsg: "not a stdin"},
		{name: "url", input: "https://example.com/sol.yaml", constraint: "^1", wantMsg: "not a url"},
		{name: "local file", input: "./sol.yaml", constraint: "^1", wantMsg: "not a local-file"},
		{name: "bare pinned", input: "foo@1.0.0", constraint: "^1", wantMsg: "use one or the other"},
		{name: "catalog-qualified pinned", input: "foo@1.0.0", catalog: "official", constraint: "^1", wantMsg: "use one or the other"},
		{name: "registry pinned version", input: "ghcr.io/x/y@1", constraint: "^1", wantMsg: "use one or the other"},
		{name: "registry pinned tag", input: "ghcr.io/x/y:1", constraint: "^1", wantMsg: "use one or the other"},
		{name: "registry pinned digest", input: "ghcr.io/x/y@sha256:abc", constraint: "^1", wantMsg: "use one or the other"},
		{name: "oci pinned tag", input: "oci://localhost:5000/y:1", constraint: "^1", wantMsg: "use one or the other"},
		{name: "invalid constraint", input: "foo", constraint: "not-valid!!", wantMsg: `invalid version constraint "not-valid!!"`},
		{name: "latest is not a constraint", input: "foo", constraint: "latest", wantMsg: "invalid version constraint"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Classify(tt.input, WithCatalog(tt.catalog), WithVersion(tt.constraint))
			require.ErrorIs(t, err, ErrInvalidRef)
			assert.Contains(t, err.Error(), tt.wantMsg)
			assert.Contains(t, err.Error(), tt.constraint)
			assert.Equal(t, Classification{}, got)
		})
	}
}

func TestClassify_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantMsg string
	}{
		{"unsupported scheme", "ftp://x", "unsupported scheme"},
		{"bare multiple at", "a@b@c", "multiple '@'"},
		{"bare empty name", "@1.0", "empty name"},
		{"bare empty version", "name@", "empty version"},
		{"registry missing name", "ghcr.io/", "missing artifact name"},
		{"registry empty version", "ghcr.io/x/y@", "empty version"},
		{"registry empty tag", "ghcr.io/x/y:", "empty tag"},
		{"registry multiple at", "ghcr.io/x/y@1@2", "multiple '@'"},
		{"oci empty", "oci://", "missing registry host"},
		{"oci host only", "oci://ghcr.io", "missing artifact name"},
		{"oci empty host", "oci:///x", "missing registry host"},
		{"registry parent segment", "ghcr.io/../../etc/passwd", "relative path segment"},
		{"registry current segment", "ghcr.io/org/./name", "relative path segment"},
		{"oci parent segment", "oci://ghcr.io/org/../name@1.0.0", "relative path segment"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Classify(tt.input)
			require.ErrorIs(t, err, ErrInvalidRef)
			assert.Contains(t, err.Error(), tt.wantMsg)
			assert.Contains(t, err.Error(), tt.input)
			assert.Equal(t, KindUnknown, got.Kind)
		})
	}
}

func TestKind_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind Kind
		want string
	}{
		{KindUnknown, "unknown"},
		{KindAutoDiscover, "auto-discover"},
		{KindStdin, "stdin"},
		{KindURL, "url"},
		{KindRemoteRef, "remote-ref"},
		{KindCatalogQualified, "catalog-qualified"},
		{KindBareName, "bare-name"},
		{KindLocalFile, "local-file"},
		{Kind(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.kind.String())
		})
	}
}

func BenchmarkClassify(b *testing.B) {
	inputs := []string{
		"", "-", "https://example.com/sol.yaml", "oci://ghcr.io/x/y@1",
		"ghcr.io/x/y@1", "my-app@1.0.0", "./sol.yaml",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for _, in := range inputs {
			_, _ = Classify(in)
		}
		_, _ = Classify("my-app", WithCatalog("official"), WithVersion("^1.0.0"))
	}
}
