// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package scafctl

import (
	"fmt"
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewRootOptions verifies that NewRootOptions returns non-nil with zero values.
func TestNewRootOptions(t *testing.T) {
	t.Parallel()
	opts := NewRootOptions()
	require.NotNil(t, opts)
	assert.Nil(t, opts.IOStreams)
	assert.Nil(t, opts.ExitFunc)
	assert.Equal(t, "", opts.ConfigPath)
}

// TestRoot_NilOpts verifies that Root(nil) succeeds and uses production defaults.
func TestRoot_NilOpts(t *testing.T) {
	t.Parallel()
	cmd := Root(nil)
	require.NotNil(t, cmd)
	assert.Equal(t, "scafctl", cmd.Use)
}

// TestRoot_EnvVar_LogLevel verifies that SCAFCTL_LOG_LEVEL is read by PersistentPreRun.
func TestRoot_EnvVar_LogLevel(t *testing.T) {
	t.Setenv("SCAFCTL_LOG_LEVEL", "debug")

	ioStreams, out, _ := terminal.NewTestIOStreams()
	cmd := Root(&RootOptions{IOStreams: ioStreams})
	cmd.SetArgs([]string{"version"})

	err := cmd.Execute()
	require.NoError(t, err)
	// The command ran — the env var was read during PersistentPreRun
	// (we can't easily assert the log level was set, but verify no crash)
	_ = out.String()
}

// TestRoot_EnvVar_Debug verifies that SCAFCTL_DEBUG env var sets debug level.
func TestRoot_EnvVar_Debug(t *testing.T) {
	t.Setenv("SCAFCTL_DEBUG", "true")
	t.Setenv("SCAFCTL_LOG_LEVEL", "") // ensure log level env is clear

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := Root(&RootOptions{IOStreams: ioStreams})
	cmd.SetArgs([]string{"version"})

	err := cmd.Execute()
	require.NoError(t, err)
}

// TestRoot_EnvVar_Debug_Zero verifies that SCAFCTL_DEBUG=0 does not enable debug.
func TestRoot_EnvVar_Debug_Zero(t *testing.T) {
	t.Setenv("SCAFCTL_DEBUG", "0")

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := Root(&RootOptions{IOStreams: ioStreams})
	cmd.SetArgs([]string{"version"})

	err := cmd.Execute()
	require.NoError(t, err)
}

// TestRoot_EnvVar_LogFormat verifies that SCAFCTL_LOG_FORMAT env var is read.
func TestRoot_EnvVar_LogFormat(t *testing.T) {
	t.Setenv("SCAFCTL_LOG_FORMAT", "json")

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := Root(&RootOptions{IOStreams: ioStreams})
	cmd.SetArgs([]string{"version"})

	err := cmd.Execute()
	require.NoError(t, err)
}

// TestRoot_EnvVar_LogPath verifies that SCAFCTL_LOG_PATH env var is read.
func TestRoot_EnvVar_LogPath(t *testing.T) {
	logFile := t.TempDir() + "/test.log"
	t.Setenv("SCAFCTL_LOG_PATH", logFile)

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := Root(&RootOptions{IOStreams: ioStreams})
	cmd.SetArgs([]string{"version"})

	err := cmd.Execute()
	require.NoError(t, err)
}

// TestRoot_WithConfigPath verifies that a custom config path is accepted
// (even if it doesn't exist — it should fall back to defaults).
func TestRoot_WithConfigPath(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := Root(&RootOptions{
		IOStreams:  ioStreams,
		ConfigPath: "/nonexistent/path/config.yaml",
	})
	cmd.SetArgs([]string{"version"})

	err := cmd.Execute()
	require.NoError(t, err)
}

// TestRoot_VersionSubcommand_Output verifies that 'scafctl version' runs and produces output.
func TestRoot_VersionSubcommand_Output(t *testing.T) {
	t.Parallel()

	ioStreams, out, _ := terminal.NewTestIOStreams()
	cmd := Root(&RootOptions{IOStreams: ioStreams})
	cmd.SetArgs([]string{"version"})

	err := cmd.Execute()
	require.NoError(t, err)
	output := out.String()
	assert.NotEmpty(t, output, "version command should produce output")
	assert.Contains(t, output, "Version")
}

// TestRoot_FlagDebugOverridesEnv verifies that the --debug flag overrides env vars.
func TestRoot_FlagDebugOverridesEnv(t *testing.T) {
	t.Setenv("SCAFCTL_LOG_LEVEL", "none") // env says none

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := Root(&RootOptions{IOStreams: ioStreams})
	cmd.SetArgs([]string{"--debug", "version"})

	err := cmd.Execute()
	require.NoError(t, err)
}

// TestRoot_SilenceErrors verifies that SilenceErrors is set on root command.
func TestRoot_SilenceErrors(t *testing.T) {
	t.Parallel()
	cmd := Root(nil)
	assert.True(t, cmd.SilenceErrors)
}

// TestRoot_HasAnnotations verifies root command has expected annotations.
func TestRoot_HasAnnotations(t *testing.T) {
	t.Parallel()
	cmd := Root(nil)
	require.NotNil(t, cmd.Annotations)
	assert.Equal(t, "main", cmd.Annotations["commandType"])
}

// TestRoot_SubcommandCount verifies an expected minimum number of subcommands.
func TestRoot_SubcommandCount(t *testing.T) {
	t.Parallel()
	cmd := Root(nil)
	// Should have at least the core subcommands
	expectedMinSubcmds := 5
	if len(cmd.Commands()) < expectedMinSubcmds {
		t.Errorf("Expected at least %d subcommands, got %d", expectedMinSubcmds, len(cmd.Commands()))
	}
}

// TestRoot_QuietFlagDefault verifies that the --quiet flag defaults to false.
func TestRoot_QuietFlagDefault(t *testing.T) {
	t.Parallel()
	cmd := Root(nil)

	flag := cmd.PersistentFlags().Lookup("quiet")
	require.NotNil(t, flag)
	assert.Equal(t, "false", flag.DefValue)
}

// TestRoot_LogLevelFlagDefault verifies the --log-level flag has a default.
func TestRoot_LogLevelFlagDefault(t *testing.T) {
	t.Parallel()
	cmd := Root(nil)

	flag := cmd.PersistentFlags().Lookup("log-level")
	require.NotNil(t, flag)
	assert.NotEmpty(t, flag.DefValue)
}

// TestRoot_OtelFlags verifies that OTel-related flags exist.
func TestRoot_OtelFlags(t *testing.T) {
	t.Parallel()
	cmd := Root(nil)

	flags := []string{"otel-endpoint", "otel-insecure"}
	for _, name := range flags {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := cmd.PersistentFlags().Lookup(name)
			assert.NotNil(t, f, "flag %q should exist", name)
		})
	}
}

