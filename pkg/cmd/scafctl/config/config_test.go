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
	assert.NotContains(t, subCmdNames, "show", "config show was removed; use view --show-origin")
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

func TestCommandView_DefaultOutputIsAuto(t *testing.T) {
	t.Parallel()
	// Regression guard: config view must not hardcode a specific -o default;
	// it should use the kvx idiom (auto) like every other kvx-driven command.
	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, &bytes.Buffer{}, &bytes.Buffer{}, false)

	cmd := CommandView(cliParams, ioStreams, "scafctl")

	flag := cmd.Flag("output")
	require.NotNil(t, flag, "config view must expose an -o/--output flag")
	assert.Equal(t, "auto", flag.Value.String(),
		"default -o should be 'auto' so kvx picks the format; use -o yaml explicitly if that shape is wanted")
}

func TestViewOptions_Run_ShowOrigin(t *testing.T) {
	// Cannot use t.Parallel with t.Setenv.
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(`
settings:
  defaultCatalog: from-file
logging:
  format: json
`), 0o600))

	t.Setenv("SCAFCTL_LOGGING_LEVEL", "debug")
	t.Setenv("SCAFCTL_GITHUB_TOKEN", "secret-value")

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	opts := &ViewOptions{
		IOStreams:  ioStreams,
		CliParams:  cliParams,
		ConfigPath: configPath,
		ShowOrigin: true,
	}
	opts.Output = "json"

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	require.NoError(t, opts.Run(ctx))

	out := stdout.String()
	assert.Contains(t, out, `"sources"`, "output should include sources map")
	assert.Contains(t, out, `"envOverrides"`, "output should include envOverrides list")
	assert.Contains(t, out, `"SCAFCTL_LOGGING_LEVEL"`)
	assert.Contains(t, out, `"SCAFCTL_GITHUB_TOKEN"`)
	// Token value must be redacted; the raw value must not leak.
	assert.NotContains(t, out, "secret-value", "sensitive env value must not appear in output")
	assert.Contains(t, out, appconfig.RedactedValue)
}

func TestViewOptions_Run_SourceFilter_File(t *testing.T) {
	// Cannot use t.Parallel with t.Setenv.
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(`
settings:
  defaultCatalog: from-file
`), 0o600))
	t.Setenv("SCAFCTL_LOGGING_LEVEL", "debug")

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	opts := &ViewOptions{
		IOStreams:    ioStreams,
		CliParams:    cliParams,
		ConfigPath:   configPath,
		SourceFilter: "file",
	}
	opts.Output = "json"

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	require.NoError(t, opts.Run(ctx))

	out := stdout.String()
	assert.Contains(t, out, "from-file", "file-sourced value should be present")
	assert.NotContains(t, out, `"level":"debug"`, "env-sourced value must be filtered out")
}

func TestViewOptions_Run_SourceFilter_Env(t *testing.T) {
	// Cannot use t.Parallel with t.Setenv.
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(`
settings:
  defaultCatalog: from-file
`), 0o600))
	t.Setenv("SCAFCTL_LOGGING_LEVEL", "debug")

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	opts := &ViewOptions{
		IOStreams:    ioStreams,
		CliParams:    cliParams,
		ConfigPath:   configPath,
		SourceFilter: "env",
	}
	opts.Output = "json"

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	require.NoError(t, opts.Run(ctx))

	out := stdout.String()
	assert.NotContains(t, out, "from-file", "file-sourced value must be filtered out")
	assert.Contains(t, out, `"SCAFCTL_LOGGING_LEVEL"`, "envOverrides always accompanies --source=env")
	// The logging.level leaf should survive the source filter under settings.
	assert.Contains(t, out, `"debug"`)
}

