// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celfunction

import (
	"context"
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/oakwood-commons/kvx/pkg/tui"
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

//go:embed celfunction_schema.json
var celFunctionSchemaJSON []byte

// FunctionSummary is the table-friendly output for function listing.
type FunctionSummary struct {
	Name        string `json:"name" yaml:"name" required:"true"`
	Description string `json:"description" yaml:"description"`
	Custom      bool   `json:"custom" yaml:"custom"`
}

// Options holds configuration for the get cel-functions command
type Options struct {
	BinaryName string
	IOStreams  *terminal.IOStreams
	CliParams  *settings.Run

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
			ctx := settings.IntoContext(cCmd.Context(), cliParams)

			options.IOStreams = ioStreams
			options.CliParams = cliParams
			options.BinaryName = cliParams.BinaryName

			if len(args) > 0 {
				return options.RunGetFunction(ctx, args[0])
			}
			return options.RunListFunctions(ctx)
		},
		SilenceUsage: true,
	}

	// Add kvx output flags (-o, -i, -e, -w)
	flags.AddKvxOutputFlagsToStruct(cCmd, &options.KvxOutputFlags)

	// Filter flags
	cCmd.Flags().BoolVar(&options.Custom, "custom", false, fmt.Sprintf("Show only custom %s functions", cliParams.BinaryName))
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
	if o.BinaryName == "" {
		o.BinaryName = settings.CliBinaryName
	}

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

	// Full data for json/yaml and interactive TUI
	if o.Output == "json" || o.Output == "yaml" || o.Interactive {
		output := celdetail.BuildFunctionList(funcs)
		return o.writeOutput(ctx, output)
	}

	// Table-friendly summary for auto/table/quiet
	summaries := make([]FunctionSummary, 0, len(funcs))
	for _, fn := range funcs {
		summaries = append(summaries, FunctionSummary{
			Name:        fn.Name,
			Description: fn.Description,
			Custom:      fn.Custom,
		})
	}
	return o.writeOutput(ctx, summaries)
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
	if (o.Output == "auto" || o.Output == "") && !o.Interactive {
		return o.printFunctionDetail(ctx, found)
	}

	output := celdetail.BuildFunctionDetail(found)
	return o.writeOutput(ctx, output)
}

// printFunctionDetail prints a nicely formatted function detail view
func (o *Options) printFunctionDetail(ctx context.Context, fn *celexp.ExtFunction) error {
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
		w.Plainf(" %s", tagStyle("built-in"))
	}
	w.Plainln("")

	// Description
	if fn.Description != "" {
		w.Plainln("")
		w.Plainln(keyStyle("Description:"))
		w.Plainlnf("  %s", fn.Description)
	}

	// Function names
	if len(fn.FunctionNames) > 0 {
		w.Plainln("")
		w.Plainln(keyStyle("Functions:"))
		for _, name := range fn.FunctionNames {
			w.Plainlnf("  %s", name)
		}
	}

	// Examples
	if len(fn.Examples) > 0 {
		w.Plainln("")
		w.Plainln(keyStyle("Examples:"))
		for _, ex := range fn.Examples {
			if ex.Description != "" {
				w.Plainlnf("  %s", dimStyle(ex.Description))
			}
			if ex.Expression != "" {
				w.Plainlnf("    %s", ex.Expression)
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
	kvxOpts := flags.ToKvxOutputOptions(&o.KvxOutputFlags,
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(o.CliParams.NoColor),
		kvx.WithOutputAppName(o.BinaryName+" get cel-functions"),
		kvx.WithIOStreams(o.IOStreams),
		kvx.WithOutputDisplaySchemaJSON(celFunctionSchemaJSON),
		kvx.WithOutputColumnOrder([]string{"name", "description"}),
		kvx.WithOutputColumnHints(map[string]tui.ColumnHint{
			"name":          {MaxWidth: 25, Priority: 10},
			"custom":        {Hidden: true},
			"functionNames": {Hidden: true},
			"links":         {Hidden: true},
			"examples":      {Hidden: true},
		}),
	)

	return kvxOpts.Write(data)
}
