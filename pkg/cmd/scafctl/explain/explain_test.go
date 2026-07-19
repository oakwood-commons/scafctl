// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package explain

import (
	"bytes"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandExplain(t *testing.T) {
	t.Run("creates schema-only explain command with no instance subcommands", func(t *testing.T) {
		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			Out:    outBuf,
			ErrOut: errBuf,
		}
		cliParams := &settings.Run{}

		cmd := CommandExplain(cliParams, ioStreams, "scafctl")

		assert.Equal(t, "explain <kind>[.field.path]", cmd.Use)
		assert.Contains(t, cmd.Aliases, "exp")
		assert.NotEmpty(t, cmd.Short)
		assert.NotEmpty(t, cmd.Long)

		// explain is schema-of-a-kind only; instance views live under 'inspect'.
		// It has no subcommands (the former 'explain solution' was retired in
		// favor of 'inspect solution').
		assert.Empty(t, cmd.Commands())
	})

	t.Run("explain with no args shows help instead of erroring", func(t *testing.T) {
		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			Out:    outBuf,
			ErrOut: errBuf,
		}
		cliParams := &settings.Run{}

		cmd := CommandExplain(cliParams, ioStreams, "scafctl")
		cmd.SetOut(outBuf)
		cmd.SetErr(errBuf)
		cmd.SetArgs([]string{})

		err := cmd.Execute()
		require.NoError(t, err)
		// Help output includes the usage line and available kinds.
		assert.Contains(t, outBuf.String(), "explain <kind>")
	})

	t.Run("explain shows provider schema", func(t *testing.T) {
		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			Out:    outBuf,
			ErrOut: errBuf,
		}
		cliParams := &settings.Run{}

		cmd := CommandExplain(cliParams, ioStreams, "scafctl")
		cmd.SetOut(outBuf)
		cmd.SetErr(errBuf)
		cmd.SetArgs([]string{"provider"})

		err := cmd.Execute()
		require.NoError(t, err)

		output := outBuf.String()
		assert.Contains(t, output, "KIND:")
		assert.Contains(t, output, "Descriptor")
	})
}
