package debugprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/google/cel-go/cel"
	celext "github.com/google/cel-go/ext"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/celexp/conversion"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/ptrs"
	"gopkg.in/yaml.v3"
)

// DebugProvider provides debugging capabilities for inspecting data during workflow execution.
type DebugProvider struct {
	descriptor *provider.Descriptor
}

// NewDebugProvider creates a new debug provider instance.
func NewDebugProvider() *DebugProvider {
	version, _ := semver.NewVersion("1.0.0")
	return &DebugProvider{
		descriptor: &provider.Descriptor{
			Name:        "debug",
			DisplayName: "Debug Provider",
			APIVersion:  "v1",
			Version:     version,
			Description: "Provides debugging capabilities for inspecting resolver data during workflow execution. Outputs formatted data to stdout, stderr, or file. Supports optional CEL expressions to filter or transform data before output.",
			Capabilities: []provider.Capability{
				provider.CapabilityFrom,
				provider.CapabilityTransform,
				provider.CapabilityValidation,
				provider.CapabilityAuthentication,
				provider.CapabilityAction,
			},
			Schema: provider.SchemaDefinition{
				Properties: map[string]provider.PropertyDefinition{
					"expression": {
						Type:        provider.PropertyTypeString,
						Required:    false,
						Description: "Optional CEL expression to filter or transform resolver data before output. If not provided, outputs all resolver data. Resolver data from context is available as variables in the expression.",
						Example:     "user.name",
						MaxLength:   ptrs.IntPtr(8192),
					},
					"label": {
						Type:        provider.PropertyTypeString,
						Required:    false,
						Description: "Optional label or message to add context to the debug output",
						Example:     "User data after transformation",
						MaxLength:   ptrs.IntPtr(500),
					},
					"format": {
						Type:        provider.PropertyTypeString,
						Required:    false,
						Description: "Output format for the debug data",
						Example:     "json",
						Enum:        []any{"json", "yaml", "pretty"},
						Default:     "json",
						MaxLength:   ptrs.IntPtr(10),
					},
					"destination": {
						Type:        provider.PropertyTypeString,
						Required:    false,
						Description: "Where to output the debug data",
						Example:     "stdout",
						Enum:        []any{"stdout", "stderr", "file"},
						Default:     "stdout",
						MaxLength:   ptrs.IntPtr(10),
					},
					"file": {
						Type:        provider.PropertyTypeString,
						Required:    false,
						Description: "File path when destination is 'file'",
						Example:     "/tmp/debug.log",
						MaxLength:   ptrs.IntPtr(500),
					},
					"colorize": {
						Type:        provider.PropertyTypeBool,
						Required:    false,
						Description: "Whether to colorize the output (only applies to terminal output)",
						Example:     "false",
						Default:     false,
					},
				},
			},
			OutputSchema: provider.SchemaDefinition{
				Properties: map[string]provider.PropertyDefinition{
					"success": {
						Type:        provider.PropertyTypeBool,
						Description: "Whether the debug operation succeeded. Always true if no errors occurred",
					},
					"result": {
						Type:        provider.PropertyTypeAny,
						Description: "The filtered or transformed resolver data (if expression provided) or all resolver data (if no expression)",
					},
					"output": {
						Type:        provider.PropertyTypeString,
						Description: "The formatted debug output containing the formatted representation of the result",
					},
					"destination": {
						Type:        provider.PropertyTypeString,
						Description: "Where the output was written (stdout, stderr, or file path)",
					},
				},
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
  expression: "user.name"
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
func (p *DebugProvider) Execute(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
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
		var err error
		data, err = p.evaluateExpression(ctx, exprStr, resolverData)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate expression: %w", err)
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
		return nil, fmt.Errorf("file path is required when destination is 'file'")
	}

	// Format the data
	formatted, err := p.formatData(data, format, colorize)
	if err != nil {
		return nil, fmt.Errorf("failed to format data: %w", err)
	}

	// Add label if provided
	output := formatted
	if label != "" {
		output = fmt.Sprintf("=== %s ===\n%s", label, formatted)
	}

	// Write to destination
	destStr := destination
	if err := p.writeOutput(ctx, output, destination, filePath); err != nil {
		return nil, fmt.Errorf("failed to write output: %w", err)
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

// evaluateExpression evaluates a CEL expression with resolver data.
func (p *DebugProvider) evaluateExpression(_ context.Context, exprStr string, resolverData map[string]any) (any, error) {
	// Build CEL variables from resolver data
	celVars := make(map[string]any)
	for k, v := range resolverData {
		celVars[k] = v
	}

	// Create environment options with string extensions
	envOpts := make([]cel.EnvOption, 0, 1+len(celVars))
	envOpts = append(envOpts, celext.Strings())

	// Add resolver data variables to environment
	for k := range celVars {
		envOpts = append(envOpts, cel.Variable(k, cel.DynType))
	}

	// Compile the expression
	expr := celexp.Expression(exprStr)
	compiled, err := expr.Compile(envOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to compile expression: %w", err)
	}

	// Evaluate the CEL expression
	result, err := compiled.Eval(celVars)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate expression: %w", err)
	}

	// Convert CEL types to Go types (handles ref.Val arrays, maps, etc.)
	goResult := conversion.GoToCelValue(result)
	return conversion.CelValueToGo(goResult), nil
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
