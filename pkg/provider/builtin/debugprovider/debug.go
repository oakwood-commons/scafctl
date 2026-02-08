// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package debugprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/ptrs"
	"gopkg.in/yaml.v3"
)

// ProviderName is the name of this provider used for error wrapping and identification.
const ProviderName = "debug"

// DebugProvider provides debugging capabilities for inspecting data during workflow execution.
type DebugProvider struct {
	descriptor *provider.Descriptor
}

// NewDebugProvider creates a new debug provider instance.
func NewDebugProvider() *DebugProvider {
	version, _ := semver.NewVersion("1.0.0")
	return &DebugProvider{
		descriptor: &provider.Descriptor{
			Name:         "debug",
			DisplayName:  "Debug Provider",
			APIVersion:   "v1",
			Version:      version,
			Description:  "Provides debugging capabilities for inspecting resolver data during workflow execution. Outputs formatted data to stdout, stderr, or file. Supports optional CEL expressions to filter or transform data before output.",
			MockBehavior: "Returns debug output (same behavior in dry-run as debug is side-effect free)",
			Capabilities: []provider.Capability{
				provider.CapabilityFrom,
				provider.CapabilityTransform,
				provider.CapabilityValidation,
				provider.CapabilityAuthentication,
				provider.CapabilityAction,
			},
			Schema: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"expression": schemahelper.StringProp("Optional CEL expression to filter or transform resolver data before output. If not provided, outputs all resolver data. Resolver data is available under the '_' variable (e.g., _.user.name).",
					schemahelper.WithExample("_.user.name"),
					schemahelper.WithMaxLength(*ptrs.IntPtr(8192))),
				"label": schemahelper.StringProp("Optional label or message to add context to the debug output",
					schemahelper.WithExample("User data after transformation"),
					schemahelper.WithMaxLength(*ptrs.IntPtr(500))),
				"format": schemahelper.StringProp("Output format for the debug data",
					schemahelper.WithExample("json"),
					schemahelper.WithEnum("json", "yaml", "pretty"),
					schemahelper.WithDefault("json"),
					schemahelper.WithMaxLength(*ptrs.IntPtr(10))),
				"destination": schemahelper.StringProp("Where to output the debug data",
					schemahelper.WithExample("stdout"),
					schemahelper.WithEnum("stdout", "stderr", "file"),
					schemahelper.WithDefault("stdout"),
					schemahelper.WithMaxLength(*ptrs.IntPtr(10))),
				"file": schemahelper.StringProp("File path when destination is 'file'",
					schemahelper.WithExample("/tmp/debug.log"),
					schemahelper.WithMaxLength(*ptrs.IntPtr(500))),
				"colorize": schemahelper.BoolProp("Whether to colorize the output (only applies to terminal output)",
					schemahelper.WithExample("false"),
					schemahelper.WithDefault(false)),
			}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"result":      schemahelper.AnyProp("The filtered or transformed resolver data (if expression provided) or all resolver data (if no expression)"),
					"output":      schemahelper.StringProp("The formatted debug output containing the formatted representation of the result"),
					"destination": schemahelper.StringProp("Where the output was written (stdout, stderr, or file path)"),
				}),
				provider.CapabilityTransform: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"result":      schemahelper.AnyProp("The filtered or transformed resolver data (if expression provided) or all resolver data (if no expression)"),
					"output":      schemahelper.StringProp("The formatted debug output containing the formatted representation of the result"),
					"destination": schemahelper.StringProp("Where the output was written (stdout, stderr, or file path)"),
				}),
				provider.CapabilityValidation: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"valid":       schemahelper.BoolProp("Whether the debug operation succeeded (always true if no errors occurred)"),
					"errors":      schemahelper.ArrayProp("Validation errors (empty if valid)"),
					"result":      schemahelper.AnyProp("The filtered or transformed resolver data"),
					"output":      schemahelper.StringProp("The formatted debug output"),
					"destination": schemahelper.StringProp("Where the output was written"),
				}),
				provider.CapabilityAuthentication: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"authenticated": schemahelper.BoolProp("Whether authentication succeeded (always true for debug)"),
					"token":         schemahelper.StringProp("The authentication token (empty for debug provider)"),
					"result":        schemahelper.AnyProp("The filtered or transformed resolver data"),
					"output":        schemahelper.StringProp("The formatted debug output"),
					"destination":   schemahelper.StringProp("Where the output was written"),
				}),
				provider.CapabilityAction: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"success":     schemahelper.BoolProp("Whether the debug operation succeeded. Always true if no errors occurred"),
					"result":      schemahelper.AnyProp("The filtered or transformed resolver data (if expression provided) or all resolver data (if no expression)"),
					"output":      schemahelper.StringProp("The formatted debug output containing the formatted representation of the result"),
					"destination": schemahelper.StringProp("Where the output was written (stdout, stderr, or file path)"),
				}),
			},
			Examples: []provider.Example{
				{
					Name:        "Output all resolver data as JSON",
					Description: "Debug all available resolver data in JSON format to stdout",
					YAML: `name: debug-all-data
provider: debug
inputs:
  label: "Current resolver state"
  format: json`,
				},
				{
					Name:        "Filter and output specific field",
					Description: "Use CEL expression to extract and debug a specific field from resolver data",
					YAML: `name: debug-user-name
provider: debug
inputs:
  expression: "_.user.name"
  label: "User name after processing"
  format: yaml`,
				},
				{
					Name:        "Write debug output to file",
					Description: "Save debug information to a file for later inspection",
					YAML: `name: debug-to-file
provider: debug
inputs:
  format: pretty
  destination: file
  file: "/tmp/debug-output.log"`,
				},
			},
		},
	}
}

