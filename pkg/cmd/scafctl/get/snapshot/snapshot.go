// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package snapshot provides the `get snapshot` subcommand, which loads and
// displays the contents of a resolver execution snapshot file.
package snapshot

import (
	"context"
	"fmt"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// ShowOptions holds options for the get snapshot command.
type ShowOptions struct {
	BinaryName   string
	SnapshotFile string
	Verbose      bool
	NoColor      bool
	IOStreams    *terminal.IOStreams

	// Detail switches the default summary view to the per-resolver detail list.
	Detail bool

	// kvx output integration (-o/--output, -i/--interactive, -e/--expression, -w/--where)
	flags.KvxOutputFlags
}

// CommandSnapshot creates the `get snapshot` subcommand.
func CommandSnapshot(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	opts := &ShowOptions{}
	binaryName := cliParams.BinaryName
	if binaryName == "" {
		binaryName = settings.CliBinaryName
	}

	cmd := &cobra.Command{
		Use:          "snapshot [snapshot-file]",
		Short:        "Display snapshot contents",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Load and display the contents of a snapshot file.

			By default, a high-level human-readable summary is shown (metadata,
			resolver counts, and phases). Use --detail to list every resolver
			with its status, or -o/--output to emit structured data (json, yaml,
			table, etc.).
		`),
		Example: heredoc.Docf(`
			# Show snapshot summary (default)
			$ %s get snapshot snapshot.json

			# Emit the full snapshot as JSON
			$ %s get snapshot snapshot.json -o json

			# List all resolvers with status
			$ %s get snapshot snapshot.json --detail --verbose
		`, binaryName, binaryName, binaryName),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.SnapshotFile = args[0]
			opts.BinaryName = cliParams.BinaryName
			opts.Verbose = cliParams.Verbose
			opts.NoColor = cliParams.NoColor
			opts.IOStreams = ioStreams
			return runShow(cmd.Context(), opts, ioStreams)
		},
	}

	// Add kvx output flags (-o, -i, -e, -w).
	flags.AddKvxOutputFlagsToStruct(cmd, &opts.KvxOutputFlags)

	// --detail switches the default view to the per-resolver listing.
	cmd.Flags().BoolVar(&opts.Detail, "detail", false, "List every resolver with its status")

	return cmd
}

func runShow(ctx context.Context, opts *ShowOptions, ioStreams *terminal.IOStreams) error {
	if opts.BinaryName == "" {
		opts.BinaryName = settings.CliBinaryName
	}
	if opts.IOStreams == nil {
		opts.IOStreams = ioStreams
	}

	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Create a fallback Writer if one isn't in context (e.g., in tests)
	if w == nil {
		// Copy into a local so the ErrOut fixup below never mutates the
		// caller's shared IOStreams (ioStreams is a pointer).
		var streams terminal.IOStreams
		if ioStreams != nil {
			streams = *ioStreams
		}
		// Ensure ErrOut is non-nil to avoid panics in error paths.
		if streams.ErrOut == nil {
			streams.ErrOut = streams.Out
		}
		w = writer.New(&streams, settings.NewCliParams())
	}

	// Helper to write error
	writeErr := func(err error) {
		w.Errorf("%v", err)
	}

	// Load snapshot
	lgr.V(-1).Info("loading snapshot", "file", opts.SnapshotFile)
	snapshot, err := resolver.LoadSnapshot(opts.SnapshotFile)
	if err != nil {
		err = fmt.Errorf("failed to load snapshot: %w", err)
		writeErr(err)
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	// Structured output requested via -o (or -i/-e/-w): emit through kvx.
	if opts.Interactive || (opts.Output != "" && opts.Output != "auto") {
		if err := opts.writeOutput(ctx, snapshot); err != nil {
			err = fmt.Errorf("failed to write output: %w", err)
			writeErr(err)
			return exitcode.WithCode(err, exitcode.GeneralError)
		}
		return nil
	}

	// Default human-readable views.
	if opts.Detail {
		return showResolvers(snapshot, opts, w)
	}
	return showSummary(snapshot, opts, w)
}

// writeOutput writes the structured snapshot using the shared kvx pipeline.
func (o *ShowOptions) writeOutput(ctx context.Context, snapshot *resolver.Snapshot) error {
	kvxOpts := flags.ToKvxOutputOptions(&o.KvxOutputFlags,
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(o.NoColor),
		kvx.WithOutputAppName(o.BinaryName+" get snapshot"),
		kvx.WithIOStreams(o.IOStreams),
	)

	return kvxOpts.Write(snapshot)
}

func showSummary(snapshot *resolver.Snapshot, opts *ShowOptions, w *writer.Writer) error {
	if w == nil {
		return nil
	}

	w.Plainln("Snapshot Summary")
	w.Plainln("================\n")

	// Metadata
	w.Plainlnf("Solution:        %s (v%s)", snapshot.Metadata.Solution, snapshot.Metadata.Version)
	w.Plainlnf("Timestamp:       %s", snapshot.Metadata.Timestamp.Format("2006-01-02 15:04:05"))
	w.Plainlnf("Engine:          %s %s", snapshot.Metadata.Runtime.Engine.Name, snapshot.Metadata.Runtime.Engine.Version)
	w.Plainlnf("CLI:             %s %s", snapshot.Metadata.Runtime.CLI.Name, snapshot.Metadata.Runtime.CLI.Version)
	w.Plainlnf("Total Duration:  %s", snapshot.Metadata.TotalDuration)
	w.Plainlnf("Overall Status:  %s\n", snapshot.Metadata.Status)

	// Count status (only count non-nil entries for accurate totals)
	var total, success, failed, skipped int
	for _, res := range snapshot.Resolvers {
		if res == nil {
			continue
		}
		total++
		switch res.Status {
		case "success":
			success++
		case "failed":
			failed++
		case "skipped":
			skipped++
		}
	}

	w.Plainlnf("Resolvers:       %d total", len(snapshot.Resolvers))
	w.Plainlnf("  Success:       %d", success)
	w.Plainlnf("  Failed:        %d", failed)
	w.Plainlnf("  Skipped:       %d", skipped)

	if len(snapshot.Phases) > 0 {
		w.Plainlnf("\nPhases:          %d", len(snapshot.Phases))
		if opts.Verbose {
			for _, phase := range snapshot.Phases {
				w.Plainlnf("  Phase %d:       %s (%d resolvers)",
					phase.Phase, phase.Duration, len(phase.Resolvers))
			}
		}
	}

	if len(snapshot.Parameters) > 0 {
		w.Plainlnf("\nParameters:      %d", len(snapshot.Parameters))
		if opts.Verbose {
			for key, value := range snapshot.Parameters {
				w.Plainlnf("  %s: %v", key, value)
			}
		}
	}

	return nil
}

func showResolvers(snapshot *resolver.Snapshot, opts *ShowOptions, w *writer.Writer) error {
	// Count non-nil resolvers for accurate header
	var count int
	for _, res := range snapshot.Resolvers {
		if res != nil {
			count++
		}
	}

	w.Plainlnf("Resolvers (%d)", count)
	w.Plainln("=============\n")

	for name, res := range snapshot.Resolvers {
		if res == nil {
			continue
		}
		var statusIcon string
		switch res.Status {
		case "failed":
			statusIcon = "✗"
		case "skipped":
			statusIcon = "○"
		default:
			statusIcon = "✓"
		}

		w.Plainlnf("%s %s", statusIcon, name)
		w.Plainlnf("  Status:        %s", res.Status)
		w.Plainlnf("  Phase:         %d", res.Phase)
		w.Plainlnf("  Duration:      %s", res.Duration)
		w.Plainlnf("  Provider Calls: %d", res.ProviderCalls)

		if opts.Verbose {
			w.Plainlnf("  Value:         %v", res.Value)
			if res.ValueSizeBytes > 0 {
				w.Plainlnf("  Value Size:    %d bytes", res.ValueSizeBytes)
			}
			if res.Sensitive {
				w.Plainln("  Sensitive:     yes")
			}
		}

		if res.Error != "" {
			w.Plainlnf("  Error:         %s", res.Error)
		}

		if len(res.FailedAttempts) > 0 {
			w.Plainlnf("  Failed Attempts: %d", len(res.FailedAttempts))
			if opts.Verbose {
				for i, attempt := range res.FailedAttempts {
					w.Plainlnf("    %d. %s: %s", i+1, attempt.Provider, attempt.Error)
				}
			}
		}

		w.Plainln("")
	}

	return nil
}