func TestViewOptions_Run_SourceFilter_EnvOnSettings(t *testing.T) {
	// Cannot use t.Parallel with t.Setenv. This test exercises the
	// FilterMapBySource path against a real settings.* env override
	// (SCAFCTL_SETTINGS_DEFAULTCATALOG), which the SCAFCTL_LOGGING_LEVEL test
	// does not cover.
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(`
settings:
  defaultCatalog: from-file
`), 0o600))
	t.Setenv("SCAFCTL_SETTINGS_DEFAULTCATALOG", "from-env")

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	opts := &ViewOptions{
		IOStreams:    ioStreams,
		CliParams:    cliParams,
		ConfigPath:   configPath,
		SourceFilter: "env",
	}
	opts.Output = "json"

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	require.NoError(t, opts.Run(ctx))

	out := stdout.String()
	assert.Contains(t, out, "from-env", "settings.defaultCatalog=from-env must survive --source=env")
	assert.NotContains(t, out, "from-file", "file-sourced value must be filtered out")
}

func TestViewOptions_Run_SourceFilter_Default(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(""), 0o600))

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	opts := &ViewOptions{
		IOStreams:    ioStreams,
		CliParams:    cliParams,
		ConfigPath:   configPath,
		SourceFilter: "default",
	}
	opts.Output = "json"

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	require.NoError(t, opts.Run(ctx))

	out := stdout.String()
	// The default settings.defaultCatalog is "official" (built-in). With no
	// user config or env override present, --source=default must include it.
	assert.Contains(t, out, `"official"`, "built-in default settings must be present under --source=default")
}

func TestViewOptions_Run_SourceFilter_DropIn(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, appconfig.ConfigDirName), 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(tmpDir, appconfig.ConfigDirName, "10-x.yaml"),
		[]byte("logging:\n  format: json\n"),
		0o600,
	))
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(""), 0o600))

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	opts := &ViewOptions{
		IOStreams:    ioStreams,
		CliParams:    cliParams,
		ConfigPath:   configPath,
		SourceFilter: "dropin",
	}
	opts.Output = "json"

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	require.NoError(t, opts.Run(ctx))

	out := stdout.String()
	// The dropin fragment only touches logging.format, so under --source=dropin
	// the payload must include that key and exclude the built-in default
	// settings.defaultCatalog=official.
	assert.Contains(t, out, `"json"`, "dropin-sourced logging.format=json must appear")
	assert.NotContains(t, out, `"official"`,
		"only dropin-sourced values may appear under --source=dropin")
}

func TestViewOptions_Run_ShowOrigin_ExposesAllSections(t *testing.T) {
	t.Parallel()
	// `view` emits the full Config struct (matching the pre-fold `show`
	// behavior), so every top-level section's keys must appear in `sources`.
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(""), 0o600))

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	opts := &ViewOptions{
		IOStreams:  ioStreams,
		CliParams:  cliParams,
		ConfigPath: configPath,
		ShowOrigin: true,
	}
	opts.Output = "json"

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	require.NoError(t, opts.Run(ctx))

	out := stdout.String()
	// Spot-check keys from sections beyond settings/catalogs.
	assert.Contains(t, out, `"logging.level"`, "logging.* must appear in sources")
	assert.Contains(t, out, `"httpclient.timeout"`, "httpclient.* must appear in sources")
	assert.Contains(t, out, `"resolver.timeout"`, "resolver.* must appear in sources")
}

func TestViewOptions_Run_RedactsSensitiveConfigLeaves(t *testing.T) {
	t.Parallel()
	// Regression: `view` emits the full Config, so client secrets configured
	// on disk must be redacted before reaching stdout. The user-visible
	// `auth.handlers` structure and non-sensitive fields must survive intact.
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(`
auth:
  entra:
    clientId: visible-id
    clientSecret: super-secret-do-not-leak
  handlers:
    github:
      hostname:
        aliases:
          alias-a: https://api.github.com/
`), 0o600))

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	opts := &ViewOptions{
		IOStreams:  ioStreams,
		CliParams:  cliParams,
		ConfigPath: configPath,
	}
	opts.Output = "json"

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	require.NoError(t, opts.Run(ctx))

	out := stdout.String()
	assert.NotContains(t, out, "super-secret-do-not-leak",
		"client secrets from disk must never appear verbatim in view output")
	assert.Contains(t, out, appconfig.RedactedValue,
		"redaction placeholder must be present in place of the secret")
	assert.Contains(t, out, "visible-id",
		"non-sensitive auth fields must survive redaction")
	assert.Contains(t, out, "alias-a",
		"auth.handlers structure must survive redaction")
}

