// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/styles"
	"gopkg.in/yaml.v3"
)

type (
	MessageType                  string
	CustomWriteOutputFunc[T any] func(ioStreams *terminal.IOStreams, data T) error
)

const (
	MessageTypeSuccess MessageType = "success"
	MessageTypeWarning MessageType = "warning"
	MessageTypeError   MessageType = "error"
	MessageTypeInfo    MessageType = "info"
)

// WriteMessageOptions defines configuration options for writing messages to the terminal.
// It allows customization of output streams, message formatting, color usage, and error handling.
//
// Fields:
//
//	IOStreams:   The terminal IO streams to use for output.
//	MessageType: The type of message to be written (e.g., info, warning, error).
//	NewLine:     Whether to append a newline after the message.
//	NoColor:     If true, disables colored output.
//	ExitOnError: If true, exits the program on error messages.
//	ExitFunc:    Optional function to call when exiting due to an error.
type WriteMessageOptions struct {
	IOStreams   *terminal.IOStreams
	MessageType MessageType
	NewLine     bool
	NoColor     bool
	ExitOnError bool
	ExitFunc    func(code int)
}

// SuccessMessage returns a success message string, optionally prefixed with a success icon.
// If noColor is true, the message is returned without any styling or icon.
// Otherwise, the message is prefixed with a styled success icon.
func SuccessMessage(msg string, noColor bool) string {
	if noColor {
		return msg
	}
	return styles.SuccessStyle.Render("✅") + msg
}

// WarningMessage returns a warning message string, optionally prefixed with a warning icon and styled.
// If noColor is true, the message is returned without styling or icon.
// Otherwise, the message is prefixed with a styled warning icon.
func WarningMessage(msg string, noColor bool) string {
	if noColor {
		return msg
	}
	return styles.WarningStyle.Render("⚠️") + msg
}

// ErrorMessage returns an error message string, optionally prefixed with a styled error icon.
// If noColor is true, the message is returned without styling or icon.
// Otherwise, the message is prefixed with a styled "❌" icon and the text is rendered in red.
// Multi-line messages are styled per-line to avoid double-spacing on Windows
// (lipgloss inserts \r\n for each embedded newline on Windows terminals).
func ErrorMessage(msg string, noColor bool) string {
	if noColor {
		return msg
	}
	icon := styles.ErrorStyle.Render("❌")
	lines := strings.Split(msg, "\n")
	for i, line := range lines {
		lines[i] = styles.ErrorTextStyle.Render(line)
	}
	return icon + strings.Join(lines, "\n")
}

// InfoMessage formats an informational message for terminal output.
// If noColor is true, the message is returned without styling.
// Otherwise, it prepends an informational icon and applies styling.
func InfoMessage(msg string, noColor bool) string {
	if noColor {
		return msg
	}
	return styles.InfoStyle.Render("💡") + msg
}

// DebugMessage formats a debug message for terminal output.
// If noColor is true, the message is returned without styling.
// Otherwise, it prepends a debug icon and applies magenta bold styling.
func DebugMessage(msg string, noColor bool) string {
	if noColor {
		return msg
	}
	return styles.DebugStyle.Render("🐛") + msg
}

// VerboseMessage formats a verbose/trace message for terminal output.
// If noColor is true, the message is returned without styling.
// Otherwise, it applies a subtle gray style for user-facing diagnostic output.
func VerboseMessage(msg string, noColor bool) string {
	if noColor {
		return msg
	}
	return styles.VerboseStyle.Render("▸") + msg
}

// WriteDebug writes a debug message to the output stream.
// The message is formatted with a debug icon and styled in magenta if color is enabled.
// A newline is appended to the message by default.
//
// Parameters:
//
//	ioStreams - Pointer to terminal.IOStreams for output operations.
//	msg       - The debug message to write.
//	noColor   - If true, disables colored output and icon.
func WriteDebug(ioStreams *terminal.IOStreams, msg string, noColor bool) {
	fmt.Fprintf(ioStreams.Out, "%s\n", DebugMessage(msg, noColor))
}

// NewWriteMessageOptions creates and returns a new WriteMessageOptions instance with the provided configuration.
// It sets up the IOStreams, message type, color usage, and error handling behavior.
//
// Parameters:
//
//	ioStreams   - Pointer to terminal.IOStreams for input/output operations.
//	messageType - Type of message to be written (e.g., info, error).
//	noColor     - If true, disables colored output.
//	exitOnError - If true, exits on error.
//
// Returns:
//
//	Pointer to a configured WriteMessageOptions struct.
func NewWriteMessageOptions(ioStreams *terminal.IOStreams, messageType MessageType, noColor, exitOnError bool) *WriteMessageOptions {
	return &WriteMessageOptions{
		IOStreams:   ioStreams,
		NewLine:     true,
		NoColor:     noColor,
		ExitOnError: exitOnError,
		MessageType: messageType,
	}
}

