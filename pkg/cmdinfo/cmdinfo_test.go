// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package cmdinfo

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildTestTree() *cobra.Command {
	root := &cobra.Command{Use: "mycli", Short: "Test CLI"}

	run := &cobra.Command{Use: "run", Short: "Run things", GroupID: "core"}
	run.AddCommand(&cobra.Command{Use: "solution", Short: "Run a solution", Aliases: []string{"sol"}})
	run.AddCommand(&cobra.Command{Use: "resolver", Short: "Run resolvers"})

	get := &cobra.Command{Use: "get", Short: "Get things", GroupID: "inspect"}
	get.AddCommand(&cobra.Command{Use: "provider", Short: "List providers"})

	hidden := &cobra.Command{Use: "internal", Short: "Internal only", Hidden: true}

	root.AddGroup(&cobra.Group{ID: "core", Title: "Core"})
	root.AddGroup(&cobra.Group{ID: "inspect", Title: "Inspect"})
	root.AddCommand(run, get, hidden)

	return root
}

func TestCollectCommands_All(t *testing.T) {
	t.Parallel()
	root := buildTestTree()
	commands := CollectCommands(root, false)

	require.NotEmpty(t, commands)

	// Should include parent and leaf commands
	names := make([]string, 0, len(commands))
	for _, c := range commands {
		names = append(names, c.Name)
	}
	assert.Contains(t, names, "mycli get")
	assert.Contains(t, names, "mycli get provider")
	assert.Contains(t, names, "mycli run")
	assert.Contains(t, names, "mycli run solution")
	assert.Contains(t, names, "mycli run resolver")

	// Hidden commands should not appear
	assert.NotContains(t, names, "mycli internal")
}

func TestCollectCommands_LeafOnly(t *testing.T) {
	t.Parallel()
	root := buildTestTree()
	commands := CollectCommands(root, true)

	names := make([]string, 0, len(commands))
	for _, c := range commands {
		names = append(names, c.Name)
	}

	// Leaf commands should appear
	assert.Contains(t, names, "mycli get provider")
	assert.Contains(t, names, "mycli run solution")
	assert.Contains(t, names, "mycli run resolver")

	// Parent commands should not appear (they have children)
	assert.NotContains(t, names, "mycli get")
	assert.NotContains(t, names, "mycli run")
}

func TestCollectCommands_Aliases(t *testing.T) {
	t.Parallel()
	root := buildTestTree()
	commands := CollectCommands(root, true)

	for _, c := range commands {
		if c.Name == "mycli run solution" {
			assert.Equal(t, []string{"sol"}, c.Aliases)
			return
		}
	}
	t.Fatal("mycli run solution not found")
}

func TestCollectCommands_Groups(t *testing.T) {
	t.Parallel()
	root := buildTestTree()
	commands := CollectCommands(root, true)

	for _, c := range commands {
		if c.Name == "mycli run solution" {
			assert.Equal(t, "core", c.Group)
		}
		if c.Name == "mycli get provider" {
			assert.Equal(t, "inspect", c.Group)
		}
	}
}

func TestCollectCommands_Sorted(t *testing.T) {
	t.Parallel()
	root := buildTestTree()
	commands := CollectCommands(root, false)

	for i := 1; i < len(commands); i++ {
		assert.LessOrEqual(t, commands[i-1].Name, commands[i].Name, "commands should be sorted alphabetically")
	}
}

func TestCollectCommands_EmptyRoot(t *testing.T) {
	t.Parallel()
	root := &cobra.Command{Use: "empty"}
	commands := CollectCommands(root, true)
	// Root itself is a leaf since it has no children
	require.Len(t, commands, 1)
	assert.Equal(t, "empty", commands[0].Name)
}

func BenchmarkCollectCommands(b *testing.B) {
	root := buildTestTree()
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		CollectCommands(root, true)
	}
}
