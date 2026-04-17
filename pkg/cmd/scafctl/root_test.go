// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package scafctl

import (
	"strings"
	"sync"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestRoot_CommandProperties(t *testing.T) {
	t.Parallel()
	cmd := Root(nil)
	if cmd == nil {
		t.Fatal("Root() returned nil")
	}
	if cmd.Use != "scafctl" {
		t.Errorf("Root().Use = %q, want %q", cmd.Use, "scafctl")
	}
	if cmd.Short != "A configuration discovery and scaffolding tool" {
		t.Errorf("Root().Short = %q, want %q", cmd.Short, "A configuration discovery and scaffolding tool")
	}
	if cmd.Annotations["commandType"] != "main" {
		t.Errorf("Root().Annotations[\"commandType\"] = %q, want %q", cmd.Annotations["commandType"], "main")
	}
}

func TestRoot_PersistentFlags(t *testing.T) {
	t.Parallel()
	cmd := Root(nil)
	flags := cmd.PersistentFlags()
	if flags.Lookup("log-level") == nil {
		t.Error("Expected 'log-level' persistent flag to be defined")
	}
	if flags.Lookup("quiet") == nil {
		t.Error("Expected 'quiet' persistent flag to be defined")
	}
	if flags.Lookup("no-color") == nil {
		t.Error("Expected 'no-color' persistent flag to be defined")
	}
	if flags.Lookup("pprof") == nil {
		t.Error("Expected 'pprof' persistent flag to be defined")
	}
	if flags.Lookup("pprof-output-dir") == nil {
		t.Error("Expected 'pprof-output-dir' persistent flag to be defined")
	}
	if flags.Lookup("cwd") == nil {
		t.Error("Expected 'cwd' persistent flag to be defined")
	}
}

func TestRoot_HiddenFlags(t *testing.T) {
	t.Parallel()
	cmd := Root(nil)
	pprofFlag := cmd.PersistentFlags().Lookup("pprof")
	if pprofFlag == nil {
		t.Fatal("Expected 'pprof' flag to exist")
	}
	if !pprofFlag.Hidden {
		t.Error("Expected 'pprof' flag to be hidden")
	}
	pprofOutFlag := cmd.PersistentFlags().Lookup("pprof-output-dir")
	if pprofOutFlag == nil {
		t.Fatal("Expected 'pprof-output-dir' flag to exist")
	}
	if !pprofOutFlag.Hidden {
		t.Error("Expected 'pprof-output-dir' flag to be hidden")
	}
}

func TestRoot_HasVersionSubcommand(t *testing.T) {
	t.Parallel()
	cmd := Root(nil)
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Name() == "version" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected 'version' subcommand to be added")
	}
}

func TestRoot_HasOptionsSubcommand(t *testing.T) {
	t.Parallel()
	cmd := Root(nil)
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Name() == "options" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected 'options' subcommand to be added")
	}
}

func TestRoot_CommandGroups(t *testing.T) {
	t.Parallel()
	cmd := Root(nil)

	groups := cmd.Groups()
	if len(groups) == 0 {
		t.Fatal("Expected command groups to be defined")
	}

	wantGroups := []string{"core", "inspect", "scaffold", "config", "plugin"}
	gotIDs := make(map[string]bool)
	for _, g := range groups {
		gotIDs[g.ID] = true
	}
	for _, id := range wantGroups {
		if !gotIDs[id] {
			t.Errorf("Expected group %q to be defined", id)
		}
	}

	// Verify key commands have group assignments.
	subCmds := make(map[string]string)
	for _, sub := range cmd.Commands() {
		subCmds[sub.Name()] = sub.GroupID
	}
	wantAssignments := map[string]string{
		"run":     "core",
		"lint":    "core",
		"get":     "inspect",
		"explain": "inspect",
		"new":     "scaffold",
		"build":   "scaffold",
		"config":  "config",
		"auth":    "config",
		"plugins": "plugin",
		"mcp":     "plugin",
	}
	for name, wantGroup := range wantAssignments {
		if got := subCmds[name]; got != wantGroup {
			t.Errorf("command %q: GroupID = %q, want %q", name, got, wantGroup)
		}
	}
}

