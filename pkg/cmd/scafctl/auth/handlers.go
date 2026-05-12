// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"sort"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	authpkg "github.com/oakwood-commons/scafctl/pkg/auth"
	authofficial "github.com/oakwood-commons/scafctl/pkg/auth/official"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
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

// CommandHandlers creates the 'auth handlers' command.
func CommandHandlers(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	var outputFlags flags.KvxOutputFlags
	outputFlags.AppName = cliParams.BinaryName

	cmd := &cobra.Command{
		Use:   "handlers",
		Short: "List registered and available auth handlers",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Show registered and auto-fetchable authentication handlers with their
			current status.

			For each handler, shows:
			  - name: handler identifier
			  - status: installed or available
			  - source: built-in, plugin, or catalog
			  - flows: supported authentication flows
			  - loggedIn: whether the handler is currently authenticated

			Installed handlers are those registered in the auth registry (either
			built-in or loaded via plugin). Available handlers are those in the
			official auth handler registry that can be auto-fetched from the catalog.

			Note: 'scafctl auth list' shows cached tokens, not handlers. This
			command shows handler discovery and installation status.

			Examples:
			  # List all auth handlers
			  scafctl auth handlers

			  # Output as JSON
			  scafctl auth handlers -o json

			  # Output as YAML
			  scafctl auth handlers -o yaml
		`), settings.CliBinaryName, cliParams.BinaryName),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			results := collectHandlerInfo(ctx)

			outputOpts := flags.ToKvxOutputOptions(&outputFlags,
				kvx.WithIOStreams(ioStreams),
				kvx.WithOutputColumnOrder([]string{"name", "displayName", "status", "source", "flows", "loggedIn"}),
				kvx.WithOutputSchemaJSON(authHandlersSchema),
			)

			return outputOpts.Write(results)
		},
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &outputFlags)
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
			if _, ok := handler.(*plugin.AuthHandlerWrapper); ok {
				result["source"] = "plugin"
			} else {
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
		result["flows"] = "unknown (install to inspect)"
	}

	return result
}
