// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandDelete_KindSubcommandsRegistered(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")

	for _, name := range []string{"solution", "provider", "auth-handler"} {
		sub, _, err := cmd.Find([]string{name})
		require.NoError(t, err)
		assert.Equal(t, name, sub.Name())
	}
}

func TestCommandDeleteKind_InheritedSharedFlags(t *testing.T) {
	t.Parallel()

	// Each subtest builds its own command: cmd.Find merges the parent's
	// persistent flags into the command tree, which races when shared.
	for _, name := range []string{"solution", "provider", "auth-handler"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ioStreams, _, _ := terminal.NewTestIOStreams()
			cmd := CommandDelete(settings.NewCliParams(), ioStreams, "scafctl/catalog")
			sub, _, err := cmd.Find([]string{name})
			require.NoError(t, err)
			assert.NotNil(t, sub.InheritedFlags().Lookup("dry-run"))
			assert.NotNil(t, sub.InheritedFlags().Lookup("insecure"))
		})
	}
}

func TestCommandDeleteKind_RequiresOneArg(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetArgs([]string{"solution"})

	require.Error(t, cmd.Execute())
}

func TestCommandDeleteKind_RequiresVersion(t *testing.T) {
	t.Parallel()

	for _, dk := range deleteKinds {
		t.Run(string(dk.Kind), func(t *testing.T) {
			t.Parallel()

			cliParams := settings.NewCliParams()
			ioStreams, _, _ := terminal.NewTestIOStreams()
			cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")
			cmd.SetContext(newCatalogTestCtx(t))
			cmd.SetArgs([]string{string(dk.Kind), "my-artifact"})

			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "version required")
		})
	}
}

func TestValidateDelete_RejectsRemoteReference(t *testing.T) {
	t.Parallel()

	for _, ref := range []string{
		"ghcr.io/org/scafctl/solutions/my-sol@1.0.0",
		"oci://ghcr.io/org/my-sol@1.0.0",
		"localhost:5000/my-sol@1.0.0",
	} {
		t.Run(ref, func(t *testing.T) {
			t.Parallel()

			err := ValidateDelete(&DeleteOptions{Reference: ref})
			require.Error(t, err)

			var invalid *catalog.InvalidReferenceError
			require.ErrorAs(t, err, &invalid)
			assert.Equal(t, ref, invalid.Input)
			assert.Contains(t, err.Error(), "full remote references are not allowed")
		})
	}
}

func TestCommandDeleteKind_RejectsRemoteReference(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"solution", "ghcr.io/org/scafctl/solutions/my-sol@1.0.0"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "full remote references are not allowed")
}

func TestCommandDeleteKind_OriginWithCatalogRejected(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"solution", "my-artifact@1.0.0", "--origin", "built", "--catalog", "ghcr.io/org"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrOriginWithCatalog)
}

func TestCommandDelete_RootStillAcceptsReference(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetArgs([]string{"--all", "extra"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--all cannot be used with positional arguments")
}