func TestRoot_UsageTemplateHidesGlobalFlags(t *testing.T) {
	t.Parallel()
	cmd := Root(nil)

	// Verify the rendered root help omits flags and references "options".
	var buf strings.Builder
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})
	err := cmd.Execute()
	require.NoError(t, err)
	output := buf.String()

	if strings.Contains(output, "Global Flags:") {
		t.Error("Root help output should not contain 'Global Flags:' section")
	}
	if !strings.Contains(output, `Use "scafctl options"`) {
		t.Error("Root help output should reference 'scafctl options' command")
	}
}

func TestRoot_ParallelConstruction(t *testing.T) {
	t.Parallel()
	// Verify that constructing multiple Root() commands concurrently
	// does not cause data races (run with -race to validate).
	const n = 10
	cmds := make([]*cobra.Command, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(idx int) {
			defer wg.Done()
			cmds[idx] = Root(nil)
		}(i)
	}
	wg.Wait()
	for i, cmd := range cmds {
		if cmd == nil {
			t.Errorf("Root() call %d returned nil", i)
		}
	}
}

func TestRoot_WithCustomIOStreams(t *testing.T) {
	t.Parallel()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := Root(&RootOptions{IOStreams: ioStreams})
	if cmd == nil {
		t.Fatal("Root() with custom IOStreams returned nil")
	}
	// Verify the command was constructed successfully with custom streams—
	// subcommands are added and the command is usable.
	if len(cmd.Commands()) == 0 {
		t.Error("Expected subcommands to be registered")
	}
}

func TestRoot_WithExitFunc(t *testing.T) {
	t.Parallel()
	var captured int
	exitCalled := false
	cmd := Root(&RootOptions{
		ExitFunc: func(code int) {
			exitCalled = true
			captured = code
		},
	})
	if cmd == nil {
		t.Fatal("Root() with ExitFunc returned nil")
	}
	// The exit func is wired through writer options, which are applied
	// during PersistentPreRun. We verify it was accepted without error.
	_ = exitCalled
	_ = captured
}

func TestRoot_CustomBinaryNameUpdatesSolutionDiscovery(t *testing.T) {
	// Restore package-level state regardless of test outcome.
	t.Cleanup(func() {
		settings.RootSolutionFolders = settings.SolutionFoldersFor(settings.CliBinaryName)
		settings.SolutionFileNames = settings.SolutionFileNamesFor(settings.CliBinaryName)
	})

	cmd := Root(&RootOptions{
		BinaryName: "cldctl",
	})
	if cmd == nil {
		t.Fatal("Root() with custom BinaryName returned nil")
	}
	if cmd.Use != "cldctl" {
		t.Errorf("Root().Use = %q, want %q", cmd.Use, "cldctl")
	}
	// Verify package-level solution discovery vars were updated
	expectedFolders := settings.SolutionFoldersFor("cldctl")
	if len(settings.RootSolutionFolders) != len(expectedFolders) {
		t.Errorf("RootSolutionFolders length = %d, want %d", len(settings.RootSolutionFolders), len(expectedFolders))
	}
	for i, f := range settings.RootSolutionFolders {
		if f != expectedFolders[i] {
			t.Errorf("RootSolutionFolders[%d] = %q, want %q", i, f, expectedFolders[i])
		}
	}
	expectedNames := settings.SolutionFileNamesFor("cldctl")
	if len(settings.SolutionFileNames) != len(expectedNames) {
		t.Errorf("SolutionFileNames length = %d, want %d", len(settings.SolutionFileNames), len(expectedNames))
	}
	for i, n := range settings.SolutionFileNames {
		if n != expectedNames[i] {
			t.Errorf("SolutionFileNames[%d] = %q, want %q", i, n, expectedNames[i])
		}
	}
}
