package scafctl

import (
	"testing"
)

func TestRoot_CommandProperties(t *testing.T) {
	cmd := Root()
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
	cmd := Root()
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
	cmd := Root()
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
	cmd := Root()
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
