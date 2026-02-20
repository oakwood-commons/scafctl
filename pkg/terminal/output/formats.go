// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

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
	// OutputFormatAuto lets kvx choose the best visual format (table or list)
	// based on the data shape. This is the default.
	OutputFormatAuto OutputFormat = "auto"

	// OutputFormatTable uses kvx bordered table view
	OutputFormatTable OutputFormat = "table"

	// OutputFormatList uses kvx list view for key-value display
	OutputFormatList OutputFormat = "list"

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
		string(OutputFormatAuto),
		string(OutputFormatTable),
		string(OutputFormatList),
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

// IsKvxFormat returns true if the format uses kvx visual output (auto, table, or list).
func IsKvxFormat(format OutputFormat) bool {
	return format == OutputFormatAuto || format == OutputFormatTable || format == OutputFormatList || format == ""
}

// IsQuietFormat returns true if the format suppresses output.
func IsQuietFormat(format OutputFormat) bool {
	return format == OutputFormatQuiet
}

// ParseOutputFormat parses a string into an OutputFormat.
// It returns the format and whether it was recognized.
func ParseOutputFormat(s string) (OutputFormat, bool) {
	switch s {
	case "auto", "":
		return OutputFormatAuto, true
	case "table":
		return OutputFormatTable, true
	case "list":
		return OutputFormatList, true
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
