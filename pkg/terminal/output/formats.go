// Package output provides output formatting utilities for scafctl commands.
// It includes support for message formatting, JSON/YAML output, and validation utilities.
//
// For kvx-enabled output with interactive TUI and table views, use the
// github.com/oakwood-commons/scafctl/pkg/terminal/kvx package instead.
package output

// OutputFormat represents supported output formats for command output.
//
//nolint:revive // The stuttering name (output.OutputFormat) is intentional for API clarity
type OutputFormat string

const (
	// OutputFormatTable uses kvx table view (default for terminal output)
	OutputFormatTable OutputFormat = "table"

	// OutputFormatJSON outputs as JSON (for piping/scripting)
	OutputFormatJSON OutputFormat = "json"

	// OutputFormatYAML outputs as YAML (for piping/scripting)
	OutputFormatYAML OutputFormat = "yaml"

	// OutputFormatQuiet suppresses all output (exit code only)
	OutputFormatQuiet OutputFormat = "quiet"
)

// String returns the string representation of the output format.
func (f OutputFormat) String() string {
	return string(f)
}

// BaseOutputFormats returns the common output formats supported by data-outputting commands.
// This list is used for flag validation and help text generation.
func BaseOutputFormats() []string {
	return []string{
		string(OutputFormatTable),
		string(OutputFormatJSON),
		string(OutputFormatYAML),
		string(OutputFormatQuiet),
	}
}

// IsStructuredFormat returns true if the format is meant for piping (json/yaml).
// These formats should not use interactive or table output.
func IsStructuredFormat(format OutputFormat) bool {
	return format == OutputFormatJSON || format == OutputFormatYAML
}

// IsTableFormat returns true if the format uses kvx table output.
func IsTableFormat(format OutputFormat) bool {
	return format == OutputFormatTable || format == ""
}

// IsQuietFormat returns true if the format suppresses output.
func IsQuietFormat(format OutputFormat) bool {
	return format == OutputFormatQuiet
}

// ParseOutputFormat parses a string into an OutputFormat.
// It returns the format and whether it was recognized.
func ParseOutputFormat(s string) (OutputFormat, bool) {
	switch s {
	case "table", "":
		return OutputFormatTable, true
	case "json":
		return OutputFormatJSON, true
	case "yaml":
		return OutputFormatYAML, true
	case "quiet":
		return OutputFormatQuiet, true
	default:
		return "", false
	}
}
