// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	cmdflags "github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/flags"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/fileprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/detail"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// ProviderOptions holds configuration for the run provider command
type ProviderOptions struct {
	BinaryName string
	IOStreams  *terminal.IOStreams
	CliParams  *settings.Run

	// kvx output integration
	cmdflags.KvxOutputFlags

	// ProviderName is the name of the provider to execute (positional arg).
	ProviderName string

	// InputParams are the provider input parameters (--input key=value or --input @file.yaml).
	InputParams []string

	// DynamicArgs are provider inputs from positional or dynamic-flag syntax
	// (e.g. key=value or --key=value, captured after the provider name).
	DynamicArgs []string

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

	// OutputDir is the target directory for action file operations.
	// When set and capability is action, providers resolve relative paths
	// against this directory instead of CWD.
	OutputDir string

	// OnConflict is the default conflict strategy for file writes.
	OnConflict string

	// Backup enables .bak backup creation before mutating existing files.
	Backup bool
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
			options.BinaryName = cli.BinaryName
		},
	}

	cCmd := &cobra.Command{
		Use:     "provider <name>",
		Aliases: []string{"prov", "p"},
		Short:   "Execute a single provider directly",
		Long: strings.ReplaceAll(`Execute a provider directly without a solution or resolver file.

This command is useful for testing, debugging, and exploring individual
providers in isolation. It accepts the provider name as a positional
argument and provider inputs via --input flags.

PROVIDER INPUTS:
  Inputs can be passed in two equivalent ways:

  1. Positional key=value (recommended):
       key=value                After the provider name
       @file.yaml               Load inputs from a file

  2. Explicit --input flag:
       --input key=value        Repeatable flag
       --input key=val1,val2    Multiple values become an array
       --input @file.yaml       Load inputs from a YAML file
       --input @file.json       Load inputs from a JSON file

  Both forms can be mixed. When the same key appears multiple
  times, later values override earlier ones (last-wins).

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
  # Positional key=value syntax (recommended)
  scafctl run provider static value=hello
  scafctl run provider http url=https://api.example.com method=GET
  scafctl run provider env name=HOME -o json

  # Explicit --input flag syntax
  scafctl run provider static --input value=hello
  scafctl run provider http --input url=https://api.example.com --input method=GET

  # Mix both forms freely
  scafctl run provider http --input method=GET url=https://example.com timeout=30

  # Load inputs from a YAML file
  scafctl run provider http --input @inputs.yaml
  scafctl run provider http @inputs.yaml

  # Run with a specific capability
  scafctl run provider validation --input value=test --capability validation

  # Dry-run to see what would be executed
  scafctl run provider http url=https://example.com --dry-run

  # Load plugin providers from a directory
  scafctl run provider echo message=hello --plugin-dir ./plugins

  # Show execution metrics
  scafctl run provider http url=https://example.com --show-metrics

  # Explore results interactively
  scafctl run provider http url=https://api.example.com -i`, settings.CliBinaryName, cliParams.BinaryName),
		Args: cobra.MinimumNArgs(1),
		PreRun: func(_ *cobra.Command, args []string) {
			options.ProviderName = args[0]
			options.DynamicArgs = args[1:]
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
	cCmd.Flags().StringVar(&options.OutputDir, "output-dir", "", "Target directory for action file operations (applies when capability=action)")
	cCmd.Flags().StringVar(&options.OnConflict, "on-conflict", "", "Conflict strategy for file writes (error|overwrite|skip|skip-unchanged|append)")
	cCmd.Flags().BoolVar(&options.Backup, "backup", false, "Create .bak backups before mutating existing files")

	// kvx output flags — default to JSON (not table) since provider output is unstructured
	validFormats := kvx.BaseOutputFormats()
	cCmd.Flags().StringVarP(&options.Output, "output", "o", "json",
		fmt.Sprintf("Output format: %s (default: json)", strings.Join(validFormats, ", ")))
	cCmd.Flags().BoolVarP(&options.Interactive, "interactive", "i", false,
		"Launch interactive viewer to explore results (requires terminal)")
	cCmd.Flags().StringVarP(&options.Expression, "expression", "e", "",
		"CEL expression to filter/transform output data (e.g., '_.data')")

	setProviderHelpFunc(cCmd)

	return cCmd
}

// setProviderHelpFunc installs a custom help function that appends dynamic
// provider input documentation when a provider name is given.
// For example, `scafctl run provider http --help` will show the standard
// command help plus the HTTP provider's input parameters with types and descriptions.
func setProviderHelpFunc(cmd *cobra.Command) {
	defaultHelp := cmd.HelpFunc()
	cmd.SetHelpFunc(func(c *cobra.Command, args []string) {
		// Render the default help first
		defaultHelp(c, args)

		// Try to find the provider name from multiple sources.
		// Cobra does not always pass positional args to the help function
		// depending on how flags are parsed, so we check:
		// 1. The remaining non-flag arguments after cobra parsing (most reliable)
		// 2. os.Args as a fallback (scanning after "provider" subcommand)
		providerName := extractProviderName(c.Flags().Args())
		if providerName == "" {
			providerName = extractProviderNameFromOSArgs()
		}

		// Look up the provider in the registry (best effort — don't fail help on errors)
		reg, err := builtin.DefaultRegistry(c.Context())
		if err != nil {
			return
		}

		prov, ok := reg.Get(providerName)
		if !ok {
			return
		}

		helpText := detail.FormatProviderInputHelp(prov.Descriptor())
		if helpText != "" {
			fmt.Fprintln(c.OutOrStdout())
			fmt.Fprint(c.OutOrStdout(), helpText)
		}
	})
}

// extractProviderName finds the provider name from command-line arguments,
// skipping flags and the "help" argument itself.
func extractProviderName(args []string) string {
	for _, arg := range args {
		// Skip flags
		if strings.HasPrefix(arg, "-") {
			continue
		}
		// Skip the "help" argument that cobra sometimes includes
		if arg == "help" {
			continue
		}
		return arg
	}
	return ""
}

// extractProviderNameFromOSArgs scans os.Args for a provider name following
// the "provider" (or alias "prov"/"p") subcommand token. This is used as a
// fallback when cobra's help function doesn't pass positional args.
func extractProviderNameFromOSArgs() string {
	args := os.Args
	found := false
	for _, arg := range args {
		if found {
			// Skip flags after "provider"
			if strings.HasPrefix(arg, "-") {
				continue
			}
			return arg
		}
		if arg == "provider" || arg == "prov" || arg == "p" {
			found = true
		}
	}
	return ""
}

// Run executes a single provider directly
func (o *ProviderOptions) Run(ctx context.Context) error {
	if o.BinaryName == "" {
		o.BinaryName = settings.CliBinaryName
	}

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
			writeMetrics(ctx)
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
		err := fmt.Errorf("provider %q not found (use '%s get providers' to list available providers)", o.ProviderName, o.BinaryName)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	desc := prov.Descriptor()

	// Parse dynamic arguments (--key=value and key=value from argv)
	extraParsed, err := flags.ParseDynamicInputArgs(o.DynamicArgs)
	if err != nil {
		err := fmt.Errorf("failed to parse input arguments: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Merge: --input values first, then dynamic args (last-wins on conflict)
	allParams := make([]string, 0, len(o.InputParams)+len(extraParsed))
	allParams = append(allParams, o.InputParams...)
	allParams = append(allParams, extraParsed...)

	// Parse input parameters (pass stdin for @- support)
	var stdinReader io.Reader
	if o.IOStreams != nil {
		stdinReader = o.IOStreams.In
	}
	inputs, err := flags.ParseResolverFlagsWithStdin(allParams, stdinReader)
	if err != nil {
		err := fmt.Errorf("failed to parse input parameters: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	lgr.V(1).Info("parsed inputs", "count", len(inputs))

	// Validate input keys against provider schema (early typo detection)
	if desc.Schema != nil && len(desc.Schema.Properties) > 0 {
		validKeys := make([]string, 0, len(desc.Schema.Properties))
		for k := range desc.Schema.Properties {
			validKeys = append(validKeys, k)
		}
		if err := flags.ValidateInputKeys(inputs, validKeys, fmt.Sprintf("provider %q", desc.Name)); err != nil {
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
	}

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

	// Inject IOStreams so streaming providers (exec, message, etc.) can write to the terminal.
	// For structured output modes (json/yaml), route provider stdout to stderr to
	// avoid corrupting the structured envelope that kvx writes to stdout.
	// For quiet mode, discard all provider output to honour the --quiet contract.
	if o.IOStreams != nil {
		providerOut := o.IOStreams.Out
		providerErr := o.IOStreams.ErrOut
		switch strings.ToLower(o.Output) {
		case "json", "yaml":
			providerOut = o.IOStreams.ErrOut
		case "quiet":
			providerOut = io.Discard
			providerErr = io.Discard
		}
		ctx = provider.WithIOStreams(ctx, &provider.IOStreams{
			Out:    providerOut,
			ErrOut: providerErr,
		})
	}

	// Inject conflict strategy and backup into context for file providers
	if o.OnConflict != "" {
		if !fileprovider.ConflictStrategy(o.OnConflict).IsValid() {
			err := fmt.Errorf("invalid --on-conflict value %q (valid: error, overwrite, skip, skip-unchanged, append)", o.OnConflict)
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
		ctx = provider.WithConflictStrategy(ctx, o.OnConflict)
	}
	if o.Backup {
		ctx = provider.WithBackup(ctx, true)
	}

	// Set output directory for action capabilities.
	// In dry-run mode, resolve the path without creating the directory.
	if o.OutputDir != "" && capability == provider.CapabilityAction {
		absDir, err := provider.AbsFromContext(ctx, o.OutputDir)
		if err != nil {
			w.Errorf("failed to resolve output directory %q: %v", o.OutputDir, err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
		if !o.DryRun {
			if err := os.MkdirAll(absDir, 0o755); err != nil {
				w.Errorf("failed to create output directory %q: %v", absDir, err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}
		}
		ctx = provider.WithOutputDirectory(ctx, absDir)
	}

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
	kvxOpts := cmdflags.NewKvxOutputOptionsFromFlags(
		o.Output,
		o.Interactive,
		o.Expression,
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(o.CliParams.NoColor),
		kvx.WithOutputAppName(o.BinaryName+" run provider"),
		kvx.WithOutputHelp(o.BinaryName+" run provider", []string{
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
