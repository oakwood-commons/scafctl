package snapshot

import (
	"context"
	"fmt"
	"os"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// DiffOptions holds options for the diff command
type DiffOptions struct {
	BeforeFile      string
	AfterFile       string
	Format          string
	IgnoreUnchanged bool
	IgnoreFields    []string
	Output          string
}

// CommandDiff creates the snapshot diff command
func CommandDiff(_ *settings.Run, ioStreams terminal.IOStreams, binaryName string) *cobra.Command {
	opts := &DiffOptions{}

	cmd := &cobra.Command{
		Use:   "diff [before-snapshot] [after-snapshot]",
		Short: "Compare two snapshots",
		Long: heredoc.Doc(`
			Compare two resolver execution snapshots and show differences.
			
			This is useful for:
			  - Debugging: See what changed between executions
			  - Testing: Validate resolver behavior with golden files
			  - CI/CD: Detect configuration drift
			  - Development: Verify changes don't affect other resolvers
			
			Supported output formats:
			  - human: Human-readable diff with sections (default)
			  - json: Machine-readable JSON format
			  - unified: Git-style unified diff format
		`),
		Example: heredoc.Docf(`
			# Compare two snapshots
			$ %s snapshot diff before.json after.json
			
			# Show only changed resolvers
			$ %s snapshot diff before.json after.json --ignore-unchanged
			
			# Ignore timing fields
			$ %s snapshot diff before.json after.json --ignore-fields duration,providerCalls
			
			# Output as JSON
			$ %s snapshot diff before.json after.json --format json
			
			# Output unified diff format
			$ %s snapshot diff before.json after.json --format unified
			
			# Save diff to file
			$ %s snapshot diff before.json after.json --output diff.txt
		`, binaryName, binaryName, binaryName, binaryName, binaryName, binaryName),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.BeforeFile = args[0]
			opts.AfterFile = args[1]
			return runDiff(cmd.Context(), opts, ioStreams)
		},
	}

	cmd.Flags().StringVarP(&opts.Format, "format", "f", "human", "Output format: human, json, unified")
	cmd.Flags().BoolVar(&opts.IgnoreUnchanged, "ignore-unchanged", false, "Omit unchanged resolvers from output")
	cmd.Flags().StringSliceVar(&opts.IgnoreFields, "ignore-fields", []string{}, "Fields to ignore (e.g., duration,providerCalls)")
	cmd.Flags().StringVarP(&opts.Output, "output", "o", "", "Write output to file instead of stdout")

	return cmd
}

func runDiff(ctx context.Context, opts *DiffOptions, ioStreams terminal.IOStreams) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Helper to write error
	writeErr := func(err error) {
		if w != nil {
			w.Errorf("%v", err)
		}
	}

	// Load before snapshot
	lgr.V(-1).Info("loading before snapshot", "file", opts.BeforeFile)
	before, err := resolver.LoadSnapshot(opts.BeforeFile)
	if err != nil {
		err = fmt.Errorf("failed to load before snapshot: %w", err)
		writeErr(err)
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	// Load after snapshot
	lgr.V(-1).Info("loading after snapshot", "file", opts.AfterFile)
	after, err := resolver.LoadSnapshot(opts.AfterFile)
	if err != nil {
		err = fmt.Errorf("failed to load after snapshot: %w", err)
		writeErr(err)
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	// Prepare diff options
	diffOpts := &resolver.DiffOptions{
		IgnoreUnchanged: opts.IgnoreUnchanged,
		IgnoreFields:    opts.IgnoreFields,
	}

	// Compute diff
	lgr.V(-1).Info("computing diff")
	diff := resolver.DiffSnapshotsWithOptions(before, after, diffOpts)

	// Determine output writer
	out := ioStreams.Out
	if opts.Output != "" {
		file, err := os.Create(opts.Output)
		if err != nil {
			err = fmt.Errorf("failed to create output file: %w", err)
			writeErr(err)
			return exitcode.WithCode(err, exitcode.GeneralError)
		}
		defer file.Close()
		out = file
	}

	// Format and output diff
	var output string
	switch opts.Format {
	case "human":
		output = resolver.FormatDiffHuman(diff)

	case "json":
		jsonOutput, err := resolver.FormatDiffJSON(diff)
		if err != nil {
			err = fmt.Errorf("failed to format JSON: %w", err)
			writeErr(err)
			return exitcode.WithCode(err, exitcode.GeneralError)
		}
		output = jsonOutput

	case "unified":
		output = resolver.FormatDiffUnified(diff)

	default:
		err := fmt.Errorf("unsupported format: %s (supported: human, json, unified)", opts.Format)
		writeErr(err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	if _, err := fmt.Fprint(out, output); err != nil {
		err = fmt.Errorf("failed to write output: %w", err)
		writeErr(err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	// Print summary to stderr if output is redirected
	if opts.Output != "" {
		fmt.Fprintf(ioStreams.ErrOut, "Diff saved to %s\n", opts.Output)
		fmt.Fprintf(ioStreams.ErrOut, "Total: %d | Added: %d | Removed: %d | Modified: %d | Unchanged: %d\n",
			diff.Summary.TotalResolvers,
			diff.Summary.Added,
			diff.Summary.Removed,
			diff.Summary.Modified,
			diff.Summary.Unchanged)
	}

	return nil
}
