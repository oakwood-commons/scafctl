// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solution

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/soldiff"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	addedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")).Bold(true)
	removedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Bold(true)
	changedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFF00"))
	headerStyle  = lipgloss.NewStyle().Bold(true)
	summaryStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FFFF"))
)

// DiffOptions holds options for the solution diff command.
type DiffOptions struct {
	FileA  string
	FileB  string
	Output string
}

// CommandDiff creates the solution diff subcommand.
func CommandDiff(_ *settings.Run, ioStreams terminal.IOStreams, binaryName string) *cobra.Command {
	opts := &DiffOptions{}

	cmd := &cobra.Command{
		Use:          "diff [solution-a] [solution-b]",
		Short:        "Compare two solution files structurally",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Compare two solution files and show structural differences.

			Unlike text-based diff, this understands the solution schema and
			reports meaningful changes to metadata, resolvers, actions,
			and test cases.

			This is useful for:
			  - Code review: See structural impact of YAML changes
			  - Configuration drift: Detect when a solution has drifted
			  - Refactoring: Confirm no accidental additions or removals
			  - Version comparison: Document what changed between releases

			Supported output formats:
			  - table: Human-readable table view (default)
			  - json: Machine-readable JSON
			  - yaml: Machine-readable YAML
		`),
		Example: heredoc.Docf(`
			# Compare two solutions
			$ %[1]s solution diff solution-v1.yaml solution-v2.yaml

			# Output as JSON
			$ %[1]s solution diff solution-v1.yaml solution-v2.yaml -o json

			# Output as YAML
			$ %[1]s solution diff solution-v1.yaml solution-v2.yaml -o yaml
		`, binaryName),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.FileA = args[0]
			opts.FileB = args[1]
			return runDiff(cmd.Context(), opts, ioStreams)
		},
	}

	cmd.Flags().StringVarP(&opts.Output, "output", "o", "table", "Output format: table, json, yaml")

	return cmd
}

func runDiff(ctx context.Context, opts *DiffOptions, ioStreams terminal.IOStreams) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	lgr.V(-1).Info("comparing solutions", "fileA", opts.FileA, "fileB", opts.FileB)

	result, err := soldiff.CompareFiles(ctx, opts.FileA, opts.FileB)
	if err != nil {
		if w != nil {
			w.Errorf("%v", err)
		}
		return exitcode.WithCode(fmt.Errorf("diff failed: %w", err), exitcode.FileNotFound)
	}

	switch opts.Output {
	case "json":
		enc := json.NewEncoder(ioStreams.Out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return exitcode.WithCode(fmt.Errorf("failed to encode JSON: %w", err), exitcode.GeneralError)
		}

	case "yaml":
		enc := yaml.NewEncoder(ioStreams.Out)
		enc.SetIndent(2)
		if err := enc.Encode(result); err != nil {
			return exitcode.WithCode(fmt.Errorf("failed to encode YAML: %w", err), exitcode.GeneralError)
		}

	case "table":
		noColor := false
		if w != nil {
			noColor = w.CliParams().NoColor
		}
		formatHuman(ioStreams, result, noColor)

	default:
		err := fmt.Errorf("unsupported output format: %s (supported: table, json, yaml)", opts.Output)
		if w != nil {
			w.Errorf("%v", err)
		}
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	return nil
}

func formatHuman(ioStreams terminal.IOStreams, result *soldiff.Result, noColor bool) {
	out := ioStreams.Out

	render := func(s lipgloss.Style, text string) string {
		if noColor {
			return text
		}
		return s.Render(text)
	}

	fmt.Fprintf(out, "%s\n\n", render(headerStyle, fmt.Sprintf("Solution Diff: %s ↔ %s", result.PathA, result.PathB)))

	if len(result.Changes) == 0 {
		fmt.Fprintln(out, "No structural differences found.")
		return
	}

	fmt.Fprintf(out, "%s\n", render(headerStyle, fmt.Sprintf("Changes (%d):", result.Summary.Total)))
	for _, c := range result.Changes {
		switch c.Type {
		case "added":
			fmt.Fprintf(out, "  %s %s\n", render(addedStyle, "+ added   "), c.Field)
		case "removed":
			fmt.Fprintf(out, "  %s %s\n", render(removedStyle, "- removed "), c.Field)
		case "changed":
			fmt.Fprintf(out, "  %s %s: %s → %s\n", render(changedStyle, "~ changed "), c.Field, render(removedStyle, fmt.Sprintf("%q", fmtValue(c.OldValue))), render(addedStyle, fmt.Sprintf("%q", fmtValue(c.NewValue))))
		}
	}

	summary := fmt.Sprintf("\nSummary: %d total | %d added | %d removed | %d changed",
		result.Summary.Total, result.Summary.Added, result.Summary.Removed, result.Summary.Changed)
	fmt.Fprintln(out, render(summaryStyle, summary))
}

func fmtValue(v any) string {
	if v == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%v", v)
}