func TestViewOptions_Run_SourceFilter_Invalid(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(""), 0o600))

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	opts := &ViewOptions{
		IOStreams:    ioStreams,
		CliParams:    cliParams,
		ConfigPath:   configPath,
		SourceFilter: "bogus",
	}
	opts.Output = "yaml"

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	err := opts.Run(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --source")
}

func TestViewOptions_Run_EmbedderBinaryName(t *testing.T) {
	// Cannot use t.Parallel with t.Setenv.
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(""), 0o600))

	t.Setenv("MYCLI_LOGGING_LEVEL", "info")
	t.Setenv("SCAFCTL_LOGGING_LEVEL", "debug")

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"

	opts := &ViewOptions{
		IOStreams:  ioStreams,
		CliParams:  cliParams,
		ConfigPath: configPath,
		BinaryName: "mycli",
		ShowOrigin: true,
	}
	opts.Output = "json"

	// Wire the embedder's env prefix into the manager options via context, so
	// the domain layer uses MYCLI_ instead of SCAFCTL_.
	ctx := appconfig.WithManagerOptions(
		context.Background(),
		[]appconfig.ManagerOption{appconfig.WithEnvPrefix("MYCLI")},
	)
	ctx = writer.WithWriter(ctx, writer.New(ioStreams, cliParams))

	require.NoError(t, opts.Run(ctx))

	out := stdout.String()
	assert.Contains(t, out, `"MYCLI_LOGGING_LEVEL"`, "embedder prefix should be detected")
	assert.NotContains(t, out, `"SCAFCTL_LOGGING_LEVEL"`, "default prefix should be ignored under embedder")
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

func TestUnsetOptions_Run_NestedArrayKey(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `auth:
  customOAuth2:
    - name: my-handler
      clientId: abc123
      scopes:
        - repo:read
        - repo:write
  github:
    clientId: test-id
settings:
  defaultCatalog: official
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
		Key:        "auth.customOAuth2",
	}

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	err = opts.Run(ctx)
	require.NoError(t, err)

	// Verify the key was removed from file entirely.
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "customOAuth2")
	assert.NotContains(t, string(data), "my-handler")
	// Other keys remain.
	assert.Contains(t, string(data), "github")
}

func TestUnsetOptions_Run_KeyNotInFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Config file exists but doesn't have the key we're unsetting.
	configContent := `settings:
  defaultCatalog: official
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

	// Should succeed (idempotent) and report that key was not present.
	err = opts.Run(ctx)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "not present")
}

func TestUnsetOptions_Run_BrokenConfig(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Config file has valid YAML but would fail validation during Load()
	// (e.g., invalid catalog type). Unset should still work because it
	// operates directly on the YAML without requiring Load().
	configContent := `catalogs:
  - name: bad
    type: invalid-type-that-fails-validation
    url: something
auth:
  customOAuth2:
    - name: stale-handler
      clientId: old-value
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
		Key:        "auth.customOAuth2",
	}

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	// Should succeed even though the config would fail Load() validation.
	err = opts.Run(ctx)
	require.NoError(t, err)

	// Verify the key was removed.
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "customOAuth2")
	assert.NotContains(t, string(data), "stale-handler")
	// Other content remains.
	assert.Contains(t, string(data), "bad")
}

// TestCommandConfig_UnknownSubcommandErrors verifies that an unknown
// subcommand errors (non-zero) while a bare invocation shows help and exits 0.
func TestCommandConfig_UnknownSubcommandErrors(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandConfig(cliParams, ioStreams, "scafctl")
	cmd.SetArgs([]string{"bogus-xyz"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")

	cmd2 := CommandConfig(cliParams, ioStreams, "scafctl")
	cmd2.SetArgs([]string{})
	cmd2.SilenceErrors = true
	cmd2.SilenceUsage = true
	assert.NoError(t, cmd2.Execute())
}
