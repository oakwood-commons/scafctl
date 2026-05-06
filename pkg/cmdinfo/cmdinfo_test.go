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

func TestFindCommand(t *testing.T) {
	t.Parallel()
	root := buildTestTree()

	tests := []struct {
		name     string
		path     string
		wantName string
		wantNil  bool
	}{
		{name: "empty path returns root", path: "", wantName: "mycli"},
		{name: "direct child", path: "run", wantName: "run"},
		{name: "nested child", path: "run solution", wantName: "solution"},
		{name: "with root prefix", path: "mycli run solution", wantName: "solution"},
		{name: "alias lookup", path: "run sol", wantName: "solution"},
		{name: "not found", path: "nonexistent", wantNil: true},
		{name: "nested not found", path: "run nonexistent", wantNil: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := FindCommand(root, tt.path)
			if tt.wantNil {
				assert.Nil(t, cmd)
			} else {
				require.NotNil(t, cmd)
				assert.Equal(t, tt.wantName, cmd.Name())
			}
		})
	}
}

func TestGetCommandDetail(t *testing.T) {
	t.Parallel()

	root := &cobra.Command{
		Use:     "mycli",
		Short:   "Test CLI",
		Long:    "A test CLI for unit tests",
		Example: "  mycli run solution",
	}

	child := &cobra.Command{
		Use:     "run",
		Short:   "Run things",
		Aliases: []string{"r"},
	}
	child.Flags().StringP("file", "f", "", "Solution file path")
	child.Flags().BoolP("verbose", "v", false, "Enable verbose output")
	_ = child.MarkFlagRequired("file")

	leaf := &cobra.Command{
		Use:   "solution [name]",
		Short: "Run a solution",
	}
	child.AddCommand(leaf)
	root.AddCommand(child)

	t.Run("root command", func(t *testing.T) {
		t.Parallel()
		detail := GetCommandDetail(root)
		assert.Equal(t, "mycli", detail.Name)
		assert.Equal(t, "Test CLI", detail.Short)
		assert.Equal(t, "A test CLI for unit tests", detail.Long)
		assert.Equal(t, "  mycli run solution", detail.Examples)
		assert.Contains(t, detail.Subcommands, "run")
	})

	t.Run("command with flags", func(t *testing.T) {
		t.Parallel()
		detail := GetCommandDetail(child)
		assert.Equal(t, "mycli run", detail.Name)
		assert.Equal(t, []string{"r"}, detail.Aliases)
		assert.Contains(t, detail.Subcommands, "solution")

		// Check flags
		require.GreaterOrEqual(t, len(detail.Flags), 2)
		flagNames := make(map[string]FlagInfo)
		for _, f := range detail.Flags {
			flagNames[f.Name] = f
		}
		fileFlag, ok := flagNames["file"]
		require.True(t, ok)
		assert.Equal(t, "f", fileFlag.Shorthand)
		assert.Equal(t, "string", fileFlag.Type)
		assert.Equal(t, "Solution file path", fileFlag.Description)
		assert.True(t, fileFlag.Required)

		verboseFlag, ok := flagNames["verbose"]
		require.True(t, ok)
		assert.Equal(t, "v", verboseFlag.Shorthand)
		assert.Equal(t, "bool", verboseFlag.Type)
		assert.False(t, verboseFlag.Required)
	})

	t.Run("leaf command no subcommands", func(t *testing.T) {
		t.Parallel()
		detail := GetCommandDetail(leaf)
		assert.Equal(t, "mycli run solution", detail.Name)
		assert.Empty(t, detail.Subcommands)
		assert.Equal(t, "mycli run solution [name]", detail.Usage)
	})
}

func BenchmarkGetCommandDetail(b *testing.B) {
	root := buildTestTree()
	cmd := FindCommand(root, "run solution")
	require.NotNil(b, cmd)
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		GetCommandDetail(cmd)
	}
}
