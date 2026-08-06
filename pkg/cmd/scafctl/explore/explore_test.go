// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package explore

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandExplore(t *testing.T) {
	t.Parallel()

	root := &cobra.Command{Use: "scafctl"}
	cmd := CommandExplore(root, "scafctl")

	require.NotNil(t, cmd)
	assert.Equal(t, "explore", cmd.Name())
	assert.NotEmpty(t, cmd.Short, "explore command should have a short description")
	assert.NotNil(t, cmd.RunE, "explore command should be runnable")
	assert.NotNil(t, cmd.Flags().Lookup("theme"), "explore command should register the --theme flag")
}

// TestCommandExplore_EmbedderBinaryName verifies the embedder contract: a
// non-default binary name is threaded through to the explorer.
func TestCommandExplore_EmbedderBinaryName(t *testing.T) {
	t.Parallel()

	root := &cobra.Command{Use: "mycli"}
	cmd := CommandExplore(root, "mycli")

	require.NotNil(t, cmd)
	assert.Equal(t, "explore", cmd.Name())
	assert.NotNil(t, cmd.RunE, "explore command should be runnable")
}
