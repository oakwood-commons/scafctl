// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"bufio"
	"fmt"
	"io"
	"os"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CommandSet creates the 'secrets set' command.
func CommandSet(_ *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	var (
		valueFlag     string
		fileFlag      string
		overwriteFlag bool
	)

	cmd := &cobra.Command{
		Use:   "set <name> [value]",
		Short: "Set a secret value",
		Long: `Store a secret value. The value can be provided via:
  - Argument: scafctl secrets set name value
  - Flag:     scafctl secrets set name --value value
  - File:     scafctl secrets set name --file /path/to/file
  - Stdin:    echo value | scafctl secrets set name

If the secret already exists, use --overwrite to replace it.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)
			if w == nil {
				return fmt.Errorf("writer not initialized in context")
			}
			name := args[0]

			// Validate name
			if err := secrets.ValidateUserSecretName(name); err != nil {
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			store, err := secrets.New()
			if err != nil {
				err := fmt.Errorf("failed to initialize secrets store: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.ConfigError)
			}

			// Check if secret exists
			exists, err := store.Exists(ctx, name)
			if err != nil {
				err := fmt.Errorf("failed to check if secret exists: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			if exists && !overwriteFlag {
				err := fmt.Errorf("secret '%s' already exists. Use --overwrite to replace it", name)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			// Determine value source
			var value []byte
			sourceCount := 0

			// From argument
			if len(args) == 2 {
				value = []byte(args[1])
				sourceCount++
			}

			// From --value flag
			if valueFlag != "" {
				value = []byte(valueFlag)
				sourceCount++
			}

			// From --file flag
			if fileFlag != "" {
				fileData, err := os.ReadFile(fileFlag)
				if err != nil {
					err := fmt.Errorf("failed to read file '%s': %w", fileFlag, err)
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.FileNotFound)
				}
				value = fileData
				sourceCount++
			}

			// From stdin (if not a terminal and no other source)
			if sourceCount == 0 {
				// Check if stdin is a file (pipe or redirect)
				if f, ok := ioStreams.In.(*os.File); ok {
					stat, err := f.Stat()
					if err == nil && (stat.Mode()&os.ModeCharDevice) == 0 {
						// Not a terminal, read from stdin
						reader := bufio.NewReader(ioStreams.In)
						stdinData, err := io.ReadAll(reader)
						if err != nil {
							err := fmt.Errorf("failed to read from stdin: %w", err)
							w.Errorf("%v", err)
							return exitcode.WithCode(err, exitcode.GeneralError)
						}
						value = stdinData
						sourceCount++
					}
				}
			}

			if sourceCount == 0 {
				err := fmt.Errorf("no value provided. Use argument, --value, --file, or pipe to stdin")
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			if sourceCount > 1 {
				err := fmt.Errorf("multiple value sources provided. Use only one of: argument, --value, --file, or stdin")
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			// Store the secret
			if err := store.Set(ctx, name, value); err != nil {
				err := fmt.Errorf("failed to set secret '%s': %w", name, err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			if exists {
				w.Successf("Updated secret '%s' (%d bytes)\n", name, len(value))
			} else {
				w.Successf("Created secret '%s' (%d bytes)\n", name, len(value))
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&valueFlag, "value", "", "Secret value")
	cmd.Flags().StringVar(&fileFlag, "file", "", "Read value from file")
	cmd.Flags().BoolVar(&overwriteFlag, "overwrite", false, "Overwrite existing secret")

	return cmd
}
