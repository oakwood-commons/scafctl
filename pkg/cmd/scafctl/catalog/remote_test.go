// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/kvx/pkg/tui"
	appconfig "github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
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
	assert.Contains(t, subCmdNames, "default")
	assert.Contains(t, subCmdNames, "set-default")
	assert.Contains(t, subCmdNames, "list")
}

// TestCommandRemote_DefaultCanonicalAndDeprecated verifies the canonical
// 'default' command and the hidden deprecated 'set-default' twin.
func TestCommandRemote_DefaultCanonicalAndDeprecated(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, &bytes.Buffer{}, &bytes.Buffer{}, false)

	cmd := CommandRemote(cliParams, ioStreams, "scafctl/catalog")

	var canonical, deprecated *cobra.Command
	for _, c := range cmd.Commands() {
		switch c.Name() {
		case "default":
			canonical = c
		case "set-default":
			deprecated = c
		}
	}

	require.NotNil(t, canonical, "canonical 'default' command must be registered")
	assert.False(t, canonical.Hidden)
	assert.Empty(t, canonical.Deprecated)

	require.NotNil(t, deprecated, "deprecated 'set-default' twin must be registered")
	assert.True(t, deprecated.Hidden)
	assert.NotEmpty(t, deprecated.Deprecated)
	assert.Contains(t, deprecated.Deprecated, "catalog remote default")
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
	// Default falls back to "official" (the built-in default) after removing
	assert.Equal(t, "official", cfg.Settings.DefaultCatalog)
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

	// Both the canonical 'default' command and the hidden deprecated
	// 'set-default' twin must render their help examples with the embedder
	// binary name, never a hardcoded "scafctl".
	var defaultCmd, setDefaultCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		switch sub.Name() {
		case "default":
			defaultCmd = sub
		case "set-default":
			setDefaultCmd = sub
		}
	}
	require.NotNil(t, defaultCmd, "canonical 'default' command must be registered")
	require.NotNil(t, setDefaultCmd, "deprecated 'set-default' twin must be registered")

	assert.Contains(t, defaultCmd.Long, "mycli catalog remote default",
		"canonical Long examples must use the embedder binary name")
	assert.NotContains(t, defaultCmd.Long, "scafctl catalog remote",
		"canonical Long must not hardcode scafctl")

	assert.Contains(t, setDefaultCmd.Long, "mycli catalog remote set-default",
		"deprecated twin Long examples must use the embedder binary name")
	assert.NotContains(t, setDefaultCmd.Long, "scafctl catalog remote",
		"deprecated twin Long must not hardcode scafctl")
	assert.Contains(t, setDefaultCmd.Deprecated, "mycli catalog remote default",
		"deprecation message must point at the embedder binary")
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

// TestRunRemoteList_EmbedderConfigDefaults verifies that catalogs injected
// via WithBaseConfig (the embedder pattern) appear in the listing when
// ManagerOptionsFromContext propagates the options through the context.
func TestRunRemoteList_EmbedderConfigDefaults(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Empty on-disk config -- no catalogs defined by the user.
	err := os.WriteFile(configPath, []byte(""), 0o600)
	require.NoError(t, err)

	// Embedder injects its own catalog via WithBaseConfig.
	embedderDefaults := []byte(`
catalogs:
  - name: embedder-catalog
    type: oci
    url: oci://ghcr.io/embedder/catalog
`)
	configOpts := []appconfig.ManagerOption{
		appconfig.WithBaseConfig(embedderDefaults),
	}

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	ctx = appconfig.WithManagerOptions(ctx, configOpts)

	outputOpts := kvxOutputForTest(ioStreams)

	err = runRemoteList(ctx, configPath, outputOpts)
	require.NoError(t, err)

	output := stdout.String()
	assert.Contains(t, output, "embedder-catalog", "embedder catalog must appear in listing")
	assert.Contains(t, output, "oci://ghcr.io/embedder/catalog")
}

func TestRemoteListSchema_ValidJSON(t *testing.T) {
	t.Parallel()

	var schema map[string]any
	err := json.Unmarshal(remoteListSchemaJSON, &schema)
	require.NoError(t, err, "remote_list_schema.json must be valid JSON")

	items, ok := schema["items"].(map[string]any)
	require.True(t, ok, "schema must have items object")

	props, ok := items["properties"].(map[string]any)
	require.True(t, ok, "items must have properties")

	// Verify all RemoteListItem fields are in the schema.
	expectedFields := []string{"name", "type", "url", "path", "authProvider", "authScope", "default"}
	for _, field := range expectedFields {
		_, exists := props[field]
		assert.True(t, exists, "schema missing field %q", field)
	}
}

