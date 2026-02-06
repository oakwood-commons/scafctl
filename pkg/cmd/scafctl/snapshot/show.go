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
	SnapshotFile string
	Format       string
	Verbose      bool
}

// CommandShow creates the snapshot show command
func CommandShow(_ *settings.Run, ioStreams terminal.IOStreams, binaryName string) *cobra.Command {
	opts := &ShowOptions{}

	cmd := &cobra.Command{
		Use:   "show [snapshot-file]",
		Short: "Display snapshot contents",
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
			return runShow(cmd.Context(), opts, ioStreams)
		},
	}

	cmd.Flags().StringVarP(&opts.Format, "format", "f", "summary", "Output format: summary, json, resolvers")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "Show detailed information")

	return cmd
}

func runShow(ctx context.Context, opts *ShowOptions, ioStreams terminal.IOStreams) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Helper to write error
	writeErr := func(err error) {
		if w != nil {
			w.Errorf("%v", err)
		}
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
		return showSummary(snapshot, opts, ioStreams)

	case "json":
		encoder := json.NewEncoder(ioStreams.Out)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(snapshot); err != nil {
			err = fmt.Errorf("failed to encode JSON: %w", err)
			writeErr(err)
			return exitcode.WithCode(err, exitcode.GeneralError)
		}

	case "resolvers":
		return showResolvers(snapshot, opts, ioStreams)

	default:
		err := fmt.Errorf("unsupported format: %s (supported: summary, json, resolvers)", opts.Format)
		writeErr(err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	return nil
}

func showSummary(snapshot *resolver.Snapshot, opts *ShowOptions, ioStreams terminal.IOStreams) error {
	out := ioStreams.Out

	fmt.Fprintf(out, "Snapshot Summary\n")
	fmt.Fprintf(out, "================\n\n")

	// Metadata
	fmt.Fprintf(out, "Solution:        %s (v%s)\n", snapshot.Metadata.Solution, snapshot.Metadata.Version)
	fmt.Fprintf(out, "Timestamp:       %s\n", snapshot.Metadata.Timestamp.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(out, "scafctl Version: %s\n", snapshot.Metadata.ScafctlVersion)
	fmt.Fprintf(out, "Total Duration:  %s\n", snapshot.Metadata.TotalDuration)
	fmt.Fprintf(out, "Overall Status:  %s\n\n", snapshot.Metadata.Status)

	// Count status
	var success, failed, skipped int
	for _, res := range snapshot.Resolvers {
		switch res.Status {
		case "success":
			success++
		case "failed":
			failed++
		case "skipped":
			skipped++
		}
	}

	fmt.Fprintf(out, "Resolvers:       %d total\n", len(snapshot.Resolvers))
	fmt.Fprintf(out, "  Success:       %d\n", success)
	fmt.Fprintf(out, "  Failed:        %d\n", failed)
	fmt.Fprintf(out, "  Skipped:       %d\n", skipped)

	if len(snapshot.Phases) > 0 {
		fmt.Fprintf(out, "\nPhases:          %d\n", len(snapshot.Phases))
		if opts.Verbose {
			for _, phase := range snapshot.Phases {
				fmt.Fprintf(out, "  Phase %d:       %s (%d resolvers)\n",
					phase.Phase, phase.Duration, len(phase.Resolvers))
			}
		}
	}

	if len(snapshot.Parameters) > 0 {
		fmt.Fprintf(out, "\nParameters:      %d\n", len(snapshot.Parameters))
		if opts.Verbose {
			for key, value := range snapshot.Parameters {
				fmt.Fprintf(out, "  %s: %v\n", key, value)
			}
		}
	}

	return nil
}

func showResolvers(snapshot *resolver.Snapshot, opts *ShowOptions, ioStreams terminal.IOStreams) error {
	out := ioStreams.Out

	fmt.Fprintf(out, "Resolvers (%d)\n", len(snapshot.Resolvers))
	fmt.Fprintf(out, "=============\n\n")

	for name, res := range snapshot.Resolvers {
		var statusIcon string
		switch res.Status {
		case "failed":
			statusIcon = "✗"
		case "skipped":
			statusIcon = "○"
		default:
			statusIcon = "✓"
		}

		fmt.Fprintf(out, "%s %s\n", statusIcon, name)
		fmt.Fprintf(out, "  Status:        %s\n", res.Status)
		fmt.Fprintf(out, "  Phase:         %d\n", res.Phase)
		fmt.Fprintf(out, "  Duration:      %s\n", res.Duration)
		fmt.Fprintf(out, "  Provider Calls: %d\n", res.ProviderCalls)

		if opts.Verbose {
			fmt.Fprintf(out, "  Value:         %v\n", res.Value)
			if res.ValueSizeBytes > 0 {
				fmt.Fprintf(out, "  Value Size:    %d bytes\n", res.ValueSizeBytes)
			}
			if res.Sensitive {
				fmt.Fprintf(out, "  Sensitive:     yes\n")
			}
		}

		if res.Error != "" {
			fmt.Fprintf(out, "  Error:         %s\n", res.Error)
		}

		if len(res.FailedAttempts) > 0 {
			fmt.Fprintf(out, "  Failed Attempts: %d\n", len(res.FailedAttempts))
			if opts.Verbose {
				for i, attempt := range res.FailedAttempts {
					fmt.Fprintf(out, "    %d. %s: %s\n", i+1, attempt.Provider, attempt.Error)
				}
			}
		}

		fmt.Fprintf(out, "\n")
	}

	return nil
}
