// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandInspect(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandInspect(cliParams, ioStreams, "scafctl/catalog")

	require.NotNil(t, cmd)
	assert.Equal(t, "inspect <name[@version]>", cmd.Use)
	assert.Contains(t, cmd.Aliases, "info")
	assert.Contains(t, cmd.Aliases, "show")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandInspect_Flags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandInspect(cliParams, ioStreams, "scafctl/catalog")

	flagTests := []struct {
		name     string
		defValue string
	}{
		{"catalog", ""},
		{"referrers", "false"},
		{"artifact-type", ""},
		{"insecure", "false"},
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

func TestCommandInspect_CatalogFlagShorthand(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandInspect(cliParams, ioStreams, "scafctl/catalog")

	f := cmd.Flags().ShorthandLookup("c")
	require.NotNil(t, f, "shorthand -c should exist")
	assert.Equal(t, "catalog", f.Name)
}

func TestCommandInspect_RequiresExactlyOneArg(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandInspect(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg(s)")
}

func TestCommandInspect_InvalidReference(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandInspect(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"@@@invalid"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestCommandInspect_ReferrersRequiresCatalog(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandInspect(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"my-solution@1.0.0", "--referrers"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "catalog required")
}

func TestCommandInspect_ReferrersInvalidCatalog(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandInspect(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	// Provide --catalog but the URL resolution will fail (no config)
	cmd.SetArgs([]string{"my-solution@1.0.0", "--referrers", "--catalog", "nonexistent-catalog"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestCommandInspect_MultipleArgs(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandInspect(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetArgs([]string{"sol1", "sol2"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg(s)")
}

func TestArtifactDetail_Fields(t *testing.T) {
	t.Parallel()

	detail := ArtifactDetail{
		Name:      "my-solution",
		Version:   "1.0.0",
		Kind:      "solution",
		Digest:    "sha256:abc123",
		Size:      1234,
		CreatedAt: "2025-01-01 00:00:00",
		Catalog:   "local",
		Annotations: map[string]string{
			"org.opencontainers.image.title": "my-solution",
		},
	}

	assert.Equal(t, "my-solution", detail.Name)
	assert.Equal(t, "1.0.0", detail.Version)
	assert.Equal(t, "solution", detail.Kind)
	assert.Equal(t, "sha256:abc123", detail.Digest)
	assert.Equal(t, int64(1234), detail.Size)
	assert.Equal(t, "local", detail.Catalog)
	assert.Contains(t, detail.Annotations, "org.opencontainers.image.title")
}

func BenchmarkCommandInspect(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		CommandInspect(cliParams, ioStreams, "scafctl/catalog")
	}
}
