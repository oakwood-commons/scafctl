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

func TestCommandAttach_Structure(t *testing.T) {
	t.Parallel()

	ioStreams := &terminal.IOStreams{}
	cliParams := &settings.Run{BinaryName: "scafctl"}
	cmd := CommandAttach(cliParams, ioStreams, "")

	assert.Equal(t, "attach <name@version>", cmd.Use)
	assert.Contains(t, cmd.Short, "Attach")

	// Required flags
	fileFlag := cmd.Flags().Lookup("file")
	require.NotNil(t, fileFlag)

	typeFlag := cmd.Flags().Lookup("type")
	require.NotNil(t, typeFlag)

	catalogFlag := cmd.Flags().Lookup("catalog")
	require.NotNil(t, catalogFlag)

	insecureFlag := cmd.Flags().Lookup("insecure")
	require.NotNil(t, insecureFlag)
}

func TestCommandAttach_RequiresFileAndType(t *testing.T) {
	t.Parallel()

	ioStreams := &terminal.IOStreams{}
	cliParams := &settings.Run{BinaryName: "scafctl"}
	cmd := CommandAttach(cliParams, ioStreams, "")

	// file and type should be required
	for _, name := range []string{"file", "type"} {
		f := cmd.Flags().Lookup(name)
		require.NotNil(t, f)
		// Cobra marks required flags with an annotation
		ann := f.Annotations
		if ann != nil {
			_, ok := ann["cobra_annotation_bash_completion_one_required_flag"]
			assert.True(t, ok, "flag %q should be marked required", name)
		}
	}
}

func TestCommandAttach_ExactArgs(t *testing.T) {
	t.Parallel()

	ioStreams := &terminal.IOStreams{}
	cliParams := &settings.Run{BinaryName: "scafctl"}
	cmd := CommandAttach(cliParams, ioStreams, "")

	// Should require exactly 1 arg
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestCommandAttach_MissingVersion(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandAttach(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"my-solution", "--file", "sbom.json", "--type", "application/spdx+json"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version required")
}

func TestCommandAttach_FileFlagShorthand(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandAttach(cliParams, ioStreams, "scafctl/catalog")

	f := cmd.Flags().ShorthandLookup("f")
	require.NotNil(t, f, "shorthand -f should exist")
	assert.Equal(t, "file", f.Name)
}

func TestCommandAttach_TypeFlagShorthand(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandAttach(cliParams, ioStreams, "scafctl/catalog")

	f := cmd.Flags().ShorthandLookup("t")
	require.NotNil(t, f, "shorthand -t should exist")
	assert.Equal(t, "type", f.Name)
}

func TestCommandAttach_CatalogFlagShorthand(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandAttach(cliParams, ioStreams, "scafctl/catalog")

	f := cmd.Flags().ShorthandLookup("c")
	require.NotNil(t, f, "shorthand -c should exist")
	assert.Equal(t, "catalog", f.Name)
}

func TestCommandAttach_FileNotFound(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandAttach(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"my-solution@1.0.0", "--file", "/nonexistent/file.json", "--type", "application/spdx+json", "--catalog", "ghcr.io/myorg"})

	err := cmd.Execute()
	require.Error(t, err)
}

func BenchmarkCommandAttach(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		CommandAttach(cliParams, ioStreams, "scafctl/catalog")
	}
}
