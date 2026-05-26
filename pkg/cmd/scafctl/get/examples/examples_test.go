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