// Descriptor returns the provider descriptor.
func (p *DebugProvider) Descriptor() *provider.Descriptor {
	return p.descriptor
}

// Execute performs the debug operation.
func (p *DebugProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	inputs, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected map[string]any, got %T", ProviderName, input)
	}
	lgr := logger.FromContext(ctx)

	// Check for dry-run mode
	if dryRun := provider.DryRunFromContext(ctx); dryRun {
		return p.executeDryRun(inputs)
	}

	// Get resolver data from context
	resolverData, _ := provider.ResolverContextFromContext(ctx)

	// Determine what data to output
	var data any
	exprStr, hasExpr := inputs["expression"].(string)

	if hasExpr && exprStr != "" {
		// Evaluate CEL expression to filter/transform data
		// Resolver data is available under the '_' variable (e.g., _.user.name)
		var err error
		data, err = celexp.EvaluateExpression(ctx, exprStr, resolverData, nil)
		if err != nil {
			return nil, fmt.Errorf("%s: failed to evaluate expression: %w", ProviderName, err)
		}
	} else {
		// Use all resolver data
		data = resolverData
	}

	// Get optional fields
	label, _ := inputs["label"].(string)
	format, _ := inputs["format"].(string)
	if format == "" {
		format = "json"
	}
	destination, _ := inputs["destination"].(string)
	if destination == "" {
		destination = "stdout"
	}
	filePath, _ := inputs["file"].(string)
	colorize, _ := inputs["colorize"].(bool)

	// Validate file path if destination is file
	if destination == "file" && filePath == "" {
		return nil, fmt.Errorf("%s: file path is required when destination is 'file'", ProviderName)
	}

	// Format the data
	formatted, err := p.formatData(data, format, colorize)
	if err != nil {
		return nil, fmt.Errorf("%s: failed to format data: %w", ProviderName, err)
	}

	// Add label if provided
	output := formatted
	if label != "" {
		output = fmt.Sprintf("=== %s ===\n%s", label, formatted)
	}

	// Write to destination
	destStr := destination
	if err := p.writeOutput(ctx, output, destination, filePath); err != nil {
		return nil, fmt.Errorf("%s: failed to write output: %w", ProviderName, err)
	}

	if destination == "file" {
		destStr = filePath
	}

	lgr.V(1).Info("Debug operation completed", "format", format, "destination", destStr, "hasExpression", hasExpr)

	// Return result
	return &provider.Output{
		Data: map[string]any{
			"success":     true,
			"result":      data,
			"output":      output,
			"destination": destStr,
		},
	}, nil
}

