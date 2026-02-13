// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package scafctl

import (
	"sync"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
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
