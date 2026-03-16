// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package options

import (
	"bytes"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandOptions_Properties(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandOptions(cliParams, ioStreams, "scafctl")

	assert.Equal(t, "options", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
}

func TestCommandOptions_PrintsGlobalFlags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	var stdout bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &bytes.Buffer{}, false)

	root := &cobra.Command{Use: "scafctl"}
	root.PersistentFlags().String("log-level", "none", "Set the log level")
	root.PersistentFlags().BoolP("quiet", "q", false, "Do not print additional information")
	root.PersistentFlags().Bool("no-color", false, "Disable color output")
	root.PersistentFlags().String("config", "", "Path to config file")

	optCmd := CommandOptions(cliParams, ioStreams, "scafctl")
	root.AddCommand(optCmd)

	root.SetArgs([]string{"options"})
	err := root.Execute()
	require.NoError(t, err)

	output := stdout.String()
	assert.Contains(t, output, "The following options can be passed to any command:")
	assert.Contains(t, output, "--log-level")
	assert.Contains(t, output, "--quiet")
	assert.Contains(t, output, "--no-color")
	assert.Contains(t, output, "--config")
}

func TestCommandOptions_HiddenFlagsNotShown(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	var stdout bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &bytes.Buffer{}, false)

	root := &cobra.Command{Use: "scafctl"}
	root.PersistentFlags().String("pprof", "", "Enable profiling")
	require.NoError(t, root.PersistentFlags().MarkHidden("pprof"))
	root.PersistentFlags().BoolP("quiet", "q", false, "Do not print additional information")

	optCmd := CommandOptions(cliParams, ioStreams, "scafctl")
	root.AddCommand(optCmd)

	root.SetArgs([]string{"options"})
	err := root.Execute()
	require.NoError(t, err)

	output := stdout.String()
	assert.NotContains(t, output, "--pprof")
	assert.Contains(t, output, "--quiet")
}

func BenchmarkCommandOptions(b *testing.B) {
	cliParams := settings.NewCliParams()
	var stdout bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &bytes.Buffer{}, false)

	root := &cobra.Command{Use: "scafctl"}
	root.PersistentFlags().String("log-level", "none", "Set the log level")
	root.PersistentFlags().BoolP("quiet", "q", false, "Suppress output")

	optCmd := CommandOptions(cliParams, ioStreams, "scafctl")
	root.AddCommand(optCmd)

	b.ResetTimer()
	for b.Loop() {
		stdout.Reset()
		root.SetArgs([]string{"options"})
		_ = root.Execute()
	}
}
