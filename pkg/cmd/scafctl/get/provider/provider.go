// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// Options holds configuration for the get provider command
type Options struct {
	IOStreams *terminal.IOStreams
	CliParams *settings.Run

	// kvx output integration
	flags.KvxOutputFlags

	// Filter options
	Capability string // Filter by capability
	Category   string // Filter by category

	// For dependency injection in tests
	registry *provider.Registry
}

// CommandProvider creates the 'get provider' subcommand
func CommandProvider(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	options := &Options{}

	cCmd := &cobra.Command{
		Use:     "provider [name]",
		Aliases: []string{"providers", "prov", "p"},
		Short:   "List or get provider information",
		Long: `List all registered providers or get details about a specific provider.

Without arguments, prints a simple list of provider names and descriptions.
Use -i/--interactive to launch a TUI for browsing, filtering, and viewing
provider details.

With a provider name argument, shows detailed information about that provider
including its full schema, examples, and configuration options.

OUTPUT FORMATS:
  (default) Simple list of provider names and descriptions
  table     Table view with key information
  json      Full provider information as JSON
  yaml      Full provider information as YAML
  quiet     Provider names only (one per line)

INTERACTIVE MODE (-i):
  ↑↓         Navigate provider list
  →/enter    View provider details
  ←/esc      Go back to list
  /          Filter providers
  c          Copy example YAML to clipboard
  q          Quit

FILTERS:
  --capability  Filter providers by supported capability (from, transform, validation, action)
  --category    Filter providers by category (network, storage, security, etc.)

Examples:
  # List all providers
  scafctl get providers

  # Browse providers interactively
  scafctl get providers -i

  # List providers in table format
  scafctl get providers -o table

  # List providers supporting transform capability
  scafctl get providers --capability=transform

  # Get details about a specific provider
  scafctl get provider http

  # Get provider details as JSON
  scafctl get provider http -o json`,
		RunE: func(cCmd *cobra.Command, args []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Use)
			ctx := settings.IntoContext(context.Background(), cliParams)

			options.IOStreams = ioStreams
			options.CliParams = cliParams

			if len(args) > 0 {
				return options.RunGetProvider(ctx, args[0])
			}
			return options.RunListProviders(ctx)
		},
		SilenceUsage: true,
	}

	// Add output flags - default is simple list, -i launches custom TUI
	validFormats := []string{"table", "json", "yaml", "quiet"}
	cCmd.Flags().StringVarP(&options.Output, "output", "o", "",
		fmt.Sprintf("Output format: %s", strings.Join(validFormats, ", ")))
	cCmd.Flags().BoolVarP(&options.Interactive, "interactive", "i", false,
		"Launch interactive TUI for browsing providers")
	cCmd.Flags().StringVarP(&options.Expression, "expression", "e", "",
		"CEL expression to filter/transform output data")

	// Filter flags
	cCmd.Flags().StringVar(&options.Capability, "capability", "", "Filter by capability (from, transform, validation, action)")
	cCmd.Flags().StringVar(&options.Category, "category", "", "Filter by category")

	return cCmd
}

