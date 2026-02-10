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
	t.Run("creates explain command with subcommands", func(t *testing.T) {
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

		// Has solution subcommand for explaining specific solution instances
		subCmds := cmd.Commands()
		assert.Len(t, subCmds, 1)
		assert.Equal(t, "solution [path]", subCmds[0].Use)
	})

	t.Run("explain command requires kind argument", func(t *testing.T) {
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
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires at least 1 arg")
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
