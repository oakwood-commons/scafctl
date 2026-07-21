// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package extract

import (
	"bytes"
	"os"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newPrintWriter returns a Writer whose stdout and stderr are directed to the
// same buffer, so tests can assert on the combined text output of printFileList
// regardless of which stream each line targets.
func newPrintWriter() (*bytes.Buffer, *writer.Writer) {
	buf := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{
		In:     os.Stdin,
		Out:    buf,
		ErrOut: buf,
	}
	cliParams := settings.NewCliParams()
	cliParams.NoColor = true
	return buf, writer.New(ioStreams, cliParams)
}

func TestCommandExtractBundle(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandExtractBundle(cliParams, ioStreams, "")

	require.NotNil(t, cmd)
	assert.Equal(t, "bundle <artifact-ref>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.True(t, cmd.SilenceUsage)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandExtractBundle_Flags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandExtractBundle(cliParams, ioStreams, "")

	outputDirFlag := cmd.Flags().Lookup("output-dir")
	require.NotNil(t, outputDirFlag, "output-dir flag should exist")
	assert.Equal(t, ".", outputDirFlag.DefValue)

	resolverFlag := cmd.Flags().Lookup("resolver")
	require.NotNil(t, resolverFlag, "resolver flag should exist")
	assert.Equal(t, "[]", resolverFlag.DefValue)

	actionFlag := cmd.Flags().Lookup("action")
	require.NotNil(t, actionFlag, "action flag should exist")
	assert.Equal(t, "[]", actionFlag.DefValue)

	includeFlag := cmd.Flags().Lookup("include")
	require.NotNil(t, includeFlag, "include flag should exist")
	assert.Equal(t, "[]", includeFlag.DefValue)

	listOnlyFlag := cmd.Flags().Lookup("list-only")
	require.NotNil(t, listOnlyFlag, "list-only flag should exist")
	assert.Equal(t, "false", listOnlyFlag.DefValue)

	flattenFlag := cmd.Flags().Lookup("flatten")
	require.NotNil(t, flattenFlag, "flatten flag should exist")
	assert.Equal(t, "false", flattenFlag.DefValue)
}

func TestCommandExtractBundle_RequiresExactlyOneArg(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	// No args should fail
	cmd := CommandExtractBundle(cliParams, ioStreams, "")
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)

	// Two args should fail
	cmd2 := CommandExtractBundle(cliParams, ioStreams, "")
	cmd2.SilenceErrors = true
	cmd2.SetArgs([]string{"ref1", "ref2"})
	err = cmd2.Execute()
	assert.Error(t, err)
}

func BenchmarkCommandExtractBundle(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandExtractBundle(cliParams, ioStreams, "")
	}
}

func TestPrintFileList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		files       []bundler.BundleFileEntry
		resolvers   []string
		actions     []string
		wantContain []string
		wantAbsent  []string
	}{
		{
			name: "files with resolver header",
			files: []bundler.BundleFileEntry{
				{Path: "templates/main.tf.tmpl", Size: 1024},
				{Path: "scripts/setup.sh", Size: 512},
			},
			resolvers: []string{"mainTfTemplate"},
			wantContain: []string{
				"Files needed for resolver(s): mainTfTemplate",
				"templates/main.tf.tmpl",
				"scripts/setup.sh",
				"Total: 2 file(s)",
			},
			wantAbsent: []string{
				"Files needed for action(s):",
			},
		},
		{
			name: "files with action header",
			files: []bundler.BundleFileEntry{
				{Path: "run.sh", Size: 2048},
			},
			actions: []string{"deploy"},
			wantContain: []string{
				"Files needed for action(s): deploy",
				"run.sh",
				"Total: 1 file(s)",
			},
			wantAbsent: []string{
				"Files needed for resolver(s):",
			},
		},
		{
			name:  "empty file list",
			files: nil,
			wantContain: []string{
				"Total: 0 file(s)",
			},
			wantAbsent: []string{
				"Files needed for resolver(s):",
				"Files needed for action(s):",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			buf, w := newPrintWriter()
			printFileList(w, tc.files, tc.resolvers, tc.actions)
			out := buf.String()
			for _, want := range tc.wantContain {
				assert.Contains(t, out, want)
			}
			for _, absent := range tc.wantAbsent {
				assert.NotContains(t, out, absent)
			}
		})
	}
}

// TestFilterFilesByInclude exercises the include-glob filtering branch of
// runExtract's file selection logic without any catalog or network access.
// The catalog-fetch path of runExtract remains covered by integration tests.
func TestFilterFilesByInclude(t *testing.T) {
	t.Parallel()

	manifest := []bundler.BundleFileEntry{
		{Path: "templates/main.tf.tmpl", Size: 100},
		{Path: "templates/vars.tf.tmpl", Size: 200},
		{Path: "scripts/setup.sh", Size: 300},
		{Path: "README.md", Size: 400},
	}

	tests := []struct {
		name    string
		pattern string
		want    []string
	}{
		{
			name:    "glob matches template files",
			pattern: "templates/*.tmpl",
			want:    []string{"templates/main.tf.tmpl", "templates/vars.tf.tmpl"},
		},
		{
			name:    "glob matches single file",
			pattern: "README.md",
			want:    []string{"README.md"},
		},
		{
			name:    "glob matches nothing",
			pattern: "nonexistent/*.go",
			want:    nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var matched []string
			for _, entry := range manifest {
				if bundler.MatchGlob(tc.pattern, entry.Path) {
					matched = append(matched, entry.Path)
				}
			}
			assert.Equal(t, tc.want, matched)
		})
	}
}
