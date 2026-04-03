// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solution

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/soldiff"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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
	Files  []string
	Output string
}

// diffSource represents one of the two solutions to compare.
type diffSource struct {
	Value string // Path or catalog name
}

// CommandDiff creates the solution diff subcommand.
func CommandDiff(cliParams *settings.Run, ioStreams terminal.IOStreams, binaryName string) *cobra.Command {
	opts := &DiffOptions{}

	cmd := &cobra.Command{
		Use:          "diff [catalog-ref-a] [catalog-ref-b]",
		Short:        "Compare two solution files structurally",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Compare two solution files and show structural differences.

			Unlike text-based diff, this understands the solution schema and
			reports meaningful changes to metadata, resolvers, actions,
			and test cases.

			Solutions can be specified using:
			  - -f/--file for local file paths (repeatable, up to 2)
			  - Positional arguments for catalog names, remote registry refs, and URLs

			These can be combined in any order. The first source on the command
			line becomes solution A, the second becomes solution B.

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
			# Compare two local files
			$ %[1]s solution diff -f solution-v1.yaml -f solution-v2.yaml

			# Compare two catalog versions
			$ %[1]s solution diff my-app@1.0.0 my-app@2.0.0

			# Compare a local file with a catalog version
			$ %[1]s solution diff -f modified.yaml my-app@1.0.0

			# Output as JSON
			$ %[1]s solution diff -f solution-v1.yaml -f solution-v2.yaml -o json

			# Output as YAML
			$ %[1]s solution diff -f solution-v1.yaml -f solution-v2.yaml -o yaml
		`, binaryName),
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Ensure Writer is available in the context for output methods.
			ctx := cmd.Context()
			if writer.FromContext(ctx) == nil {
				ctx = writer.WithWriter(ctx, writer.New(&ioStreams, cliParams))
				cmd.SetContext(ctx)
			}

			w := writer.FromContext(ctx)

			// Validate positional args are catalog references
			for _, arg := range args {
				if err := get.ValidatePositionalRef(arg, "", "scafctl solution diff"); err != nil {
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.InvalidInput)
				}
			}

			totalSources := len(opts.Files) + len(args)
			if totalSources != 2 {
				err := fmt.Errorf("exactly 2 sources required (got %d); use -f for local files and positional args for catalog/registry refs", totalSources)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			// Resolve slot ordering by walking os.Args to preserve the
			// user's intended A→B ordering when mixing -f and positional args.
			sources := resolveDiffSlotOrder(os.Args, cmd.Flags(), opts.Files, args)

			return runDiff(cmd.Context(), sources[0].Value, sources[1].Value, opts.Output)
		},
	}

	cmd.Flags().StringArrayVarP(&opts.Files, "file", "f", nil, "Local solution file path (repeatable, up to 2)")
	cmd.Flags().StringVarP(&opts.Output, "output", "o", "table", "Output format: table, json, yaml")

	return cmd
}

// resolveDiffSlotOrder determines the declaration order of -f flags and
// positional args, producing an ordered [2]diffSource slice that maps to
// solution A and B respectively. It uses the FlagSet to auto-discover
// value-bearing flags, avoiding a hard-coded maintenance list.
//
// Falls back to files-first ordering when osArgs cannot be parsed
// (e.g., in unit tests using cmd.SetArgs).
func resolveDiffSlotOrder(osArgs []string, flags *pflag.FlagSet, flagFiles, positionalArgs []string) []diffSource {
	// Fast path: no mixing — ordering is trivial.
	if len(flagFiles) == 2 {
		return []diffSource{{Value: flagFiles[0]}, {Value: flagFiles[1]}}
	}
	if len(positionalArgs) == 2 {
		return []diffSource{{Value: positionalArgs[0]}, {Value: positionalArgs[1]}}
	}

	// Mixed mode: walk osArgs to determine declaration order.
	sources := make([]diffSource, 0, len(flagFiles)+len(positionalArgs))
	fileIdx, posIdx := 0, 0

	// Find the start of our args: skip past "diff" subcommand token.
	startIdx := 0
	for i, arg := range osArgs {
		if arg == "diff" {
			startIdx = i + 1
			break
		}
	}

	for i := startIdx; i < len(osArgs); i++ {
		arg := osArgs[i]
		switch {
		case (arg == "-f" || arg == "--file") && i+1 < len(osArgs):
			if fileIdx < len(flagFiles) {
				sources = append(sources, diffSource{Value: flagFiles[fileIdx]})
				fileIdx++
			}
			i++ // skip the value token
		case strings.HasPrefix(arg, "-f=") || strings.HasPrefix(arg, "--file="):
			if fileIdx < len(flagFiles) {
				sources = append(sources, diffSource{Value: flagFiles[fileIdx]})
				fileIdx++
			}
		case strings.HasPrefix(arg, "-"):
			// Skip non-file flags. Use the FlagSet to determine whether
			// the flag consumes a value token (NoOptDefVal == "").
			name := strings.TrimLeft(arg, "-")
			if strings.ContainsRune(name, '=') {
				break // value is inline, nothing to skip
			}
			// Use ShorthandLookup for single-character names (e.g. "-o")
			// because Lookup("o") resolves long names only and returns nil
			// for shorthands, causing their value token to be misread as a
			// positional argument.
			var f *pflag.Flag
			if len(name) == 1 {
				f = flags.ShorthandLookup(name)
			} else {
				f = flags.Lookup(name)
			}
			if f != nil && f.NoOptDefVal == "" {
				i++ // flag takes a value, skip the next token
			}
		default:
			// Positional arg
			if posIdx < len(positionalArgs) && arg == positionalArgs[posIdx] {
				sources = append(sources, diffSource{Value: positionalArgs[posIdx]})
				posIdx++
			}
		}
	}

	if len(sources) == 2 {
		return sources
	}

	return filesThenPositional(flagFiles, positionalArgs)
}

// filesThenPositional returns sources ordered files-first, then positional args.
func filesThenPositional(flagFiles, positionalArgs []string) []diffSource {
	sources := make([]diffSource, 0, len(flagFiles)+len(positionalArgs))
	for _, f := range flagFiles {
		sources = append(sources, diffSource{Value: f})
	}
	for _, p := range positionalArgs {
		sources = append(sources, diffSource{Value: p})
	}
	return sources
}

func runDiff(ctx context.Context, refA, refB, outputFmt string) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	lgr.V(-1).Info("comparing solutions", "refA", refA, "refB", refB)

	result, err := soldiff.CompareFiles(ctx, refA, refB)
	if err != nil {
		if w != nil {
			w.Errorf("%v", err)
		}
		return exitcode.WithCode(fmt.Errorf("diff failed: %w", err), exitcode.FileNotFound)
	}

	switch outputFmt {
	case "json":
		out := w.IOStreams().Out
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return exitcode.WithCode(fmt.Errorf("failed to encode JSON: %w", err), exitcode.GeneralError)
		}

	case "yaml":
		out := w.IOStreams().Out
		enc := yaml.NewEncoder(out)
		enc.SetIndent(2)
		if err := enc.Encode(result); err != nil {
			return exitcode.WithCode(fmt.Errorf("failed to encode YAML: %w", err), exitcode.GeneralError)
		}

	case "table":
		formatHuman(w, result)

	default:
		err := fmt.Errorf("unsupported output format: %s (supported: table, json, yaml)", outputFmt)
		if w != nil {
			w.Errorf("%v", err)
		}
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	return nil
}

func formatHuman(w *writer.Writer, result *soldiff.Result) {
	render := func(s lipgloss.Style, text string) string {
		if w.NoColor() {
			return text
		}
		return s.Render(text)
	}

	w.Plainlnf("%s", render(headerStyle, fmt.Sprintf("Solution Diff: %s ↔ %s", result.PathA, result.PathB)))
	w.Plainln("")

	if len(result.Changes) == 0 {
		w.Plainln("No structural differences found.")
		return
	}

	w.Plainlnf("%s", render(headerStyle, fmt.Sprintf("Changes (%d):", result.Summary.Total)))
	for _, c := range result.Changes {
		switch c.Type {
		case "added":
			w.Plainlnf("  %s %s", render(addedStyle, "+ added   "), c.Field)
		case "removed":
			w.Plainlnf("  %s %s", render(removedStyle, "- removed "), c.Field)
		case "changed":
			w.Plainlnf("  %s %s: %s → %s", render(changedStyle, "~ changed "), c.Field, render(removedStyle, fmt.Sprintf("%q", fmtValue(c.OldValue))), render(addedStyle, fmt.Sprintf("%q", fmtValue(c.NewValue))))
		}
	}

	summary := fmt.Sprintf("\nSummary: %d total | %d added | %d removed | %d changed",
		result.Summary.Total, result.Summary.Added, result.Summary.Removed, result.Summary.Changed)
	w.Plainln(render(summaryStyle, summary))
}

func fmtValue(v any) string {
	if v == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%v", v)
}
