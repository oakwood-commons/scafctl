// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package catalog provides commands for inspecting and managing the local catalog.
package catalog

import (
	"context"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CommandCatalog creates the catalog command group.
func CommandCatalog(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "catalog",
		Aliases:      []string{"cat"},
		Short:        "Manage the local artifact catalog",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Manage the local artifact catalog.

			The catalog stores solutions, providers, and auth handlers as versioned
			OCI artifacts for later execution with 'scafctl run'.

			The local catalog is stored at:
			  - Linux: ~/.local/share/scafctl/catalog/
			  - macOS: ~/.local/share/scafctl/catalog/
			  - Windows: %LOCALAPPDATA%\scafctl\catalog\
		`),
	}

	cmd.AddCommand(CommandList(cliParams, ioStreams, path))
	cmd.AddCommand(CommandInspect(cliParams, ioStreams, path))
	cmd.AddCommand(CommandDelete(cliParams, ioStreams, path))
	cmd.AddCommand(CommandPrune(cliParams, ioStreams, path))
	cmd.AddCommand(CommandSave(cliParams, ioStreams, path))
	cmd.AddCommand(CommandLoad(cliParams, ioStreams, path))
	cmd.AddCommand(CommandPush(cliParams, ioStreams, path))
	cmd.AddCommand(CommandPull(cliParams, ioStreams, path))
	cmd.AddCommand(CommandTag(cliParams, ioStreams, path))
	cmd.AddCommand(CommandTags(cliParams, ioStreams, path))
	cmd.AddCommand(CommandLogin(cliParams, ioStreams, path))
	cmd.AddCommand(CommandLogout(cliParams, ioStreams, path))
	cmd.AddCommand(CommandRemote(cliParams, ioStreams, path))
	cmd.AddCommand(CommandAttach(cliParams, ioStreams, path))

	return cmd
}

// hintOnAuthError checks if the error looks like a 401/403 and prints a
// helpful hint suggesting 'catalog login'. This keeps the hint logic in one
// place for push, pull, and delete.
func hintOnAuthError(ctx context.Context, w *writer.Writer, registry string, err error) {
	errStr := err.Error()
	if !strings.Contains(errStr, "401") && !strings.Contains(errStr, "403") &&
		!strings.Contains(errStr, "Unauthorized") && !strings.Contains(errStr, "Forbidden") {
		return
	}

	bin := settings.BinaryNameFromContext(ctx)

	var customHandlers []config.CustomOAuth2Config
	if cfg := config.FromContext(ctx); cfg != nil {
		customHandlers = cfg.Auth.CustomOAuth2
	}

	if handler := catalog.InferAuthHandler(registry, customHandlers); handler != "" {
		w.Infof("Hint: authenticate with '%s auth login %s' then '%s catalog login %s --auth-provider %s'",
			bin, handler, bin, registry, handler)
	} else {
		w.Infof("Hint: run '%s catalog login %s' to authenticate", bin, registry)
	}
}

// resolveAuthHandler attempts to find and return the auth handler for a registry.
// It checks:
//  1. Catalog config authProvider field (when catalogFlag resolves to a named catalog)
//  2. InferAuthHandler (built-in + custom OAuth2 mappings)
//
// Returns nil if no handler can be resolved or the handler cannot be loaded.
// This is best-effort — push/pull/delete still work via credential store alone.
func resolveAuthHandler(ctx context.Context, registry, catalogFlag string) auth.Handler {
	var handlerName string
	cfg := config.FromContext(ctx)

	// Try catalog config first (has explicit authProvider field)
	if catalogFlag != "" && !catalog.LooksLikeCatalogURL(catalogFlag) && cfg != nil {
		if cat, ok := cfg.GetCatalog(catalogFlag); ok && cat.AuthProvider != "" {
			handlerName = cat.AuthProvider
		}
	}

	// Fall back to default catalog's authProvider
	if handlerName == "" && cfg != nil {
		if cat, ok := cfg.GetDefaultCatalog(); ok && cat.AuthProvider != "" {
			handlerName = cat.AuthProvider
		}
	}

	// Fall back to inference from registry host
	if handlerName == "" {
		var customHandlers []config.CustomOAuth2Config
		if cfg != nil {
			customHandlers = cfg.Auth.CustomOAuth2
		}
		handlerName = catalog.InferAuthHandler(registry, customHandlers)
	}

	if handlerName == "" {
		return nil
	}

	handler, err := auth.GetHandler(ctx, handlerName)
	if err != nil {
		return nil
	}

	return handler
}

// resolveAuthScope returns the authScope configured for the named catalog, if any.
func resolveAuthScope(ctx context.Context, catalogFlag string) string {
	cfg := config.FromContext(ctx)
	if catalogFlag != "" && cfg != nil {
		if cat, ok := cfg.GetCatalog(catalogFlag); ok {
			return cat.AuthScope
		}
	}
	return ""
}
