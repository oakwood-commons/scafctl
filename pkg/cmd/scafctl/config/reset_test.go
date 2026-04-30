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

func TestCommandReset_Registration(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, &bytes.Buffer{}, &bytes.Buffer{}, false)

	cmd := CommandReset(cliParams, ioStreams, "scafctl/config")
	assert.Equal(t, "reset", cmd.Use)
}

func TestResetOptions_RequiresForce(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("custom: true"), 0o600))

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	opts := &ResetOptions{
		IOStreams:  ioStreams,
		CliParams:  cliParams,
		ConfigPath: configPath,
		Force:      false,
	}

	err := opts.Run(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--force")
}

func TestResetOptions_ResetsConfigFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("custom: true\nsettings:\n  noColor: true"), 0o600))

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	opts := &ResetOptions{
		IOStreams:  ioStreams,
		CliParams:  cliParams,
		ConfigPath: configPath,
		Force:      true,
	}

	err := opts.Run(ctx)
	require.NoError(t, err)

	// Config file should exist and contain defaults (not our custom value).
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "custom: true")
}

func TestResetOptions_AllSucceeds(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(""), 0o600))

	// The --all flag removes real XDG cache/data/state dirs which we cannot
	// inject in a unit test. This test validates the --force + --all
	// combination succeeds without error.
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	opts := &ResetOptions{
		IOStreams:  ioStreams,
		CliParams:  cliParams,
		ConfigPath: configPath,
		Force:      true,
		All:        true,
	}

	err := opts.Run(ctx)
	require.NoError(t, err)

	// Config file should be recreated with defaults.
	_, err = os.Stat(configPath)
	require.NoError(t, err, "config file should exist after reset")
}

func TestResetOptions_NoExistingConfig(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	// Don't create the file -- reset should still succeed.

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	opts := &ResetOptions{
		IOStreams:  ioStreams,
		CliParams:  cliParams,
		ConfigPath: configPath,
		Force:      true,
	}

	err := opts.Run(ctx)
	require.NoError(t, err)

	// Config file should be created from defaults.
	_, err = os.Stat(configPath)
	require.NoError(t, err)
}

func TestResetOptions_EmbedderBinaryName(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams := terminal.NewIOStreams(nil, &bytes.Buffer{}, &bytes.Buffer{}, false)

	cmd := CommandReset(cliParams, ioStreams, "mycli/config")
	assert.Equal(t, "reset", cmd.Use)
	assert.NotContains(t, cmd.Long, "scafctl")
}

func TestResetOptions_UsesEmbedderDefaults(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("custom: true"), 0o600))

	embedderDefaults := []byte(`auth:
  entra:
    clientId: embedder-client-id
    tenantId: embedder-tenant
catalogs:
  - name: local
    type: filesystem
  - name: corp-registry
    type: oci
    url: oci://registry.corp.example.com/myorg
settings:
  defaultCatalog: corp-registry
`)

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	ctx = appconfig.WithBaseDefaults(ctx, embedderDefaults)

	opts := &ResetOptions{
		IOStreams:  ioStreams,
		CliParams:  cliParams,
		ConfigPath: configPath,
		Force:      true,
	}

	err := opts.Run(ctx)
	require.NoError(t, err)

	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	content := string(data)

	assert.Contains(t, content, "embedder-client-id", "reset should use embedder defaults")
	assert.Contains(t, content, "embedder-tenant", "reset should use embedder defaults")
	assert.Contains(t, content, "corp-registry", "reset should include embedder catalogs")
	assert.NotContains(t, content, "custom: true", "user customizations should be removed")
}

func TestResetOptions_NilWriter(t *testing.T) {
	t.Parallel()

	// Run without a writer in context.
	ctx := context.Background()
	opts := &ResetOptions{Force: true}

	err := opts.Run(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writer not initialized")
}

func TestResetOptions_RemoveError(t *testing.T) {
	t.Parallel()

	// Point configPath at an unremovable path (directory instead of file).
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "protected")
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "child"), 0o700))

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	opts := &ResetOptions{
		IOStreams:  ioStreams,
		CliParams:  cliParams,
		ConfigPath: configDir, // directory, not a file -- os.Remove will fail
		Force:      true,
	}

	err := opts.Run(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "removing config file")
}

func TestResetOptions_SuccessOutput(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	opts := &ResetOptions{
		IOStreams:  ioStreams,
		CliParams:  cliParams,
		ConfigPath: configPath,
		Force:      true,
	}

	err := opts.Run(ctx)
	require.NoError(t, err)

	output := stdout.String() + stderr.String()
	assert.Contains(t, output, "Reset config file")
}

func BenchmarkCommandReset(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandReset(cliParams, ioStreams, "scafctl/config")
	}
}
