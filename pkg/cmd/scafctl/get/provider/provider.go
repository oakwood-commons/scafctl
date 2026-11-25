// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	_ "embed"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
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
	Operation  string // Show details for a single operation of a named provider

	// For dependency injection in tests
	registry *provider.Registry
}

// CommandProvider creates the 'get provider' subcommand
func CommandProvider(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	options := &Options{}
	binName := cliParams.BinaryName
	if binName == "" {
		binName = settings.CliBinaryName
	}

	cCmd := &cobra.Command{
		Use:     "provider [name]",
		Aliases: []string{"providers", "prov", "p"},
		Short:   "List or get provider information",
		Long: strings.ReplaceAll(`List all registered providers or get details about a specific provider.

Without arguments, prints a simple list of provider names and descriptions.
Use -i/--interactive to launch a rich kvx TUI for browsing, filtering, and
viewing provider details using a card-list and sectioned detail view.

With a provider name argument, shows detailed information about that provider
including its operations, full schema, examples, and configuration options.
Use --operation <op> with a provider name to inspect a single operation's
description, capabilities, write classification, input/output schemas, and
examples.

OUTPUT FORMATS:
  (default) Simple list of provider names and descriptions
  table     Table view with key information
  json      Full provider information as JSON
  yaml      Full provider information as YAML
  quiet     Provider names only (one per line)

FILTERS:
  --capability  Filter providers by supported capability (from, transform, validation, action)
  --category    Filter providers by category (network, storage, security, etc.)
  --operation   Show details for a single operation (requires a provider name)

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

  # Inspect a single operation on an operation-based provider
  scafctl get provider github --operation create_issue

  # Get provider details as JSON
  scafctl get provider http -o json`, settings.CliBinaryName, binName),
		RunE: func(cCmd *cobra.Command, args []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Use)
			ctx := settings.IntoContext(cCmd.Context(), cliParams)

			options.IOStreams = ioStreams
			options.CliParams = cliParams
			options.BinaryName = cliParams.BinaryName

			if len(args) > 0 {
				return options.RunGetProvider(ctx, args[0])
			}
			if options.Operation != "" {
				err := fmt.Errorf("--operation requires a provider name argument (e.g., '%s get provider <name> --operation %s')", binName, options.Operation)
				if w := writer.FromContext(ctx); w != nil {
					w.Errorf("%v", err)
				}
				return exitcode.WithCode(err, exitcode.ValidationFailed)
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
	cCmd.Flags().StringVar(&options.Operation, "operation", "", "Show details for a single operation (requires a provider name)")

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
		output = o.appendCachedPlugins(ctx, output)
	}

	if w != nil {
		w.Verbosef("Total providers: %d", len(output))
	}
	lgr.V(1).Info("total providers to display", "count", len(output))

	return o.writeOutput(ctx, output)
}

// RunGetProvider gets details about a specific provider
func (o *Options) RunGetProvider(ctx context.Context, name string) error {
	if o.BinaryName == "" {
		o.BinaryName = settings.CliBinaryName
	}

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

				// Attempt to load full schema from cached binary.
				var cachedProbeErr error
				cache := plugin.NewCache(settings.PluginCacheDirFor(o.BinaryName))
				if binPath, version, cached := cache.GetLatestBinary(op.CatalogRef); cached {
					lgr.V(1).Info("probing cached official plugin for full schema", "name", name, "catalogRef", op.CatalogRef, "path", binPath)
					desc, probeErr := plugin.ProbePluginDescriptor(ctx, binPath, name)
					if probeErr == nil {
						return o.renderResolvedDescriptor(ctx, desc, "official", map[string]any{
							"version": version,
						})
					}
					cachedProbeErr = probeErr
					lgr.V(1).Info("failed to probe official plugin descriptor, falling back to minimal metadata", "name", name, "error", probeErr)
				}

				if o.Operation != "" {
					var err error
					if cachedProbeErr != nil {
						err = fmt.Errorf("cannot inspect operation %q for official provider %q: failed to load cached plugin descriptor: %w",
							o.Operation, name, cachedProbeErr)
					} else {
						err = fmt.Errorf("cannot inspect operation %q for official provider %q: plugin is not cached locally (run '%s plugins install %s' first)",
							o.Operation, name, o.BinaryName, op.CatalogRef)
					}
					if w := writer.FromContext(ctx); w != nil {
						w.Errorf("%s", provdetail.SanitizeInlineText(err.Error()))
					}
					return exitcode.WithCode(err, exitcode.FileNotFound)
				}

				// Fallback: minimal metadata with hint.
				if !o.useDefaultDetailView() {
					detail := official.Detail(op)
					detail["hint"] = fmt.Sprintf("Run '%s plugins install %s' to fetch the plugin and view full schema", o.BinaryName, op.CatalogRef)
					return o.writeDetailOutput(ctx, detail)
				}

				// Default: human-readable text
				w := writer.FromContext(ctx)
				if w != nil {
					w.Plainlnf("Provider %q is an official plugin provider (catalog: %s, version: %s).\n"+
						"It is auto-fetched on first use in a solution. Use '%s plugins install %s' to pre-fetch and view full schema.",
						name, op.CatalogRef, op.DefaultVersion, o.BinaryName, op.CatalogRef)
				}
				return nil
			}
			lgr.V(1).Info("provider not found in official registry either", "name", name)
		} else {
			lgr.V(1).Info("official registry not available in context")
		}

		// Fallback: check the local plugin cache.
		cache := plugin.NewCache(settings.PluginCacheDirFor(o.BinaryName))
		if binPath, version, found := cache.GetLatestBinary(name); found {
			// Probe the cached binary for its full descriptor.
			lgr.V(1).Info("probing cached plugin for full schema", "name", name, "path", binPath)
			if desc, probeErr := plugin.ProbePluginDescriptor(ctx, binPath, name); probeErr == nil {
				lgr.V(1).Info("found provider in plugin cache with full descriptor", "name", name, "version", version)
				return o.renderResolvedDescriptor(ctx, desc, "local", map[string]any{
					"version": version,
					"path":    binPath,
				})
			}
			lgr.V(1).Info("cached binary failed full probe", "name", name, "path", binPath)

			// Validate the cached binary at least exposes providers before reporting it.
			if _, ok := plugin.ProbePluginDescription(ctx, binPath, name); !ok {
				lgr.V(1).Info("cached binary is not a valid provider plugin", "name", name, "path", binPath)
			} else {
				if o.Operation != "" {
					err := fmt.Errorf("cannot inspect operation %q for cached plugin provider %q: full descriptor is unavailable", o.Operation, name)
					if w := writer.FromContext(ctx); w != nil {
						w.Errorf("%s", provdetail.SanitizeInlineText(err.Error()))
					}
					return exitcode.WithCode(err, exitcode.FileNotFound)
				}
				lgr.V(1).Info("found provider in plugin cache (minimal info)", "name", name, "version", version, "path", binPath)
				w := writer.FromContext(ctx)
				cachedDetail := map[string]any{
					"name":    name,
					"version": version,
					"source":  "local",
					"path":    binPath,
				}
				if !o.useDefaultDetailView() {
					return o.writeDetailOutput(ctx, cachedDetail)
				}
				if w != nil {
					w.Plainlnf("Provider %q is a locally cached plugin provider (version: %s).\n"+
						"Binary: %s\n"+
						"Use '%s run provider %s' to execute it directly.", name, version, binPath, o.BinaryName, name)
				}
				return nil
			}
		}

		err := fmt.Errorf("provider %q not found", name)
		if w := writer.FromContext(ctx); w != nil {
			w.Errorf("%v", err)
		}
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	lgr.V(1).Info("found provider in built-in registry", "name", name)
	return o.renderResolvedDescriptor(ctx, p.Descriptor(), "builtin", nil)
}

