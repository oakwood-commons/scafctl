// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	catalogpkg "github.com/oakwood-commons/scafctl/pkg/catalog"
	appconfig "github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
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
		{"pre-release", "false"},
		{"all", "false"},
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

func TestCommandList_NameAtVersionStripped(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(context.Background(), w)

	cliParams := settings.NewCliParams()
	cmd := CommandList(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--name", "email-notifier@1.0.0"})

	err := cmd.Execute()
	require.NoError(t, err)

	// The @version should be stripped so it searches for "email-notifier", not
	// the literal "email-notifier@1.0.0" (which would never match an annotation).
	// If stripping didn't happen, we'd get "No artifacts found" even when one exists.
	output := buf.String()
	assert.NotContains(t, output, "email-notifier@1.0.0",
		"name@version should not appear literally in output -- @ must be stripped")
}

func TestCommandList_FullOCIRefConflictsWithCatalog(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandList(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"--name", "ghcr.io/myorg/solutions/my-solution@1.0.0", "--catalog", "my-registry"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflicting options")
}

func TestRunList_InvalidConstraintSyntax_ReturnsInvalidInput(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandList(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"--name", "my-app", "--version", "not-valid!!"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err), "invalid constraint syntax should return InvalidInput exit code")
}

func TestRunList_ConflictingVersionFlags_ReturnsGeneralError(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandList(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"--name", "my-app@1.0.0", "--version", "^1.0.0"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflicting options")
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

func TestArtifactListSchema_DigestHiddenInTable(t *testing.T) {
	t.Parallel()

	var schema map[string]any
	err := json.Unmarshal(artifactListSchema, &schema)
	require.NoError(t, err)

	items := schema["items"].(map[string]any)
	props := items["properties"].(map[string]any)
	digest := props["digest"].(map[string]any)

	// Digest should be hidden from table view (deprecated flag set)
	deprecated, ok := digest["deprecated"]
	assert.True(t, ok, "digest column should have deprecated flag")
	assert.Equal(t, true, deprecated, "digest column should be deprecated (hidden from table view)")
}

func TestArtifactListSchema_HiddenFields(t *testing.T) {
	t.Parallel()

	var schema map[string]any
	err := json.Unmarshal(artifactListSchema, &schema)
	require.NoError(t, err)

	items := schema["items"].(map[string]any)
	props := items["properties"].(map[string]any)

	// version, createdAt, and digest should be hidden
	for _, field := range []string{"version", "createdAt", "digest"} {
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

	w := writer.New(ioStreams, &settings.Run{})
	err := writeArtifactList(context.Background(), w, artifacts, false, outputOpts, nil, len(artifacts))
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

	w := writer.New(ioStreams, &settings.Run{})
	err := writeArtifactList(context.Background(), w, artifacts, true, outputOpts, nil, len(artifacts))
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

	w := writer.New(ioStreams, &settings.Run{})
	err := writeArtifactList(context.Background(), w, artifacts, true, outputOpts, nil, len(artifacts))
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

	w := writer.New(ioStreams, &settings.Run{})
	err := writeArtifactList(context.Background(), w, artifacts, true, outputOpts, nil, len(artifacts))
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

	w := writer.New(ioStreams, &settings.Run{})
	err := writeArtifactList(context.Background(), w, artifacts, true, outputOpts, nil, len(artifacts))
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

	w := writer.New(ioStreams, &settings.Run{})
	err := writeArtifactList(context.Background(), w, artifacts, true, outputOpts, nil, len(artifacts))
	require.NoError(t, err)

	var items []ArtifactListItem
	err = json.Unmarshal(out.Bytes(), &items)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "my-registry", items[0].Catalog)
}

func TestWriteArtifactList_CrossCatalogDedup(t *testing.T) {
	t.Parallel()

	ioStreams, out, _ := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "json"

	now := time.Now()
	artifacts := []catalogpkg.ArtifactInfo{
		{
			Reference: catalogpkg.Reference{Name: "app", Kind: catalogpkg.ArtifactKindSolution, Version: semver.MustParse("1.0.0")},
			Digest:    "sha256:abc",
			CreatedAt: now,
			Catalog:   "local",
		},
		{
			Reference: catalogpkg.Reference{Name: "app", Kind: catalogpkg.ArtifactKindSolution, Version: semver.MustParse("1.0.0")},
			Digest:    "",
			Catalog:   "remote",
		},
	}

	w := writer.New(ioStreams, &settings.Run{})
	err := writeArtifactList(context.Background(), w, artifacts, true, outputOpts, nil, len(artifacts))
	require.NoError(t, err)

	var items []ArtifactListItem
	err = json.Unmarshal(out.Bytes(), &items)
	require.NoError(t, err)
	require.Len(t, items, 1, "duplicate artifact should be merged")
	assert.Equal(t, "local, remote", items[0].Catalog)
	assert.Equal(t, "sha256:abc", items[0].Digest, "should use row with digest")
}

func TestWriteArtifactList_LatestOnlyAfterDedup(t *testing.T) {
	t.Parallel()

	ioStreams, out, _ := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "json"

	now := time.Now()
	artifacts := []catalogpkg.ArtifactInfo{
		{
			Reference: catalogpkg.Reference{Name: "app", Kind: catalogpkg.ArtifactKindSolution, Version: semver.MustParse("2.0.0")},
			Digest:    "sha256:222",
			CreatedAt: now,
			Catalog:   "local",
		},
		{
			Reference: catalogpkg.Reference{Name: "app", Kind: catalogpkg.ArtifactKindSolution, Version: semver.MustParse("1.0.0")},
			Digest:    "sha256:111",
			CreatedAt: now,
			Catalog:   "local",
		},
		{
			Reference: catalogpkg.Reference{Name: "app", Kind: catalogpkg.ArtifactKindSolution, Version: semver.MustParse("1.0.0")},
			Catalog:   "remote",
		},
	}

	w := writer.New(ioStreams, &settings.Run{})
	// showAll=false → only latest version per name+kind
	err := writeArtifactList(context.Background(), w, artifacts, false, outputOpts, nil, len(artifacts))
	require.NoError(t, err)

	var items []ArtifactListItem
	err = json.Unmarshal(out.Bytes(), &items)
	require.NoError(t, err)
	require.Len(t, items, 1, "should show only latest version")
	assert.Equal(t, "2.0.0", items[0].Tag)
	assert.Equal(t, "local", items[0].Catalog)
}

func TestWriteArtifactList_EmptyRespectsQuiet(t *testing.T) {
	t.Parallel()

	ioStreams, _, errBuf := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)

	// With quiet=true, the "No artifacts found" message should be suppressed.
	w := writer.New(ioStreams, &settings.Run{IsQuiet: true})
	err := writeArtifactList(context.Background(), w, nil, false, outputOpts, nil, 0)
	require.NoError(t, err)
	assert.Empty(t, errBuf.String(), "quiet mode should suppress info messages")
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

func TestCommandList_EmbedderBinaryName(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandList(cliParams, ioStreams, "mycli/catalog")

	require.NotNil(t, cmd)
	assert.Equal(t, "list", cmd.Use)
	assert.NotNil(t, cmd.RunE)
}

func TestWriteArtifactList_NameWithoutAllVersions_ShowsLatestOnly(t *testing.T) {
	t.Parallel()

	ioStreams, out, _ := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "json"

	now := time.Now()
	artifacts := []catalogpkg.ArtifactInfo{
		{
			Reference: catalogpkg.Reference{
				Name:    "email-notifier",
				Kind:    catalogpkg.ArtifactKindSolution,
				Version: semver.MustParse("2.0.0"),
			},
			Digest:    "sha256:aaa",
			CreatedAt: now,
			Catalog:   "local",
		},
		{
			Reference: catalogpkg.Reference{
				Name:    "email-notifier",
				Kind:    catalogpkg.ArtifactKindSolution,
				Version: semver.MustParse("1.0.0"),
			},
			Digest:    "sha256:bbb",
			CreatedAt: now,
			Catalog:   "local",
		},
	}

	// showAll=false simulates --name without --all-versions.
	// Previously --name implied --all-versions; now it does not.
	w := writer.New(ioStreams, &settings.Run{})
	err := writeArtifactList(context.Background(), w, artifacts, false, outputOpts, nil, len(artifacts))
	require.NoError(t, err)

	var items []ArtifactListItem
	err = json.Unmarshal(out.Bytes(), &items)
	require.NoError(t, err)
	assert.Len(t, items, 1, "--name without --all-versions should show only the latest version")
	assert.Equal(t, "2.0.0", items[0].Version)
}

func TestWriteArtifactList_LatestOnly_AssertsSingleResult(t *testing.T) {
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

	w := writer.New(ioStreams, &settings.Run{})
	err := writeArtifactList(context.Background(), w, artifacts, false, outputOpts, nil, len(artifacts))
	require.NoError(t, err)

	var items []ArtifactListItem
	err = json.Unmarshal(out.Bytes(), &items)
	require.NoError(t, err)
	assert.Len(t, items, 1, "showAll=false should keep only the latest version per name+kind")
	assert.Equal(t, "2.0.0", items[0].Version)
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

	w := writer.New(ioStreams, &settings.Run{})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = writeArtifactList(context.Background(), w, artifacts, false, outputOpts, nil, len(artifacts))
	}
}

// staleDegraded builds an AuthDegradedError from a real RemoteCatalog marked
// stale, exercising the same NewAuthDegradedError path the command uses.
func staleDegraded(t *testing.T) *catalogpkg.AuthDegradedError {
	t.Helper()
	rc, err := catalogpkg.NewRemoteCatalog(catalogpkg.RemoteCatalogConfig{Registry: "ghcr.io"})
	require.NoError(t, err)
	rc.SetStaleForTesting()
	rc.SetCredentialSourceForTest("github auth handler token")
	d := catalogpkg.NewAuthDegradedError(rc)
	require.NotNil(t, d)
	return d
}

func TestWriteArtifactList_EmptyStale_FailsLoudly(t *testing.T) {
	ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	w := writer.New(ioStreams, &settings.Run{})

	degraded := staleDegraded(t)
	err := writeArtifactList(context.Background(), w, nil, false, outputOpts, degraded, 0)

	require.Error(t, err, "empty result on stale credentials must return an error")
	assert.Equal(t, exitcode.CatalogError, exitcode.GetCode(err))
	assert.NotContains(t, outBuf.String(), "No artifacts found in catalog.",
		"must not assert emptiness when credentials were rejected")
	// The fatal path emits an explicit error line to stderr (Cobra silences the
	// returned error), not just a partial-listing warning.
	assert.Contains(t, errBuf.String(), "Cannot list catalog")
	assert.NotContains(t, errBuf.String(), "showing anonymous results only",
		"empty result should use the fatal error framing, not the partial warning")
	assert.Contains(t, errBuf.String(), "ghcr.io")
	assert.Contains(t, errBuf.String(), "auth login")
}

// When the raw anonymous listing returned artifacts (rawResultCount > 0) but
// user filters (pre-release, version constraint, --catalog) removed them all,
// the empty final set is due to filtering, NOT the auth failure. This must be
// non-fatal (exit 0) even though credentials were rejected: we still warn that
// the listing was degraded, but we do not claim the auth failure emptied the
// catalog.
func TestWriteArtifactList_EmptyStaleButFilteredOut_NonFatal(t *testing.T) {
	ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	w := writer.New(ioStreams, &settings.Run{})

	degraded := staleDegraded(t)
	// rawResultCount = 3: the anonymous listing had results, but filters left
	// nothing in `artifacts`.
	err := writeArtifactList(context.Background(), w, nil, false, outputOpts, degraded, 3)

	require.NoError(t, err, "filter-emptied listing must not be treated as a fatal auth failure")
	assert.NotContains(t, errBuf.String(), "Cannot list catalog",
		"must not use the fatal empty-catalog framing when emptiness came from filtering")
	assert.Contains(t, errBuf.String(), "Catalog listing is incomplete",
		"should still surface the degraded/partial warning")
	assert.Contains(t, outBuf.String(), "No artifacts found in catalog.")
}

func TestWriteArtifactList_EmptyNoStale_Unchanged(t *testing.T) {
	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	w := writer.New(ioStreams, &settings.Run{})

	err := writeArtifactList(context.Background(), w, nil, false, outputOpts, nil, 0)
	require.NoError(t, err)
	assert.Contains(t, outBuf.String(), "No artifacts found in catalog.")
}

func TestWriteArtifactList_NonEmptyStale_WarnsButSucceeds(t *testing.T) {
	ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	w := writer.New(ioStreams, &settings.Run{})

	artifacts := []catalogpkg.ArtifactInfo{{
		Reference: catalogpkg.Reference{
			Name:    "demo-app",
			Kind:    catalogpkg.ArtifactKindSolution,
			Version: semver.MustParse("1.0.0"),
		},
		Catalog: "remote",
	}}
	degraded := staleDegraded(t)

	err := writeArtifactList(context.Background(), w, artifacts, false, outputOpts, degraded, len(artifacts))
	require.NoError(t, err, "partial (non-empty) result must still succeed")
	assert.Contains(t, outBuf.String(), "demo-app")
	assert.Contains(t, errBuf.String(), "incomplete")
	assert.Contains(t, errBuf.String(), "auth login")
}

func TestWriteArtifactList_EmptyStaleJSON_MarkerOnStderr(t *testing.T) {
	ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "json"
	w := writer.New(ioStreams, &settings.Run{})

	degraded := staleDegraded(t)
	err := writeArtifactList(context.Background(), w, nil, false, outputOpts, degraded, 0)

	require.Error(t, err)
	assert.Equal(t, exitcode.CatalogError, exitcode.GetCode(err))
	// stdout stays a parseable JSON array (an empty [] here); the degraded
	// signal is on stderr, not asserted as an authoritative empty catalog.
	assert.NotContains(t, outBuf.String(), "No artifacts found")
	// The structured degraded marker goes to stderr, preserving stdout's contract.
	assert.Contains(t, errBuf.String(), `"degraded":true`)
	assert.Contains(t, errBuf.String(), `"authError"`)
}

// TestWriteArtifactList_EmptyStale_HandlerOnly covers the credential-source
// fallback when only an auth handler (no explicit source string) is known.
func TestWriteArtifactList_EmptyStale_HandlerOnly(t *testing.T) {
	ioStreams, _, errBuf := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	w := writer.New(ioStreams, &settings.Run{})

	// Handler set but no explicit credential source -> "<handler> auth handler
	// credentials" wording.
	degraded := &catalogpkg.AuthDegradedError{Registry: "ghcr.io", Handler: "github"}
	err := writeArtifactList(context.Background(), w, nil, false, outputOpts, degraded, 0)

	require.Error(t, err)
	assert.Equal(t, exitcode.CatalogError, exitcode.GetCode(err))
	assert.Contains(t, errBuf.String(), "auth handler credentials")
	assert.Contains(t, errBuf.String(), "auth login github")
}

// TestWriteArtifactList_EmptyStale_GenericSourceAndRegistry covers the generic
// "stored credentials" wording and the catalog-login fallback for a registry
// with no inferable auth handler.
func TestWriteArtifactList_EmptyStale_GenericSourceAndRegistry(t *testing.T) {
	ioStreams, _, errBuf := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	w := writer.New(ioStreams, &settings.Run{})

	// An arbitrary private registry with no known handler and no source.
	rc, err := catalogpkg.NewRemoteCatalog(catalogpkg.RemoteCatalogConfig{Registry: "private.example.com"})
	require.NoError(t, err)
	rc.SetStaleForTesting()
	degraded := catalogpkg.NewAuthDegradedError(rc)
	require.NotNil(t, degraded)

	wErr := writeArtifactList(context.Background(), w, nil, false, outputOpts, degraded, 0)
	require.Error(t, wErr)
	assert.Contains(t, errBuf.String(), "stored credentials")
	assert.Contains(t, errBuf.String(), "private.example.com")
	assert.Contains(t, errBuf.String(), "catalog login private.example.com")
}

// TestWriteArtifactList_PrefersRecordedHandler verifies the fix hint uses the
// handler that actually supplied the rejected credentials, even when the
// registry host is not one InferAuthHandler recognizes.
func TestWriteArtifactList_PrefersRecordedHandler(t *testing.T) {
	ioStreams, _, errBuf := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	w := writer.New(ioStreams, &settings.Run{})

	// Non-inferable registry, but a handler was recorded on the error.
	degraded := &catalogpkg.AuthDegradedError{Registry: "private.example.com", Handler: "corp-sso"}
	err := writeArtifactList(context.Background(), w, nil, false, outputOpts, degraded, 0)

	require.Error(t, err)
	assert.Contains(t, errBuf.String(), "auth login corp-sso",
		"should recommend logging in with the recorded handler, not catalog login")
	assert.NotContains(t, errBuf.String(), "catalog login")
}

// TestWriteArtifactList_QuietSuppressesDegradedOutput verifies that quiet output
// emits no degraded warning while still returning the failing exit code.
func TestWriteArtifactList_QuietSuppressesDegradedOutput(t *testing.T) {
	ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = kvx.OutputFormatQuiet
	w := writer.New(ioStreams, &settings.Run{})

	degraded := staleDegraded(t)
	err := writeArtifactList(context.Background(), w, nil, false, outputOpts, degraded, 0)

	require.Error(t, err, "exit code still conveys the failure")
	assert.Equal(t, exitcode.CatalogError, exitcode.GetCode(err))
	assert.Empty(t, errBuf.String(), "quiet output must suppress the degraded warning")
	assert.Empty(t, outBuf.String(), "quiet output must suppress stdout")
}

// TestWriteArtifactList_EmptyStaleStructured_WritesEmptyArray verifies the
// structured (json/yaml) empty+stale path writes a parseable empty array to
// stdout (so consumers can parse stdout) while the marker goes to stderr.
func TestWriteArtifactList_EmptyStaleStructured_WritesEmptyArray(t *testing.T) {
	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "json"
	w := writer.New(ioStreams, &settings.Run{})

	degraded := staleDegraded(t)
	err := writeArtifactList(context.Background(), w, nil, false, outputOpts, degraded, 0)

	require.Error(t, err)
	assert.Contains(t, outBuf.String(), "[]", "stdout should contain a parseable empty array")
}

// TestRunList_CatalogOfficialDisabled_ReturnsInvalidInput verifies that an
// explicit `--catalog official` selection fails with a clear InvalidInput error
// when settings.disableOfficialCatalog is set, rather than silently returning an
// empty result that looks like "no artifacts found".
func TestRunList_CatalogOfficialDisabled_ReturnsInvalidInput(t *testing.T) {
	t.Parallel()

	cfg := &appconfig.Config{
		Settings: appconfig.Settings{DisableOfficialCatalog: true},
		Catalogs: []appconfig.CatalogConfig{
			{
				Name: appconfig.CatalogNameOfficial,
				Type: appconfig.CatalogTypeOCI,
				URL:  "oci://ghcr.io/oakwood-commons",
			},
		},
	}

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := appconfig.WithConfig(writer.WithWriter(context.Background(), w), cfg)

	cliParams := settings.NewCliParams()
	cmd := CommandList(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--catalog", appconfig.CatalogNameOfficial})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err),
		"disabled official catalog explicitly selected should return InvalidInput")
	assert.Contains(t, err.Error(), "official catalog is disabled")
	assert.NotContains(t, buf.String(), "No artifacts found",
		"should not present a disabled catalog as an empty result")
}

// unreachableOCIURL points at a closed local port so remote catalog listing
// fails fast (connection refused, ~ms) instead of hanging on the network. This
// lets runList's catalog-selection switch and remote loop be exercised
// deterministically without a real registry.
const unreachableOCIURL = "oci://127.0.0.1:1/nope"

// unreachableOCIURL2 is a second closed-port URL, distinct from unreachableOCIURL
// so DefaultListCatalogs does not dedup the official fallback against a default
// that happens to share the same URL.
const unreachableOCIURL2 = "oci://127.0.0.1:2/nope"

// runListWithConfig executes `catalog list` with the given config in context
// and the supplied args, returning combined stdout+stderr and the error.
func runListWithConfig(t *testing.T, cfg *appconfig.Config, args ...string) (string, error) {
	t.Helper()

	// Bound the context so a filtered (rather than refused) localhost port can
	// never hang the test; the unreachable OCI URLs normally fail fast with
	// "connection refused".
	baseCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)

	// Share a single verbose-enabled CliParams between the writer and the
	// command so per-catalog verbose diagnostics ("Searching remote catalog",
	// "Skipping catalog") are observable in the captured output.
	cliParams := settings.NewCliParams()
	cliParams.Verbose = true

	w := writer.New(ioStreams, cliParams)
	ctx := appconfig.WithConfig(writer.WithWriter(baseCtx, w), cfg)

	cmd := CommandList(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(ctx)
	cmd.SetArgs(args)

	err := cmd.Execute()
	return buf.String(), err
}

// TestRunList_DefaultScope_QueriesDefaultThenOfficial verifies the bare-list
// (default) switch arm: runList selects catalog.DefaultListCatalogs(cfg) and
// searches the default catalog followed by the official fallback. Both remote
// catalogs are unreachable (connection refused), so the command still completes
// with exit 0 and reports no artifacts, but the selection + remote loop over
// both catalogs is exercised.
func TestRunList_DefaultScope_QueriesDefaultThenOfficial(t *testing.T) {
	t.Parallel()

	cfg := &appconfig.Config{
		Settings: appconfig.Settings{DefaultCatalog: "corp"},
		Catalogs: []appconfig.CatalogConfig{
			{Name: "corp", Type: appconfig.CatalogTypeOCI, URL: unreachableOCIURL},
			{Name: appconfig.CatalogNameOfficial, Type: appconfig.CatalogTypeOCI, URL: unreachableOCIURL2},
		},
	}

	out, err := runListWithConfig(t, cfg)
	require.NoError(t, err, "unreachable remote catalogs are non-fatal for bare list")
	// Both the primary default and the official fallback should have been tried.
	assert.Contains(t, out, `failed to list from remote catalog "corp"`)
	assert.Contains(t, out, `failed to list from remote catalog "official"`)
}

// TestRunList_CatalogScope_SelectsNamedCatalog verifies the `--catalog <name>`
// switch arm: only the named configured OCI catalog is queried.
func TestRunList_CatalogScope_SelectsNamedCatalog(t *testing.T) {
	t.Parallel()

	cfg := &appconfig.Config{
		Catalogs: []appconfig.CatalogConfig{
			{Name: "corp", Type: appconfig.CatalogTypeOCI, URL: unreachableOCIURL},
			{Name: appconfig.CatalogNameOfficial, Type: appconfig.CatalogTypeOCI, URL: unreachableOCIURL},
		},
	}

	out, err := runListWithConfig(t, cfg, "--catalog", "corp")
	require.NoError(t, err)
	assert.Contains(t, out, `failed to list from remote catalog "corp"`)
	// Only the explicitly named catalog is queried -- official is not consulted.
	assert.NotContains(t, out, `failed to list from remote catalog "official"`)
}

// TestRunList_AllScope_SelectsEveryOCICatalog verifies the `--all` switch arm:
// every configured OCI catalog is queried (non-OCI entries are skipped). Under
// --all, per-catalog errors are demoted to verbose; with verbose enabled the
// per-catalog diagnostics prove both OCI entries were iterated and the non-OCI
// entry was not.
func TestRunList_AllScope_SelectsEveryOCICatalog(t *testing.T) {
	t.Parallel()

	cfg := &appconfig.Config{
		Catalogs: []appconfig.CatalogConfig{
			{Name: "corp", Type: appconfig.CatalogTypeOCI, URL: unreachableOCIURL},
			{Name: "mirror", Type: appconfig.CatalogTypeOCI, URL: unreachableOCIURL},
			// A non-OCI catalog must be skipped by the --all selection.
			{Name: "local-files", Type: appconfig.CatalogTypeFilesystem, URL: ""},
		},
	}

	out, err := runListWithConfig(t, cfg, "--all")
	require.NoError(t, err)
	// Both OCI catalogs are iterated: verbose "Searching remote catalog" fires
	// for each before the (refused) network call, and the demoted-to-verbose
	// "Skipping catalog" fires on failure.
	assert.Contains(t, out, `Searching remote catalog "corp"`)
	assert.Contains(t, out, `Searching remote catalog "mirror"`)
	assert.Contains(t, out, `Skipping catalog "corp"`)
	assert.Contains(t, out, `Skipping catalog "mirror"`)
	// The non-OCI filesystem catalog is dropped by the Type==OCI selection
	// guard and never iterated.
	assert.NotContains(t, out, `"local-files"`)
	// The run still completes successfully and reports no artifacts.
	assert.Contains(t, out, "No artifacts found")
}