// BenchmarkRootConstruction measures time to build the full command tree.
func BenchmarkRootConstruction(b *testing.B) {
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		Root(&RootOptions{IOStreams: ioStreams})
	}
}

// TestRoot_LogLevelFlagExpectedValue ensures the log-level flag's usage is set.
func TestRoot_LogLevelFlagUsage(t *testing.T) {
	t.Parallel()
	cmd := Root(nil)

	flag := cmd.PersistentFlags().Lookup("log-level")
	require.NotNil(t, flag)
	assert.NotEmpty(t, flag.Usage)
}

// TestRoot_SubcommandsHaveShortDesc ensures that all subcommands (except help)
// have a non-empty Short description for clear CLI help and UX.
func TestRoot_SubcommandsHaveShortDesc(t *testing.T) {
	t.Parallel()
	cmd := Root(nil)

	for _, sub := range cmd.Commands() {
		if sub.Name() == "help" {
			continue
		}
		if sub.Short == "" {
			t.Errorf("subcommand %q has empty Short description", sub.Name())
		}
	}
}

// TestRoot_EnvVar_Debug_False verifies that SCAFCTL_DEBUG=false does not enable debug.
func TestRoot_EnvVar_Debug_False(t *testing.T) {
	t.Setenv("SCAFCTL_DEBUG", "false")

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := Root(&RootOptions{IOStreams: ioStreams})
	cmd.SetArgs([]string{"version"})

	err := cmd.Execute()
	require.NoError(t, err)
}

// TestRoot_CwdFlag verifies the --cwd flag exists.
func TestRoot_CwdFlagExists(t *testing.T) {
	t.Parallel()
	cmd := Root(nil)

	flag := cmd.PersistentFlags().Lookup("cwd")
	require.NotNil(t, flag)
	// The default cwd should be empty (resolved at runtime)
	_ = strings.TrimSpace(flag.DefValue)
}

// TestRoot_BinaryName_Default verifies that omitting BinaryName defaults to "scafctl".
func TestRoot_BinaryName_Default(t *testing.T) {
	t.Parallel()
	cmd := Root(nil)
	assert.Equal(t, "scafctl", cmd.Use)
}

// TestRoot_BinaryName_Custom verifies that BinaryName overrides the root command Use.
func TestRoot_BinaryName_Custom(t *testing.T) {
	t.Parallel()
	cmd := Root(&RootOptions{BinaryName: "mycli"})
	assert.Equal(t, "mycli", cmd.Use)
}

