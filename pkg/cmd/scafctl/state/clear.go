// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"fmt"
	"os"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/fingerprint"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// clearOptions holds the options for the clear command.
type clearOptions struct {
	Path             string
	Action           string
	FingerprintsOnly bool
}

// CommandClear creates the 'state clear' command.
func CommandClear(_ *settings.Run, _ *terminal.IOStreams, _ string) *cobra.Command {
	opts := &clearOptions{}

	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Clear state values",
		Long:  "Remove stored parameters, persisted resolvers, and fingerprints from a state file, preserving metadata.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runClear(cmd, opts)
		},
	}

	cmd.Flags().StringVar(&opts.Path, "path", "", "State file path (relative to working directory or absolute)")
	cmd.Flags().StringVar(&opts.Action, "action", "", "Clear fingerprints for a specific action only")
	cmd.Flags().BoolVar(&opts.FingerprintsOnly, "fingerprints-only", false, "Clear only fingerprint entries, keep parameters and persisted resolvers")
	_ = cmd.MarkFlagRequired("path")
	cmd.MarkFlagsMutuallyExclusive("action", "fingerprints-only")

	return cmd
}

func runClear(cmd *cobra.Command, opts *clearOptions) error {
	ctx := cmd.Context()
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	// Resolve and verify the file exists.
	cwd, err := os.Getwd()
	if err != nil {
		err := fmt.Errorf("cannot determine working directory: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	resolved, resolveErr := state.ResolveStatePath(opts.Path, cwd)
	if resolveErr != nil {
		w.Errorf("%v", resolveErr)
		return exitcode.WithCode(resolveErr, exitcode.InvalidInput)
	}
	if _, statErr := os.Stat(resolved); os.IsNotExist(statErr) {
		err := fmt.Errorf("state file not found: %s", resolved)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	sd, err := state.LoadFromFile(opts.Path, cwd)
	if err != nil {
		err := fmt.Errorf("failed to load state: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	count := clearEntries(sd, opts)

	if err := state.SaveToFile(opts.Path, cwd, sd); err != nil {
		err := fmt.Errorf("failed to save state: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	w.Successf("Cleared %d entries\n", count)
	return nil
}

// clearEntries removes entries from state data based on the clear options.
// Returns the number of entries removed.
func clearEntries(sd *state.Data, opts *clearOptions) int {
	// --action: clear fingerprints for a specific action only
	if opts.Action != "" {
		return fingerprint.ClearAction(sd, opts.Action)
	}

	// --fingerprints-only: clear all fingerprints, keep parameters and persisted resolvers
	if opts.FingerprintsOnly {
		count := len(sd.Fingerprints)
		sd.Fingerprints = make(map[string]*state.FingerprintEntry)
		return count
	}

	// Default: clear everything
	count := len(sd.Parameters) + len(sd.Resolvers) + len(sd.Fingerprints)
	sd.Parameters = make(map[string]any)
	sd.Resolvers = make(map[string]*state.PersistedEntry)
	sd.Fingerprints = make(map[string]*state.FingerprintEntry)
	return count
}
