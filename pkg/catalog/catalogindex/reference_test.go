// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalogindex

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

const testDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func referenceTestIndex() *Index {
	return FromConfig(&config.Config{
		Catalogs: []config.CatalogConfig{
			{Name: config.CatalogNameOfficial, URL: "oci://ghcr.io/oakwood-commons"},
			{Name: "Corp", URL: "https://reg.example.com/team"},
			{Name: "port", URL: "oci://localhost:5000"},
			{Name: "files", Path: "/tmp/catalog"},
		},
	})
}

func TestIndex_ParseReference(t *testing.T) {
	idx := referenceTestIndex()

	tests := []struct {
		name        string
		kind        catalog.ArtifactKind
		input       string
		wantName    string
		wantVersion string
		wantDigest  string
		wantRemote  string
		wantBuilt   bool
		wantKind    catalog.ArtifactKind
	}{
		{
			name:     "short name",
			kind:     catalog.ArtifactKindSolution,
			input:    "foo",
			wantName: "foo",
			wantKind: catalog.ArtifactKindSolution,
		},
		{
			name:        "short name with version and whitespace",
			kind:        catalog.ArtifactKindSolution,
			input:       "  foo@1.2.3  ",
			wantName:    "foo",
			wantVersion: "1.2.3",
			wantKind:    catalog.ArtifactKindSolution,
		},
		{
			name:       "short name with digest",
			kind:       catalog.ArtifactKindSolution,
			input:      "foo@" + testDigest,
			wantName:   "foo",
			wantDigest: testDigest,
			wantKind:   catalog.ArtifactKindSolution,
		},
		{
			name:        "local alias sets built",
			kind:        catalog.ArtifactKindSolution,
			input:       "local/foo@1.2.3",
			wantName:    "foo",
			wantVersion: "1.2.3",
			wantBuilt:   true,
			wantKind:    catalog.ArtifactKindSolution,
		},
		{
			name:      "local alias is case-insensitive",
			kind:      catalog.ArtifactKindSolution,
			input:     "LOCAL/foo",
			wantName:  "foo",
			wantBuilt: true,
			wantKind:  catalog.ArtifactKindSolution,
		},
		{
			name:        "official alias translates to registry",
			kind:        catalog.ArtifactKindSolution,
			input:       "official/foo@1.2.3",
			wantName:    "foo",
			wantVersion: "1.2.3",
			wantRemote:  "ghcr.io/oakwood-commons",
			wantKind:    catalog.ArtifactKindSolution,
		},
		{
			name:       "configured alias is case-insensitive",
			kind:       catalog.ArtifactKindProvider,
			input:      "corp/bar",
			wantName:   "bar",
			wantRemote: "reg.example.com/team",
			wantKind:   catalog.ArtifactKindProvider,
		},
		{
			name:       "alias with latest leaves version open",
			kind:       catalog.ArtifactKindSolution,
			input:      "official/foo@latest",
			wantName:   "foo",
			wantRemote: "ghcr.io/oakwood-commons",
			wantKind:   catalog.ArtifactKindSolution,
		},
		{
			name:       "alias with digest",
			kind:       catalog.ArtifactKindSolution,
			input:      "official/foo@" + testDigest,
			wantName:   "foo",
			wantDigest: testDigest,
			wantRemote: "ghcr.io/oakwood-commons",
			wantKind:   catalog.ArtifactKindSolution,
		},
		{
			name:        "alias to registry without repository",
			kind:        catalog.ArtifactKindSolution,
			input:       "port/foo@1.0.0",
			wantName:    "foo",
			wantVersion: "1.0.0",
			wantRemote:  "localhost:5000",
			wantKind:    catalog.ArtifactKindSolution,
		},
		{
			name:        "full remote reference with kind segment",
			kind:        catalog.ArtifactKindSolution,
			input:       "ghcr.io/oakwood-commons/solutions/foo@1.2.3",
			wantName:    "foo",
			wantVersion: "1.2.3",
			wantRemote:  "ghcr.io/oakwood-commons",
			wantKind:    catalog.ArtifactKindSolution,
		},
		{
			name:        "full remote reference with oci scheme and no kind segment",
			kind:        catalog.ArtifactKindSolution,
			input:       "oci://ghcr.io/oakwood-commons/foo@1.2.3",
			wantName:    "foo",
			wantVersion: "1.2.3",
			wantRemote:  "ghcr.io/oakwood-commons",
			wantKind:    catalog.ArtifactKindSolution,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := idx.ParseReference(tt.kind, tt.input)
			require.NoError(t, err)

			assert.Equal(t, tt.wantKind, ref.Kind)
			assert.Equal(t, tt.wantName, ref.Name)
			assert.Equal(t, tt.wantDigest, ref.Digest)
			assert.Equal(t, tt.wantRemote, ref.Remote)
			assert.Equal(t, tt.wantBuilt, ref.Built)
			if tt.wantVersion == "" {
				assert.Nil(t, ref.Version)
			} else {
				require.NotNil(t, ref.Version)
				assert.Equal(t, tt.wantVersion, ref.Version.String())
			}
		})
	}
}

