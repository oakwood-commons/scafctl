// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	appconfig "github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandConfig(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, &bytes.Buffer{}, &bytes.Buffer{}, false)

	cmd := CommandConfig(cliParams, ioStreams, "scafctl")

	assert.Equal(t, "config", cmd.Use)
	assert.Equal(t, []string{"cfg"}, cmd.Aliases)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Check subcommands
	subCmds := cmd.Commands()
	subCmdNames := make([]string, len(subCmds))
	for i, c := range subCmds {
		subCmdNames[i] = c.Name()
	}

	assert.Contains(t, subCmdNames, "view")
	assert.Contains(t, subCmdNames, "get")
	assert.Contains(t, subCmdNames, "set")
	assert.Contains(t, subCmdNames, "unset")
	assert.Contains(t, subCmdNames, "reset")
}

func TestViewOptions_Run(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create a test config file
	configContent := `
catalogs:
  - name: test
    type: filesystem
    path: ./test
settings:
  defaultCatalog: test
  logLevel: 1
`
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	opts := &ViewOptions{
		IOStreams:  ioStreams,
		CliParams:  cliParams,
		ConfigPath: configPath,
	}
	opts.Output = "yaml"

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	err = opts.Run(ctx)
	require.NoError(t, err)

	output := stdout.String()
	assert.Contains(t, output, "test")
	assert.Contains(t, output, "filesystem")
}

func TestGetOptions_Run(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
logging:
  level: 2
settings:
  noColor: true
`
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	opts := &GetOptions{
		IOStreams:  ioStreams,
		CliParams:  cliParams,
		ConfigPath: configPath,
		Key:        "logging.level",
	}

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	err = opts.Run(ctx)
	require.NoError(t, err)

	output := stdout.String()
	assert.Contains(t, output, "2")
}

func TestGetOptions_Run_NotFound(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create empty config
	err := os.WriteFile(configPath, []byte(""), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	opts := &GetOptions{
		IOStreams:  ioStreams,
		CliParams:  cliParams,
		ConfigPath: configPath,
		Key:        "nonexistent.key",
	}

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	err = opts.Run(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestSetOptions_Run(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create empty config
	err := os.WriteFile(configPath, []byte(""), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	opts := &SetOptions{
		IOStreams:  ioStreams,
		CliParams:  cliParams,
		ConfigPath: configPath,
		Key:        "logging.level",
		Value:      "2",
	}

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	err = opts.Run(ctx)
	require.NoError(t, err)

	// Verify the value was set
	mgr := appconfig.NewManager(configPath)
	cfg, err := mgr.Load()
	require.NoError(t, err)
	assert.Equal(t, "2", cfg.Logging.Level)
}

func TestSetOptions_parseValue(t *testing.T) {
	t.Parallel()

	opts := &SetOptions{}

	tests := []struct {
		input    string
		expected any
	}{
		{"true", true},
		{"false", false},
		{"TRUE", true},
		{"FALSE", false},
		{"42", 42},
		{"-1", -1},
		{"hello", "hello"},
		{"3.14", "3.14"}, // Floats stay as strings
	}

	for _, tt := range tests {
		result := opts.parseValue(tt.input)
		assert.Equal(t, tt.expected, result, "input: %s", tt.input)
	}
}

func TestUnsetOptions_Run(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
logging:
  level: "2"
settings:
  noColor: true
`
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	opts := &UnsetOptions{
		IOStreams:  ioStreams,
		CliParams:  cliParams,
		ConfigPath: configPath,
		Key:        "logging.level",
	}

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	err = opts.Run(ctx)
	require.NoError(t, err)

	// Verify the value was reset to default
	mgr := appconfig.NewManager(configPath)
	cfg, err := mgr.Load()
	require.NoError(t, err)
	assert.Equal(t, "none", cfg.Logging.Level)
}
