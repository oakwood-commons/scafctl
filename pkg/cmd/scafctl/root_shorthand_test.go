// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package scafctl

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRoot_NoShorthandConflicts walks the entire command tree constructed by
// Root(nil) and forces Cobra to merge persistent flags into every subcommand.
// If any ancestor persistent flag shares a single-letter shorthand with a
// descendant local or persistent flag, pflag panics during merge.
func TestRoot_NoShorthandConflicts(t *testing.T) {
	t.Parallel()

	rootCmd, cleanup := Root(nil)
	defer cleanup()
	require.NotNil(t, rootCmd)

	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		require.NotPanics(t, func() {
			inherited := cmd.InheritedFlags()
			local := cmd.LocalNonPersistentFlags()
			local.VisitAll(func(f *pflag.Flag) {
				if f.Shorthand == "" {
					return
				}
				conflict := inherited.ShorthandLookup(f.Shorthand)
				assert.Nilf(t, conflict,
					"command %q local flag --%s (-%s) collides with inherited flag --%s",
					cmd.CommandPath(), f.Name, f.Shorthand, flagName(conflict))
			})
			_ = cmd.Flags()
		}, "shorthand conflict on %q", cmd.CommandPath())

		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}

	walk(rootCmd)
}

func flagName(f *pflag.Flag) string {
	if f == nil {
		return ""
	}
	return f.Name
}

func TestRoot_DebugShorthandCapitalD(t *testing.T) {
	t.Parallel()

	t.Run("root binds -D to --debug and leaves -d unbound", func(t *testing.T) {
		t.Parallel()
		rootCmd, cleanup := Root(nil)
		defer cleanup()

		dUpper := rootCmd.PersistentFlags().ShorthandLookup("D")
		require.NotNil(t, dUpper, "expected -D shorthand on root persistent flags")
		assert.Equal(t, "debug", dUpper.Name)

		assert.Nil(t, rootCmd.PersistentFlags().ShorthandLookup("d"),
			"root persistent flags must not bind -d so subcommands can use -d for --description/--data")
	})

	t.Run("-D enables debug log level at runtime", func(t *testing.T) {
		t.Parallel()
		emptyCfg := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(emptyCfg, []byte("{}\n"), 0o600))

		ioStreams, _, _ := terminal.NewTestIOStreams()
		var gotParams *settings.Run
		cmd, cleanup := Root(&RootOptions{
			IOStreams:  ioStreams,
			ConfigPath: emptyCfg,
			PreRunHook: func(c *cobra.Command, _ []string) error {
				gotParams, _ = settings.FromContext(c.Context())
				return nil
			},
		})
		defer cleanup()

		cmd.SetArgs([]string{"-D", "version", "-o", "json"})
		require.NoError(t, cmd.Execute())
		require.NotNil(t, gotParams)
		assert.Equal(t, "debug", gotParams.MinLogLevel)
	})

	t.Run("subcommands with local -d execute through Root without conflict", func(t *testing.T) {
		t.Parallel()
		emptyCfg := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(emptyCfg, []byte("{}\n"), 0o600))

		for _, args := range [][]string{
			{"new", "solution", "demo-app", "-d", "Demo description"},
			{"eval", "cel", "--expression", "int(_.x) + 1", "-d", `{"x": 41}`},
			{"eval", "template", "-t", "{{ .env }}", "-d", `{"env": "prod"}`},
		} {
			ioStreams, stdout, _ := terminal.NewTestIOStreams()
			cmd, cleanup := Root(&RootOptions{
				IOStreams:  ioStreams,
				ConfigPath: emptyCfg,
			})
			cmd.SetArgs(args)
			err := cmd.Execute()
			cleanup()
			require.NoErrorf(t, err, "args %v should succeed", args)
			assert.NotEmpty(t, stdout.String())
		}
	})
}