// formatData formats the data according to the specified format.
func (p *DebugProvider) formatData(data any, format string, colorize bool) (string, error) {
	var output string
	var err error

	switch format {
	case "json":
		var jsonBytes []byte
		jsonBytes, err = json.MarshalIndent(data, "", "  ")
		if err != nil {
			return "", err
		}
		output = string(jsonBytes)
		if colorize {
			output = p.colorizeJSON(output)
		}

	case "yaml":
		var yamlBytes []byte
		yamlBytes, err = yaml.Marshal(data)
		if err != nil {
			return "", err
		}
		output = string(yamlBytes)

	case "pretty":
		output = fmt.Sprintf("%+v", data)

	default:
		return "", fmt.Errorf("unsupported format: %s", format)
	}

	return output, nil
}

// colorizeJSON adds basic ANSI color codes to JSON output.
func (p *DebugProvider) colorizeJSON(jsonStr string) string {
	// Basic colorization using ANSI codes
	const (
		colorReset  = "\033[0m"
		colorRed    = "\033[31m"
		colorGreen  = "\033[32m"
		colorYellow = "\033[33m"
		colorPurple = "\033[35m"
	)

	// Simple colorization
	result := strings.ReplaceAll(jsonStr, "\":", "\""+colorYellow+":"+colorReset)
	result = strings.ReplaceAll(result, "true", colorGreen+"true"+colorReset)
	result = strings.ReplaceAll(result, "false", colorRed+"false"+colorReset)
	result = strings.ReplaceAll(result, "null", colorPurple+"null"+colorReset)

	return result
}

// writeOutput writes the formatted output to the specified destination.
func (p *DebugProvider) writeOutput(ctx context.Context, output, destination, filePath string) error {
	lgr := logger.FromContext(ctx)

	switch destination {
	case "stdout":
		fmt.Fprintln(os.Stdout, output)
		return nil

	case "stderr":
		fmt.Fprintln(os.Stderr, output)
		return nil

	case "file":
		if filePath == "" {
			return fmt.Errorf("file path is required when destination is 'file'")
		}

		file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return fmt.Errorf("failed to open file: %w", err)
		}
		defer func() {
			if cerr := file.Close(); cerr != nil {
				lgr.Error(cerr, "Failed to close debug output file")
			}
		}()

		if _, err := fmt.Fprintln(file, output); err != nil {
			return fmt.Errorf("failed to write to file: %w", err)
		}

		return nil

	default:
		return fmt.Errorf("unsupported destination: %s", destination)
	}
}

// executeDryRun handles dry-run mode.
func (p *DebugProvider) executeDryRun(inputs map[string]any) (*provider.Output, error) {
	exprStr, _ := inputs["expression"].(string)
	format, _ := inputs["format"].(string)
	if format == "" {
		format = "json"
	}
	destination, _ := inputs["destination"].(string)
	if destination == "" {
		destination = "stdout"
	}
	label, _ := inputs["label"].(string)

	message := fmt.Sprintf("Would execute debug with format=%s, destination=%s", format, destination)
	if exprStr != "" {
		message += fmt.Sprintf(", expression=%s", exprStr)
	}
	if label != "" {
		message += fmt.Sprintf(", label=%s", label)
	}

	return &provider.Output{
		Data: map[string]any{
			"success":     true,
			"result":      "[DRY-RUN] Data not evaluated",
			"output":      "",
			"destination": destination,
			"_dryRun":     true,
			"_message":    message,
		},
	}, nil
}
