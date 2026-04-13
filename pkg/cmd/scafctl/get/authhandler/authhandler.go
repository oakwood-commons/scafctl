// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package authhandler

import (
	"context"
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// authHandlerSchema is a JSON Schema that controls how the auth handler table is
// rendered. Column headers are renamed via "title", widths capped via "maxLength",
// and hidden columns via "deprecated": true.
//
// The column order is set separately via WithOutputColumnOrder because JSON Schema
// does not define ordering.
var authHandlerSchema = []byte(`{
	"type": "array",
	"items": {
		"type": "object",
		"required": ["name"],
		"properties": {
			"displayName": {
				"type": "string",
				"title": "Display Name",
				"maxLength": 35
			},
			"name": {
				"type": "string",
				"title": "Name",
				"maxLength": 20
			},
			"capabilities": {
				"type": "array",
				"title": "Capabilities"
			},
			"flows": {
				"type": "array",
				"title": "Flows"
			}
		}
	}
}`)

//go:embed authhandler_schema.json
var authHandlerDisplaySchemaJSON []byte

// HandlerSummary is the table-friendly output for auth handler listing.
type HandlerSummary struct {
	DisplayName  string `json:"displayName" yaml:"displayName"`
	Name         string `json:"name" yaml:"name" required:"true"`
	Capabilities string `json:"capabilities" yaml:"capabilities"`
	Flows        string `json:"flows" yaml:"flows"`
}

// Options holds configuration for the get authhandler command.
type Options struct {
	BinaryName string
	IOStreams  *terminal.IOStreams
	CliParams  *settings.Run

	flags.KvxOutputFlags
}

// CommandAuthHandler creates the get authhandler subcommand.
func CommandAuthHandler(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	options := &Options{}

	cCmd := &cobra.Command{
		Use:     "authhandler [name]",
		Aliases: []string{"authhandlers", "ah", "auth-handler", "auth-handlers", "handlers", "handler"},
		Short:   "List or get auth handler information",
		Long: strings.ReplaceAll(heredoc.Doc(`
			List all registered auth handlers or get details about a specific handler.

			Without arguments, lists all registered authentication handlers
			with their names, display names, supported flows, and capabilities.

			With a handler name argument, shows detailed information about that
			specific authentication handler.

			OUTPUT FORMATS:
			  (default) Table view with key information
			  json      Full handler information as JSON
			  yaml      Full handler information as YAML
			  quiet     Handler names only (one per line)

			Examples:
			  # List all auth handlers
			  scafctl get authhandlers

			  # List auth handlers as JSON
			  scafctl get authhandlers -o json

			  # Get details about a specific handler
			  scafctl get authhandler entra

			  # Get handler details as YAML
			  scafctl get authhandler github -o yaml
		`), settings.CliBinaryName, cliParams.BinaryName),
		RunE: func(cCmd *cobra.Command, args []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Use)
			ctx := settings.IntoContext(cCmd.Context(), cliParams)

			options.IOStreams = ioStreams
			options.CliParams = cliParams
			options.BinaryName = cliParams.BinaryName

			if len(args) > 0 {
				return options.RunGetHandler(ctx, args[0])
			}
			return options.RunListHandlers(ctx)
		},
		SilenceUsage: true,
	}

	flags.AddKvxOutputFlagsToStruct(cCmd, &options.KvxOutputFlags)
	return cCmd
}

