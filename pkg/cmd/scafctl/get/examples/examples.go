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

	outputOpts := flags.ToKvxOutputOptions(&o.KvxOutputFlags,
		kvx.WithIOStreams(o.IOStreams),
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(o.CliParams.NoColor),
		kvx.WithOutputAppName(o.CliParams.BinaryName+" get examples"),
		kvx.WithOutputDisplaySchemaJSON(examplesSchemaJSON),
		kvx.WithOutputColumnOrder([]string{"displayName", "name", "category", "path", "tags", "description"}),
		kvx.WithOutputColumnHints(map[string]tui.ColumnHint{
			"displayName": {MaxWidth: 28, Priority: 10},
			"name":        {MaxWidth: 24, Priority: 8},
			"category":    {MaxWidth: 16, Priority: 7},
			// Path is the copy/paste handle for `get examples <path>` -- keep it
			// visible and wide enough that it does not truncate.
			"path":        {MaxWidth: 48, Priority: 9},
			"tags":        {MaxWidth: 20, Priority: 4},
			"description": {Priority: 3},
		}),
	)

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
	// Tell the user exactly how to view a solution (the path is the handle).
	if !structured && !quiet {
		bin := o.CliParams.BinaryName
		w.PlainStderrf("")
		w.PlainStderrf("Tip: run '%s get examples <path>' to view a solution (e.g. '%s get examples %s').",
			bin, bin, items[0].Path)
	}
	return nil
}

func (o *Options) runGet(ctx context.Context, query string) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	if o.Category != "" {
		return exitcode.WithCode(fmt.Errorf("--category can only be used in list mode"), exitcode.InvalidInput)
	}

	// Resolve the query (path, metadata.name, or basename) to a concrete path.
	exPath, err := exampleslib.ResolveExample(query)
	if err != nil {
		switch {
		case errors.Is(err, exampleslib.ErrPathTraversal):
			w.Errorf("Invalid example path: %s", query)
			return exitcode.WithCode(fmt.Errorf("invalid example path: %w", err), exitcode.InvalidInput)
		case errors.Is(err, exampleslib.ErrAmbiguousExample):
			w.Errorf("%v", err)
			w.PlainStderrf("Pass the full path to disambiguate, e.g. '%s get examples <path>'.", o.CliParams.BinaryName)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		case errors.Is(err, exampleslib.ErrExampleNotFound):
			w.Errorf("Example not found: %s", query)
			w.PlainStderrf("Run '%s get examples' to list available examples.", o.CliParams.BinaryName)
			return exitcode.WithCode(err, exitcode.FileNotFound)
		default:
			return err
		}
	}

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
