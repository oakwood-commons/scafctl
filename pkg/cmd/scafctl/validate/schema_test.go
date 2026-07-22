// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSchemaJSON = `{
  "type": "object",
  "properties": {"name": {"type": "string"}},
  "required": ["name"],
  "additionalProperties": false
}`

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

func TestCommandValidateSchema_Metadata(t *testing.T) {
	t.Parallel()
	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"

	cmd := CommandValidateSchema(cliParams, streams, "mycli/validate")
	assert.Equal(t, "schema", cmd.Name())
	assert.Contains(t, cmd.Short, "mycli")
	assert.NotNil(t, cmd.Flags().Lookup("schema"))
	assert.NotNil(t, cmd.Flags().Lookup("data"))
}

func TestCommandValidateSchema_ValidData(t *testing.T) {
	dir := t.TempDir()
	schemaPath := writeFile(t, dir, "schema.json", testSchemaJSON)
	dataPath := writeFile(t, dir, "data.json", `{"name":"hi"}`)

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandValidateSchema(cliParams, streams, "scafctl/validate")
	cmd.SetArgs([]string{"--schema", schemaPath, "--data", dataPath})
	err := cmd.Execute()
	require.NoError(t, err)
}

func TestCommandValidateSchema_InvalidData(t *testing.T) {
	dir := t.TempDir()
	schemaPath := writeFile(t, dir, "schema.json", testSchemaJSON)
	dataPath := writeFile(t, dir, "data.json", `{"extra":"nope"}`)

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandValidateSchema(cliParams, streams, "scafctl/validate")
	cmd.SetArgs([]string{"--schema", schemaPath, "--data", dataPath})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Equal(t, exitcode.ValidationFailed, exitcode.GetCode(err))
}

func TestCommandValidateSchema_MissingSchemaFile(t *testing.T) {
	dir := t.TempDir()
	dataPath := writeFile(t, dir, "data.json", `{"name":"hi"}`)

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandValidateSchema(cliParams, streams, "scafctl/validate")
	cmd.SetArgs([]string{"--schema", filepath.Join(dir, "nope.json"), "--data", dataPath})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Equal(t, exitcode.FileNotFound, exitcode.GetCode(err))
}

func TestCommandValidateSchema_NoFlagsShowsHelp(t *testing.T) {
	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	// Bare 'validate schema' with no flags should show help, not error.
	cmd := CommandValidateSchema(cliParams, streams, "scafctl/validate")
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	require.NoError(t, err, "bare invocation should show help, not error")
}

func TestCommandValidateSchema_OneFlagMissingErrors(t *testing.T) {
	dir := t.TempDir()
	dataPath := writeFile(t, dir, "data.json", `{"name":"hi"}`)

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	// Only --data provided: a clear, actionable InvalidInput error naming the
	// missing flag (not a raw cobra "required flag(s) not set").
	cmd := CommandValidateSchema(cliParams, streams, "scafctl/validate")
	cmd.SetArgs([]string{"--data", dataPath})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
	assert.Contains(t, err.Error(), "--schema")
}

func TestCommandValidateSchema_BadSchema(t *testing.T) {
	dir := t.TempDir()
	schemaPath := writeFile(t, dir, "schema.json", "this: is: bad: yaml:")
	dataPath := writeFile(t, dir, "data.json", `{"name":"hi"}`)

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandValidateSchema(cliParams, streams, "scafctl/validate")
	cmd.SetArgs([]string{"--schema", schemaPath, "--data", dataPath})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
}

func TestCommandValidateSchema_YAMLFiles(t *testing.T) {
	dir := t.TempDir()
	schemaPath := writeFile(t, dir, "schema.yaml",
		"type: object\nproperties:\n  name:\n    type: string\nrequired:\n  - name\n")
	dataPath := writeFile(t, dir, "data.yaml", "name: hi\n")

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandValidateSchema(cliParams, streams, "scafctl/validate")
	cmd.SetArgs([]string{"--schema", schemaPath, "--data", dataPath})
	require.NoError(t, cmd.Execute())
}

func TestCommandValidateSchema_DataFromStdin(t *testing.T) {
	dir := t.TempDir()
	schemaPath := writeFile(t, dir, "schema.json", testSchemaJSON)

	streams, _, _ := terminal.NewTestIOStreams()
	streams.In = io.NopCloser(strings.NewReader(`{"name":"hi"}`))
	cliParams := settings.NewCliParams()

	cmd := CommandValidateSchema(cliParams, streams, "scafctl/validate")
	cmd.SetArgs([]string{"--schema", schemaPath, "--data", "-"})
	require.NoError(t, cmd.Execute())
}
