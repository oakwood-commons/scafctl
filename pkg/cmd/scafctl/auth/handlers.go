// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	_ "embed"
	"fmt"
	"sort"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	authpkg "github.com/oakwood-commons/scafctl/pkg/auth"
	authofficial "github.com/oakwood-commons/scafctl/pkg/auth/official"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// authHandlersSchema controls table column display for the handlers command.
var authHandlersSchema = []byte(`{
	"type": "array",
	"items": {
		"type": "object",
		"required": ["name", "status", "source"],
		"properties": {
			"name":        { "type": "string", "title": "Name", "maxLength": 16 },
			"displayName": { "type": "string", "title": "Display Name", "maxLength": 30 },
			"status":      { "type": "string", "title": "Status", "maxLength": 16 },
			"source":      { "type": "string", "title": "Source", "maxLength": 16 },
			"flows":       { "type": "string", "title": "Flows", "maxLength": 40 },
			"loggedIn":    { "type": "boolean", "title": "Logged In" }
		}
	}
}`)

// authHandlersDisplaySchema drives the interactive TUI card-list and detail
// view for the handlers list (auth handlers -i).
//
//go:embed auth_handlers_schema.json
var authHandlersDisplaySchema []byte

// authHandlerDetailSchema drives the interactive TUI detail view for a single
// handler (auth handlers <name> -i) and the visual single-handler detail.
//
//go:embed auth_handler_detail_schema.json
var authHandlerDetailSchema []byte

