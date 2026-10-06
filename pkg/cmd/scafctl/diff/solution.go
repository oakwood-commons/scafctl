// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package diff

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/diffreport"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/soldiff"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// SolutionDiffOptions holds options for the diff solution command.
type SolutionDiffOptions struct {
	Files []string

	flags.KvxOutputFlags
}

// diffSource represents one of the two solutions to compare.
type diffSource struct {
	Value string // Path or catalog name
}

// CommandDiffSolution creates the `diff solution` subcommand.
func CommandDiffSolution(cliParams *settings.Run, ioStreams terminal.IOStreams, binaryName string) *cobra.Command {
	opts := &SolutionDiffOptions{}
	opts.AppName = cliParams.BinaryName

	cmd := &cobra.Command{
		Use:          subSolution + " [catalog-ref-a] [catalog-ref-b]",
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

			The default output is a human-readable diff report. Use -o json/yaml
			for the full structured result, or -o table/list/tree (and -i) for the
			flattened, filterable change list.
		`),
		Example: heredoc.Docf(`
			# Compare two local files
			$ %[1]s diff solution -f solution-v1.yaml -f solution-v2.yaml

			# Compare two catalog versions
			$ %[1]s diff solution my-app@1.0.0 my-app@2.0.0

			# Compare a local file with a catalog version
			$ %[1]s diff solution -f modified.yaml my-app@1.0.0

			# Output as JSON
			$ %[1]s diff solution -f solution-v1.yaml -f solution-v2.yaml -o json

			# Output as YAML
			$ %[1]s diff solution -f solution-v1.yaml -f solution-v2.yaml -o yaml
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
				if err := get.ValidatePositionalRef(arg, "", binaryName+" diff solution"); err != nil {
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

			return runSolutionDiff(cmd.Context(), opts, &ioStreams, binaryName, sources[0].Value, sources[1].Value)
		},
	}

	cmd.Flags().StringArrayVarP(&opts.Files, "file", "f", nil, "Local solution file path (repeatable, up to 2)")
	flags.AddKvxOutputFlagsToStruct(cmd, &opts.KvxOutputFlags)

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

	// Find the start of our args: skip past the "solution" subcommand token.
	// Under the polymorphic verb path (`scafctl diff solution ...`), the
	// "diff" token is the top-level verb, so we key off the actual
	// subcommand name to locate where the user's flags/args begin. Falling
	// back to "diff" here would point startIdx at "solution" and mis-parse.
	startIdx := 0
	for i, arg := range osArgs {
		if arg == subSolution {
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

func runSolutionDiff(ctx context.Context, opts *SolutionDiffOptions, ioStreams *terminal.IOStreams, binaryName, refA, refB string) error {
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

	return writeDiffOutput(ctx, w, ioStreams, &opts.KvxOutputFlags, binaryName+" diff solution", result, reportFromSolutionDiff(result))
}

// reportFromSolutionDiff maps a soldiff.Result into the shared diff report.
func reportFromSolutionDiff(result *soldiff.Result) *diffreport.Report {
	r := diffreport.New("solution", result.PathA, result.PathB)
	for _, c := range result.Changes {
		r.Add(diffreport.Entry{
			Path:   c.Field,
			Kind:   changeKindFromString(c.Type),
			Before: c.OldValue,
			After:  c.NewValue,
		})
	}
	return r
}

func changeKindFromString(t string) diffreport.ChangeKind {
	switch t {
	case "added":
		return diffreport.ChangeAdded
	case "removed":
		return diffreport.ChangeRemoved
	default: // "changed"
		return diffreport.ChangeModified
	}
}
