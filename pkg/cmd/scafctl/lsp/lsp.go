// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package lsp provides the `lsp` command, which starts the scafctl language
// server over stdio. All server logic lives in pkg/lsp; this is thin wiring.
package lsp

import (
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	pkglsp "github.com/oakwood-commons/scafctl/pkg/lsp"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandLsp creates the `lsp` command.
func CommandLsp(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	binaryName := cliParams.BinaryName
	if binaryName == "" {
		binaryName = settings.CliBinaryName
	}

	// stdio selects the stdio transport. It is a no-op flag because stdio is the
	// only (and default) transport, but many LSP clients -- following the
	// convention established by typescript-language-server and Microsoft's own
	// json/html/css servers -- append `--stdio` when launching a server, and
	// vscode-languageclient does so for TransportKind.stdio. Accepting it makes
	// the server usable by any such client instead of exiting on an unknown flag.
	var stdio bool

	cmd := &cobra.Command{
		Use:   "lsp",
		Short: "Run the language server over stdio (for editor integration)",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Start the scafctl language server, communicating over stdin/stdout
			using the Language Server Protocol (LSP).

			The server publishes lint diagnostics for solution files as you edit
			them. It is meant to be launched by an editor / LSP client (such as a
			VS Code extension), not run interactively -- stdout is the JSON-RPC
			channel, so anything written there other than protocol messages would
			corrupt the session.

			The --stdio flag is accepted for compatibility with LSP clients that
			pass it by convention; stdio is the only transport, so it has no
			effect.
		`), settings.CliBinaryName, binaryName),
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cmd.Name())
			ctx := settings.IntoContext(cmd.Context(), cliParams)
			ctx = logger.WithLogger(ctx, logger.FromContext(cmd.Context()))

			// Best-effort registry; lint degrades gracefully without it.
			registry, _ := builtin.DefaultRegistry(ctx)

			// Report the embedder's version when embedded, else the engine build.
			version := cliParams.EmbedderVersion
			if version == "" {
				version = settings.VersionInformation.BuildVersion
			}
			server := pkglsp.NewServer(cliParams.BinaryName, version, registry)
			return server.Run()
		},
	}

	// Accept --stdio as a transport selector (see the stdio var comment). It is
	// intentionally not marked hidden so `lsp --help` documents the convention.
	cmd.Flags().BoolVar(&stdio, "stdio", false, "Use the stdio transport (default and only transport; accepted for LSP client compatibility)")

	cmd.AddCommand(commandDocumentSelectors(cliParams, ioStreams))

	return cmd
}