// CommandHandlers creates the 'auth handlers' command.
func CommandHandlers(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	var outputFlags flags.KvxOutputFlags
	outputFlags.AppName = cliParams.BinaryName

	cmd := &cobra.Command{
		Use:   "handlers [name]",
		Short: "List registered and available auth handlers",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Show registered and auto-fetchable authentication handlers with their
			current status.

			Without arguments, lists all handlers. For each handler, shows:
			  - name: handler identifier
			  - status: installed or available
			  - source: built-in, plugin, or catalog
			  - flows: supported authentication flows
			  - loggedIn: whether the handler is currently authenticated

			With a handler name argument, shows detailed information about that
			handler, including its display name, supported flows, and capabilities.

			Installed handlers are those registered in the auth registry (either
			built-in or loaded via plugin). Available handlers are those in the
			official auth handler registry that can be auto-fetched from the catalog.

			Use 'scafctl auth handlers install <name>' to pre-fetch a handler.
			Use 'scafctl auth handlers remove <name>' to delete a cached handler.

			Note: 'scafctl auth list' shows cached tokens, not handlers. This
			command shows handler discovery and installation status.

			Examples:
			  # List all auth handlers
			  scafctl auth handlers

			  # Explore handlers interactively (TUI)
			  scafctl auth handlers -i

			  # Show details for a specific handler
			  scafctl auth handlers github

			  # Explore a single handler interactively (TUI)
			  scafctl auth handlers github -i

			  # Show handler details as YAML
			  scafctl auth handlers github -o yaml

			  # Install a handler from the catalog
			  scafctl auth handlers install github

			  # Remove a cached handler
			  scafctl auth handlers remove github

			  # Output as JSON
			  scafctl auth handlers -o json

			  # Output as YAML
			  scafctl auth handlers -o yaml
		`), settings.CliBinaryName, cliParams.BinaryName),
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			names := handlerCompletionNames(cmd.Context())
			return names, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if len(args) == 1 {
				return runHandlerDetail(ctx, args[0], &outputFlags, ioStreams, cliParams)
			}

			results := collectHandlerInfo(ctx)

			// Interactive mode: launch the TUI card-list with enriched rows
			// (flows and capabilities as arrays) and the display schema.
			// Structured formats (json/yaml/...) keep the flat output below.
			if outputFlags.Interactive {
				interactiveOpts := flags.ToKvxOutputOptions(&outputFlags,
					kvx.WithIOStreams(ioStreams),
					kvx.WithOutputAppName(cliParams.BinaryName+" auth handlers"),
					kvx.WithOutputDisplaySchemaJSON(authHandlersDisplaySchema),
				)
				if !kvx.IsStructuredFormat(interactiveOpts.Format) {
					return interactiveOpts.Write(collectHandlerInfoInteractive(ctx))
				}
			}

			outputOpts := flags.ToKvxOutputOptions(&outputFlags,
				kvx.WithIOStreams(ioStreams),
				kvx.WithOutputColumnOrder([]string{"name", "displayName", "status", "source", "flows", "loggedIn"}),
				kvx.WithOutputSchemaJSON(authHandlersSchema),
			)

			if err := outputOpts.Write(results); err != nil {
				return err
			}

			// Show hint about catalog discovery only for human-readable output.
			if !kvx.IsStructuredFormat(outputOpts.Format) && !kvx.IsQuietFormat(outputOpts.Format) {
				w := writer.FromContext(ctx)
				if w != nil {
					w.Plainln("")
					w.Infof("Tip: run '%s catalog list --kind auth-handler' to discover additional handlers from configured catalogs.", cliParams.BinaryName)
				}
			}

			return nil
		},
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &outputFlags)

	cmd.AddCommand(commandHandlersInstall(cliParams, ioStreams))
	cmd.AddCommand(commandHandlersRemove(cliParams, ioStreams))

	return cmd
}

// collectHandlerInfo gathers information about all known auth handlers:
// registered (installed) and official (available for auto-fetch).
func collectHandlerInfo(ctx context.Context) []map[string]any {
	authReg := authpkg.RegistryFromContext(ctx)
	officialReg := authofficial.RegistryFromContext(ctx)

	// Collect all unique handler names from both registries.
	nameSet := make(map[string]struct{})
	if authReg != nil {
		for _, name := range authReg.List() {
			nameSet[name] = struct{}{}
		}
	}
	if officialReg != nil {
		for _, name := range officialReg.Names() {
			nameSet[name] = struct{}{}
		}
	}

	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	sort.Strings(names)

	results := make([]map[string]any, 0, len(names))
	for _, name := range names {
		result := buildHandlerInfoResult(ctx, name, authReg, officialReg)
		results = append(results, result)
	}

	return results
}

// collectHandlerInfoInteractive gathers enriched handler info for the
// interactive TUI, with flows and capabilities as arrays and an install hint
// for available (not-yet-installed) handlers.
func collectHandlerInfoInteractive(ctx context.Context) []any {
	authReg := authpkg.RegistryFromContext(ctx)
	officialReg := authofficial.RegistryFromContext(ctx)

	nameSet := make(map[string]struct{})
	if authReg != nil {
		for _, name := range authReg.List() {
			nameSet[name] = struct{}{}
		}
	}
	if officialReg != nil {
		for _, name := range officialReg.Names() {
			nameSet[name] = struct{}{}
		}
	}

	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	sort.Strings(names)

	rows := make([]any, 0, len(names))
	for _, name := range names {
		rows = append(rows, buildHandlerInteractiveResult(ctx, name, authReg, officialReg))
	}

	return rows
}

// buildHandlerInteractiveResult builds a single enriched handler result for the
// interactive TUI. Installed handlers include their flows and capabilities as
// arrays; available handlers include an install hint; unknown handlers are
// marked not-found.
func buildHandlerInteractiveResult(ctx context.Context, name string, authReg *authpkg.Registry, officialReg *authofficial.Registry) map[string]any {
	if authReg != nil && authReg.Has(name) {
		if handler, err := authReg.Get(name); err == nil {
			return buildHandlerDetailMap(ctx, handler)
		}
	}

	if officialReg != nil && officialReg.Has(name) {
		return buildAvailableHandlerCard(ctx, name)
	}

	return map[string]any{
		"name":         name,
		"displayName":  name,
		"status":       "not-found",
		"source":       "",
		"loggedIn":     false,
		"flows":        []string{},
		"capabilities": []string{},
	}
}

// buildAvailableHandlerCard builds the interactive card for an available but
// not-yet-installed handler, including an install hint.
func buildAvailableHandlerCard(ctx context.Context, name string) map[string]any {
	binName := settings.BinaryNameFromContext(ctx)
	return map[string]any{
		"name":         name,
		"displayName":  name,
		"status":       "available",
		"source":       "catalog",
		"loggedIn":     false,
		"flows":        []string{},
		"capabilities": []string{},
		"hint":         fmt.Sprintf("Run '%s auth handlers install %s' to fetch and inspect it.", binName, name),
	}
}

// buildHandlerInfoResult builds a single handler info result map.
func buildHandlerInfoResult(ctx context.Context, name string, authReg *authpkg.Registry, officialReg *authofficial.Registry) map[string]any {
	result := map[string]any{
		"name":        name,
		"displayName": name,
		"status":      "not-found",
		"source":      "",
		"flows":       "",
		"loggedIn":    false,
	}

	// Check if handler is installed (registered in auth registry).
	if authReg != nil && authReg.Has(name) {
		handler, err := authReg.Get(name)
		if err == nil {
			result["status"] = "installed"
			result["displayName"] = handler.DisplayName()
			switch handler.(type) {
			case *plugin.AuthHandlerWrapper, *plugin.LazyAuthHandlerWrapper:
				result["source"] = "plugin"
			default:
				result["source"] = "built-in"
			}

			// Collect supported flows.
			flows := handler.SupportedFlows()
			flowNames := make([]string, 0, len(flows))
			for _, f := range flows {
				flowNames = append(flowNames, string(f))
			}
			result["flows"] = strings.Join(flowNames, ", ")

			// Check authentication status.
			if status, err := handler.Status(ctx); err == nil {
				result["loggedIn"] = status.Authenticated
			}
		}
	} else if officialReg != nil && officialReg.Has(name) {
		// Handler is in the official registry but not installed yet.
		result["status"] = "available"
		result["source"] = "catalog"
		binName := settings.BinaryNameFromContext(ctx)
		result["flows"] = "run '" + binName + " auth handlers install " + name + "' to inspect"
	}

	return result
}

// handlerCompletionNames returns the sorted union of installed and available
// auth handler names for shell completion.
func handlerCompletionNames(ctx context.Context) []string {
	nameSet := make(map[string]struct{})
	if authReg := authpkg.RegistryFromContext(ctx); authReg != nil {
		for _, n := range authReg.List() {
			nameSet[n] = struct{}{}
		}
	}
	if officialReg := authofficial.RegistryFromContext(ctx); officialReg != nil {
		for _, n := range officialReg.Names() {
			nameSet[n] = struct{}{}
		}
	}
	names := make([]string, 0, len(nameSet))
	for n := range nameSet {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// runHandlerDetail shows detailed information about a single auth handler.
// Installed handlers render their full details via kvx; available
// (not-yet-installed) handlers print an install hint; unknown handlers return a
// not-found error.
func runHandlerDetail(ctx context.Context, name string, outputFlags *flags.KvxOutputFlags, ioStreams *terminal.IOStreams, cliParams *settings.Run) error {
	authReg := authpkg.RegistryFromContext(ctx)
	officialReg := authofficial.RegistryFromContext(ctx)

	if authReg != nil && authReg.Has(name) {
		handler, err := authReg.Get(name)
		if err != nil {
			return exitcode.WithCode(fmt.Errorf("failed to load auth handler %q: %w", name, err), exitcode.GeneralError)
		}
		detail := buildHandlerDetailMap(ctx, handler)

		// Interactive mode: wrap in an array and reuse the list display schema
		// so the TUI enters list view and the user can drill into the detail
		// card. Structured formats fall through to the flat detail map below.
		if outputFlags.Interactive {
			interactiveOpts := flags.ToKvxOutputOptions(outputFlags,
				kvx.WithIOStreams(ioStreams),
				kvx.WithOutputAppName(cliParams.BinaryName+" auth handlers"),
				kvx.WithOutputDisplaySchemaJSON(authHandlersDisplaySchema),
			)
			if !kvx.IsStructuredFormat(interactiveOpts.Format) {
				return interactiveOpts.Write([]any{detail})
			}
		}

		outputOpts := flags.ToKvxOutputOptions(outputFlags,
			kvx.WithIOStreams(ioStreams),
			kvx.WithOutputDisplaySchemaJSON(authHandlerDetailSchema),
			kvx.WithOutputColumnOrder([]string{"name", "displayName", "source", "status", "loggedIn", "flows", "capabilities"}),
		)
		return outputOpts.Write(detail)
	}

	if officialReg != nil && officialReg.Has(name) {
		// Interactive mode: render a minimal card showing the install hint.
		if outputFlags.Interactive {
			interactiveOpts := flags.ToKvxOutputOptions(outputFlags,
				kvx.WithIOStreams(ioStreams),
				kvx.WithOutputAppName(cliParams.BinaryName+" auth handlers"),
				kvx.WithOutputDisplaySchemaJSON(authHandlersDisplaySchema),
			)
			if !kvx.IsStructuredFormat(interactiveOpts.Format) {
				return interactiveOpts.Write([]any{buildAvailableHandlerCard(ctx, name)})
			}
		}

		// Structured output: render through kvx so -o json/yaml/csv/toml produce valid output.
		detail := buildAvailableHandlerCard(ctx, name)
		outputOpts := flags.ToKvxOutputOptions(outputFlags,
			kvx.WithIOStreams(ioStreams),
			kvx.WithOutputColumnOrder([]string{"name", "displayName", "source", "status", "loggedIn", "flows", "capabilities", "hint"}),
		)
		if kvx.IsStructuredFormat(outputOpts.Format) {
			return outputOpts.Write(detail)
		}

		// Default human-readable: print a friendly hint.
		if w := writer.FromContext(ctx); w != nil {
			w.Infof("Auth handler %q is available but not installed.", name)
			w.Infof("Run '%s auth handlers install %s' to fetch and inspect it.", cliParams.BinaryName, name)
		}
		return nil
	}

	return exitcode.WithCode(fmt.Errorf("auth handler %q not found", name), exitcode.FileNotFound)
}

// buildHandlerDetailMap builds the structured detail representation of an
// installed auth handler.
func buildHandlerDetailMap(ctx context.Context, handler authpkg.Handler) map[string]any {
	flows := handler.SupportedFlows()
	flowNames := make([]string, 0, len(flows))
	for _, f := range flows {
		flowNames = append(flowNames, string(f))
	}

	caps := handler.Capabilities()
	capNames := make([]string, 0, len(caps))
	for _, c := range caps {
		capNames = append(capNames, string(c))
	}

	source := "built-in"
	switch handler.(type) {
	case *plugin.AuthHandlerWrapper, *plugin.LazyAuthHandlerWrapper:
		source = "plugin"
	}

	loggedIn := false
	if status, err := handler.Status(ctx); err == nil && status != nil {
		loggedIn = status.Authenticated
	}

	return map[string]any{
		"name":         handler.Name(),
		"displayName":  handler.DisplayName(),
		"status":       "installed",
		"source":       source,
		"flows":        flowNames,
		"capabilities": capNames,
		"loggedIn":     loggedIn,
	}
}
