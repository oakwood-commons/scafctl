// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmplfunction

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	gotmpldetail "github.com/oakwood-commons/scafctl/pkg/gotmpl/detail"
	gotmplext "github.com/oakwood-commons/scafctl/pkg/gotmpl/ext"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// Options holds configuration for the get go-template-functions command
type Options struct {
	IOStreams *terminal.IOStreams
	CliParams *settings.Run

	// kvx output integration
	flags.KvxOutputFlags

	// Filter options
	Custom bool // Show only custom (scafctl-specific) functions
	Sprig  bool // Show only sprig library functions

	// For dependency injection in tests
	allFn    func() gotmpl.ExtFunctionList
	customFn func() gotmpl.ExtFunctionList
	sprigFn  func() gotmpl.ExtFunctionList
}

// CommandGotmplFunction creates the 'get go-template-functions' subcommand
func CommandGotmplFunction(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	options := &Options{}

	cCmd := &cobra.Command{
		Use:     "go-template-functions",
		Aliases: []string{"gotmpl-funcs", "gotmpl", "gtf"},
		Short:   "List available Go template extension functions",
		Long: `List all available Go template extension functions, including sprig
library functions and custom scafctl-specific functions.

By default, lists all functions. Use --custom or --sprig to filter.

OUTPUT FORMATS:
  table     Table view with key information (default)
  json      Full function information as JSON
  yaml      Full function information as YAML
  quiet     Function names only (one per line)

Examples:
  # List all Go template functions
  scafctl get go-template-functions

  # List only custom scafctl functions
  scafctl get go-template-functions --custom

  # List only sprig library functions
  scafctl get go-template-functions --sprig

  # Output as JSON
  scafctl get go-template-functions -o json

  # Get details about a specific function
  scafctl get go-template-functions toHcl

  # Browse interactively
  scafctl get go-template-functions -i`,
		RunE: func(cCmd *cobra.Command, args []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Use)
			ctx := settings.IntoContext(cCmd.Context(), cliParams)

			options.IOStreams = ioStreams
			options.CliParams = cliParams

			if len(args) > 0 {
				return options.RunGetFunction(ctx, args[0])
			}
			return options.RunListFunctions(ctx)
		},
		SilenceUsage: true,
	}

	// Add output flags
	validFormats := []string{"table", "json", "yaml", "quiet"}
	cCmd.Flags().StringVarP(&options.Output, "output", "o", "",
		fmt.Sprintf("Output format: %s", strings.Join(validFormats, ", ")))
	cCmd.Flags().BoolVarP(&options.Interactive, "interactive", "i", false,
		"Launch interactive TUI for browsing functions")
	cCmd.Flags().StringVarP(&options.Expression, "expression", "e", "",
		"CEL expression to filter/transform output data")

	// Filter flags
	cCmd.Flags().BoolVar(&options.Custom, "custom", false, "Show only custom scafctl functions")
	cCmd.Flags().BoolVar(&options.Sprig, "sprig", false, "Show only sprig library functions")

	return cCmd
}

// getFunctions returns the appropriate function list based on flags
func (o *Options) getFunctions() gotmpl.ExtFunctionList {
	allFn := gotmplext.All
	customFn := gotmplext.Custom
	sprigFn := gotmplext.Sprig

	// Allow test injection
	if o.allFn != nil {
		allFn = o.allFn
	}
	if o.customFn != nil {
		customFn = o.customFn
	}
	if o.sprigFn != nil {
		sprigFn = o.sprigFn
	}

	switch {
	case o.Custom:
		return customFn()
	case o.Sprig:
		return sprigFn()
	default:
		return allFn()
	}
}

