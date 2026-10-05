// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCatalogKindNames(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"solution", "provider", "auth-handler"}, catalogKindNames())
}

func TestIsCatalogKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want bool
	}{
		{"solution", true},
		{"solutions", true},
		{"sol", true},
		{"provider", true},
		{"providers", true},
		{"auth-handler", true},
		{"auth-handlers", true},
		{"bogus-kind", false},
		{"", false},
		{"Solution", false},
		{"ghcr.io/org/scafctl/solutions/x@1.0.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isCatalogKind(tt.name))
		})
	}
}

func TestCheckUnknownKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		binary  string
		args    []string
		wantErr bool
	}{
		{name: "no args", binary: "scafctl", args: nil},
		{name: "single arg is a reference", binary: "scafctl", args: []string{"just-a-name"}},
		{name: "known kind with name", binary: "scafctl", args: []string{"solution", "x@1.0.0"}},
		{name: "alias with name", binary: "scafctl", args: []string{"sol", "x@1.0.0"}},
		{name: "unknown kind with name", binary: "scafctl", args: []string{"bogus-kind", "x@1.0.0"}, wantErr: true},
		{name: "unknown kind non-default binary", binary: "mycli", args: []string{"bogus-kind", "x@1.0.0"}, wantErr: true},
		{name: "unknown kind with extra args", binary: "scafctl", args: []string{"bogus-kind", "a", "b"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := checkUnknownKind("pull", tt.binary, tt.args)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}

			require.ErrorIs(t, err, ErrUnknownKind)
			assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
			msg := err.Error()
			assert.Contains(t, msg, `"bogus-kind"`)
			assert.Contains(t, msg, "valid kinds: solution, provider, auth-handler")
			assert.Contains(t, msg, tt.binary+" catalog pull <kind> <name>")
		})
	}
}

func TestRootArgs(t *testing.T) {
	t.Parallel()

	called := false
	next := func(_ *cobra.Command, _ []string) error {
		called = true
		return nil
	}
	validate := rootArgs("push", "scafctl", next)

	require.NoError(t, validate(&cobra.Command{}, []string{"bogus-kind", "x@1.0.0"}))
	assert.False(t, called, "wrapped validator must be skipped for an unknown kind")

	require.NoError(t, validate(&cobra.Command{}, []string{"ghcr.io/org/scafctl/solutions/x@1.0.0"}))
	assert.True(t, called, "wrapped validator must run for a normal reference")
}

// TestRootCommands_UnknownKind runs each root command with a mistyped kind and
// checks it reports the valid kinds instead of an argument-count error, and
// that a single bare argument keeps its existing validation error.
func TestRootCommands_UnknownKind(t *testing.T) {
	t.Parallel()

	roots := []struct {
		verb  string
		build func(*settings.Run, *terminal.IOStreams) *cobra.Command
	}{
		{"pull", func(c *settings.Run, io *terminal.IOStreams) *cobra.Command {
			return CommandPull(c, io, "scafctl/catalog")
		}},
		{"push", func(c *settings.Run, io *terminal.IOStreams) *cobra.Command {
			return CommandPush(c, io, "scafctl/catalog")
		}},
		{"delete", func(c *settings.Run, io *terminal.IOStreams) *cobra.Command {
			return CommandDelete(c, io, "scafctl/catalog")
		}},
	}

	for _, root := range roots {
		for _, binary := range []string{"scafctl", "mycli"} {
			t.Run(root.verb+"/"+binary, func(t *testing.T) {
				t.Parallel()

				cliParams := settings.NewCliParams()
				cliParams.BinaryName = binary
				ioStreams, _, _ := terminal.NewTestIOStreams()

				cmd := root.build(cliParams, ioStreams)
				cmd.SetContext(newCatalogTestCtx(t))
				cmd.SetArgs([]string{"bogus-kind", "x@1.0.0"})

				err := cmd.Execute()

				require.ErrorIs(t, err, ErrUnknownKind)
				assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
				assert.Contains(t, err.Error(), "valid kinds: solution, provider, auth-handler")
				assert.Contains(t, err.Error(), binary+" catalog "+root.verb+" <kind> <name>")
			})
		}

		t.Run(root.verb+"/single bare arg keeps its own error", func(t *testing.T) {
			t.Parallel()

			ioStreams, _, _ := terminal.NewTestIOStreams()
			cmd := root.build(settings.NewCliParams(), ioStreams)
			cmd.SetContext(newCatalogTestCtx(t))
			cmd.SetArgs([]string{"just-a-name"})

			err := cmd.Execute()

			require.Error(t, err)
			assert.NotErrorIs(t, err, ErrUnknownKind)
			assert.ErrorIs(t, err, ErrRemoteReferenceRequired)
		})
	}
}

func BenchmarkCheckUnknownKind(b *testing.B) {
	args := []string{"bogus-kind", "x@1.0.0"}

	b.ReportAllocs()
	for b.Loop() {
		_ = checkUnknownKind("pull", "scafctl", args)
	}
}
