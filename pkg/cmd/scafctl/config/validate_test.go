// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const validConfigYAML = `
catalogs:
  - name: test
    type: filesystem
    path: ./test
settings:
  defaultCatalog: test
`

func writeValidConfig(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(validConfigYAML), 0o600))
	return configPath
}

func runValidate(t *testing.T, configPath, outputFormat string) (stdout, stderr string, err error) {
	t.Helper()

	var outBuf, errBuf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &outBuf, &errBuf, false)
	cliParams := settings.NewCliParams()
	cliParams.NoColor = true

	opts := &ValidateOptions{
		IOStreams: ioStreams,
		CliParams: cliParams,
		File:      configPath,
		KvxOutputFlags: flags.KvxOutputFlags{
			Output:         outputFormat,
			FormatExplicit: outputFormat != "",
		},
	}

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	runErr := opts.Run(ctx)
	return outBuf.String(), errBuf.String(), runErr
}

func parseCSV(t *testing.T, s string) [][]string {
	t.Helper()
	records, err := csv.NewReader(strings.NewReader(s)).ReadAll()
	require.NoError(t, err, "stdout must be parseable CSV when -o csv is used")
	return records
}

func TestCommandValidate_RegistersOnlyOutputFlag(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, &bytes.Buffer{}, &bytes.Buffer{}, false)

	cmd := CommandValidate(cliParams, ioStreams, "scafctl config")

	outputFlag := cmd.Flag("output")
	require.NotNil(t, outputFlag, "config validate must expose -o/--output")
	assert.Equal(t, "auto", outputFlag.Value.String())

	assert.Nil(t, cmd.Flag("interactive"),
		"data-only validator must not expose -i/--interactive")
	assert.Nil(t, cmd.Flag("expression"),
		"data-only validator must not expose -e/--expression")
	assert.Nil(t, cmd.Flag("where"),
		"data-only validator must not expose -w/--where")
}

func TestCommandValidate_OutputHelpListsOnlyAllowedFormats(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, &bytes.Buffer{}, &bytes.Buffer{}, false)

	cmd := CommandValidate(cliParams, ioStreams, "scafctl config")
	outputFlag := cmd.Flag("output")
	require.NotNil(t, outputFlag)

	for _, allowed := range validateOutputFormats {
		assert.Contains(t, outputFlag.Usage, allowed,
			"--output help must advertise %q", allowed)
	}
	for _, excluded := range []string{"table", "list", "tree", "mermaid", "test"} {
		assert.NotContains(t, outputFlag.Usage, excluded,
			"--output help must not advertise %q, which is not a meaningful format for a scalar validator",
			excluded)
	}
}

func TestCommandValidate_RejectsExcludedFormat(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	cliParams.NoColor = true
	ioStreams := terminal.NewIOStreams(nil, &bytes.Buffer{}, &bytes.Buffer{}, false)

	cmd := CommandValidate(cliParams, ioStreams, "scafctl config")
	cmd.SetArgs([]string{"-o", "mermaid"})
	err := cmd.Execute()

	require.Error(t, err,
		"PreRunE must reject formats not in the allowed subset even though mermaid is a valid base kvx format")
	assert.Contains(t, err.Error(), "invalid output format: mermaid")
}

func TestValidateOptions_Run_Human_Default(t *testing.T) {
	t.Parallel()
	configPath := writeValidConfig(t)

	stdout, _, err := runValidate(t, configPath, "")
	require.NoError(t, err)

	assert.Contains(t, stdout, "Valid: "+configPath)
	assert.Contains(t, stdout, "Version:")
	assert.Contains(t, stdout, "Catalogs:")
	assert.Contains(t, stdout, "Default catalog: test")
}

func TestValidateOptions_Run_JSON(t *testing.T) {
	t.Parallel()
	configPath := writeValidConfig(t)

	stdout, _, err := runValidate(t, configPath, "json")
	require.NoError(t, err)

	var got ValidateResult
	require.NoError(t, json.Unmarshal([]byte(stdout), &got),
		"stdout must be parseable JSON when -o json is used")
	assert.True(t, got.Valid)
	assert.Equal(t, configPath, got.File)
	assert.Equal(t, "test", got.DefaultCatalog)
	assert.Empty(t, got.Error)
	// Catalogs count includes merged defaults; just assert at least our one.
	assert.GreaterOrEqual(t, got.Catalogs, 1)
}

func TestValidateOptions_Run_YAML(t *testing.T) {
	t.Parallel()
	configPath := writeValidConfig(t)

	stdout, _, err := runValidate(t, configPath, "yaml")
	require.NoError(t, err)

	var got ValidateResult
	require.NoError(t, yaml.Unmarshal([]byte(stdout), &got),
		"stdout must be parseable YAML when -o yaml is used")
	assert.True(t, got.Valid)
	assert.Equal(t, configPath, got.File)
}

func TestValidateOptions_Run_TOML(t *testing.T) {
	t.Parallel()
	configPath := writeValidConfig(t)

	stdout, _, err := runValidate(t, configPath, "toml")
	require.NoError(t, err)

	// Keys must be the lowercase json/yaml/csv contract, not Go field names.
	assert.Contains(t, stdout, "file =")
	assert.Contains(t, stdout, "valid =")
	assert.Contains(t, stdout, "defaultCatalog =")
	assert.NotContains(t, stdout, "File =",
		"TOML keys must match the structured-format contract, not Go field names")
	assert.NotContains(t, stdout, "DefaultCatalog =",
		"TOML keys must match the structured-format contract, not Go field names")

	var got ValidateResult
	require.NoError(t, toml.Unmarshal([]byte(stdout), &got),
		"stdout must be parseable TOML when -o toml is used")
	assert.True(t, got.Valid)
	assert.Equal(t, configPath, got.File)
	assert.Equal(t, "test", got.DefaultCatalog)
}

func TestValidateOptions_Run_CSV_Success(t *testing.T) {
	t.Parallel()
	configPath := writeValidConfig(t)

	stdout, _, err := runValidate(t, configPath, "csv")
	require.NoError(t, err)

	records := parseCSV(t, stdout)
	// Two rows: header + a single data row for the single ValidateResult.
	require.Len(t, records, 2, "CSV output must be header + one data row, not a single %%v cell")
	assert.Equal(t,
		[]string{"file", "valid", "version", "catalogs", "defaultCatalog", "error"},
		records[0],
		"CSV header order must match validateCSVColumnOrder for stable scripting")

	row := records[1]
	require.Len(t, row, 6)
	assert.Equal(t, configPath, row[0])
	assert.Equal(t, "true", row[1])
	assert.Equal(t, "test", row[4])
	assert.Empty(t, row[5], "error cell must be empty on success")
}

func TestValidateOptions_Run_CSV_MissingFile(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "nope.yaml")

	stdout, _, err := runValidate(t, missing, "csv")
	require.Error(t, err,
		"missing file must still return a non-zero error even when emitting CSV")

	records := parseCSV(t, stdout)
	require.Len(t, records, 2,
		"CSV must stay header + row on the failure path so the schema is stable")
	assert.Equal(t,
		[]string{"file", "valid", "version", "catalogs", "defaultCatalog", "error"},
		records[0])

	row := records[1]
	require.Len(t, row, 6)
	assert.Equal(t, missing, row[0])
	assert.Equal(t, "false", row[1])
	assert.NotEmpty(t, row[5], "error cell must be populated on failure")
	assert.Contains(t, strings.ToLower(row[5]), "not found")
}

func TestValidateOptions_Run_Quiet(t *testing.T) {
	t.Parallel()
	configPath := writeValidConfig(t)

	stdout, stderr, err := runValidate(t, configPath, "quiet")
	require.NoError(t, err)
	assert.Empty(t, stdout, "-o quiet must suppress all stdout")
	// Writer Successf/Infof go to stdout; nothing should land on stderr either.
	assert.Empty(t, stderr)
}

func TestValidateOptions_Run_MissingFile_Human(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "nope.yaml")

	_, _, err := runValidate(t, missing, "")
	require.Error(t, err)

	var ec *exitcode.ExitError
	require.True(t, errors.As(err, &ec),
		"error must carry an exit code so CI reports non-zero")
	assert.Equal(t, exitcode.FileNotFound, ec.Code)
}

func TestValidateOptions_Run_MissingFile_JSON(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "nope.yaml")

	stdout, _, err := runValidate(t, missing, "json")
	require.Error(t, err,
		"missing file must still return a non-zero error even when emitting structured output")

	var got ValidateResult
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	assert.False(t, got.Valid)
	assert.Equal(t, missing, got.File)
	assert.NotEmpty(t, got.Error)
	assert.Contains(t, strings.ToLower(got.Error), "not found")
}

func TestValidateOptions_Run_InvalidYAML_JSON(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	// Mixed mapping/sequence at the same indentation level is a hard YAML parse error.
	require.NoError(t, os.WriteFile(configPath, []byte("catalogs:\n  - name: ok\n  bad: {unclosed\n"), 0o600))

	stdout, _, err := runValidate(t, configPath, "json")
	require.Error(t, err)

	var got ValidateResult
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	assert.False(t, got.Valid)
	assert.NotEmpty(t, got.Error)
}