// RunListFunctions lists all Go template extension functions
func (o *Options) RunListFunctions(ctx context.Context) error {
	funcs := o.getFunctions()

	// Interactive mode
	if o.Interactive {
		if !kvx.IsTerminal(o.IOStreams.Out) {
			err := fmt.Errorf("interactive mode requires a terminal")
			if w := writer.FromContext(ctx); w != nil {
				w.Errorf("%v", err)
			}
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
	}

	// Default (no -o flag): simple list
	if o.Output == "" && !o.Interactive {
		return o.printSimpleList(ctx, funcs)
	}

	// Build output data
	output := gotmpldetail.BuildFunctionList(funcs)

	return o.writeOutput(ctx, output)
}

// RunGetFunction gets details about a specific function
func (o *Options) RunGetFunction(ctx context.Context, name string) error {
	funcs := o.getFunctions()

	// Find the function by name (case-insensitive)
	var found *gotmpl.ExtFunction
	for i := range funcs {
		if strings.EqualFold(funcs[i].Name, name) {
			found = &funcs[i]
			break
		}
	}

	if found == nil {
		err := fmt.Errorf("go template function %q not found", name)
		if w := writer.FromContext(ctx); w != nil {
			w.Errorf("%v", err)
		}
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	// Default: custom formatted view
	if o.Output == "" && !o.Interactive {
		return o.printFunctionDetail(ctx, found)
	}

	output := gotmpldetail.BuildFunctionDetail(found)
	return o.writeOutput(ctx, output)
}

// printSimpleList prints a simple list of function names and descriptions
func (o *Options) printSimpleList(ctx context.Context, funcs gotmpl.ExtFunctionList) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return nil
	}
	noColor := w.NoColor()

	for _, fn := range funcs {
		name := fn.Name
		if !noColor {
			if fn.Custom {
				name = "\033[1;32m" + name + "\033[0m" // Bold green for custom
			} else {
				name = "\033[1;94m" + name + "\033[0m" // Bold blue for sprig
			}
		}

		desc := fn.Description
		if len(desc) > 80 {
			desc = desc[:77] + "..."
		}
		w.Plainlnf("  %s", name)
		if desc != "" {
			dimDesc := desc
			if !noColor {
				dimDesc = "\033[2m" + desc + "\033[0m"
			}
			w.Plainlnf("    %s", dimDesc)
		}
	}
	return nil
}

// printFunctionDetail prints a nicely formatted function detail view
func (o *Options) printFunctionDetail(ctx context.Context, fn *gotmpl.ExtFunction) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return nil
	}
	noColor := w.NoColor()

	keyStyle := func(s string) string {
		if noColor {
			return s
		}
		return "\033[1;94m" + s + "\033[0m"
	}
	dimStyle := func(s string) string {
		if noColor {
			return s
		}
		return "\033[2m" + s + "\033[0m"
	}
	tagStyle := func(s string) string {
		if noColor {
			return "[" + s + "]"
		}
		return "\033[38;5;85;48;5;235m " + s + " \033[0m"
	}

	// Name and type
	w.Plainf("%s %s", keyStyle("Name:"), fn.Name)
	if fn.Custom {
		w.Plainf(" %s", tagStyle("custom"))
	} else {
		w.Plainf(" %s", tagStyle("sprig"))
	}
	w.Plainln("")

	// Description
	if fn.Description != "" {
		w.Plainln("")
		w.Plainln(keyStyle("Description:"))
		w.Plainlnf("  %s", fn.Description)
	}

	// Examples
	if len(fn.Examples) > 0 {
		w.Plainln("")
		w.Plainln(keyStyle("Examples:"))
		for _, ex := range fn.Examples {
			if ex.Description != "" {
				w.Plainlnf("  %s", dimStyle(ex.Description))
			}
			if ex.Template != "" {
				w.Plainlnf("    %s", ex.Template)
			}
		}
	}

	// Links
	if len(fn.Links) > 0 {
		w.Plainln("")
		w.Plainln(keyStyle("Links:"))
		for _, link := range fn.Links {
			w.Plainlnf("  %s", link)
		}
	}

	return nil
}

// writeOutput writes the output using kvx
func (o *Options) writeOutput(ctx context.Context, data any) error {
	if o.Output == "quiet" {
		return o.writeQuietOutput(ctx, data)
	}

	kvxOpts := flags.NewKvxOutputOptionsFromFlags(
		o.Output,
		o.Interactive,
		o.Expression,
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(o.CliParams.NoColor),
		kvx.WithOutputAppName("scafctl get go-template-functions"),
		kvx.WithOutputHelp("scafctl get go-template-functions", []string{
			"Go Template Extension Functions Viewer",
			"",
			"Navigate: ↑↓ arrows | Back: ← | Enter: →",
			"Search: / or F3 | Expression: F6",
			"Copy path: F5 | Quit: q or F10",
		}),
	)
	kvxOpts.IOStreams = o.IOStreams

	return kvxOpts.Write(data)
}

// writeQuietOutput prints just the function names
func (o *Options) writeQuietOutput(ctx context.Context, data any) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return nil
	}
	switch v := data.(type) {
	case []map[string]any:
		for _, item := range v {
			if name, ok := item["name"].(string); ok {
				w.Plainln(name)
			}
		}
	case map[string]any:
		if name, ok := v["name"].(string); ok {
			w.Plainln(name)
		}
	}
	return nil
}
