// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package snapshot

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// ShowOptions holds options for the show command
type ShowOptions struct {
	BinaryName   string
	SnapshotFile string
	Format       string
	Verbose      bool
}

// CommandShow creates the snapshot show command
func CommandShow(cliParams *settings.Run, ioStreams terminal.IOStreams, binaryName string) *cobra.Command {
	opts := &ShowOptions{}

	cmd := &cobra.Command{
		Use:          "show [snapshot-file]",
		Short:        "Display snapshot contents",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Load and display the contents of a snapshot file.
			
			Supports multiple output formats:
			  - summary: High-level overview (default)
			  - json: Full JSON output
			  - resolvers: List of all resolvers with status
		`),
		Example: heredoc.Docf(`
			# Show snapshot summary
			$ %s snapshot show snapshot.json
			
			# Show full JSON
			$ %s snapshot show snapshot.json --format json
			
			# Show resolver details
			$ %s snapshot show snapshot.json --format resolvers --verbose
		`, binaryName, binaryName, binaryName),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.SnapshotFile = args[0]
			opts.BinaryName = binaryName
			opts.Verbose = cliParams.Verbose
			return runShow(cmd.Context(), opts, ioStreams)
		},
	}

	cmd.Flags().StringVarP(&opts.Format, "format", "f", "summary", "Output format: summary, json, resolvers")

	return cmd
}

func runShow(ctx context.Context, opts *ShowOptions, ioStreams terminal.IOStreams) error {
	if opts.BinaryName == "" {
		opts.BinaryName = settings.CliBinaryName
	}

	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Create a fallback Writer if one isn't in context (e.g., in tests)
	if w == nil {
		// Ensure ErrOut is non-nil to avoid panics in error paths
		streams := &ioStreams
		if streams.ErrOut == nil {
			streams.ErrOut = streams.Out
		}
		w = writer.New(streams, settings.NewCliParams())
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

	switch opts.Format {
	case "summary":
		return showSummary(snapshot, opts, w)

	case "json":
		encoder := json.NewEncoder(ioStreams.Out)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(snapshot); err != nil {
			err = fmt.Errorf("failed to encode JSON: %w", err)
			writeErr(err)
			return exitcode.WithCode(err, exitcode.GeneralError)
		}

	case "resolvers":
		return showResolvers(snapshot, opts, w)

	default:
		err := fmt.Errorf("unsupported format: %s (supported: summary, json, resolvers)", opts.Format)
		writeErr(err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	return nil
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
	w.Plainlnf("%s Version: %s", opts.BinaryName, snapshot.Metadata.ScafctlVersion)
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
