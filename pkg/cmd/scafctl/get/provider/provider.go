// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	_ "embed"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	provdetail "github.com/oakwood-commons/scafctl/pkg/provider/detail"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

//go:embed provider_schema.json
var providerSchemaJSON []byte

// Options holds configuration for the get provider command
type Options struct {
	BinaryName string
	IOStreams  *terminal.IOStreams
	CliParams  *settings.Run

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
		Long: strings.ReplaceAll(`List all registered providers or get details about a specific provider.

Without arguments, prints a simple list of provider names and descriptions.
Use -i/--interactive to launch a rich kvx TUI for browsing, filtering, and
viewing provider details using a card-list and sectioned detail view.

With a provider name argument, shows detailed information about that provider
including its full schema, examples, and configuration options.

OUTPUT FORMATS:
  (default) Simple list of provider names and descriptions
  table     Table view with key information
  json      Full provider information as JSON
  yaml      Full provider information as YAML
  quiet     Provider names only (one per line)

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
  scafctl get provider http -o json`, settings.CliBinaryName, cliParams.BinaryName),
		RunE: func(cCmd *cobra.Command, args []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Use)
			ctx := settings.IntoContext(cCmd.Context(), cliParams)

			options.IOStreams = ioStreams
			options.CliParams = cliParams
			options.BinaryName = cliParams.BinaryName

			if len(args) > 0 {
				return options.RunGetProvider(ctx, args[0])
			}
			return options.RunListProviders(ctx)
		},
		SilenceUsage: true,
	}

	// Add kvx output flags (-o, -i, -e, -w)
	flags.AddKvxOutputFlagsToStruct(cCmd, &options.KvxOutputFlags)

	// Filter flags
	cCmd.Flags().StringVar(&options.Capability, "capability", "", "Filter by capability (from, transform, validation, action)")
	cCmd.Flags().StringVar(&options.Category, "category", "", "Filter by category")

	return cCmd
}

// Summary is the table-friendly output for provider listing.
type Summary struct {
	Name         string `json:"name" yaml:"name" required:"true"`
	DisplayName  string `json:"displayName" yaml:"displayName"`
	Version      string `json:"version" yaml:"version"`
	Source       string `json:"source" yaml:"source"`
	Category     string `json:"category" yaml:"category"`
	Capabilities string `json:"capabilities" yaml:"capabilities"`
	Description  string `json:"description" yaml:"description"`
	Deprecated   bool   `json:"deprecated" yaml:"deprecated"`
	Beta         bool   `json:"beta" yaml:"beta"`
}

// RunListProviders lists all providers
func (o *Options) RunListProviders(ctx context.Context) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	if o.BinaryName == "" {
		o.BinaryName = settings.CliBinaryName
	}

	reg := o.getRegistry(ctx)
	providers := reg.ListProviders()
	lgr.V(1).Info("loaded built-in provider registry", "count", len(providers))
	if w != nil {
		w.Verbosef("Found %d built-in providers", len(providers))
	}

	// Apply filters
	filtered := o.filterProviders(providers)
	lgr.V(2).Info("applied filters to built-in providers",
		"capability", o.Capability,
		"category", o.Category,
		"before", len(providers),
		"after", len(filtered),
	)
	if w != nil && (o.Capability != "" || o.Category != "") {
		w.Verbosef("After filtering (capability=%q, category=%q): %d providers", o.Capability, o.Category, len(filtered))
	}

	// Build structured output for kvx
	output := make([]Summary, 0, len(filtered))
	for _, p := range filtered {
		desc := p.Descriptor()
		lgr.V(3).Info("including built-in provider", "name", desc.Name)
		output = append(output, Summary{
			Name:         desc.Name,
			DisplayName:  desc.DisplayName,
			Version:      desc.Version.String(),
			Source:       "builtin",
			Description:  desc.Description,
			Capabilities: strings.Join(CapabilitiesToStrings(desc.Capabilities), ", "),
			Category:     desc.Category,
			Deprecated:   desc.IsDeprecated,
			Beta:         desc.Beta,
		})
	}

	// Include official providers only when no filters are active
	// (official providers don't expose capability/category metadata).
	if o.Capability == "" && o.Category == "" {
		output = o.appendOfficialProviders(ctx, output)
	}

	if w != nil {
		w.Verbosef("Total providers: %d", len(output))
	}
	lgr.V(1).Info("total providers to display", "count", len(output))

	return o.writeOutput(ctx, output)
}

