// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	pkglsp "github.com/oakwood-commons/scafctl/pkg/lsp"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// commandDocumentSelectors creates the `lsp document-selectors` subcommand. It
// reports the solution/action file names this binary auto-discovers, so an
// editor (e.g. the VS Code extension) can build its LSP document selector from
// the same source of truth as CLI discovery instead of hardcoding a list that
// drifts. Consumed programmatically; run with `-o json` for a stable contract.
func commandDocumentSelectors(cliParams *settings.Run, ioStreams *terminal.IOStreams) *cobra.Command {
	var outputFlags flags.KvxOutputFlags

	// Resolve the effective binary name once (falls back to CliBinaryName when
	// an embedder passes an empty name), so help text, discovery, and the kvx
	// app title all use the same non-empty value.
	effectiveBinaryName := settings.SanitizeBinaryName(cliParams.BinaryName)

	cmd := &cobra.Command{
		Use:   "document-selectors",
		Short: "Print the solution file names editors should attach the language server to",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Print the solution and action file names that scafctl auto-discovers,
			partitioned by editor language (YAML vs JSON), along with the effective
			binary name.

			This is a machine-readable contract for editor integrations (such as
			the VS Code extension): the client queries this command at startup and
			builds its LSP document selector from the result, so editor targeting
			stays in lockstep with CLI auto-discovery -- including .json solutions,
			taskfile.*, actions.*, and any embedder binary name -- rather than
			hardcoding a list that drifts.

			Use -o json for a stable, parseable contract:

			  scafctl lsp document-selectors -o json
		`), settings.CliBinaryName, effectiveBinaryName),
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)
			if w == nil {
				return fmt.Errorf("writer not initialized in context")
			}

			files := pkglsp.RecognizedFilesFor(cliParams.BinaryName)

			// Build options from the populated outputFlags so --where,
			// FormatExplicit, and AppName survive; use the effective binary
			// name (files.BinaryName is already resolved with fallback) for the
			// kvx app title.
			outputFlags.AppName = files.BinaryName + " lsp document-selectors"
			outputOpts := flags.ToKvxOutputOptions(
				&outputFlags,
				kvx.WithOutputContext(ctx),
				kvx.WithOutputNoColor(cliParams.NoColor),
			)
			outputOpts.IOStreams = ioStreams

			if err := outputOpts.Write(files); err != nil {
				return fmt.Errorf("print recognized solution files: %w", err)
			}
			return nil
		},
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &outputFlags)
	return cmd
}
