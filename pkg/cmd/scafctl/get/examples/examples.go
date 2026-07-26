// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package examples

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	exampleslib "github.com/oakwood-commons/scafctl/pkg/examples"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

//go:embed examples_schema.json
var examplesSchemaJSON []byte

// Options holds configuration for the get examples command.
type Options struct {
	IOStreams *terminal.IOStreams
	CliParams *settings.Run

	flags.KvxOutputFlags

	Category string
}

// CommandExamples creates the get examples command.
func CommandExamples(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &Options{}

	cmd := &cobra.Command{
		Use:     "examples [example-path]",
		Aliases: []string{"example"},
		Short:   "List examples or get a specific example",
		Long: strings.ReplaceAll(heredoc.Doc(`
			List embedded examples or show the contents of one example.

			Without arguments, lists available examples.
			With an example-path argument, prints that example to stdout.

			Examples:
			  # List all examples
			  scafctl get examples

			  # Filter list by category
			  scafctl get examples --category solutions

			  # Get one example by path
			  scafctl get examples resolver-demo.yaml
		`), settings.CliBinaryName, cliParams.BinaryName),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cmd.Use)
			ctx := settings.IntoContext(cmd.Context(), cliParams)

			if lgr := logger.FromContext(cmd.Context()); lgr != nil {
				ctx = logger.WithLogger(ctx, lgr)
			}

			w := writer.FromContext(cmd.Context())
			if w == nil {
				w = writer.New(ioStreams, cliParams)
			}
			ctx = writer.WithWriter(ctx, w)

			opts.IOStreams = ioStreams
			opts.CliParams = cliParams
			opts.AppName = cliParams.BinaryName

			if len(args) > 0 {
				return opts.runGet(ctx, args[0])
			}

			return opts.runList(ctx)
		},
		SilenceUsage: true,
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &opts.KvxOutputFlags)
	cmd.Flags().StringVar(&opts.Category, "category", "", "Filter by category (e.g., solutions, resolvers, actions)")

	return cmd
}

func (o *Options) runList(ctx context.Context) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	items, err := exampleslib.Scan(o.Category)
	if err != nil {
		return err
	}

	outputOpts := o.listOutputOpts(ctx)
	structured := kvx.IsStructuredFormat(outputOpts.Format)
	quiet := kvx.IsQuietFormat(outputOpts.Format)

	if len(items) == 0 {
		// Structured/quiet consumers must always receive a parseable, empty
		// document on stdout -- never human text. Emit [] (a non-nil empty
		// slice so JSON/YAML render "[]", not "null") and put any guidance on
		// stderr only.
		if structured {
			if writeErr := outputOpts.Write([]exampleslib.Example{}); writeErr != nil {
				return writeErr
			}
		}
		if !structured && !quiet {
			if o.Category != "" {
				w.WarnStderrf("No examples found in category %q.", o.Category)
				if cats := exampleslib.Categories(); len(cats) > 0 {
					w.PlainStderrf("Available categories: %s", strings.Join(cats, ", "))
				}
			} else {
				w.WarnStderrf("No examples available.")
			}
		}
		return nil
	}

	if err := outputOpts.Write(items); err != nil {
		return err
	}

	// Tip: the list shows metadata, but the content lives in the embedded FS.
	// Tell the user how to view a specific example.
	if !structured && !quiet {
		bin := o.CliParams.BinaryName
		w.PlainStderrf("")
		w.PlainStderrf("Tip: run '%s get examples <name>' to view an example (e.g. '%s get examples %s').",
			bin, bin, items[0].Name)
	}
	return nil
}