// RunListHandlers lists all registered auth handlers.
func (o *Options) RunListHandlers(ctx context.Context) error {
	if o.BinaryName == "" {
		o.BinaryName = settings.CliBinaryName
	}

	handlerNames := auth.ListHandlers(ctx)
	if len(handlerNames) == 0 {
		err := fmt.Errorf("no auth handlers registered")
		if w := writer.FromContext(ctx); w != nil {
			w.Errorf("%v", err)
		}
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	useFullOutput := o.Output == "json" || o.Output == "yaml" || o.Interactive

	var results []map[string]any
	var summaries []HandlerSummary
	if useFullOutput {
		results = make([]map[string]any, 0, len(handlerNames))
	} else {
		summaries = make([]HandlerSummary, 0, len(handlerNames))
	}

	loaded := 0
	for _, name := range handlerNames {
		handler, err := auth.GetHandler(ctx, name)
		if err != nil {
			if w := writer.FromContext(ctx); w != nil {
				w.Warningf("Failed to load handler %s: %v", name, err)
			}
			continue
		}
		loaded++
		if useFullOutput {
			results = append(results, buildHandlerRow(handler))
		} else {
			summaries = append(summaries, buildHandlerSummary(handler))
		}
	}

	if loaded == 0 {
		err := fmt.Errorf("no auth handlers could be loaded")
		if w := writer.FromContext(ctx); w != nil {
			w.Errorf("%v", err)
		}
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	if useFullOutput {
		return o.writeOutput(ctx, results)
	}
	return o.writeOutput(ctx, summaries)
}

// RunGetHandler shows details for a single named auth handler.
func (o *Options) RunGetHandler(ctx context.Context, name string) error {
	handler, err := auth.GetHandler(ctx, name)
	if err != nil {
		if w := writer.FromContext(ctx); w != nil {
			w.Errorf("auth handler %q not found", name)
		}
		return exitcode.WithCode(fmt.Errorf("auth handler %q not found", name), exitcode.FileNotFound)
	}

	if o.Output == "" && !o.Interactive {
		return o.printHandlerDetail(ctx, handler)
	}

	return o.writeOutput(ctx, buildHandlerDetail(handler))
}

// printHandlerDetail prints a formatted single-handler view to the terminal.
func (o *Options) printHandlerDetail(ctx context.Context, handler auth.Handler) error {
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
	capStyle := func(s string) string {
		if noColor {
			return "[" + s + "]"
		}
		return "\033[38;5;85;48;5;235m " + s + " \033[0m"
	}

	w.Plainlnf("%s %s", keyStyle("Name:"), handler.Name())
	w.Plainlnf("%s %s", keyStyle("Display Name:"), handler.DisplayName())
	w.Plainln("")

	flows := handler.SupportedFlows()
	flowStrs := make([]string, len(flows))
	for i, f := range flows {
		flowStrs[i] = capStyle(string(f))
	}
	w.Plainlnf("%s %s", keyStyle("Supported Flows:"), strings.Join(flowStrs, " "))

	caps := handler.Capabilities()
	capStrs := make([]string, len(caps))
	for i, c := range caps {
		capStrs[i] = capStyle(string(c))
	}
	w.Plainlnf("%s %s", keyStyle("Capabilities:"), strings.Join(capStrs, " "))

	w.Plainln("")
	w.Plainln(dimStyle("Use -o json or -o yaml for full structured output."))

	return nil
}

// writeOutput dispatches to kvx output.
func (o *Options) writeOutput(ctx context.Context, data any) error {
	kvxOpts := flags.NewKvxOutputOptionsFromFlags(
		o.Output,
		o.Interactive,
		o.Expression,
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(o.CliParams.NoColor),
		kvx.WithOutputAppName(o.BinaryName+" get authhandler"),
		kvx.WithOutputDisplaySchemaJSON(authHandlerDisplaySchemaJSON),
		// Column order: Display Name first, then Name, Capabilities, Flows.
		// JSON Schema does not define ordering, so it is set explicitly here.
		kvx.WithOutputColumnOrder([]string{"displayName", "name", "capabilities", "flows"}),
		// Schema-derived hints: header renames (title), max widths (maxLength), etc.
		kvx.WithOutputSchemaJSON(authHandlerSchema),
	)
	kvxOpts.IOStreams = o.IOStreams
	return kvxOpts.Write(data)
}

// buildHandlerRow builds a summary row for the list view.
func buildHandlerRow(handler auth.Handler) map[string]any {
	flows := handler.SupportedFlows()
	flowStrs := make([]string, len(flows))
	for i, f := range flows {
		flowStrs[i] = string(f)
	}

	caps := handler.Capabilities()
	capStrs := make([]string, len(caps))
	for i, c := range caps {
		capStrs[i] = string(c)
	}

	return map[string]any{
		"name":         handler.Name(),
		"displayName":  handler.DisplayName(),
		"flows":        flowStrs,
		"capabilities": capStrs,
	}
}

// buildHandlerDetail builds a fully detailed map for a single handler.
func buildHandlerDetail(handler auth.Handler) map[string]any {
	return buildHandlerRow(handler)
}

// buildHandlerSummary builds a flat summary for table view.
func buildHandlerSummary(handler auth.Handler) HandlerSummary {
	flows := handler.SupportedFlows()
	flowStrs := make([]string, len(flows))
	for i, f := range flows {
		flowStrs[i] = string(f)
	}

	caps := handler.Capabilities()
	capStrs := make([]string, len(caps))
	for i, c := range caps {
		capStrs[i] = string(c)
	}

	return HandlerSummary{
		Name:         handler.Name(),
		DisplayName:  handler.DisplayName(),
		Flows:        strings.Join(flowStrs, ", "),
		Capabilities: strings.Join(capStrs, ", "),
	}
}
