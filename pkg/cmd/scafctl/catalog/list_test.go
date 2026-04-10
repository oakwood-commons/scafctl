// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	catalogpkg "github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandList(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandList(cliParams, ioStreams, "scafctl/catalog")

	require.NotNil(t, cmd)
	assert.Equal(t, "list", cmd.Use)
	assert.Contains(t, cmd.Aliases, "ls")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandList_Flags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandList(cliParams, ioStreams, "scafctl/catalog")

	flagTests := []struct {
		name     string
		defValue string
	}{
		{"kind", ""},
		{"name", ""},
		{"catalog", ""},
		{"insecure", "false"},
		{"all-versions", "false"},
	}

	for _, tt := range flagTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := cmd.Flags().Lookup(tt.name)
			require.NotNil(t, f, "flag %q should exist", tt.name)
			assert.Equal(t, tt.defValue, f.DefValue, "flag %q default value", tt.name)
		})
	}
}

func TestCommandList_CatalogFlagShorthand(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandList(cliParams, ioStreams, "scafctl/catalog")

	f := cmd.Flags().ShorthandLookup("c")
	require.NotNil(t, f, "shorthand -c should exist")
	assert.Equal(t, "catalog", f.Name)
}

func TestCommandList_InvalidKind(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandList(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"--kind", "not-a-valid-kind"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestCommandList_AllVersionsFlag(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandList(cliParams, ioStreams, "scafctl/catalog")

	f := cmd.Flags().Lookup("all-versions")
	require.NotNil(t, f, "all-versions flag should exist")
	assert.Equal(t, "false", f.DefValue)
}

func TestCommandList_NameFlag(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandList(cliParams, ioStreams, "scafctl/catalog")

	f := cmd.Flags().Lookup("name")
	require.NotNil(t, f, "name flag should exist")
	assert.Equal(t, "", f.DefValue)
}

func TestArtifactListSchema_ValidJSON(t *testing.T) {
	t.Parallel()

	var schema map[string]any
	err := json.Unmarshal(artifactListSchema, &schema)
	require.NoError(t, err, "artifactListSchema must be valid JSON")

	items, ok := schema["items"].(map[string]any)
	require.True(t, ok, "schema must have items object")

	props, ok := items["properties"].(map[string]any)
	require.True(t, ok, "items must have properties")

	// Verify all ArtifactListItem fields are in the schema
	expectedFields := []string{"name", "tag", "kind", "catalog", "version", "digest", "createdAt"}
	for _, field := range expectedFields {
		_, exists := props[field]
		assert.True(t, exists, "schema missing field %q", field)
	}
}

func TestArtifactListSchema_RequiredFields(t *testing.T) {
	t.Parallel()

	var schema map[string]any
	err := json.Unmarshal(artifactListSchema, &schema)
	require.NoError(t, err)

	items := schema["items"].(map[string]any)
	required, ok := items["required"].([]any)
	require.True(t, ok, "schema must have required array")

	requiredNames := make([]string, len(required))
	for i, v := range required {
		requiredNames[i] = v.(string)
	}

	// name, tag, kind, catalog are high priority (resist truncation)
	assert.Contains(t, requiredNames, "name")
	assert.Contains(t, requiredNames, "tag")
	assert.Contains(t, requiredNames, "kind")
	assert.Contains(t, requiredNames, "catalog")

	// digest should NOT be in required (lower priority, truncates first)
	assert.NotContains(t, requiredNames, "digest")
}

func TestArtifactListSchema_DigestVisible(t *testing.T) {
	t.Parallel()

	var schema map[string]any
	err := json.Unmarshal(artifactListSchema, &schema)
	require.NoError(t, err)

	items := schema["items"].(map[string]any)
	props := items["properties"].(map[string]any)
	digest := props["digest"].(map[string]any)

	// Digest should be visible (no deprecated flag)
	_, hasDeprecated := digest["deprecated"]
	assert.False(t, hasDeprecated, "digest column should not be deprecated (must be visible)")
	assert.Equal(t, "Digest", digest["title"])
}

func TestArtifactListSchema_HiddenFields(t *testing.T) {
	t.Parallel()

	var schema map[string]any
	err := json.Unmarshal(artifactListSchema, &schema)
	require.NoError(t, err)

	items := schema["items"].(map[string]any)
	props := items["properties"].(map[string]any)

	// version and createdAt should be hidden
	for _, field := range []string{"version", "createdAt"} {
		fieldMap := props[field].(map[string]any)
		deprecated, ok := fieldMap["deprecated"]
		assert.True(t, ok, "field %q should have deprecated", field)
		assert.Equal(t, true, deprecated, "field %q should be deprecated", field)
	}
}

func TestWriteArtifactList_LatestOnly(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "json"

	now := time.Now()
	artifacts := []catalogpkg.ArtifactInfo{
		{
			Reference: catalogpkg.Reference{
				Name:    "my-solution",
				Kind:    catalogpkg.ArtifactKindSolution,
				Version: semver.MustParse("2.0.0"),
			},
			Digest:    "sha256:aaa",
			CreatedAt: now,
			Catalog:   "local",
		},
		{
			Reference: catalogpkg.Reference{
				Name:    "my-solution",
				Kind:    catalogpkg.ArtifactKindSolution,
				Version: semver.MustParse("1.0.0"),
			},
			Digest:    "sha256:bbb",
			CreatedAt: now,
			Catalog:   "local",
		},
	}

	err := writeArtifactList(artifacts, false, outputOpts)
	require.NoError(t, err)
}

func TestWriteArtifactList_AllVersions(t *testing.T) {
	t.Parallel()

	ioStreams, out, _ := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "json"

	now := time.Now()
	artifacts := []catalogpkg.ArtifactInfo{
		{
			Reference: catalogpkg.Reference{
				Name:    "my-solution",
				Kind:    catalogpkg.ArtifactKindSolution,
				Version: semver.MustParse("2.0.0"),
			},
			Digest:    "sha256:aaa",
			CreatedAt: now,
			Catalog:   "local",
		},
		{
			Reference: catalogpkg.Reference{
				Name:    "my-solution",
				Kind:    catalogpkg.ArtifactKindSolution,
				Version: semver.MustParse("1.0.0"),
			},
			Digest:    "sha256:bbb",
			CreatedAt: now,
			Catalog:   "local",
		},
	}

	err := writeArtifactList(artifacts, true, outputOpts)
	require.NoError(t, err)

	var items []ArtifactListItem
	err = json.Unmarshal(out.Bytes(), &items)
	require.NoError(t, err)
	assert.Len(t, items, 2, "all versions should be included")
	// Sorted descending by version
	assert.Equal(t, "2.0.0", items[0].Tag)
	assert.Equal(t, "1.0.0", items[1].Tag)
}

func TestWriteArtifactList_TagFallsBackToVersion(t *testing.T) {
	t.Parallel()

	ioStreams, out, _ := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "json"

	artifacts := []catalogpkg.ArtifactInfo{
		{
			Reference: catalogpkg.Reference{
				Name:    "foo",
				Kind:    catalogpkg.ArtifactKindSolution,
				Version: semver.MustParse("1.0.0"),
			},
			Digest:    "sha256:ccc",
			CreatedAt: time.Now(),
			Catalog:   "local",
		},
	}

	err := writeArtifactList(artifacts, true, outputOpts)
	require.NoError(t, err)

	var items []ArtifactListItem
	err = json.Unmarshal(out.Bytes(), &items)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "1.0.0", items[0].Tag, "tag should fall back to version when empty")
}

func TestWriteArtifactList_PreservesDigest(t *testing.T) {
	t.Parallel()

	ioStreams, out, _ := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "json"

	artifacts := []catalogpkg.ArtifactInfo{
		{
			Reference: catalogpkg.Reference{
				Name:    "foo",
				Kind:    catalogpkg.ArtifactKindSolution,
				Version: semver.MustParse("1.0.0"),
			},
			Digest:    "sha256:abc123def456",
			CreatedAt: time.Now(),
			Catalog:   "local",
		},
	}

	err := writeArtifactList(artifacts, true, outputOpts)
	require.NoError(t, err)

	var items []ArtifactListItem
	err = json.Unmarshal(out.Bytes(), &items)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "sha256:abc123def456", items[0].Digest, "digest should be preserved in full")
}