// RunGetProvider gets details about a specific provider
func (o *Options) RunGetProvider(ctx context.Context, name string) error {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("looking up provider", "name", name)

	reg := o.getRegistry(ctx)
	p, ok := reg.Get(name)
	if !ok {
		lgr.V(1).Info("provider not found in built-in registry, checking official registry", "name", name)
		// Check if it's an official provider
		if officialReg := official.RegistryFromContext(ctx); officialReg != nil {
			if op, found := officialReg.Get(name); found {
				lgr.V(1).Info("found provider in official registry", "name", name, "catalogRef", op.CatalogRef)

				// Structured output for -o flag (including quiet) or interactive mode.
				// Quiet mode is handled by kvx (produces no output), consistent with built-in providers.
				if (o.Output != "" && o.Output != "auto") || o.Interactive {
					detail := official.Detail(op)
					return o.writeOutput(ctx, detail)
				}

				// Default: human-readable text
				w := writer.FromContext(ctx)
				if w != nil {
					w.Plainlnf("Provider %q is an official plugin provider (catalog: %s, version: %s).\n"+
						"It is auto-fetched on first use in a solution. Use 'plugins install' to pre-fetch.", name, op.CatalogRef, op.DefaultVersion)
				}
				return nil
			}
			lgr.V(1).Info("provider not found in official registry either", "name", name)
		} else {
			lgr.V(1).Info("official registry not available in context")
		}

		err := fmt.Errorf("provider %q not found", name)
		if w := writer.FromContext(ctx); w != nil {
			w.Errorf("%v", err)
		}
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	lgr.V(1).Info("found provider in built-in registry", "name", name)
	desc := p.Descriptor()

	// Default: custom formatted view (unless -o is specified)
	if (o.Output == "auto" || o.Output == "") && !o.Interactive {
		return o.printProviderDetail(ctx, desc)
	}

	// Structured output for -o flag
	output := BuildProviderDetail(*desc)
	output["source"] = "builtin"
	return o.writeOutput(ctx, output)
}

// printProviderDetail prints a nicely formatted provider detail view
func (o *Options) printProviderDetail(ctx context.Context, desc *provider.Descriptor) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return nil
	}
	noColor := w.NoColor()

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
	w.Plainlnf("%s %s", keyStyle("Name:"), valueStyle(desc.Name))
	if desc.DisplayName != "" && desc.DisplayName != desc.Name {
		w.Plainlnf("%s %s", keyStyle("Display Name:"), valueStyle(desc.DisplayName))
	}
	w.Plainlnf("%s %s", keyStyle("Version:"), valueStyle(desc.Version.String()))
	w.Plainlnf("%s %s", keyStyle("API Version:"), valueStyle(desc.APIVersion))
	w.Plainln("")

	// Description
	w.Plainln(keyStyle("Description:"))
	w.Plainlnf("  %s\n", valueStyle(desc.Description))

	// Capabilities
	w.Plainf("%s ", keyStyle("Capabilities:"))
	caps := make([]string, 0, len(desc.Capabilities))
	for _, cap := range desc.Capabilities {
		caps = append(caps, capStyle(string(cap)))
	}
	w.Plainln(strings.Join(caps, " "))

	// Status flags
	if desc.Beta {
		w.Plainln(warnStyle("⚠ This provider is in BETA"))
	}
	if desc.IsDeprecated {
		w.Plainln(errorStyle("⚠ This provider is DEPRECATED"))
	}
	w.Plainln("")

	// Category/Tags
	if desc.Category != "" {
		w.Plainlnf("%s %s", keyStyle("Category:"), valueStyle(desc.Category))
	}
	if len(desc.Tags) > 0 {
		w.Plainlnf("%s %s", keyStyle("Tags:"), valueStyle(strings.Join(desc.Tags, ", ")))
	}

	// Schema properties
	if desc.Schema != nil && len(desc.Schema.Properties) > 0 {
		// Build required set
		requiredSet := make(map[string]bool, len(desc.Schema.Required))
		for _, name := range desc.Schema.Required {
			requiredSet[name] = true
		}
		w.Plainln("")
		w.Plainln(keyStyle("Schema Properties:"))
		for name, prop := range desc.Schema.Properties {
			required := ""
			if requiredSet[name] {
				required = warnStyle(" *")
			}
			typeStr := prop.Type
			if typeStr == "" {
				typeStr = "any"
			}
			w.Plainlnf("  %s %s%s", keyStyle(name), dimStyle("("+typeStr+")"), required)
			if prop.Description != "" {
				w.Plainlnf("    %s", dimStyle(prop.Description))
			}
			if prop.Default != nil {
				w.Plainlnf("    %s %s", dimStyle("Default:"), string(prop.Default))
			}
			if len(prop.Enum) > 0 {
				enumStrs := make([]string, len(prop.Enum))
				for i, v := range prop.Enum {
					enumStrs[i] = fmt.Sprintf("%v", v)
				}
				w.Plainlnf("    %s %s", dimStyle("Enum:"), strings.Join(enumStrs, ", "))
			}
		}
	}

	// Output schemas
	if len(desc.OutputSchemas) > 0 {
		w.Plainln("")
		w.Plainln(keyStyle("Output Schemas:"))
		for cap, schema := range desc.OutputSchemas {
			w.Plainlnf("  %s", capStyle(string(cap)))
			if schema != nil {
				for name, prop := range schema.Properties {
					typeStr := prop.Type
					if typeStr == "" {
						typeStr = "any"
					}
					w.Plainlnf("    %s %s", name, dimStyle("("+typeStr+")"))
					if prop.Description != "" {
						w.Plainlnf("      %s", dimStyle(prop.Description))
					}
				}
			}
		}
	}

	// Examples
	if len(desc.Examples) > 0 {
		w.Plainln("")
		w.Plainln(keyStyle("Examples:"))
		for _, ex := range desc.Examples {
			w.Plainlnf("  %s", keyStyle(ex.Name))
			if ex.Description != "" {
				w.Plainlnf("    %s", dimStyle(ex.Description))
			}
			if ex.YAML != "" {
				w.Plainln("    ---")
				for _, line := range strings.Split(ex.YAML, "\n") {
					if line != "" {
						w.Plainlnf("    %s", line)
					}
				}
			}
		}
	}

	// CLI Usage examples (auto-generated from schema)
	cliExamples := GenerateCLIExamples(desc)
	if len(cliExamples) > 0 {
		w.Plainln("")
		w.Plainln(keyStyle("CLI Usage:"))
		for _, example := range cliExamples {
			w.Plainlnf("  %s", dimStyle(example))
		}
	}

	// Links
	if len(desc.Links) > 0 {
		w.Plainln("")
		w.Plainln(keyStyle("Links:"))
		for _, link := range desc.Links {
			w.Plainlnf("  %s: %s", link.Name, link.URL)
		}
	}

	// Maintainers
	if len(desc.Maintainers) > 0 {
		w.Plainln("")
		w.Plainln(keyStyle("Maintainers:"))
		for _, m := range desc.Maintainers {
			w.Plainlnf("  %s <%s>", m.Name, m.Email)
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

// BuildProviderDetail delegates to pkg/provider/detail.BuildProviderDetail.
func BuildProviderDetail(desc provider.Descriptor) map[string]any {
	return provdetail.BuildProviderDetail(desc)
}

// GenerateCLIExamples delegates to pkg/provider/detail.GenerateCLIExamples.
func GenerateCLIExamples(desc *provider.Descriptor) []string {
	return provdetail.GenerateCLIExamples(desc)
}

// SchemaPlaceholder delegates to pkg/provider/detail.SchemaPlaceholder.
func SchemaPlaceholder(name string, prop *jsonschema.Schema) string {
	return provdetail.SchemaPlaceholder(name, prop)
}

// BuildSchemaOutput delegates to pkg/provider/detail.BuildSchemaOutput.
func BuildSchemaOutput(schema *jsonschema.Schema) map[string]any {
	return provdetail.BuildSchemaOutput(schema)
}

// CapabilitiesToStrings delegates to pkg/provider/detail.CapabilitiesToStrings.
func CapabilitiesToStrings(caps []provider.Capability) []string {
	return provdetail.CapabilitiesToStrings(caps)
}

// getRegistry returns the provider registry
func (o *Options) getRegistry(ctx context.Context) *provider.Registry {
	lgr := logger.FromContext(ctx)

	if o.registry != nil {
		lgr.V(2).Info("using injected test registry")
		return o.registry
	}

	reg, err := builtin.DefaultRegistry(ctx)
	if err != nil {
		lgr.V(1).Info("failed to load default registry, falling back to global", "error", err)
		return provider.GetGlobalRegistry()
	}
	lgr.V(2).Info("using default built-in registry")
	return reg
}

// appendOfficialProviders appends official plugin providers from context to the output slice.
func (o *Options) appendOfficialProviders(ctx context.Context, output []Summary) []Summary {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	items := official.ListItems(ctx)
	if items == nil {
		lgr.V(1).Info("official provider registry not available in context")
		if w != nil {
			w.Verbosef("Official provider registry not available (check config: settings.disableOfficialProviders)")
		}
		return output
	}

	lgr.V(1).Info("loaded official provider registry", "count", len(items))
	if w != nil {
		w.Verbosef("Found %d official plugin providers (auto-fetched on first use)", len(items))
	}
	for _, item := range items {
		lgr.V(3).Info("including official provider", "name", item.Name)
		output = append(output, Summary{
			Name:        item.Name,
			DisplayName: item.DisplayName,
			Version:     item.Version,
			Source:      item.Source,
			Description: item.Description,
		})
	}
	return output
}

// writeOutput writes the output using kvx
func (o *Options) writeOutput(ctx context.Context, data any) error {
	// Use the shared kvx output infrastructure with display schema for rich TUI rendering
	kvxOpts := flags.ToKvxOutputOptions(&o.KvxOutputFlags,
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(o.CliParams.NoColor),
		kvx.WithOutputAppName(o.BinaryName+" get provider"),
		kvx.WithOutputDisplaySchemaJSON(providerSchemaJSON),
		kvx.WithIOStreams(o.IOStreams),
		kvx.WithOutputColumnOrder([]string{"name", "source", "description"}),
		kvx.WithOutputColumnHints(map[string]tui.ColumnHint{
			"name":         {MaxWidth: 20, Priority: 10},
			"source":       {MaxWidth: 10, Priority: 8},
			"displayName":  {Hidden: true},
			"version":      {Hidden: true},
			"category":     {Hidden: true},
			"capabilities": {Hidden: true},
			"deprecated":   {Hidden: true},
			"beta":         {Hidden: true},
		}),
	)

	return kvxOpts.Write(data)
}