// listOutputOpts builds the kvx output options for the example listing (shared
// by the full list and the multi-match filtered list). The display schema drives
// the interactive (-i) card + detail view; the column hints tune the plain
// table. name and path are internal fetch handles, not list columns.
func (o *Options) listOutputOpts(ctx context.Context) *kvx.OutputOptions {
	return flags.ToKvxOutputOptions(&o.KvxOutputFlags,
		kvx.WithIOStreams(o.IOStreams),
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(o.CliParams.NoColor),
		kvx.WithOutputAppName(o.CliParams.BinaryName+" get examples"),
		kvx.WithOutputDisplaySchemaJSON(examplesSchemaJSON),
		kvx.WithOutputColumnOrder([]string{"name", "category", "description"}),
		kvx.WithOutputColumnHints(map[string]tui.ColumnHint{
			// `name` is the PRIMARY column: it is exactly what the user types in
			// `get examples <name>` (what you see is what you type, matching
			// `get provider`). Fixed columns get a MaxWidth so the table fits;
			// description is a flex column that absorbs the remaining terminal
			// width (MaxWidth is its minimum, not a cap).
			"name":        {MaxWidth: 34, Priority: 10},
			"category":    {MaxWidth: 14, Priority: 8},
			"description": {MaxWidth: 40, Priority: 6, Flex: true},
			// displayName/tags/path/content are not table columns; they appear in
			// the -i detail view and/or -o json/yaml. name/path are the fetch
			// handles; content powers the -i detail "Solution" section.
			"displayName": {Hidden: true},
			"tags":        {Hidden: true},
			"path":        {Hidden: true},
			"content":     {Hidden: true},
		}),
	)
}

func (o *Options) runGet(ctx context.Context, query string) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	if o.Category != "" {
		return exitcode.WithCode(fmt.Errorf("--category can only be used in list mode"), exitcode.InvalidInput)
	}

	// Resolve the query (exact path, metadata.name, or basename) to matching
	// example(s).
	matches, err := exampleslib.MatchExamples(query)
	if err != nil {
		switch {
		case errors.Is(err, exampleslib.ErrPathTraversal):
			w.Errorf("Invalid example path: %s", query)
			return exitcode.WithCode(fmt.Errorf("invalid example path: %w", err), exitcode.InvalidInput)
		case errors.Is(err, exampleslib.ErrExampleNotFound):
			w.Errorf("Example not found: %s", query)
			w.PlainStderrf("Run '%s get examples' to list available examples.", o.CliParams.BinaryName)
			return exitcode.WithCode(err, exitcode.FileNotFound)
		default:
			return err
		}
	}

	// More than one example matched the name/basename: show the matches as a
	// filtered list so the user can pick one, rather than guessing or erroring.
	if len(matches) > 1 {
		outputOpts := flags.ToKvxOutputOptions(&o.KvxOutputFlags,
			kvx.WithIOStreams(o.IOStreams),
			kvx.WithOutputContext(ctx),
			kvx.WithOutputNoColor(o.CliParams.NoColor),
			kvx.WithOutputAppName(o.CliParams.BinaryName+" get examples"),
			kvx.WithOutputDisplaySchemaJSON(examplesSchemaJSON),
			kvx.WithOutputColumnOrder([]string{"displayName", "category", "path"}),
			kvx.WithOutputColumnHints(map[string]tui.ColumnHint{
				"displayName": {MaxWidth: 32, Priority: 10},
				"category":    {MaxWidth: 16, Priority: 8},
				// Show path here so the user can disambiguate the matches.
				"path":        {MaxWidth: 48, Priority: 6},
				"name":        {Hidden: true},
				"tags":        {Hidden: true},
				"description": {Hidden: true},
				"content":     {Hidden: true},
			}),
		)
		if !kvx.IsStructuredFormat(outputOpts.Format) && !kvx.IsQuietFormat(outputOpts.Format) {
			w.WarnStderrf("%d examples match %q -- pick one by its path:", len(matches), query)
		}
		return outputOpts.Write(matches)
	}

	exPath := matches[0].Path

	content, err := exampleslib.Read(exPath)
	if err != nil {
		if errors.Is(err, exampleslib.ErrPathTraversal) {
			w.Errorf("Invalid example path: %s", exPath)
			return exitcode.WithCode(fmt.Errorf("invalid example path: %w", err), exitcode.InvalidInput)
		}
		w.Errorf("Example not found: %s", exPath)
		return exitcode.WithCode(fmt.Errorf("failed to read example: %w", err), exitcode.FileNotFound)
	}

	if o.FormatExplicit || o.Interactive {
		var parsed any
		if err := yaml.Unmarshal([]byte(content), &parsed); err != nil {
			return fmt.Errorf("failed to parse example as YAML: %w", err)
		}

		outputOpts := flags.ToKvxOutputOptions(
			&o.KvxOutputFlags,
			kvx.WithIOStreams(o.IOStreams),
			kvx.WithOutputContext(ctx),
			kvx.WithOutputNoColor(o.CliParams.NoColor),
			kvx.WithOutputAppName(o.CliParams.BinaryName+" get examples"),
		)
		return outputOpts.Write(parsed)
	}

	w.Plain(content)
	return nil
}
