// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// ProviderOptions holds configuration for the run provider command
type ProviderOptions struct {
	IOStreams *terminal.IOStreams
	CliParams *settings.Run

	// kvx output integration
	flags.KvxOutputFlags

	// ProviderName is the name of the provider to execute (positional arg).
	ProviderName string

	// InputParams are the provider input parameters (--input key=value or --input @file.yaml).
	InputParams []string

	// Capability specifies which capability to execute.
	// Defaults to the first capability declared by the provider.
	Capability string

	// DryRun shows what would be executed without running the provider.
	DryRun bool

	// PluginDirs are directories to scan for plugin providers.
	PluginDirs []string

	// ShowMetrics shows provider execution metrics after completion.
	ShowMetrics bool

	// Redact redacts sensitive fields in the output.
	Redact bool
}

// CommandProvider creates the 'run provider' subcommand
func CommandProvider(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	options := &ProviderOptions{}

	cfg := runCommandConfig{
		cliParams: cliParams,
		ioStreams: ioStreams,
		path:      path,
		runner:    options,
		getOutputFn: func() string {
			return options.Output
		},
		setIOStreamFn: func(ios *terminal.IOStreams, cli *settings.Run) {
			options.IOStreams = ios
			options.CliParams = cli
		},
	}

	cCmd := &cobra.Command{
		Use:     "provider <name>",
		Aliases: []string{"prov", "p"},
		Short:   "Execute a single provider directly",
		Long: `Execute a provider directly without a solution or resolver file.

This command is useful for testing, debugging, and exploring individual
providers in isolation. It accepts the provider name as a positional
argument and provider inputs via --input flags.

PROVIDER INPUTS:
  Inputs are passed using --input flags in several formats:
    --input key=value         Simple key-value pair
    --input key=val1,val2     Multiple values become an array
    --input @file.yaml        Load inputs from a YAML file
    --input @file.json        Load inputs from a JSON file

  Multiple --input flags can be combined. When the same key appears
  in both a file and a flag, the flag value takes precedence.

CAPABILITIES:
  Providers declare capabilities (from, transform, validation, action,
  authentication). By default, the first declared capability is used.
  Use --capability to select a specific one.

PLUGIN PROVIDERS:
  Use --plugin-dir to load plugin providers from a directory.
  Multiple --plugin-dir flags can be specified.

OUTPUT FORMATS:
  json     JSON output (default)
  yaml     YAML output
  table    Table view
  quiet    Suppress output (exit code only)

EXIT CODES:
  0  Success
  1  Provider execution failed
  2  Validation failed (invalid inputs)

Examples:
  # Run the static provider with a simple value
  scafctl run provider static --input value=hello

  # Run the env provider to read an environment variable
  scafctl run provider env --input name=HOME -o json

  # Run the http provider with multiple inputs
  scafctl run provider http --input url=https://api.example.com --input method=GET

  # Run the exec provider to execute a command
  scafctl run provider exec --input command=echo --input args=hello

  # Load inputs from a YAML file
  scafctl run provider http --input @inputs.yaml

  # Run with a specific capability
  scafctl run provider validation --input value=test --capability validation

  # Dry-run to see what would be executed
  scafctl run provider http --input url=https://example.com --dry-run

  # Load plugin providers from a directory
  scafctl run provider echo --input message=hello --plugin-dir ./plugins

  # Show execution metrics
  scafctl run provider http --input url=https://example.com --show-metrics

  # Explore results interactively
  scafctl run provider http --input url=https://api.example.com -i`,
		Args: cobra.ExactArgs(1),
		PreRun: func(_ *cobra.Command, args []string) {
			options.ProviderName = args[0]
		},
		RunE:         makeRunEFunc(cfg, "provider"),
		SilenceUsage: true,
	}

	// Provider input flags
	cCmd.Flags().StringArrayVar(&options.InputParams, "input", nil, "Provider input parameters (key=value or @file.yaml)")
	cCmd.Flags().StringVar(&options.Capability, "capability", "", "Capability to execute (default: first declared capability)")
	cCmd.Flags().BoolVar(&options.DryRun, "dry-run", false, "Show what would be executed without running the provider")
	cCmd.Flags().StringArrayVar(&options.PluginDirs, "plugin-dir", nil, "Directory to scan for plugin providers")
	cCmd.Flags().BoolVar(&options.ShowMetrics, "show-metrics", false, "Show provider execution metrics after completion (output to stderr)")
	cCmd.Flags().BoolVar(&options.Redact, "redact", false, "Redact sensitive fields in output")

	// kvx output flags — default to JSON (not table) since provider output is unstructured
	validFormats := kvx.BaseOutputFormats()
	cCmd.Flags().StringVarP(&options.Output, "output", "o", "json",
		fmt.Sprintf("Output format: %s (default: json)", strings.Join(validFormats, ", ")))
	cCmd.Flags().BoolVarP(&options.Interactive, "interactive", "i", false,
		"Launch interactive viewer to explore results (requires terminal)")
	cCmd.Flags().StringVarP(&options.Expression, "expression", "e", "",
		"CEL expression to filter/transform output data (e.g., '_.data')")

	return cCmd
}