func TestRemoteListSchema_RequiredFields(t *testing.T) {
	t.Parallel()

	var schema map[string]any
	err := json.Unmarshal(remoteListSchemaJSON, &schema)
	require.NoError(t, err)

	items, ok := schema["items"].(map[string]any)
	require.True(t, ok, "schema must have items object")
	required, ok := items["required"].([]any)
	require.True(t, ok, "schema must have required array")

	requiredNames := make([]string, 0, len(required))
	for _, v := range required {
		s, ok := v.(string)
		require.True(t, ok, "required entry must be a string, got %T", v)
		requiredNames = append(requiredNames, s)
	}

	assert.Contains(t, requiredNames, "name")
	assert.Contains(t, requiredNames, "type")

	// Deprecated fields should not be required.
	assert.NotContains(t, requiredNames, "path")
	assert.NotContains(t, requiredNames, "authScope")
}

func TestRemoteListSchemaJSON_ParsesWithDisplay(t *testing.T) {
	t.Parallel()

	hints, ds, err := tui.ParseSchemaWithDisplay(remoteListSchemaJSON)
	require.NoError(t, err, "remote_list_schema.json must parse without error")
	assert.NotNil(t, hints, "should produce column hints")
	assert.NotNil(t, ds, "should produce display schema")
}

func TestRemoteListSchema_DeprecatedFieldsHidden(t *testing.T) {
	t.Parallel()

	var schema map[string]any
	err := json.Unmarshal(remoteListSchemaJSON, &schema)
	require.NoError(t, err)

	items, ok := schema["items"].(map[string]any)
	require.True(t, ok, "schema must have items object")
	props, ok := items["properties"].(map[string]any)
	require.True(t, ok, "items must have properties")

	for _, field := range []string{"path", "authScope"} {
		propRaw, exists := props[field]
		require.True(t, exists, "schema missing field %q", field)
		prop, ok := propRaw.(map[string]any)
		require.True(t, ok, "%s property must be an object", field)
		deprecated, ok := prop["deprecated"]
		assert.True(t, ok, "%s should have deprecated flag", field)
		assert.Equal(t, true, deprecated, "%s should be deprecated", field)
	}

	// Column hints also mark them hidden.
	assert.True(t, remoteListColumnHints["path"].Hidden, "path column hint should be Hidden")
	assert.True(t, remoteListColumnHints["authScope"].Hidden, "authScope column hint should be Hidden")
}

// TestCommandRemoteList_RunE_JSONOutput exercises the full RunE path through
// cobra, verifying that structured JSON output contains the expected items.
func TestCommandRemoteList_RunE_JSONOutput(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
catalogs:
  - name: prod
    type: oci
    url: oci://ghcr.io/myorg/catalog
    authProvider: gh-token
  - name: local
    type: filesystem
    path: ./catalogs
settings:
  defaultCatalog: prod
`
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	cmd := commandRemoteList(cliParams, ioStreams, "")
	// Attach a root command with --config so RunE can read it.
	root := &cobra.Command{Use: "scafctl"}
	root.PersistentFlags().String("config", configPath, "")
	root.AddCommand(cmd)

	root.SetArgs([]string{"list", "-o", "json"})
	root.SetContext(ctx)
	err = root.Execute()
	require.NoError(t, err)

	var items []RemoteListItem
	err = json.Unmarshal(stdout.Bytes(), &items)
	require.NoError(t, err, "output must be valid JSON array of RemoteListItem")
	require.GreaterOrEqual(t, len(items), 2, "must include at least the two configured catalogs")

	// Find our configured catalogs by name.
	byName := make(map[string]RemoteListItem, len(items))
	for _, item := range items {
		byName[item.Name] = item
	}

	prod, ok := byName["prod"]
	require.True(t, ok, "prod catalog must appear in output")
	assert.Equal(t, "oci", prod.Type)
	assert.Equal(t, "oci://ghcr.io/myorg/catalog", prod.URL)
	assert.Equal(t, "gh-token", prod.AuthProvider)
	assert.True(t, prod.Default, "prod should be the default catalog")

	local, ok := byName["local"]
	require.True(t, ok, "local catalog must appear in output")
	assert.Equal(t, "filesystem", local.Type)
	assert.False(t, local.Default)
}

func TestCommandRemoteList_TUISnapshot(t *testing.T) {
	t.Parallel()

	items := []RemoteListItem{
		{
			Name:         "prod",
			Type:         "oci",
			URL:          "oci://ghcr.io/myorg/catalog",
			AuthProvider: "gh-token",
			Default:      true,
		},
		{
			Name: "local",
			Type: "filesystem",
			Path: "./catalogs",
		},
	}

	out, err := kvx.Snapshot(items,
		kvx.WithDisplaySchemaJSON(remoteListSchemaJSON),
		kvx.WithColumnHints(remoteListColumnHints),
		kvx.WithDimensions(120, 30),
		kvx.WithNoColor(true),
		kvx.WithAppName("scafctl catalog remote list"),
	)
	require.NoError(t, err)

	// Card list should render both items with name as title and url/path as subtitle.
	assert.Contains(t, out, "prod", "snapshot must contain the OCI catalog name")
	assert.Contains(t, out, "local", "snapshot must contain the filesystem catalog name")
	assert.Contains(t, out, "oci", "snapshot must show the OCI type badge")
	assert.Contains(t, out, "filesystem", "snapshot must show the filesystem type badge")
}