// RunListProviders lists all providers
func (o *Options) RunListProviders(ctx context.Context) error {
	reg := o.getRegistry(ctx)
	providers := reg.ListProviders()

	// Apply filters
	filtered := o.filterProviders(providers)

	// Interactive mode (-i): launch custom TUI
	if o.Interactive {
		if !kvx.IsTerminal(o.IOStreams.Out) {
			err := fmt.Errorf("interactive mode requires a terminal")
			if w := writer.FromContext(ctx); w != nil {
				w.Errorf("%v", err)
			}
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
		return RunTUI(filtered, o.IOStreams.Out)
	}

	// Default (no -o flag): simple list
	if o.Output == "" {
		return printSimpleList(filtered, o.IOStreams.Out)
	}

	// Build output data for explicit output formats (-o table/json/yaml/quiet)
	output := make([]map[string]any, 0, len(filtered))
	for _, p := range filtered {
		desc := p.Descriptor()
		output = append(output, map[string]any{
			"name":         desc.Name,
			"displayName":  desc.DisplayName,
			"version":      desc.Version.String(),
			"description":  desc.Description,
			"capabilities": capabilitiesToStrings(desc.Capabilities),
			"category":     desc.Category,
			"deprecated":   desc.Deprecated, //nolint:staticcheck // Intentionally display deprecated status
			"beta":         desc.Beta,
		})
	}

	return o.writeOutput(ctx, output)
}

// RunGetProvider gets details about a specific provider
func (o *Options) RunGetProvider(ctx context.Context, name string) error {
	reg := o.getRegistry(ctx)
	p, ok := reg.Get(name)
	if !ok {
		err := fmt.Errorf("provider %q not found", name)
		if w := writer.FromContext(ctx); w != nil {
			w.Errorf("%v", err)
		}
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	desc := p.Descriptor()

	// Default: custom formatted view (unless -o is specified)
	if o.Output == "" && !o.Interactive {
		return o.printProviderDetail(desc)
	}

	// Structured output for -o flag
	output := buildProviderDetail(*desc)
	return o.writeOutput(ctx, output)
}

// printProviderDetail prints a nicely formatted provider detail view
func (o *Options) printProviderDetail(desc *provider.Descriptor) error {
	out := o.IOStreams.Out
	noColor := o.CliParams.NoColor

	// Style helpers
	keyStyle := func(s string) string {
		if noColor {
			return s
		}
		return "\033[1;94m" + s + "\033[0m" // Bold blue
	}
	valueStyle := func(s string) string {
		return s
	}
	capStyle := func(s string) string {
		if noColor {
			return "[" + s + "]"
		}
		return "\033[38;5;85;48;5;235m " + s + " \033[0m" // Green on dark bg
	}
	warnStyle := func(s string) string {
		if noColor {
			return s
		}
		return "\033[1;33m" + s + "\033[0m" // Bold yellow
	}
	errorStyle := func(s string) string {
		if noColor {
			return s
		}
		return "\033[1;31m" + s + "\033[0m" // Bold red
	}
	dimStyle := func(s string) string {
		if noColor {
			return s
		}
		return "\033[2m" + s + "\033[0m" // Dim
	}

	// Header
	fmt.Fprintf(out, "%s %s\n", keyStyle("Name:"), valueStyle(desc.Name))
	if desc.DisplayName != "" && desc.DisplayName != desc.Name {
		fmt.Fprintf(out, "%s %s\n", keyStyle("Display Name:"), valueStyle(desc.DisplayName))
	}
	fmt.Fprintf(out, "%s %s\n", keyStyle("Version:"), valueStyle(desc.Version.String()))
	fmt.Fprintf(out, "%s %s\n", keyStyle("API Version:"), valueStyle(desc.APIVersion))
	fmt.Fprintln(out)

	// Description
	fmt.Fprintf(out, "%s\n", keyStyle("Description:"))
	fmt.Fprintf(out, "  %s\n\n", valueStyle(desc.Description))

	// Capabilities
	fmt.Fprintf(out, "%s ", keyStyle("Capabilities:"))
	caps := make([]string, 0, len(desc.Capabilities))
	for _, cap := range desc.Capabilities {
		caps = append(caps, capStyle(string(cap)))
	}
	fmt.Fprintln(out, strings.Join(caps, " "))

	// Status flags
	if desc.Beta {
		fmt.Fprintln(out, warnStyle("⚠ This provider is in BETA"))
	}
	if desc.Deprecated { //nolint:staticcheck // Intentionally showing deprecated status
		fmt.Fprintln(out, errorStyle("⚠ This provider is DEPRECATED"))
	}
	fmt.Fprintln(out)

	// Category/Tags
	if desc.Category != "" {
		fmt.Fprintf(out, "%s %s\n", keyStyle("Category:"), valueStyle(desc.Category))
	}
	if len(desc.Tags) > 0 {
		fmt.Fprintf(out, "%s %s\n", keyStyle("Tags:"), valueStyle(strings.Join(desc.Tags, ", ")))
	}

	// Mock behavior
	if desc.MockBehavior != "" {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "%s\n", keyStyle("Mock Behavior:"))
		fmt.Fprintf(out, "  %s\n", dimStyle(desc.MockBehavior))
	}

	// Schema properties
	if desc.Schema != nil && len(desc.Schema.Properties) > 0 {
		// Build required set
		requiredSet := make(map[string]bool, len(desc.Schema.Required))
		for _, name := range desc.Schema.Required {
			requiredSet[name] = true
		}
		fmt.Fprintln(out)
		fmt.Fprintf(out, "%s\n", keyStyle("Schema Properties:"))
		for name, prop := range desc.Schema.Properties {
			required := ""
			if requiredSet[name] {
				required = warnStyle(" *")
			}
			typeStr := prop.Type
			if typeStr == "" {
				typeStr = "any"
			}
			fmt.Fprintf(out, "  %s %s%s\n", keyStyle(name), dimStyle("("+typeStr+")"), required)
			if prop.Description != "" {
				fmt.Fprintf(out, "    %s\n", dimStyle(prop.Description))
			}
			if prop.Default != nil {
				fmt.Fprintf(out, "    %s %s\n", dimStyle("Default:"), string(prop.Default))
			}
			if len(prop.Enum) > 0 {
				enumStrs := make([]string, len(prop.Enum))
				for i, v := range prop.Enum {
					enumStrs[i] = fmt.Sprintf("%v", v)
				}
				fmt.Fprintf(out, "    %s %s\n", dimStyle("Enum:"), strings.Join(enumStrs, ", "))
			}
		}
	}

	// Output schemas
	if len(desc.OutputSchemas) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "%s\n", keyStyle("Output Schemas:"))
		for cap, schema := range desc.OutputSchemas {
			fmt.Fprintf(out, "  %s\n", capStyle(string(cap)))
			if schema != nil {
				for name, prop := range schema.Properties {
					typeStr := prop.Type
					if typeStr == "" {
						typeStr = "any"
					}
					fmt.Fprintf(out, "    %s %s\n", name, dimStyle("("+typeStr+")"))
					if prop.Description != "" {
						fmt.Fprintf(out, "      %s\n", dimStyle(prop.Description))
					}
				}
			}
		}
	}

	// Examples
	if len(desc.Examples) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "%s\n", keyStyle("Examples:"))
		for _, ex := range desc.Examples {
			fmt.Fprintf(out, "  %s\n", keyStyle(ex.Name))
			if ex.Description != "" {
				fmt.Fprintf(out, "    %s\n", dimStyle(ex.Description))
			}
			if ex.YAML != "" {
				fmt.Fprintln(out, "    ---")
				for _, line := range strings.Split(ex.YAML, "\n") {
					if line != "" {
						fmt.Fprintf(out, "    %s\n", line)
					}
				}
			}
		}
	}

	// Links
	if len(desc.Links) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "%s\n", keyStyle("Links:"))
		for _, link := range desc.Links {
			fmt.Fprintf(out, "  %s: %s\n", link.Name, link.URL)
		}
	}

	// Maintainers
	if len(desc.Maintainers) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "%s\n", keyStyle("Maintainers:"))
		for _, m := range desc.Maintainers {
			fmt.Fprintf(out, "  %s <%s>\n", m.Name, m.Email)
		}
	}

	return nil
}

