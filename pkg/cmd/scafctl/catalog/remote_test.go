// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	appconfig "github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func kvxOutputForTest(ioStreams *terminal.IOStreams) *kvx.OutputOptions {
	opts := kvx.NewOutputOptions(ioStreams)
	opts.Format = "json"
	return opts
}

func TestCommandRemote_SubcommandRegistration(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, &bytes.Buffer{}, &bytes.Buffer{}, false)

	cmd := CommandRemote(cliParams, ioStreams, "scafctl/catalog")

	assert.Equal(t, "remote", cmd.Use)

	subCmds := cmd.Commands()
	subCmdNames := make([]string, len(subCmds))
	for i, c := range subCmds {
		subCmdNames[i] = c.Name()
	}

	assert.Contains(t, subCmdNames, "add")
	assert.Contains(t, subCmdNames, "remove")
	assert.Contains(t, subCmdNames, "set-default")
	assert.Contains(t, subCmdNames, "list")
}

func TestRunRemoteAdd(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	err := os.WriteFile(configPath, []byte(""), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	opts := &RemoteAddOptions{
		IOStreams:  ioStreams,
		CliParams:  cliParams,
		ConfigPath: configPath,
		Name:       "test-registry",
		Type:       "oci",
		URL:        "oci://ghcr.io/myorg",
		SetDefault: true,
	}

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	err = runRemoteAdd(ctx, opts)
	require.NoError(t, err)

	mgr := appconfig.NewManager(configPath)
	cfg, err := mgr.Load()
	require.NoError(t, err)

	cat, ok := cfg.GetCatalog("test-registry")
	assert.True(t, ok)
	assert.Equal(t, "oci", cat.Type)
	assert.Equal(t, "oci://ghcr.io/myorg", cat.URL)
	assert.Equal(t, "test-registry", cfg.Settings.DefaultCatalog)
}

func TestRunRemoteAdd_WithAuthProvider(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	err := os.WriteFile(configPath, []byte(""), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	opts := &RemoteAddOptions{
		IOStreams:    ioStreams,
		CliParams:    cliParams,
		ConfigPath:   configPath,
		Name:         "my-registry",
		Type:         "oci",
		URL:          "oci://ghcr.io/myorg",
		AuthProvider: "github",
		AuthScope:    "repo",
	}

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	err = runRemoteAdd(ctx, opts)
	require.NoError(t, err)

	mgr := appconfig.NewManager(configPath)
	cfg, err := mgr.Load()
	require.NoError(t, err)

	cat, ok := cfg.GetCatalog("my-registry")
	assert.True(t, ok)
	assert.Equal(t, "github", cat.AuthProvider)
	assert.Equal(t, "repo", cat.AuthScope)
}

func TestRunRemoteAdd_InvalidType(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	err := os.WriteFile(configPath, []byte(""), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	opts := &RemoteAddOptions{
		IOStreams:  ioStreams,
		CliParams:  cliParams,
		ConfigPath: configPath,
		Name:       "test",
		Type:       "invalid",
	}

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	err = runRemoteAdd(ctx, opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid catalog type")
}

func TestRunRemoteAdd_MissingURL(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	err := os.WriteFile(configPath, []byte(""), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	opts := &RemoteAddOptions{
		IOStreams:  ioStreams,
		CliParams:  cliParams,
		ConfigPath: configPath,
		Name:       "test",
		Type:       "oci",
		// URL intentionally empty
	}

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	err = runRemoteAdd(ctx, opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--url is required")
}

func TestRunRemoteAdd_MissingPath(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	err := os.WriteFile(configPath, []byte(""), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	opts := &RemoteAddOptions{
		IOStreams:  ioStreams,
		CliParams:  cliParams,
		ConfigPath: configPath,
		Name:       "test",
		Type:       "filesystem",
	}

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	err = runRemoteAdd(ctx, opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--path is required")
}

func TestRunRemoteRemove(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
catalogs:
  - name: test
    type: oci
    url: oci://ghcr.io/myorg
settings:
  defaultCatalog: test
`
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	opts := &RemoteRemoveOptions{
		IOStreams:  ioStreams,
		CliParams:  cliParams,
		ConfigPath: configPath,
		Name:       "test",
	}

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	err = runRemoteRemove(ctx, opts)
	require.NoError(t, err)

	mgr := appconfig.NewManager(configPath)
	cfg, err := mgr.Load()
	require.NoError(t, err)

	_, ok := cfg.GetCatalog("test")
	assert.False(t, ok)
	// Default falls back to "local" (the built-in default) after removing
	assert.Equal(t, "local", cfg.Settings.DefaultCatalog)
}

func TestRunRemoteSetDefault(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
catalogs:
  - name: cat1
    type: filesystem
    path: ./cat1
  - name: cat2
    type: oci
    url: oci://ghcr.io/myorg
settings:
  defaultCatalog: cat1
`
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	opts := &RemoteSetDefaultOptions{
		IOStreams:  ioStreams,
		CliParams:  cliParams,
		ConfigPath: configPath,
		Name:       "cat2",
	}

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	err = runRemoteSetDefault(ctx, opts)
	require.NoError(t, err)

	mgr := appconfig.NewManager(configPath)
	cfg, err := mgr.Load()
	require.NoError(t, err)
	assert.Equal(t, "cat2", cfg.Settings.DefaultCatalog)
}

func TestRunRemoteSetDefault_Nonexistent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	err := os.WriteFile(configPath, []byte(""), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	opts := &RemoteSetDefaultOptions{
		IOStreams:  ioStreams,
		CliParams:  cliParams,
		ConfigPath: configPath,
		Name:       "nonexistent",
	}

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	err = runRemoteSetDefault(ctx, opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRunRemoteList(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
catalogs:
  - name: local-cat
    type: filesystem
    path: ./catalogs
  - name: ghcr
    type: oci
    url: oci://ghcr.io/myorg
settings:
  defaultCatalog: ghcr
`
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	outputOpts := kvxOutputForTest(ioStreams)

	err = runRemoteList(ctx, configPath, outputOpts)
	require.NoError(t, err)

	output := stdout.String()
	assert.Contains(t, output, "local-cat")
	assert.Contains(t, output, "ghcr")
}

func TestRunRemoteList_WithAuthFields(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
catalogs:
  - name: gcp-catalog
    type: oci
    url: oci://us-central1-docker.pkg.dev/proj/repo
    authProvider: gcp
    authScope: https://www.googleapis.com/auth/cloud-platform
  - name: quay-catalog
    type: oci
    url: oci://quay.io/myorg/catalog
    authProvider: quay
settings:
  defaultCatalog: gcp-catalog
`
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	outputOpts := kvxOutputForTest(ioStreams)

	err = runRemoteList(ctx, configPath, outputOpts)
	require.NoError(t, err)

	output := stdout.String()
	// Verify auth fields appear in JSON output.
	assert.Contains(t, output, `"authProvider": "gcp"`)
	assert.Contains(t, output, `"authScope": "https://www.googleapis.com/auth/cloud-platform"`)
	assert.Contains(t, output, `"authProvider": "quay"`)
	assert.Contains(t, output, `"default": true`)
}

func TestCommandRemote_EmbedderBinaryName(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams := terminal.NewIOStreams(nil, &bytes.Buffer{}, &bytes.Buffer{}, false)

	cmd := CommandRemote(cliParams, ioStreams, "mycli/catalog")

	assert.Equal(t, "remote", cmd.Use)
	assert.NotEmpty(t, cmd.Commands(), "subcommands should be registered for embedder binary")
}

func TestRunRemoteList_Empty(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	err := os.WriteFile(configPath, []byte(""), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	outputOpts := kvxOutputForTest(ioStreams)

	err = runRemoteList(ctx, configPath, outputOpts)
	require.NoError(t, err)
}
