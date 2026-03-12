// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"encoding/json"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// CommandGet creates the 'secrets get' command.
func CommandGet(_ *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	var (
		outputFormat string
		noNewline    bool
		allFlag      bool
	)

	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Get a secret value",
		Long:  "Retrieve and display the value of a secret. By default, prints the raw value to stdout.\nUse --all to access internal secrets (e.g. auth tokens).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)
			if w == nil {
				return fmt.Errorf("writer not initialized in context")
			}
			name := args[0]

			// Validate name
			if err := secrets.ValidateSecretName(name, allFlag); err != nil {
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			store, err := secrets.New()
			if err != nil {
				err := fmt.Errorf("failed to initialize secrets store: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.ConfigError)
			}

			value, err := store.Get(ctx, name)
			if err != nil {
				err := fmt.Errorf("failed to get secret '%s': %w", name, err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.FileNotFound)
			}

			// Handle different output formats
			switch outputFormat {
			case "json":
				data := map[string]interface{}{
					"name":  name,
					"value": string(value),
				}
				jsonBytes, err := json.Marshal(data)
				if err != nil {
					err := fmt.Errorf("failed to encode as JSON: %w", err)
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.GeneralError)
				}
				if _, err := ioStreams.Out.Write(jsonBytes); err != nil {
					err := fmt.Errorf("failed to write JSON: %w", err)
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.GeneralError)
				}
				w.Plainln("")
			case "yaml":
				data := map[string]interface{}{
					"name":  name,
					"value": string(value),
				}
				encoder := yaml.NewEncoder(ioStreams.Out)
				encoder.SetIndent(2)
				if err := encoder.Encode(data); err != nil {
					err := fmt.Errorf("failed to encode as YAML: %w", err)
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.GeneralError)
				}
			default:
				// Raw output
				if _, err := ioStreams.Out.Write(value); err != nil {
					err := fmt.Errorf("failed to write value: %w", err)
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.GeneralError)
				}
				if !noNewline {
					w.Plainln("")
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "Output format: json, yaml (default: raw)")
	cmd.Flags().BoolVar(&noNewline, "no-newline", false, "Don't print trailing newline in raw output")
	cmd.Flags().BoolVarP(&allFlag, "all", "a", false, "Include internal secrets (e.g. auth tokens)")

	return cmd
}