// filterProviders applies capability and category filters
func (o *Options) filterProviders(providers []provider.Provider) []provider.Provider {
	if o.Capability == "" && o.Category == "" {
		return providers
	}

	var filtered []provider.Provider
	for _, p := range providers {
		desc := p.Descriptor()

		// Check capability filter
		if o.Capability != "" {
			hasCapability := false
			for _, cap := range desc.Capabilities {
				if strings.EqualFold(string(cap), o.Capability) {
					hasCapability = true
					break
				}
			}
			if !hasCapability {
				continue
			}
		}

		// Check category filter
		if o.Category != "" {
			if !strings.EqualFold(desc.Category, o.Category) {
				continue
			}
		}

		filtered = append(filtered, p)
	}

	return filtered
}

// buildProviderDetail builds a detailed map for a single provider
func buildProviderDetail(desc provider.Descriptor) map[string]any {
	output := map[string]any{
		"name":         desc.Name,
		"displayName":  desc.DisplayName,
		"apiVersion":   desc.APIVersion,
		"version":      desc.Version.String(),
		"description":  desc.Description,
		"capabilities": capabilitiesToStrings(desc.Capabilities),
		"mockBehavior": desc.MockBehavior,
	}

	if desc.Category != "" {
		output["category"] = desc.Category
	}
	if len(desc.Tags) > 0 {
		output["tags"] = desc.Tags
	}
	if desc.Icon != "" {
		output["icon"] = desc.Icon
	}
	if desc.Deprecated { //nolint:staticcheck // Intentionally display deprecated status
		output["deprecated"] = true
	}
	if desc.Beta {
		output["beta"] = true
	}

	// Add schema information
	if desc.Schema != nil && len(desc.Schema.Properties) > 0 {
		output["schema"] = buildSchemaOutput(desc.Schema)
	}

	// Add output schemas
	if len(desc.OutputSchemas) > 0 {
		outputSchemas := make(map[string]any)
		for cap, schema := range desc.OutputSchemas {
			outputSchemas[string(cap)] = buildSchemaOutput(schema)
		}
		output["outputSchemas"] = outputSchemas
	}

	// Add links
	if len(desc.Links) > 0 {
		links := make([]map[string]string, 0, len(desc.Links))
		for _, link := range desc.Links {
			links = append(links, map[string]string{
				"name": link.Name,
				"url":  link.URL,
			})
		}
		output["links"] = links
	}

	// Add examples
	if len(desc.Examples) > 0 {
		examples := make([]map[string]any, 0, len(desc.Examples))
		for _, ex := range desc.Examples {
			examples = append(examples, map[string]any{
				"name":        ex.Name,
				"description": ex.Description,
				"yaml":        ex.YAML,
			})
		}
		output["examples"] = examples
	}

	// Add maintainers
	if len(desc.Maintainers) > 0 {
		maintainers := make([]map[string]string, 0, len(desc.Maintainers))
		for _, m := range desc.Maintainers {
			maintainers = append(maintainers, map[string]string{
				"name":  m.Name,
				"email": m.Email,
			})
		}
		output["maintainers"] = maintainers
	}

	return output
}