// Run executes a single provider directly
func (o *ProviderOptions) Run(ctx context.Context) error {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("running provider",
		"name", o.ProviderName,
		"capability", o.Capability,
		"dryRun", o.DryRun,
		"output", o.Output,
		"pluginDirs", o.PluginDirs,
		"showMetrics", o.ShowMetrics)

	w := writer.FromContext(ctx)

	// Enable metrics collection if requested
	if o.ShowMetrics {
		provider.GlobalMetrics.Enable()
	}
	defer func() {
		if o.ShowMetrics {
			writeMetrics(o.IOStreams.ErrOut)
		}
	}()

	// Build provider registry
	reg, err := builtin.DefaultRegistry(ctx)
	if err != nil {
		lgr.V(0).Info("warning: failed to register some providers", "error", err)
		reg = provider.GetGlobalRegistry()
	}

	// Load plugin providers if requested
	if len(o.PluginDirs) > 0 {
		lgr.V(1).Info("loading plugin providers", "dirs", o.PluginDirs)
		if err := plugin.RegisterPluginProviders(reg, o.PluginDirs); err != nil {
			w.Warningf("failed to load some plugins: %v", err)
		}
	}

	// Look up the provider
	prov, ok := reg.Get(o.ProviderName)
	if !ok {
		err := fmt.Errorf("provider %q not found (use 'scafctl get providers' to list available providers)", o.ProviderName)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	desc := prov.Descriptor()

	// Parse input parameters
	inputs, err := ParseResolverFlags(o.InputParams)
	if err != nil {
		err := fmt.Errorf("failed to parse input parameters: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	lgr.V(1).Info("parsed inputs", "count", len(inputs))

	// Resolve capability
	capability, err := o.resolveCapability(desc)
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	lgr.V(1).Info("using capability", "capability", capability)

	// Set up execution context
	ctx = provider.WithExecutionMode(ctx, capability)
	ctx = provider.WithDryRun(ctx, o.DryRun)

	// Execute the provider
	start := time.Now()
	result, err := provider.Execute(ctx, prov, inputs)
	elapsed := time.Since(start)

	if err != nil {
		w.Errorf("provider execution failed: %v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	lgr.V(1).Info("provider executed",
		"duration", elapsed.Round(time.Millisecond),
		"dryRun", result.DryRun,
		"hasWarnings", len(result.Output.Warnings) > 0)

	// Build output
	output := o.buildOutput(result, elapsed)

	// Redact sensitive fields if requested
	if o.Redact && len(desc.SensitiveFields) > 0 {
		o.redactSensitiveFields(output, desc.SensitiveFields)
	}

	return o.writeOutput(ctx, output)
}

// resolveCapability determines which capability to use for execution.
// If --capability is specified, validates and uses it.
// Otherwise, defaults to the first declared capability.
func (o *ProviderOptions) resolveCapability(desc *provider.Descriptor) (provider.Capability, error) {
	if o.Capability != "" {
		requested := provider.Capability(o.Capability)
		if !requested.IsValid() {
			return "", fmt.Errorf("invalid capability %q (valid: from, transform, validation, authentication, action)", o.Capability)
		}

		// Check if the provider supports this capability
		for _, c := range desc.Capabilities {
			if c == requested {
				return requested, nil
			}
		}

		capStrs := make([]string, len(desc.Capabilities))
		for i, c := range desc.Capabilities {
			capStrs[i] = string(c)
		}
		return "", fmt.Errorf("provider %q does not support capability %q (supported: %v)", desc.Name, o.Capability, capStrs)
	}

	// Default to first capability
	if len(desc.Capabilities) == 0 {
		return "", fmt.Errorf("provider %q declares no capabilities", desc.Name)
	}

	return desc.Capabilities[0], nil
}

// buildOutput constructs the output map from an execution result
func (o *ProviderOptions) buildOutput(result *provider.ExecutionResult, elapsed time.Duration) map[string]any {
	output := map[string]any{
		"data": result.Output.Data,
	}

	if len(result.Output.Warnings) > 0 {
		output["warnings"] = result.Output.Warnings
	}

	if len(result.Output.Metadata) > 0 {
		output["metadata"] = result.Output.Metadata
	}

	if result.DryRun {
		output["dryRun"] = true
	}

	if o.ShowMetrics {
		output["__execution"] = map[string]any{
			"provider": result.Provider.Descriptor().Name,
			"duration": elapsed.Round(time.Millisecond).String(),
			"dryRun":   result.DryRun,
		}
	}

	return output
}

// redactSensitiveFields replaces sensitive field values with "[REDACTED]" in the output
func (o *ProviderOptions) redactSensitiveFields(output map[string]any, sensitiveFields []string) {
	data, ok := output["data"]
	if !ok {
		return
	}

	dataMap, ok := data.(map[string]any)
	if !ok {
		return
	}

	for _, field := range sensitiveFields {
		if _, exists := dataMap[field]; exists {
			dataMap[field] = "[REDACTED]"
		}
	}
}

// writeOutput writes provider output using kvx
func (o *ProviderOptions) writeOutput(ctx context.Context, output map[string]any) error {
	kvxOpts := flags.NewKvxOutputOptionsFromFlags(
		o.Output,
		o.Interactive,
		o.Expression,
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(o.CliParams.NoColor),
		kvx.WithOutputAppName("scafctl run provider"),
		kvx.WithOutputHelp("scafctl run provider", []string{
			"Provider Output Viewer",
			"",
			"Navigate: ↑↓ arrows | Back: ← | Enter: →",
			"Search: / or F3 | Expression: F6",
			"Copy path: F5 | Quit: q or F10",
		}),
	)
	kvxOpts.IOStreams = o.IOStreams

	return kvxOpts.Write(output)
}