func TestWriteArtifactList_SortsByNameThenVersionDescending(t *testing.T) {
	t.Parallel()

	ioStreams, out, _ := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "json"

	now := time.Now()
	artifacts := []catalogpkg.ArtifactInfo{
		{
			Reference: catalogpkg.Reference{Name: "bravo", Kind: catalogpkg.ArtifactKindSolution, Version: semver.MustParse("1.0.0")},
			CreatedAt: now, Catalog: "local",
		},
		{
			Reference: catalogpkg.Reference{Name: "alpha", Kind: catalogpkg.ArtifactKindSolution, Version: semver.MustParse("1.0.0")},
			CreatedAt: now, Catalog: "local",
		},
		{
			Reference: catalogpkg.Reference{Name: "alpha", Kind: catalogpkg.ArtifactKindSolution, Version: semver.MustParse("2.0.0")},
			CreatedAt: now, Catalog: "local",
		},
	}

	err := writeArtifactList(artifacts, true, outputOpts)
	require.NoError(t, err)

	var items []ArtifactListItem
	err = json.Unmarshal(out.Bytes(), &items)
	require.NoError(t, err)
	require.Len(t, items, 3)
	assert.Equal(t, "alpha", items[0].Name)
	assert.Equal(t, "2.0.0", items[0].Tag)
	assert.Equal(t, "alpha", items[1].Name)
	assert.Equal(t, "1.0.0", items[1].Tag)
	assert.Equal(t, "bravo", items[2].Name)
}

func TestWriteArtifactList_CatalogColumn(t *testing.T) {
	t.Parallel()

	ioStreams, out, _ := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "json"

	artifacts := []catalogpkg.ArtifactInfo{
		{
			Reference: catalogpkg.Reference{Name: "foo", Kind: catalogpkg.ArtifactKindSolution, Version: semver.MustParse("1.0.0")},
			CreatedAt: time.Now(), Catalog: "my-registry",
		},
	}

	err := writeArtifactList(artifacts, true, outputOpts)
	require.NoError(t, err)

	var items []ArtifactListItem
	err = json.Unmarshal(out.Bytes(), &items)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "my-registry", items[0].Catalog)
}

func TestRunList_RemoteCatalogRequiresName(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandList(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"--catalog", "my-registry"})

	err := cmd.Execute()
	require.Error(t, err)
}

func BenchmarkCommandList(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		CommandList(cliParams, ioStreams, "scafctl/catalog")
	}
}

func BenchmarkWriteArtifactList(b *testing.B) {
	ioStreams, _, _ := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "json"

	now := time.Now()
	artifacts := []catalogpkg.ArtifactInfo{
		{
			Reference: catalogpkg.Reference{Name: "sol", Kind: catalogpkg.ArtifactKindSolution, Version: semver.MustParse("1.0.0")},
			Digest:    "sha256:abc",
			CreatedAt: now,
			Catalog:   "local",
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = writeArtifactList(artifacts, false, outputOpts)
	}
}
