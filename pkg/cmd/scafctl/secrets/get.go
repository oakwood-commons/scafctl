package secrets

import (
	"encoding/json"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// CommandGet creates the 'secrets get' command.
func CommandGet(_ *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	var (
		outputFormat string
		noNewline    bool
	)

	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Get a secret value",
		Long:  "Retrieve and display the value of a secret. By default, prints the raw value to stdout.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]

			// Validate name
			if err := ValidateUserSecretName(name); err != nil {
				return err
			}

			store, err := secrets.New()
			if err != nil {
				return fmt.Errorf("failed to initialize secrets store: %w", err)
			}

			value, err := store.Get(ctx, name)
			if err != nil {
				return fmt.Errorf("failed to get secret '%s': %w", name, err)
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
					return fmt.Errorf("failed to encode as JSON: %w", err)
				}
				if _, err := ioStreams.Out.Write(jsonBytes); err != nil {
					return fmt.Errorf("failed to write JSON: %w", err)
				}
				if _, err := fmt.Fprintln(ioStreams.Out); err != nil {
					return fmt.Errorf("failed to write newline: %w", err)
				}
			case "yaml":
				data := map[string]interface{}{
					"name":  name,
					"value": string(value),
				}
				encoder := yaml.NewEncoder(ioStreams.Out)
				encoder.SetIndent(2)
				if err := encoder.Encode(data); err != nil {
					return fmt.Errorf("failed to encode as YAML: %w", err)
				}
			default:
				// Raw output
				if _, err := ioStreams.Out.Write(value); err != nil {
					return fmt.Errorf("failed to write value: %w", err)
				}
				if !noNewline {
					if _, err := fmt.Fprintln(ioStreams.Out); err != nil {
						return fmt.Errorf("failed to write newline: %w", err)
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "Output format: json, yaml (default: raw)")
	cmd.Flags().BoolVar(&noNewline, "no-newline", false, "Don't print trailing newline in raw output")

	return cmd
}
