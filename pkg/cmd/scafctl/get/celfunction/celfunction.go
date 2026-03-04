// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celfunction

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	celdetail "github.com/oakwood-commons/scafctl/pkg/celexp/detail"
	"github.com/oakwood-commons/scafctl/pkg/celexp/ext"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// Options holds configuration for the get cel-functions command
type Options struct {
	IOStreams *terminal.IOStreams
	CliParams *settings.Run

	// kvx output integration
	flags.KvxOutputFlags

	// Filter options
	Custom  bool // Show only custom (scafctl-specific) functions
	BuiltIn bool // Show only built-in (cel-go) functions

	// For dependency injection in tests
	allFn     func() celexp.ExtFunctionList
	customFn  func() celexp.ExtFunctionList
	builtInFn func() celexp.ExtFunctionList
}

// CommandCelFunction creates the 'get cel-functions' subcommand
func CommandCelFunction(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	options := &Options{}

	cCmd := &cobra.Command{
		Use:     "cel-functions",
		Aliases: []string{"cel-funcs", "cel", "cf"},
		Short:   "List available CEL extension functions",
		Long: `List all available CEL extension functions, including built-in cel-go
extensions and custom scafctl-specific functions.

By default, lists all functions. Use --custom or --builtin to filter.

OUTPUT FORMATS:
  table     Table view with key information (default)
  json      Full function information as JSON
  yaml      Full function information as YAML
  quiet     Function names only (one per line)

Examples:
  # List all CEL functions
  scafctl get cel-functions

  # List only custom scafctl functions
  scafctl get cel-functions --custom

  # List only built-in cel-go functions
  scafctl get cel-functions --builtin

  # Output as JSON
  scafctl get cel-functions -o json

  # Get details about a specific function
  scafctl get cel-functions map.merge

  # Browse interactively
  scafctl get cel-functions -i`,
		RunE: func(cCmd *cobra.Command, args []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Use)
			ctx := settings.IntoContext(context.Background(), cliParams)

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
	cCmd.Flags().BoolVar(&options.BuiltIn, "builtin", false, "Show only built-in cel-go functions")

	return cCmd
}

// getFunctions returns the appropriate function list based on flags
func (o *Options) getFunctions() celexp.ExtFunctionList {
	allFn := ext.All
	customFn := ext.Custom
	builtInFn := ext.BuiltIn

	// Allow test injection
	if o.allFn != nil {
		allFn = o.allFn
	}
	if o.customFn != nil {
		customFn = o.customFn
	}
	if o.builtInFn != nil {
		builtInFn = o.builtInFn
	}

	switch {
	case o.Custom:
		return customFn()
	case o.BuiltIn:
		return builtInFn()
	default:
		return allFn()
	}
}

// RunListFunctions lists all CEL extension functions
func (o *Options) RunListFunctions(ctx context.Context) error {
	funcs := o.getFunctions()

	// Populate function names via CEL env introspection
	if err := ext.SetFunctionNames(funcs); err != nil {
		if w := writer.FromContext(ctx); w != nil {
			w.Warningf("could not resolve function names: %v", err)
		}
	}

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
		return o.printSimpleList(funcs)
	}

	// Build output data
	output := celdetail.BuildFunctionList(funcs)

	return o.writeOutput(ctx, output)
}

// RunGetFunction gets details about a specific function
func (o *Options) RunGetFunction(ctx context.Context, name string) error {
	funcs := o.getFunctions()

	// Populate function names
	if err := ext.SetFunctionNames(funcs); err != nil {
		if w := writer.FromContext(ctx); w != nil {
			w.Warningf("could not resolve function names: %v", err)
		}
	}

	// Find the function by name (case-insensitive)
	var found *celexp.ExtFunction
	for i := range funcs {
		if strings.EqualFold(funcs[i].Name, name) {
			found = &funcs[i]
			break
		}
	}

	if found == nil {
		err := fmt.Errorf("CEL function %q not found", name)
		if w := writer.FromContext(ctx); w != nil {
			w.Errorf("%v", err)
		}
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	// Default: custom formatted view
	if o.Output == "" && !o.Interactive {
		return o.printFunctionDetail(found)
	}

	output := celdetail.BuildFunctionDetail(found)
	return o.writeOutput(ctx, output)
}

// printSimpleList prints a simple list of function names and descriptions
func (o *Options) printSimpleList(funcs celexp.ExtFunctionList) error {
	out := o.IOStreams.Out
	noColor := o.CliParams.NoColor

	for _, fn := range funcs {
		name := fn.Name
		if !noColor {
			if fn.Custom {
				name = "\033[1;32m" + name + "\033[0m" // Bold green for custom
			} else {
				name = "\033[1;94m" + name + "\033[0m" // Bold blue for built-in
			}
		}

		desc := fn.Description
		if len(desc) > 80 {
			desc = desc[:77] + "..."
		}
		fmt.Fprintf(out, "  %s\n", name)
		if desc != "" {
			dimDesc := desc
			if !noColor {
				dimDesc = "\033[2m" + desc + "\033[0m"
			}
			fmt.Fprintf(out, "    %s\n", dimDesc)
		}
	}
	return nil
}

// printFunctionDetail prints a nicely formatted function detail view
func (o *Options) printFunctionDetail(fn *celexp.ExtFunction) error {
	out := o.IOStreams.Out
	noColor := o.CliParams.NoColor

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
	fmt.Fprintf(out, "%s %s", keyStyle("Name:"), fn.Name)
	if fn.Custom {
		fmt.Fprintf(out, " %s", tagStyle("custom"))
	} else {
		fmt.Fprintf(out, " %s", tagStyle("built-in"))
	}
	fmt.Fprintln(out)

	// Description
	if fn.Description != "" {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "%s\n", keyStyle("Description:"))
		fmt.Fprintf(out, "  %s\n", fn.Description)
	}

	// Function names
	if len(fn.FunctionNames) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "%s\n", keyStyle("Functions:"))
		for _, name := range fn.FunctionNames {
			fmt.Fprintf(out, "  %s\n", name)
		}
	}

	// Examples
	if len(fn.Examples) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "%s\n", keyStyle("Examples:"))
		for _, ex := range fn.Examples {
			if ex.Description != "" {
				fmt.Fprintf(out, "  %s\n", dimStyle(ex.Description))
			}
			if ex.Expression != "" {
				fmt.Fprintf(out, "    %s\n", ex.Expression)
			}
		}
	}

	// Links
	if len(fn.Links) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "%s\n", keyStyle("Links:"))
		for _, link := range fn.Links {
			fmt.Fprintf(out, "  %s\n", link)
		}
	}

	return nil
}

// writeOutput writes the output using kvx
func (o *Options) writeOutput(ctx context.Context, data any) error {
	if o.Output == "quiet" {
		return o.writeQuietOutput(data)
	}

	kvxOpts := flags.NewKvxOutputOptionsFromFlags(
		o.Output,
		o.Interactive,
		o.Expression,
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(o.CliParams.NoColor),
		kvx.WithOutputAppName("scafctl get cel-functions"),
		kvx.WithOutputHelp("scafctl get cel-functions", []string{
			"CEL Extension Functions Viewer",
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
func (o *Options) writeQuietOutput(data any) error {
	switch v := data.(type) {
	case []map[string]any:
		for _, item := range v {
			if name, ok := item["name"].(string); ok {
				fmt.Fprintln(o.IOStreams.Out, name)
			}
		}
	case map[string]any:
		if name, ok := v["name"].(string); ok {
			fmt.Fprintln(o.IOStreams.Out, name)
		}
	}
	return nil
}
