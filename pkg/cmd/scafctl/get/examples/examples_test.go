// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package examples

import (
	"encoding/json"
	"errors"
	"testing"

	exampleslib "github.com/oakwood-commons/scafctl/pkg/examples"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandExamples(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandExamples(cliParams, ioStreams, "scafctl/get")
	require.NotNil(t, cmd)
	assert.Equal(t, "examples [example-path]", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
	assert.True(t, cmd.SilenceUsage)
	assert.NotNil(t, cmd.Flags().Lookup("category"))
	assert.NotNil(t, cmd.Flags().Lookup("output"))
}

func TestCommandExamples_List_JSON(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, out, _ := terminal.NewTestIOStreams()
	cmd := CommandExamples(cliParams, ioStreams, "scafctl/get")
	cmd.SetArgs([]string{"-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)

	var items []map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &items))
	assert.NotEmpty(t, items)
	assert.Contains(t, items[0], "path")
}

func TestCommandExamples_Get(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, out, _ := terminal.NewTestIOStreams()
	cmd := CommandExamples(cliParams, ioStreams, "scafctl/get")
	cmd.SetArgs([]string{"resolver-demo.yaml"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.NotEmpty(t, out.String())
}

func TestCommandExamples_Get_JSON(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, out, _ := terminal.NewTestIOStreams()
	cmd := CommandExamples(cliParams, ioStreams, "scafctl/get")
	cmd.SetArgs([]string{"resolver-demo.yaml", "-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)

	var item map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &item))
	assert.Equal(t, "scafctl.io/v1", item["apiVersion"])
	assert.Contains(t, item, "metadata")
}

func TestCommandExamples_Get_InvalidOutputFormat(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandExamples(cliParams, ioStreams, "scafctl/get")
	cmd.SetArgs([]string{"resolver-demo.yaml", "-o", "invalid-format"})

	err := cmd.Execute()
	assert.Error(t, err)
}

func TestCommandExamples_Get_PathTraversal(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandExamples(cliParams, ioStreams, "scafctl/get")
	cmd.SetArgs([]string{"../../etc/passwd"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.True(t, errors.Is(err, exampleslib.ErrPathTraversal))
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
}

func TestCommandExamples_Get_NotFound(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandExamples(cliParams, ioStreams, "scafctl/get")
	cmd.SetArgs([]string{"nonexistent-file.yaml"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Equal(t, exitcode.FileNotFound, exitcode.GetCode(err))
}

func TestCommandExamples_Get_SlugAndPartialMatch(t *testing.T) {
	t.Parallel()

	for _, query := range []string{"state-fingerprint", "fingerprint"} {
		t.Run(query, func(t *testing.T) {
			t.Parallel()
			cliParams := settings.NewCliParams()
			ioStreams, out, _ := terminal.NewTestIOStreams()
			cmd := CommandExamples(cliParams, ioStreams, "scafctl/get")
			cmd.SetArgs([]string{query})

			err := cmd.Execute()
			require.NoError(t, err)
			assert.Contains(t, out.String(), "name: state-fingerprint-showcase")
		})
	}
}

func TestCommandExamples_Get_MultipleMatchesShowsNameCategoryPath(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, out, errOut := terminal.NewTestIOStreams()
	cmd := CommandExamples(cliParams, ioStreams, "scafctl/get")
	cmd.SetArgs([]string{"hello-world"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, errOut.String(), `4 examples match "hello-world"`)

	stdout := out.String()
	for _, want := range []string{
		"hello-world-action",
		"hello-world-catalog",
		"hello-world-resolver",
		"hello-world-solution",
		"actions/hello-world.yaml",
		"catalog/hello-world/solution.yaml",
		"resolvers/hello-world.yaml",
		"solutions/hello-world/solution.yaml",
	} {
		assert.Contains(t, stdout, want)
	}
	assert.NotContains(t, stdout, "Hello World Action", "displayName must remain hidden in multi-match table output")
}

func TestCommandExamples_Get_CategoryFlagRejected(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandExamples(cliParams, ioStreams, "scafctl/get")
	cmd.SetArgs([]string{"cel-basics", "--category", "resolvers"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
}

func TestCommandExamples_EmbedderBinaryName(t *testing.T) {
	t.Parallel()

	t.Run("custom binary name mycli", func(t *testing.T) {
		t.Parallel()
		cliParams := settings.NewCliParams()
		cliParams.BinaryName = "mycli"

		ioStreams, _, errOut := terminal.NewTestIOStreams()
		cmd := CommandExamples(cliParams, ioStreams, "mycli/get")
		assert.Contains(t, cmd.Long, "mycli get examples")
		assert.NotContains(t, cmd.Long, settings.CliBinaryName+" get examples")

		cmd.SetArgs([]string{})
		require.NoError(t, cmd.Execute())
		assert.Contains(t, errOut.String(), "mycli get examples")
		assert.NotContains(t, errOut.String(), settings.CliBinaryName+" get examples")

		ioStreamsNotFound, _, errOutNotFound := terminal.NewTestIOStreams()
		cmdNotFound := CommandExamples(cliParams, ioStreamsNotFound, "mycli/get")
		cmdNotFound.SetArgs([]string{"does-not-exist-xyz"})
		require.Error(t, cmdNotFound.Execute())
		assert.Contains(t, errOutNotFound.String(), "Run 'mycli get examples' to list available examples.")
		assert.NotContains(t, errOutNotFound.String(), settings.CliBinaryName+" get examples")
	})

	t.Run("empty binary name falls back to default", func(t *testing.T) {
		t.Parallel()
		cliParams := &settings.Run{}
		ioStreams, _, errOut := terminal.NewTestIOStreams()
		cmd := CommandExamples(cliParams, ioStreams, "get")
		assert.Contains(t, cmd.Long, settings.CliBinaryName+" get examples")

		cmd.SetArgs([]string{})
		require.NoError(t, cmd.Execute())
		assert.Contains(t, errOut.String(), settings.CliBinaryName+" get examples")

		ioStreamsNotFound, _, errOutNotFound := terminal.NewTestIOStreams()
		cmdNotFound := CommandExamples(&settings.Run{}, ioStreamsNotFound, "get")
		cmdNotFound.SetArgs([]string{"does-not-exist-xyz"})
		require.Error(t, cmdNotFound.Execute())
		assert.Contains(t, errOutNotFound.String(), "Run '"+settings.CliBinaryName+" get examples'")
	})
}