// useDefaultDetailView reports whether human-formatted detail text should be
// rendered rather than routing through kvx structured/expression output.
func (o *Options) useDefaultDetailView() bool {
	return (o.Output == "auto" || o.Output == "") && !o.Interactive && o.Expression == "" && o.Where == ""
}

// renderResolvedDescriptor renders either a single operation (--operation) or
// the full provider descriptor in human or structured kvx format.
func (o *Options) renderResolvedDescriptor(ctx context.Context, desc *provider.Descriptor, source string, extra map[string]any) error {
	if o.Operation != "" {
		if len(desc.Operations) == 0 {
			err := fmt.Errorf("provider %q does not declare per-operation metadata", desc.Name)
			if w := writer.FromContext(ctx); w != nil {
				w.Errorf("%s", provdetail.SanitizeInlineText(err.Error()))
			}
			return exitcode.WithCode(err, exitcode.FileNotFound)
		}
		op := desc.GetOperation(o.Operation)
		if op == nil {
			err := fmt.Errorf("operation %q not found on provider %q (available: %s)",
				o.Operation, desc.Name, provdetail.FormatAvailableOperations(desc.OperationNames()))
			if w := writer.FromContext(ctx); w != nil {
				w.Errorf("%s", provdetail.SanitizeInlineText(err.Error()))
			}
			return exitcode.WithCode(err, exitcode.FileNotFound)
		}
		if o.useDefaultDetailView() {
			return o.printOperationDetail(ctx, desc, op)
		}
		output := provdetail.BuildOperationDetail(*op)
		output["provider"] = desc.Name
		output["source"] = source
		return o.writeDetailOutput(ctx, output)
	}

	if o.useDefaultDetailView() {
		return o.printProviderDetail(ctx, desc)
	}

	output := BuildProviderDetail(*desc)
	output["source"] = source
	for k, v := range extra {
		output[k] = v
	}
	return o.writeDetailOutput(ctx, output)
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
	for _, cp := range desc.Capabilities {
		caps = append(caps, capStyle(string(cp)))
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

	// Operations (rich metadata) or fallback to legacy WriteOperations
	if len(desc.Operations) > 0 {
		type opRow struct {
			plainName  string
			styledName string
			plainMeta  string
			styledMeta string
			desc       string
		}

		rows := make([]opRow, 0, len(desc.Operations))
		maxNameWidth := 0
		maxMetaWidth := 0

		for _, op := range desc.Operations {
			safeOpName := provdetail.SanitizeInlineText(op.Name)
			var plainMetaParts []string
			var styledMetaParts []string

			for _, cp := range op.Capabilities {
				s := provdetail.SanitizeInlineText(string(cp))
				plainMetaParts = append(plainMetaParts, "["+s+"]")
				styledMetaParts = append(styledMetaParts, capStyle(s))
			}
			if op.IsWrite || desc.IsWriteOperation(op.Name) {
				plainMetaParts = append(plainMetaParts, "(write)")
				styledMetaParts = append(styledMetaParts, warnStyle("(write)"))
			}
			if op.IsDeprecated {
				plainMetaParts = append(plainMetaParts, "(deprecated)")
				styledMetaParts = append(styledMetaParts, errorStyle("(deprecated)"))
			}

			plainMeta := strings.Join(plainMetaParts, " ")
			styledMeta := strings.Join(styledMetaParts, " ")

			nameWidth := utf8.RuneCountInString(safeOpName)
			if nameWidth > maxNameWidth {
				maxNameWidth = nameWidth
			}
			metaWidth := utf8.RuneCountInString(plainMeta)
			if metaWidth > maxMetaWidth {
				maxMetaWidth = metaWidth
			}

			rows = append(rows, opRow{
				plainName:  safeOpName,
				styledName: keyStyle(safeOpName),
				plainMeta:  plainMeta,
				styledMeta: styledMeta,
				desc:       provdetail.SanitizeInlineText(op.Description),
			})
		}

		w.Plainln("")
		w.Plainln(keyStyle("Operations:"))
		for _, r := range rows {
			namePad := strings.Repeat(" ", maxNameWidth-utf8.RuneCountInString(r.plainName))
			var b strings.Builder
			b.WriteString("  ")
			b.WriteString(r.styledName)
			if maxMetaWidth > 0 {
				if r.plainMeta != "" {
					b.WriteString(namePad)
					b.WriteString("  ")
					b.WriteString(r.styledMeta)
					if r.desc != "" {
						b.WriteString(strings.Repeat(" ", maxMetaWidth-utf8.RuneCountInString(r.plainMeta)))
						b.WriteString("  ")
						b.WriteString(dimStyle(r.desc))
					}
				} else if r.desc != "" {
					b.WriteString(namePad)
					b.WriteString("  ")
					b.WriteString(strings.Repeat(" ", maxMetaWidth))
					b.WriteString("  ")
					b.WriteString(dimStyle(r.desc))
				}
			} else if r.desc != "" {
				b.WriteString(namePad)
				b.WriteString("  ")
				b.WriteString(dimStyle(r.desc))
			}
			w.Plainln(b.String())
		}
	} else if len(desc.WriteOperations) > 0 {
		w.Plainln("")
		w.Plainln(keyStyle("Write Operations:"))
		for _, opName := range desc.WriteOperations {
			w.Plainlnf("  %s", valueStyle(provdetail.SanitizeInlineText(opName)))
		}
	}

	// Schema properties
	if desc.Schema != nil && len(desc.Schema.Properties) > 0 {
		// Build required set
		requiredSet := make(map[string]bool, len(desc.Schema.Required))
		for _, name := range desc.Schema.Required {
			requiredSet[name] = true
		}
		propNames := make([]string, 0, len(desc.Schema.Properties))
		for name := range desc.Schema.Properties {
			propNames = append(propNames, name)
		}
		slices.Sort(propNames)

		w.Plainln("")
		w.Plainln(keyStyle("Schema Properties:"))
		for _, name := range propNames {
			prop := desc.Schema.Properties[name]
			required := ""
			if requiredSet[name] {
				required = warnStyle(" *")
			}
			typeStr := "any"
			if prop != nil && prop.Type != "" {
				typeStr = prop.Type
			}
			w.Plainlnf("  %s %s%s", keyStyle(name), dimStyle("("+typeStr+")"), required)
			if prop != nil {
				if prop.Description != "" {
					w.Plainlnf("    %s", dimStyle(provdetail.SanitizeInlineText(prop.Description)))
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
	}

	// Output schemas
	if len(desc.OutputSchemas) > 0 {
		capKeys := make([]string, 0, len(desc.OutputSchemas))
		for cp := range desc.OutputSchemas {
			capKeys = append(capKeys, string(cp))
		}
		slices.Sort(capKeys)

		w.Plainln("")
		w.Plainln(keyStyle("Output Schemas:"))
		for _, cpStr := range capKeys {
			schema := desc.OutputSchemas[provider.Capability(cpStr)]
			w.Plainlnf("  %s", capStyle(cpStr))
			if schema != nil {
				propNames := make([]string, 0, len(schema.Properties))
				for name := range schema.Properties {
					propNames = append(propNames, name)
				}
				slices.Sort(propNames)
				for _, name := range propNames {
					prop := schema.Properties[name]
					typeStr := "any"
					if prop != nil && prop.Type != "" {
						typeStr = prop.Type
					}
					w.Plainlnf("    %s %s", name, dimStyle("("+typeStr+")"))
					if prop != nil && prop.Description != "" {
						w.Plainlnf("      %s", dimStyle(provdetail.SanitizeInlineText(prop.Description)))
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

// printOperationDetail renders human-readable details for a single operation.
func (o *Options) printOperationDetail(ctx context.Context, desc *provider.Descriptor, op *provider.OperationDescriptor) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return nil
	}
	noColor := w.NoColor()

	keyStyle := func(s string) string {
		if noColor {
			return s
		}
		return "\033[1;94m" + s + "\033[0m"
	}
	capStyle := func(s string) string {
		if noColor {
			return "[" + s + "]"
		}
		return "\033[38;5;85;48;5;235m " + s + " \033[0m"
	}
	warnStyle := func(s string) string {
		if noColor {
			return s
		}
		return "\033[1;33m" + s + "\033[0m"
	}
	errorStyle := func(s string) string {
		if noColor {
			return s
		}
		return "\033[1;31m" + s + "\033[0m"
	}
	dimStyle := func(s string) string {
		if noColor {
			return s
		}
		return "\033[2m" + s + "\033[0m"
	}

	safeProvider := provdetail.SanitizeInlineText(desc.Name)
	safeOpName := provdetail.SanitizeInlineText(op.Name)
	safeDisplayName := provdetail.SanitizeInlineText(op.DisplayName)

	w.Plainlnf("%s %s", keyStyle("Provider:"), safeProvider)
	w.Plainlnf("%s %s", keyStyle("Operation:"), safeOpName)
	if safeDisplayName != "" && safeDisplayName != safeOpName {
		w.Plainlnf("%s %s", keyStyle("Display Name:"), safeDisplayName)
	}

	isWrite := op.IsWrite || desc.IsWriteOperation(op.Name)
	if isWrite {
		w.Plainlnf("%s %s", keyStyle("Write:"), warnStyle("yes (mutates external state; use in workflow actions)"))
	} else {
		w.Plainlnf("%s %s", keyStyle("Write:"), "no")
	}

	if len(op.Capabilities) > 0 {
		caps := make([]string, 0, len(op.Capabilities))
		for _, cp := range op.Capabilities {
			caps = append(caps, capStyle(provdetail.SanitizeInlineText(string(cp))))
		}
		w.Plainlnf("%s %s", keyStyle("Capabilities:"), strings.Join(caps, " "))
	}

	if op.IsDeprecated {
		if depMsg := provdetail.SanitizeInlineText(op.DeprecationMessage); depMsg != "" {
			w.Plainln(errorStyle("⚠ This operation is DEPRECATED: " + depMsg))
		} else {
			w.Plainln(errorStyle("⚠ This operation is DEPRECATED"))
		}
	}

	if cleanDesc := provdetail.SanitizeMultilineText(op.Description); cleanDesc != "" {
		w.Plainln("")
		w.Plainln(keyStyle("Description:"))
		for _, line := range strings.Split(cleanDesc, "\n") {
			w.Plainlnf("  %s", line)
		}
	}

	if len(op.Tags) > 0 {
		safeTags := make([]string, 0, len(op.Tags))
		for _, tag := range op.Tags {
			if st := provdetail.SanitizeInlineText(tag); st != "" {
				safeTags = append(safeTags, st)
			}
		}
		if len(safeTags) > 0 {
			w.Plainln("")
			w.Plainlnf("%s %s", keyStyle("Tags:"), strings.Join(safeTags, ", "))
		}
	}

	renderOperationSchemaSection(w, "Input Schema:", op.InputSchema, true, keyStyle, keyStyle, warnStyle, dimStyle)
	renderOperationSchemaSection(w, "Output Schema:", op.OutputSchema, false, keyStyle, func(s string) string { return s }, warnStyle, dimStyle)

	if len(op.Examples) > 0 {
		w.Plainln("")
		w.Plainln(keyStyle("Examples:"))
		for _, ex := range op.Examples {
			w.Plainlnf("  %s", keyStyle(provdetail.SanitizeInlineText(ex.Name)))
			if exDesc := provdetail.SanitizeInlineText(ex.Description); exDesc != "" {
				w.Plainlnf("    %s", dimStyle(exDesc))
			}
			if cleanYAML := provdetail.SanitizeMultilineText(ex.YAML); cleanYAML != "" {
				w.Plainln("    ---")
				for _, line := range strings.Split(cleanYAML, "\n") {
					if line != "" {
						w.Plainlnf("    %s", line)
					}
				}
			}
		}
	}

	return nil
}

const maxOperationSchemaDepth = 6

// isFalseSchema reports whether schema represents the boolean JSON Schema `false`
// (used by jsonschema-go for `"additionalProperties": false`).
func isFalseSchema(schema *jsonschema.Schema) bool {
	if schema == nil || schema.Not == nil {
		return false
	}
	raw, err := schema.MarshalJSON()
	return err == nil && string(raw) == "false"
}

func hasAllowedAdditionalProperties(schema *jsonschema.Schema) bool {
	return schema != nil && schema.AdditionalProperties != nil && !isFalseSchema(schema.AdditionalProperties)
}

func hasAllowedItems(schema *jsonschema.Schema) bool {
	return schema != nil && schema.Items != nil && !isFalseSchema(schema.Items)
}

func operationSchemaTypeLabel(schema *jsonschema.Schema) string {
	if schema == nil {
		return "any"
	}
	if safeType := provdetail.SanitizeInlineText(schema.Type); safeType != "" {
		return safeType
	}
	if len(schema.Types) > 0 {
		parts := make([]string, 0, len(schema.Types))
		for _, t := range schema.Types {
			if st := provdetail.SanitizeInlineText(t); st != "" {
				parts = append(parts, st)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "|")
		}
	}
	if len(schema.Properties) > 0 || schema.AdditionalProperties != nil || len(schema.PatternProperties) > 0 {
		return "object"
	}
	if schema.Items != nil {
		return "array"
	}
	return "any"
}

func renderOperationSchemaSection(
	w *writer.Writer,
	heading string,
	schema *jsonschema.Schema,
	markRequired bool,
	headingStyle, propNameStyle, warnStyle, dimStyle func(string) string,
) {
	if schema == nil {
		return
	}
	var lines []string
	collectOperationSchemaLines(&lines, schema, "  ", markRequired, 0, propNameStyle, warnStyle, dimStyle)
	if len(lines) == 0 {
		return
	}
	w.Plainln("")
	w.Plainln(headingStyle(heading))
	for _, line := range lines {
		w.Plainln(line)
	}
}

func collectOperationSchemaLines(
	lines *[]string,
	schema *jsonschema.Schema,
	indent string,
	markRequired bool,
	depth int,
	propNameStyle, warnStyle, dimStyle func(string) string,
) {
	if schema == nil || depth > maxOperationSchemaDepth {
		return
	}

	hasProps := len(schema.Properties) > 0
	hasAddl := hasAllowedAdditionalProperties(schema)
	hasPatternProps := len(schema.PatternProperties) > 0

	if hasProps {
		requiredSet := make(map[string]bool, len(schema.Required))
		for _, name := range schema.Required {
			requiredSet[name] = true
		}
		propNames := make([]string, 0, len(schema.Properties))
		for name := range schema.Properties {
			propNames = append(propNames, name)
		}
		slices.Sort(propNames)

		for _, name := range propNames {
			prop := schema.Properties[name]
			safePropName := provdetail.SanitizeInlineText(name)
			required := ""
			if markRequired && requiredSet[name] {
				required = warnStyle(" *")
			}
			typeStr := operationSchemaTypeLabel(prop)
			*lines = append(*lines, fmt.Sprintf("%s%s %s%s", indent, propNameStyle(safePropName), dimStyle("("+typeStr+")"), required))
			collectChildSchemaDetails(lines, prop, indent+"  ", markRequired, depth, propNameStyle, warnStyle, dimStyle)
		}
	}

	if hasAddl {
		valType := operationSchemaTypeLabel(schema.AdditionalProperties)
		*lines = append(*lines, fmt.Sprintf("%s%s %s", indent, propNameStyle("<key>"), dimStyle("("+valType+")")))
		collectChildSchemaDetails(lines, schema.AdditionalProperties, indent+"  ", markRequired, depth, propNameStyle, warnStyle, dimStyle)
	}

	if hasPatternProps {
		patterns := make([]string, 0, len(schema.PatternProperties))
		for p := range schema.PatternProperties {
			patterns = append(patterns, p)
		}
		slices.Sort(patterns)
		for _, pat := range patterns {
			prop := schema.PatternProperties[pat]
			safePat := provdetail.SanitizeInlineText(pat)
			*lines = append(*lines, fmt.Sprintf("%s%s %s", indent, propNameStyle(safePat), dimStyle("("+operationSchemaTypeLabel(prop)+")")))
			collectChildSchemaDetails(lines, prop, indent+"  ", markRequired, depth, propNameStyle, warnStyle, dimStyle)
		}
	}

	if !hasProps && !hasAddl && !hasPatternProps {
		descText := provdetail.SanitizeInlineText(schema.Description)
		typeLabel := operationSchemaTypeLabel(schema)
		if typeLabel != "any" && (typeLabel != "object" || descText != "") {
			*lines = append(*lines, fmt.Sprintf("%s%s %s", indent, dimStyle("Type:"), typeLabel))
		}
		if descText != "" {
			*lines = append(*lines, fmt.Sprintf("%s%s", indent, dimStyle(descText)))
		}
		if schema.Default != nil {
			if defText := provdetail.SanitizeInlineText(string(schema.Default)); defText != "" {
				*lines = append(*lines, fmt.Sprintf("%s%s %s", indent, dimStyle("Default:"), defText))
			}
		}
		if len(schema.Enum) > 0 {
			enumStrs := make([]string, len(schema.Enum))
			for i, v := range schema.Enum {
				enumStrs[i] = provdetail.SanitizeInlineText(fmt.Sprintf("%v", v))
			}
			*lines = append(*lines, fmt.Sprintf("%s%s %s", indent, dimStyle("Enum:"), strings.Join(enumStrs, ", ")))
		}
		if hasAllowedItems(schema) && depth < maxOperationSchemaDepth {
			*lines = append(*lines, fmt.Sprintf("%s%s %s", indent, dimStyle("Items:"), dimStyle("("+operationSchemaTypeLabel(schema.Items)+")")))
			collectChildSchemaDetails(lines, schema.Items, indent+"  ", markRequired, depth+1, propNameStyle, warnStyle, dimStyle)
		}
	}
}

// collectChildSchemaDetails renders metadata (description, default, enum) and
// nested structure (object properties/additionalProperties/patternProperties as
// well as array items, including arrays-of-arrays and array-valued maps) for a
// child schema node while respecting maxOperationSchemaDepth.
func collectChildSchemaDetails(
	lines *[]string,
	child *jsonschema.Schema,
	indent string,
	markRequired bool,
	depth int,
	propNameStyle, warnStyle, dimStyle func(string) string,
) {
	if child == nil {
		return
	}
	if descText := provdetail.SanitizeInlineText(child.Description); descText != "" {
		*lines = append(*lines, fmt.Sprintf("%s%s", indent, dimStyle(descText)))
	}
	if child.Default != nil {
		if defText := provdetail.SanitizeInlineText(string(child.Default)); defText != "" {
			*lines = append(*lines, fmt.Sprintf("%s%s %s", indent, dimStyle("Default:"), defText))
		}
	}
	if len(child.Enum) > 0 {
		enumStrs := make([]string, len(child.Enum))
		for i, v := range child.Enum {
			enumStrs[i] = provdetail.SanitizeInlineText(fmt.Sprintf("%v", v))
		}
		*lines = append(*lines, fmt.Sprintf("%s%s %s", indent, dimStyle("Enum:"), strings.Join(enumStrs, ", ")))
	}
	if depth >= maxOperationSchemaDepth {
		return
	}
	if len(child.Properties) > 0 || hasAllowedAdditionalProperties(child) || len(child.PatternProperties) > 0 {
		collectOperationSchemaLines(lines, child, indent, markRequired, depth+1, propNameStyle, warnStyle, dimStyle)
	}
	if hasAllowedItems(child) {
		*lines = append(*lines, fmt.Sprintf("%s%s %s", indent, dimStyle("Items:"), dimStyle("("+operationSchemaTypeLabel(child.Items)+")")))
		collectChildSchemaDetails(lines, child.Items, indent+"  ", markRequired, depth+1, propNameStyle, warnStyle, dimStyle)
	}
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
			for _, cp := range desc.Capabilities {
				if strings.EqualFold(string(cp), o.Capability) {
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

// appendCachedPlugins discovers locally cached plugin providers and adds them
// to the output list, skipping any names already present (builtin or official).
func (o *Options) appendCachedPlugins(ctx context.Context, output []Summary) []Summary {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	cache := plugin.NewCache(settings.PluginCacheDirFor(o.BinaryName))
	cached, err := cache.ListCurrentPlatform()
	if err != nil {
		lgr.V(1).Info("failed to list cached plugins", "error", err)
		return output
	}
	if len(cached) == 0 {
		return output
	}

	// Build a set of already-listed names to avoid duplicates.
	seen := make(map[string]bool, len(output))
	for _, s := range output {
		seen[s.Name] = true
	}

	infos := plugin.DescribeCachedPlugins(ctx, cached, seen)
	for _, info := range infos {
		output = append(output, Summary{
			Name:        info.Name,
			Version:     info.Version,
			Source:      "local",
			Description: info.Description,
		})
	}

	lgr.V(1).Info("appended cached plugin providers", "count", len(infos))
	if w != nil && len(infos) > 0 {
		w.Verbosef("Found %d locally cached plugin providers", len(infos))
	}

	return output
}

// writeOutput writes the provider list output using kvx with the array display schema.
func (o *Options) writeOutput(ctx context.Context, data any) error {
	// Use the shared kvx output infrastructure with display schema for rich TUI rendering
	kvxOpts := flags.ToKvxOutputOptions(&o.KvxOutputFlags,
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(o.CliParams.NoColor),
		kvx.WithOutputAppName(o.BinaryName+" get provider"),
		kvx.WithOutputDisplaySchemaJSON(providerSchemaJSON),
		kvx.WithIOStreams(o.IOStreams),
		kvx.WithOutputColumnOrder([]string{"name", "version", "source", "description"}),
		kvx.WithOutputColumnHints(map[string]tui.ColumnHint{
			"name":         {MaxWidth: 20, Priority: 10},
			"version":      {MaxWidth: 12, Priority: 7},
			"source":       {MaxWidth: 10, Priority: 8},
			"displayName":  {Hidden: true},
			"category":     {Hidden: true},
			"capabilities": {Hidden: true},
			"deprecated":   {Hidden: true},
			"beta":         {Hidden: true},
		}),
	)

	return kvxOpts.Write(data)
}

// writeDetailOutput writes single-provider or single-operation details using kvx
// without hiding detail fields (capabilities, deprecated, category, etc.).
func (o *Options) writeDetailOutput(ctx context.Context, data any) error {
	kvxOpts := flags.ToKvxOutputOptions(&o.KvxOutputFlags,
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(o.CliParams.NoColor),
		kvx.WithOutputAppName(o.BinaryName+" get provider"),
		kvx.WithIOStreams(o.IOStreams),
	)

	return kvxOpts.Write(data)
}
