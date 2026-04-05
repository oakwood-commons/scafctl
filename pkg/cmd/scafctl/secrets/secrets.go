// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package secrets provides commands for managing scafctl secrets.
package secrets

import (
	"context"
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// newStoreFromContext creates a secrets.Store, honoring the RequireSecureKeyring
// setting from the application config when available in ctx.
func newStoreFromContext(ctx context.Context) (secrets.Store, error) {
	var opts []secrets.Option
	if cfg := config.FromContext(ctx); cfg != nil {
		opts = append(opts, secrets.WithRequireSecureKeyring(cfg.Settings.RequireSecureKeyring))
	}
	return secrets.New(opts...)
}

// CommandSecrets creates the 'secrets' command.
func CommandSecrets(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "secrets",
		Aliases: []string{"secret"},
		Short:   "Manage encrypted secrets",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Securely manage encrypted secrets for authentication and configuration.

			Secrets are encrypted with AES-256-GCM and stored in XDG-compliant locations:
			  - Linux:   ~/.local/share/scafctl/secrets/
			  - macOS:   ~/.local/share/scafctl/secrets/
			  - Windows: %LOCALAPPDATA%\scafctl\secrets\

			The master encryption key is stored in your OS keychain for security.

			Internal secrets:
			  Secret names starting with the binary name followed by "." are used internally (e.g. auth tokens).
			  By default they are hidden. Use --all on subcommands to include them.
		`), settings.CliBinaryName, cliParams.BinaryName),
		SilenceUsage: true,
	}

	cmdPath := fmt.Sprintf("%s/%s", path, cmd.Use)
	cmd.AddCommand(CommandList(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandGet(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandSet(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandDelete(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandExists(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandExport(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandImport(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandRotate(cliParams, ioStreams, cmdPath))

	return cmd
}