// buildSchemaOutput converts a JSON Schema to a map for output
func buildSchemaOutput(schema *jsonschema.Schema) map[string]any {
	if schema == nil || len(schema.Properties) == 0 {
		return nil
	}

	// Build required set
	requiredSet := make(map[string]bool, len(schema.Required))
	for _, name := range schema.Required {
		requiredSet[name] = true
	}

	properties := make(map[string]any)
	for name, prop := range schema.Properties {
		propMap := map[string]any{
			"type": prop.Type,
		}
		if prop.Description != "" {
			propMap["description"] = prop.Description
		}
		if requiredSet[name] {
			propMap["required"] = true
		}
		if prop.Default != nil {
			var def any
			_ = json.Unmarshal(prop.Default, &def)
			propMap["default"] = def
		}
		if len(prop.Examples) > 0 {
			propMap["example"] = prop.Examples[0]
		}
		if len(prop.Enum) > 0 {
			propMap["enum"] = prop.Enum
		}
		properties[name] = propMap
	}

	return map[string]any{"properties": properties}
}

// capabilitiesToStrings converts []Capability to []string
func capabilitiesToStrings(caps []provider.Capability) []string {
	result := make([]string, 0, len(caps))
	for _, cap := range caps {
		result = append(result, string(cap))
	}
	return result
}

// getRegistry returns the provider registry
func (o *Options) getRegistry(ctx context.Context) *provider.Registry {
	if o.registry != nil {
		return o.registry
	}

	reg, err := builtin.DefaultRegistry(ctx)
	if err != nil {
		return provider.GetGlobalRegistry()
	}
	return reg
}

// writeOutput writes the output using kvx
func (o *Options) writeOutput(ctx context.Context, data any) error {
	// Handle quiet output specially - just print names
	if o.Output == "quiet" {
		return o.writeQuietOutput(data)
	}

	// Use the shared kvx output infrastructure
	kvxOpts := flags.NewKvxOutputOptionsFromFlags(
		o.Output,
		o.Interactive,
		o.Expression,
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(o.CliParams.NoColor),
		kvx.WithOutputAppName("scafctl get provider"),
		kvx.WithOutputHelp("scafctl get provider", []string{
			"Provider Information Viewer",
			"",
			"Navigate: ↑↓ arrows | Back: ← | Enter: →",
			"Search: / or F3 | Expression: F6",
			"Copy path: F5 | Quit: q or F10",
		}),
	)
	kvxOpts.IOStreams = o.IOStreams

	return kvxOpts.Write(data)
}

// writeQuietOutput prints just the provider names
func (o *Options) writeQuietOutput(data any) error {
	switch v := data.(type) {
	case []map[string]any:
		for _, item := range v {
			if name, ok := item["name"].(string); ok {
				fmt.Fprintln(o.IOStreams.Out, name)
			}
		}
	case map[string]any:
		if name, ok := v["name"].(string); ok {
			fmt.Fprintln(o.IOStreams.Out, name)
		} else {
			// Single provider detail - output as yaml for quiet mode
			data, _ := yaml.Marshal(v)
			fmt.Fprintln(o.IOStreams.Out, string(data))
		}
	default:
		data, _ := json.MarshalIndent(v, "", "  ")
		fmt.Fprintln(o.IOStreams.Out, string(data))
	}
	return nil
}
