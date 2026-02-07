// Package flags provides shared flag helpers for scafctl commands.
package flags

import (
	"fmt"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/spf13/cobra"
)

// KvxOutputFlags holds the flag values for kvx-enabled output.
// This struct is typically embedded in command options structs.
type KvxOutputFlags struct {
	// Output specifies the output format (table, json, yaml, quiet)
	Output string `json:"output,omitempty" yaml:"output,omitempty" doc:"Output format" example:"table" maxLength:"10"`

	// Interactive enables the kvx TUI for data exploration
	Interactive bool `json:"interactive,omitempty" yaml:"interactive,omitempty" doc:"Launch interactive TUI mode"`

	// Expression is a CEL expression to filter/transform output
	Expression string `json:"expression,omitempty" yaml:"expression,omitempty" doc:"CEL expression to filter output" example:"_.database" maxLength:"4096"`
}

// AddKvxOutputFlags adds kvx-enabled output flags to a command.
// It sets up the standard -o/--output, -i/--interactive, and -e/--expression flags.
//
// Parameters:
//   - cmd: The cobra command to add flags to
//   - outputFormat: Pointer to store the output format value (default: "table")
//   - interactive: Pointer to store the interactive mode value (default: false)
//   - expression: Pointer to store the CEL expression value (default: "")
func AddKvxOutputFlags(cmd *cobra.Command, outputFormat *string, interactive *bool, expression *string) {
	validFormats := kvx.BaseOutputFormats()

	cmd.Flags().StringVarP(outputFormat, "output", "o", "table",
		fmt.Sprintf("Output format: %s", strings.Join(validFormats, ", ")))

	cmd.Flags().BoolVarP(interactive, "interactive", "i", false,
		"Launch interactive viewer to explore results (requires terminal)")

	cmd.Flags().StringVarP(expression, "expression", "e", "",
		"CEL expression to filter/transform output data (e.g., '_.items.filter(x, x.enabled)')")
}

// AddKvxOutputFlagsToStruct adds kvx-enabled output flags to a command using a KvxOutputFlags struct.
// This is a convenience function when using the KvxOutputFlags struct directly.
func AddKvxOutputFlagsToStruct(cmd *cobra.Command, flags *KvxOutputFlags) {
	AddKvxOutputFlags(cmd, &flags.Output, &flags.Interactive, &flags.Expression)
}

// ValidateKvxOutputFormat validates the output format string.
// Returns an error if the format is not a valid output format.
func ValidateKvxOutputFormat(format string) error {
	if format == "" {
		return nil // Empty defaults to table
	}

	validFormats := kvx.BaseOutputFormats()
	for _, valid := range validFormats {
		if format == valid {
			return nil
		}
	}

	return fmt.Errorf("invalid output format: %s (valid: %s)", format, strings.Join(validFormats, ", "))
}

// ToKvxOutputOptions converts flag values to OutputOptions for writing output.
// This creates a fully configured OutputOptions instance from flag values.
func ToKvxOutputOptions(flags *KvxOutputFlags, opts ...kvx.OutputOption) *kvx.OutputOptions {
	kvxOpts := &kvx.OutputOptions{
		Interactive: flags.Interactive,
		Expression:  flags.Expression,
		PrettyPrint: true,
	}

	// Parse format string to OutputFormat
	if f, ok := kvx.ParseOutputFormat(flags.Output); ok {
		kvxOpts.Format = f
	} else {
		kvxOpts.Format = kvx.OutputFormatTable
	}

	// Apply additional options
	for _, opt := range opts {
		opt(kvxOpts)
	}

	return kvxOpts
}

// NewKvxOutputOptionsFromFlags creates a new OutputOptions from command flags and options.
// This is a convenience function that combines flag parsing with functional options.
func NewKvxOutputOptionsFromFlags(
	outputFormat string,
	interactive bool,
	expression string,
	opts ...kvx.OutputOption,
) *kvx.OutputOptions {
	flags := &KvxOutputFlags{
		Output:      outputFormat,
		Interactive: interactive,
		Expression:  expression,
	}
	return ToKvxOutputOptions(flags, opts...)
}
