// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package snapshot

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/execute"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandSave(t *testing.T) {
	cliParams := &settings.Run{}
	ioStreams := terminal.IOStreams{}

	cmd := CommandSave(cliParams, ioStreams, "scafctl")

	require.NotNil(t, cmd)
	assert.Equal(t, "save [config-file]", cmd.Use)
	assert.Equal(t, "Execute resolvers and save snapshot", cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotEmpty(t, cmd.Example)

	// Verify flags
	outputFlag := cmd.Flags().Lookup("output")
	require.NotNil(t, outputFlag, "output flag should exist")
	assert.Equal(t, "o", outputFlag.Shorthand)

	redactFlag := cmd.Flags().Lookup("redact")
	require.NotNil(t, redactFlag, "redact flag should exist")

	resolverFlag := cmd.Flags().Lookup("resolver")
	require.NotNil(t, resolverFlag, "resolver flag should exist")
	assert.Equal(t, "r", resolverFlag.Shorthand)
}

func TestCommandSave_RequiresOutputFlag(t *testing.T) {
	cliParams := &settings.Run{}
	ioStreams := terminal.IOStreams{}

	cmd := CommandSave(cliParams, ioStreams, "scafctl")

	outputFlag := cmd.Flags().Lookup("output")
	require.NotNil(t, outputFlag)

	// Check if the flag is marked as required
	annotations := outputFlag.Annotations
	_, required := annotations["cobra_annotation_bash_completion_one_required_flag"]
	assert.True(t, required || cmd.MarkFlagRequired("output") != nil, "output flag should be required")
}

func TestRunSave_MissingConfigFile(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))
	opts := &SaveOptions{
		ConfigFile: "/nonexistent/config.yaml",
		OutputFile: "/tmp/snapshot.json",
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}
	w := writer.New(&ioStreams, &settings.Run{})
	testCtx := writer.WithWriter(ctx, w)

	err := runSave(testCtx, opts, ioStreams)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestRunSave_InvalidYAML(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	// Create temp file with invalid YAML
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "invalid.yaml")
	err := os.WriteFile(configFile, []byte("invalid: yaml: content: {{"), 0o600)
	require.NoError(t, err)

	opts := &SaveOptions{
		ConfigFile: configFile,
		OutputFile: filepath.Join(tmpDir, "snapshot.json"),
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}
	w := writer.New(&ioStreams, &settings.Run{})
	testCtx := writer.WithWriter(ctx, w)

	err = runSave(testCtx, opts, ioStreams)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse config file")
}

func TestRunSave_NoResolvers(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	// Create temp file with no resolvers
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "empty.yaml")
	content := `
solution: test
version: 1.0.0
resolvers: []
`
	err := os.WriteFile(configFile, []byte(content), 0o600)
	require.NoError(t, err)

	opts := &SaveOptions{
		ConfigFile: configFile,
		OutputFile: filepath.Join(tmpDir, "snapshot.json"),
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}
	w := writer.New(&ioStreams, &settings.Run{})
	testCtx := writer.WithWriter(ctx, w)

	err = runSave(testCtx, opts, ioStreams)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no resolvers found in config file")
}

func TestRunSave_InvalidParameterFormat(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	content := `
solution: test
version: 1.0.0
resolvers:
  - name: test_resolver
    type: env
    key: TEST_VAR
`
	err := os.WriteFile(configFile, []byte(content), 0o600)
	require.NoError(t, err)

	opts := &SaveOptions{
		ConfigFile: configFile,
		OutputFile: filepath.Join(tmpDir, "snapshot.json"),
		Parameters: []string{"invalid-param-no-equals"},
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}
	w := writer.New(&ioStreams, &settings.Run{})
	testCtx := writer.WithWriter(ctx, w)

	err = runSave(testCtx, opts, ioStreams)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse resolver parameters")
}

func TestRunSave_Success(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	outputFile := filepath.Join(tmpDir, "snapshot.json")

	// Create a simple config with env resolver
	content := `
solution: test-solution
version: 1.0.0
resolvers:
  - name: test_var
    type: env
    key: PATH
`
	err := os.WriteFile(configFile, []byte(content), 0o600)
	require.NoError(t, err)

	opts := &SaveOptions{
		ConfigFile: configFile,
		OutputFile: outputFile,
		Parameters: []string{}, // No parameters - simple test
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}
	w := writer.New(&ioStreams, &settings.Run{})
	testCtx := writer.WithWriter(ctx, w)

	err = runSave(testCtx, opts, ioStreams)

	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "Snapshot saved to")
	assert.Contains(t, stdout.String(), "test-solution")

	// Verify snapshot file was created
	_, err = os.Stat(outputFile)
	assert.NoError(t, err, "snapshot file should be created")

	// Verify snapshot can be loaded
	snapshot, err := resolver.LoadSnapshot(outputFile)
	require.NoError(t, err)
	assert.Equal(t, "test-solution", snapshot.Metadata.Solution)
	assert.Equal(t, "1.0.0", snapshot.Metadata.Version)
	assert.NotEmpty(t, snapshot.Resolvers)
}

func TestRunSave_WithRedaction(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	outputFile := filepath.Join(tmpDir, "snapshot.json")

	// Create config with sensitive resolver
	content := `
solution: test-solution
version: 1.0.0
resolvers:
  - name: secret_var
    type: env
    key: PATH
    sensitive: true
  - name: normal_var
    type: env
    key: HOME
`
	err := os.WriteFile(configFile, []byte(content), 0o600)
	require.NoError(t, err)

	opts := &SaveOptions{
		ConfigFile: configFile,
		OutputFile: outputFile,
		Redact:     true,
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}
	w := writer.New(&ioStreams, &settings.Run{})
	testCtx := writer.WithWriter(ctx, w)

	err = runSave(testCtx, opts, ioStreams)

	require.NoError(t, err)

	// Load and verify redaction
	snapshot, err := resolver.LoadSnapshot(outputFile)
	require.NoError(t, err)

	secretResolver := snapshot.Resolvers["secret_var"]
	require.NotNil(t, secretResolver)
	assert.Equal(t, "<redacted>", secretResolver.Value)
	assert.True(t, secretResolver.Sensitive)

	normalResolver := snapshot.Resolvers["normal_var"]
	require.NotNil(t, normalResolver)
	assert.NotEqual(t, "<redacted>", normalResolver.Value)
}

func TestRegistryAdapter_Register(t *testing.T) {
	// This is a simple adapter test - detailed provider tests are in pkg/provider
	registry := provider.NewRegistry()
	adapter := execute.NewResolverRegistryAdapter(registry)

	// Should not panic
	assert.NotPanics(t, func() {
		_ = adapter.Register(nil)
	})
}

func TestRegistryAdapter_Get(t *testing.T) {
	registry := provider.NewRegistry()
	adapter := execute.NewResolverRegistryAdapter(registry)

	// Getting non-existent provider should return error
	_, err := adapter.Get("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "provider nonexistent not found")
}

func TestRegistryAdapter_List(t *testing.T) {
	registry := provider.NewRegistry()
	adapter := execute.NewResolverRegistryAdapter(registry)

	// Should not panic
	assert.NotPanics(t, func() {
		_ = adapter.List()
	})
}