// WriteMessage writes a formatted message to the appropriate output stream based on the MessageType.
// It adds icons and color formatting according to the message type (Success, Warning, Info, Error).
// If MessageTypeError is set and ExitOnError is true, the function will exit the process using ExitFunc or os.Exit.
// The message is optionally followed by a newline if NewLine is true.
// Returns an error only for MessageTypeError; otherwise, returns nil.
func (o *WriteMessageOptions) WriteMessage(msg string) {
	newLineStr := ""
	if o.NewLine {
		newLineStr = "\n"
	}
	switch o.MessageType {
	case MessageTypeSuccess:
		fmt.Fprintf(o.IOStreams.Out, "%s%s", SuccessMessage(msg, o.NoColor), newLineStr)
		return
	case MessageTypeWarning:
		fmt.Fprintf(o.IOStreams.Out, "%s%s", WarningMessage(msg, o.NoColor), newLineStr)
		return
	case MessageTypeInfo:
		fmt.Fprintf(o.IOStreams.Out, "%s%s", InfoMessage(msg, o.NoColor), newLineStr)
		return
	case MessageTypeError:
		fmt.Fprintf(o.IOStreams.ErrOut, "%s%s", ErrorMessage(msg, o.NoColor), newLineStr)
		if o.ExitOnError {
			if o.ExitFunc != nil {
				o.ExitFunc(1)
			} else {
				os.Exit(1)
			}
		}
		return
	default:
		fmt.Fprintf(o.IOStreams.Out, "%s%s", msg, newLineStr)
	}
}

// ValidateCommands checks if any unexpected CLI command arguments are present.
// If the args slice contains one or more elements, it returns an error listing the unknown commands.
// Otherwise, it returns nil, indicating that no unknown commands were detected.
func ValidateCommands(args []string) error {
	if len(args) > 0 {
		err := fmt.Errorf("unknown cli command(s) detected: '%s'", strings.Join(args, ", "))
		return err
	}
	return nil
}

// ValidateOutputType checks if the provided output type is valid.
// It returns nil if the output is empty or present in the validOutputTypes slice.
// Otherwise, it returns an error indicating the invalid output type and listing the valid types.
//
// Parameters:
//
//	output - the output type to validate.
//	validOutputTypes - a slice of valid output type strings.
//
// Returns:
//
//	error - nil if the output type is valid or empty, otherwise an error describing the invalid type.
func ValidateOutputType(output string, validOutputTypes []string) error {
	if output == "" {
		return nil
	}
	if !slices.Contains(validOutputTypes, output) {
		return fmt.Errorf("invalid output type: '%s'. Valid types are: %v", output, strings.Join(validOutputTypes, ", "))
	}
	return nil
}

// WriteOutput writes the provided data to the given IOStreams in the specified output format.
// Supported output types are "json" and "yaml". If outputType is empty and a customWriteOutputFunc is provided,
// it will be used to write the output. If outputType is unsupported or missing without a custom function,
// an error is returned.
//
// Parameters:
//   - ioStreams: The IOStreams to write output to.
//   - outputType: The format to use for output ("json", "yaml", or empty for custom).
//   - data: The data to be output.
//   - customWriteOutputFunc: Optional custom function to handle output.
//
// Returns:
//   - error: An error if writing the output fails or if the outputType is unsupported.
func WriteOutput[T any](ioStreams *terminal.IOStreams, outputType string, data T, customWriteOutputFunc CustomWriteOutputFunc[T]) error {
	switch outputType {
	case "":
		if customWriteOutputFunc != nil {
			return customWriteOutputFunc(ioStreams, data)
		}
		return fmt.Errorf("no output type specified and no custom output function provided. This is an issue with the application")
	case "json":
		return WriteJSONOutput(ioStreams, data)
	case "yaml":
		return WriteYAMLOutput(ioStreams, data)
	default:
		return fmt.Errorf("unsupported output type: %s", outputType)
	}
}

// WriteJSONOutput marshals the provided data into JSON format and writes it to the specified output stream.
// It returns an error if the data cannot be marshaled to JSON.
//
// Parameters:
//
//	ioStreams - The terminal IOStreams to write the output to.
//	data      - The data to be marshaled into JSON.
//
// Returns:
//
//	error - An error if JSON marshaling fails, otherwise nil.
func WriteJSONOutput(ioStreams *terminal.IOStreams, data any) error {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("unable to generate JSON output: %w", err)
	}
	fmt.Fprintf(ioStreams.Out, "%s\n", string(jsonBytes))
	return nil
}

// WriteYAMLOutput marshals the provided data into YAML format and writes it to the given IOStreams output.
// It returns an error if the data cannot be marshaled to YAML.
//
// Parameters:
//
//	ioStreams - the terminal IOStreams to write the output to
//	data      - the data to be marshaled into YAML
//
// Returns:
//
//	error - if marshaling fails or writing to output encounters an issue
func WriteYAMLOutput(ioStreams *terminal.IOStreams, data any) error {
	yamlBytes, err := yaml.Marshal(data)
	if err != nil {
		return fmt.Errorf("unable to generate YAML output: %w", err)
	}
	fmt.Fprintf(ioStreams.Out, "%s\n", string(yamlBytes))
	return nil
}