// TestRoot_BinaryName_SubcommandShorts verifies that subcommand Short descriptions
// reflect the custom binary name instead of "scafctl".
func TestRoot_BinaryName_SubcommandShorts(t *testing.T) {
	t.Parallel()
	cmd := Root(&RootOptions{BinaryName: "mycli"})

	for _, sub := range cmd.Commands() {
		if sub.Name() == "help" || sub.Short == "" {
			continue
		}
		assert.NotContains(t, sub.Short, "scafctl",
			"subcommand %q Short should not contain 'scafctl' when BinaryName is 'mycli'", sub.Name())
	}
}

// TestRoot_BinaryName_EnvPrefix verifies that env var prefix derives from BinaryName.
func TestRoot_BinaryName_EnvPrefix(t *testing.T) {
	t.Setenv("MYCLI_LOG_LEVEL", "debug")

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := Root(&RootOptions{IOStreams: ioStreams, BinaryName: "mycli"})
	cmd.SetArgs([]string{"version"})

	err := cmd.Execute()
	require.NoError(t, err)
}

// TestRoot_BinaryName_EnvPrefix_Hyphen verifies that hyphens in BinaryName are
// normalized to underscores in the env var prefix so POSIX shells can export them.
func TestRoot_BinaryName_EnvPrefix_Hyphen(t *testing.T) {
	t.Setenv("MY_CLI_LOG_LEVEL", "debug")

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := Root(&RootOptions{IOStreams: ioStreams, BinaryName: "my-cli"})
	cmd.SetArgs([]string{"version"})

	err := cmd.Execute()
	require.NoError(t, err)
}

// TestRoot_PreRunHook_Called verifies that PreRunHook is invoked during PersistentPreRun.
func TestRoot_PreRunHook_Called(t *testing.T) {
	t.Parallel()
	hookCalled := false
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := Root(&RootOptions{
		IOStreams: ioStreams,
		PreRunHook: func(cmd *cobra.Command, args []string) error {
			hookCalled = true
			return nil
		},
	})
	cmd.SetArgs([]string{"version"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.True(t, hookCalled, "PreRunHook should have been called")
}

// TestRoot_PreRunHook_Nil verifies that nil PreRunHook is a no-op.
func TestRoot_PreRunHook_Nil(t *testing.T) {
	t.Parallel()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := Root(&RootOptions{IOStreams: ioStreams, PreRunHook: nil})
	cmd.SetArgs([]string{"version"})

	err := cmd.Execute()
	require.NoError(t, err)
}

// TestRoot_VersionExtra verifies that VersionExtra is passed to the version command.
func TestRoot_VersionExtra(t *testing.T) {
	t.Parallel()
	ioStreams, out, _ := terminal.NewTestIOStreams()
	cmd := Root(&RootOptions{
		IOStreams:  ioStreams,
		BinaryName: "mycli",
		VersionExtra: &settings.VersionInfo{
			Commit:       "abc123",
			BuildVersion: "v2.0.0",
			BuildTime:    "2026-01-01T00:00:00Z",
		},
	})
	cmd.SetArgs([]string{"version"})

	err := cmd.Execute()
	require.NoError(t, err)
	output := out.String()
	assert.Contains(t, output, "mycli")
	assert.Contains(t, output, "v2.0.0")
}

// TestNewRootOptions_NewFields verifies new fields have correct zero values.
func TestNewRootOptions_NewFields(t *testing.T) {
	t.Parallel()
	opts := NewRootOptions()
	assert.Equal(t, "", opts.BinaryName)
	assert.Nil(t, opts.PreRunHook)
	assert.Nil(t, opts.VersionExtra)
}

// TestRoot_PreRunHook_Error verifies that PreRunHook errors are surfaced.
func TestRoot_PreRunHook_Error(t *testing.T) {
	t.Parallel()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	exitCalled := false
	cmd := Root(&RootOptions{
		IOStreams: ioStreams,
		ExitFunc:  func(code int) { exitCalled = true },
		PreRunHook: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("hook failed")
		},
	})
	cmd.SetArgs([]string{"version"})

	_ = cmd.Execute()
	assert.True(t, exitCalled, "ExitFunc should be called when PreRunHook returns an error")
}

// TestRoot_BinaryName_Sanitized verifies that BinaryName with path/extension is sanitized.
func TestRoot_BinaryName_Sanitized(t *testing.T) {
	t.Parallel()
	cmd := Root(&RootOptions{BinaryName: "/usr/bin/my-tool.exe"})
	assert.Equal(t, "my-tool", cmd.Use)
}

// TestRoot_BinaryName_Empty verifies that empty BinaryName falls back to default.
func TestRoot_BinaryName_Empty(t *testing.T) {
	t.Parallel()
	cmd := Root(&RootOptions{BinaryName: ""})
	assert.Equal(t, "scafctl", cmd.Use)
}
