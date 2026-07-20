// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package cmdutil_test

import (
	"bytes"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/cmd/cmdutil"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newGroup builds a parent command with one child subcommand, configured as a
// help-only group.
func newGroup() *cobra.Command {
	parent := cmdutil.MakeHelpOnlyGroup(&cobra.Command{
		Use:           "parent",
		Short:         "parent group",
		SilenceUsage:  true,
		SilenceErrors: true,
	})
	child := &cobra.Command{
		Use:  "child",
		RunE: func(*cobra.Command, []string) error { return nil },
	}
	parent.AddCommand(child)
	return parent
}

func TestMakeHelpOnlyGroup_BareShowsHelpExitZero(t *testing.T) {
	cmd := newGroup()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{})

	err := cmd.Execute()

	require.NoError(t, err, "bare invocation must not error")
	assert.Contains(t, out.String(), "Usage:", "bare invocation should print help")
}

func TestMakeHelpOnlyGroup_UnknownSubcommandErrors(t *testing.T) {
	cmd := newGroup()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"bogus"})

	err := cmd.Execute()

	require.Error(t, err, "unknown subcommand must return an error")
	assert.Contains(t, err.Error(), "unknown command",
		"error should identify the unknown subcommand")
}

func TestMakeHelpOnlyGroup_KnownSubcommandStillRuns(t *testing.T) {
	cmd := newGroup()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"child"})

	err := cmd.Execute()

	require.NoError(t, err, "known subcommand should execute normally")
}

func TestMakeHelpOnlyGroup_SetsArgsAndRunE(t *testing.T) {
	cmd := cmdutil.MakeHelpOnlyGroup(&cobra.Command{Use: "g"})
	assert.NotNil(t, cmd.Args, "Args should be set to NoArgs")
	assert.NotNil(t, cmd.RunE, "RunE should be set")

	// NoArgs rejects a positional, permits none.
	require.NoError(t, cmd.Args(cmd, []string{}))
	require.Error(t, cmd.Args(cmd, []string{"x"}))
}

func TestMakeHelpOnlyGroup_PreservesExistingFields(t *testing.T) {
	cmd := cmdutil.MakeHelpOnlyGroup(&cobra.Command{
		Use:     "g",
		Short:   "short desc",
		Aliases: []string{"gg"},
	})
	assert.Equal(t, "short desc", cmd.Short, "Short must be preserved")
	assert.Equal(t, []string{"gg"}, cmd.Aliases, "Aliases must be preserved")
}
