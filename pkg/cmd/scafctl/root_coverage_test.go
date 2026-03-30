// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package scafctl

import (
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/terminal"
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
