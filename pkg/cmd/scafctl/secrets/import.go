// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"fmt"
	"os"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// CommandImport creates the 'secrets import' command.
func CommandImport(_ *settings.Run, _ *terminal.IOStreams, _ string) *cobra.Command {
	var (
		dryRunFlag    bool
		overwriteFlag bool
	)

	cmd := &cobra.Command{
		Use:   "import <file>",
		Short: "Import secrets from a file",
		Long: `Import secrets from a file created by 'secrets export'.

Supports both plaintext (YAML/JSON) and encrypted formats.
Format is auto-detected from file content.

Internal secrets (scafctl.*) in the import file are automatically skipped.

Use --dry-run to preview what would be imported without making changes.
Use --overwrite to replace existing secrets.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)
			if w == nil {
				return fmt.Errorf("writer not initialized in context")
			}
			inputFile := args[0]

			store, err := newStoreFromContext(ctx)
			if err != nil {
				err := fmt.Errorf("failed to initialize secrets store: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.ConfigError)
			}

			// Read file
			fileData, err := os.ReadFile(inputFile)
			if err != nil {
				err := fmt.Errorf("failed to read file '%s': %w", inputFile, err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.FileNotFound)
			}

			// Read password for encrypted files if needed
			var password string
			if secrets.IsEncrypted(fileData) {
				w.WarnStderrf("Enter decryption password: ")
				passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd())) //nolint:gosec // G115: Fd() fits in int on all supported platforms
				if err != nil {
					err := fmt.Errorf("failed to read password: %w", err)
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.GeneralError)
				}
				password = string(passwordBytes)
				w.Plainln("")
			}

			// Parse and validate import data
			importResult, err := secrets.Import(fileData, secrets.ImportOptions{
				Password: password,
			})
			if err != nil {
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			if importResult.VersionMismatch {
				w.Warningf("Warning: Import file version '%s' does not match expected version\n",
					importResult.Version)
			}

			if len(importResult.Secrets) == 0 {
				w.Warning("No user secrets found in import file")
				return nil
			}

			// Dry run - just preview
			if dryRunFlag {
				w.Info("Dry run - would import:")
				for _, secret := range importResult.Secrets {
					w.Infof("  - %s (%d bytes)\n", secret.Name, len(secret.Value))
				}
				if importResult.SkippedInternal > 0 {
					w.Warningf("Skipped %d internal secret(s)\n", importResult.SkippedInternal)
				}
				return nil
			}

			// Import secrets
			imported := 0
			updated := 0
			skipped := 0

			for _, secret := range importResult.Secrets {
				exists, err := store.Exists(ctx, secret.Name)
				if err != nil {
					w.Warningf("Failed to check if '%s' exists: %v\n", secret.Name, err)
					continue
				}

				if exists && !overwriteFlag {
					w.Warningf("Skipping existing secret: %s (use --overwrite to replace)\n", secret.Name)
					skipped++
					continue
				}

				if err := store.Set(ctx, secret.Name, []byte(secret.Value)); err != nil {
					w.Warningf("Failed to import '%s': %v\n", secret.Name, err)
					continue
				}

				if exists {
					updated++
				} else {
					imported++
				}
			}

			// Summary
			w.Successf("Import complete:\n")
			if imported > 0 {
				w.Successf("  - Created: %d\n", imported)
			}
			if updated > 0 {
				w.Successf("  - Updated: %d\n", updated)
			}
			if skipped > 0 {
				w.Warningf("  - Skipped: %d\n", skipped)
			}
			if importResult.SkippedInternal > 0 {
				w.Warningf("  - Skipped internal: %d\n", importResult.SkippedInternal)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "Preview import without making changes")
	cmd.Flags().BoolVar(&overwriteFlag, "overwrite", false, "Overwrite existing secrets")

	return cmd
}