func TestIndex_ParseReference_AliasMatchesFullReference(t *testing.T) {
	idx := referenceTestIndex()

	viaAlias, err := idx.ParseReference(catalog.ArtifactKindSolution, "official/foo@1.2.3")
	require.NoError(t, err)
	viaRegistry, err := idx.ParseReference(catalog.ArtifactKindSolution, "ghcr.io/oakwood-commons/solutions/foo@1.2.3")
	require.NoError(t, err)

	assert.Equal(t, viaRegistry, viaAlias)
}

func TestIndex_ParseReference_Errors(t *testing.T) {
	idx := referenceTestIndex()

	tests := []struct {
		name        string
		kind        catalog.ArtifactKind
		input       string
		wantUnknown bool
	}{
		{name: "empty", kind: catalog.ArtifactKindSolution, input: ""},
		{name: "invalid short name", kind: catalog.ArtifactKindSolution, input: "Foo"},
		{name: "invalid short version", kind: catalog.ArtifactKindSolution, input: "foo@not-a-version"},
		{name: "empty alias", kind: catalog.ArtifactKindSolution, input: "/foo"},
		{name: "nested alias path", kind: catalog.ArtifactKindSolution, input: "official/sub/foo"},
		{name: "unknown alias", kind: catalog.ArtifactKindSolution, input: "nope/foo", wantUnknown: true},
		{name: "filesystem alias is not remote", kind: catalog.ArtifactKindSolution, input: "files/foo", wantUnknown: true},
		{name: "invalid name under local", kind: catalog.ArtifactKindSolution, input: "local/Foo"},
		{name: "invalid version under alias", kind: catalog.ArtifactKindSolution, input: "official/foo@bad"},
		{name: "invalid name under alias", kind: catalog.ArtifactKindSolution, input: "official/Foo"},
		{name: "kind mismatch", kind: catalog.ArtifactKindProvider, input: "ghcr.io/oakwood-commons/solutions/foo"},
		{name: "invalid remote digest", kind: catalog.ArtifactKindSolution, input: "ghcr.io/org/foo@sha256:abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := idx.ParseReference(tt.kind, tt.input)
			require.Error(t, err)
			assert.Equal(t, tt.wantUnknown, errors.Is(err, ErrUnknownCatalog))
		})
	}
}

func TestIndex_ParseReference_NilIndex(t *testing.T) {
	var idx *Index

	ref, err := idx.ParseReference(catalog.ArtifactKindSolution, "local/foo")
	require.NoError(t, err)
	assert.True(t, ref.Built)

	_, err = idx.ParseReference(catalog.ArtifactKindSolution, "official/foo")
	require.ErrorIs(t, err, ErrUnknownCatalog)
}

func BenchmarkIndex_ParseReference(b *testing.B) {
	idx := referenceTestIndex()
	inputs := []string{
		"foo@1.2.3",
		"local/foo@1.2.3",
		"official/foo@1.2.3",
		"ghcr.io/oakwood-commons/solutions/foo@1.2.3",
	}

	b.ReportAllocs()
	for b.Loop() {
		for _, in := range inputs {
			_, _ = idx.ParseReference(catalog.ArtifactKindSolution, in)
		}
	}
}
