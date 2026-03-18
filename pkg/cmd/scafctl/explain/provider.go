// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package explain

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// ProviderOptions holds configuration for the explain provider command
type ProviderOptions struct {
	IOStreams *terminal.IOStreams
	CliParams *settings.Run

	// For dependency injection in tests
	registry *provider.Registry
}

// CommandProvider creates the 'explain provider' subcommand
func CommandProvider(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	options := &ProviderOptions{}

	cCmd := &cobra.Command{
		Use:     "provider <name>",
		Aliases: []string{"providers", "prov", "p"},
		Short:   "Explain a provider's schema and capabilities",
		Long: `Show detailed documentation for a provider including its schema,
supported capabilities, examples, and configuration options.

The output includes:
  - Provider description and version
  - Supported capabilities (from, transform, validation, action)
  - Input schema with property types and validation rules
  - Output schemas for each capability
  - Usage examples with YAML configurations

Examples:
  # Explain the HTTP provider
  scafctl explain provider http

  # Explain the static provider
  scafctl explain provider static

  # Explain the file provider
  scafctl explain provider file`,
		Args: cobra.ExactArgs(1),
		RunE: func(cCmd *cobra.Command, args []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Use)
			ctx := settings.IntoContext(cCmd.Context(), cliParams)

			options.IOStreams = ioStreams
			options.CliParams = cliParams

			return options.Run(ctx, args[0])
		},
		SilenceUsage: true,
	}

	return cCmd
}

// Run executes the explain provider command
func (o *ProviderOptions) Run(ctx context.Context, name string) error {
	w := writer.New(o.IOStreams, o.CliParams)
	ctx = writer.WithWriter(ctx, w)

	desc, err := LookupProvider(ctx, name, o.registry)
	if err != nil {
		w.Errorf("%v", err)
		return err
	}

	o.printProviderExplanation(w, desc)
	return nil
}

// printProviderExplanation prints a human-readable explanation of a provider
func (o *ProviderOptions) printProviderExplanation(w *writer.Writer, desc *provider.Descriptor) {
	// Header
	w.Infof("%s (%s)", desc.DisplayName, desc.Name)
	w.Plainln("")

	// Description
	if desc.Description != "" {
		w.Plainln(desc.Description)
		w.Plainln("")
	}

	// Version info
	w.Infof("Version Information")
	w.Plainlnf("  API Version: %s", desc.APIVersion)
	w.Plainlnf("  Version:     %s", desc.Version.String())
	if desc.Category != "" {
		w.Plainlnf("  Category:    %s", desc.Category)
	}

	// Status flags
	if desc.IsDeprecated {
		w.Warningf("⚠️  This provider is DEPRECATED")
	}
	if desc.Beta {
		w.Plainlnf("  Status:      Beta")
	}

	w.Plainln("")

	// Capabilities
	w.Infof("Capabilities")
	for _, cap := range desc.Capabilities {
		w.Plainlnf("  • %s", cap)
	}
	w.Plainln("")

	// Input schema
	if desc.Schema != nil && len(desc.Schema.Properties) > 0 {
		w.Infof("Input Schema")
		o.printSchemaProperties(w, desc.Schema, "")
		w.Plainln("")
	}

	// Output schemas
	if len(desc.OutputSchemas) > 0 {
		w.Infof("Output Schemas")
		// Sort capabilities for consistent output
		caps := make([]string, 0, len(desc.OutputSchemas))
		for cap := range desc.OutputSchemas {
			caps = append(caps, string(cap))
		}
		sort.Strings(caps)

		for _, cap := range caps {
			schema := desc.OutputSchemas[provider.Capability(cap)]
			w.Plainlnf("  %s:", cap)
			o.printSchemaProperties(w, schema, "    ")
			w.Plainln("")
		}
	}

	// Examples
	if len(desc.Examples) > 0 {
		w.Infof("Examples")
		for i, ex := range desc.Examples {
			if i > 0 {
				w.Plainln("")
			}
			w.Plainlnf("  %s", ex.Name)
			if ex.Description != "" {
				w.Plainlnf("    %s", ex.Description)
			}
			w.Plainln("    ---")
			// Indent the YAML
			lines := strings.Split(strings.TrimSpace(ex.YAML), "\n")
			for _, line := range lines {
				w.Plainlnf("    %s", line)
			}
		}
		w.Plainln("")
	}

	// Tags
	if len(desc.Tags) > 0 {
		w.Infof("Tags")
		w.Plainln(strings.Join(desc.Tags, ", "))
		w.Plainln("")
	}

	// Links
	if len(desc.Links) > 0 {
		w.Infof("Links")
		for _, link := range desc.Links {
			w.Plainlnf("  • %s: %s", link.Name, link.URL)
		}
		w.Plainln("")
	}

	// Maintainers
	if len(desc.Maintainers) > 0 {
		w.Infof("Maintainers")
		for _, m := range desc.Maintainers {
			if m.Email != "" {
				w.Plainlnf("  • %s <%s>", m.Name, m.Email)
			} else {
				w.Plainlnf("  • %s", m.Name)
			}
		}
		w.Plainln("")
	}
}

// printSchemaProperties prints schema properties with formatting
func (o *ProviderOptions) printSchemaProperties(w *writer.Writer, schema *jsonschema.Schema, indent string) {
	if schema == nil || len(schema.Properties) == 0 {
		return
	}

	// Build required set
	requiredSet := make(map[string]bool, len(schema.Required))
	for _, name := range schema.Required {
		requiredSet[name] = true
	}

	// Sort properties for consistent output
	props := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		props = append(props, name)
	}
	sort.Strings(props)

	for _, name := range props {
		prop := schema.Properties[name]
		reqMarker := ""
		if requiredSet[name] {
			reqMarker = " (required)"
		}

		// Build type string
		typeStr := prop.Type
		if typeStr == "" {
			typeStr = "any"
		}
		if len(prop.Enum) > 0 {
			enumStrs := make([]string, len(prop.Enum))
			for i, e := range prop.Enum {
				enumStrs[i] = fmt.Sprint(e)
			}
			typeStr = fmt.Sprintf("%s [%s]", typeStr, strings.Join(enumStrs, "|"))
		}

		w.Plainlnf("%s  %s (%s)%s", indent, name, typeStr, reqMarker)

		if prop.Description != "" {
			w.Plainlnf("%s      %s", indent, prop.Description)
		}

		if prop.Default != nil {
			w.Plainlnf("%s      Default: %s", indent, string(prop.Default))
		}

		if len(prop.Examples) > 0 {
			w.Plainlnf("%s      Example: %v", indent, prop.Examples[0])
		}
	}
}
