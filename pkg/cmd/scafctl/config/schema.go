// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/schema"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// schemaDefaultOutput is the default -o for config schema.
// Deliberately deviates from the repo-wide "auto" convention: this command
// exists to emit a JSON Schema document for editor/IDE tooling, and
// `scafctl config schema > file.json` is the canonical usage.
const schemaDefaultOutput = "json"

// SchemaOptions holds options for the config schema command.
type SchemaOptions struct {
	IOStreams *terminal.IOStreams
	CliParams *settings.Run

	// Compact disables JSON indentation when the effective format is JSON.
	// Ignored (with a warning) for other output formats.
	Compact bool

	flags.KvxOutputFlags
}

// CommandSchema creates the 'config schema' command.
func CommandSchema(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &SchemaOptions{}

	cCmd := &cobra.Command{
		Use:   "schema",
		Short: "Output JSON Schema for config file",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Output the JSON Schema for the scafctl configuration file.

			The schema can be used by editors and IDEs for autocompletion
			and validation of config files. The default output is pretty
			JSON so 'scafctl config schema > config-schema.json' works
			out of the box.

			All standard kvx output flags are supported: -o/--output for
			format (json, yaml, table, list, tree, mermaid, quiet),
			-e/--expression for CEL filtering, -w/--where for per-item
			filters, and -i/--interactive to explore the schema in a TUI.

			Examples:
			  # Output pretty JSON (default)
			  scafctl config schema

			  # Save schema to a file
			  scafctl config schema > ~/.config/scafctl/config-schema.json

			  # Output compact JSON (no indentation)
			  scafctl config schema --compact

			  # Render the schema as YAML
			  scafctl config schema -o yaml

			  # Extract a subtree with CEL
			  scafctl config schema -e '_.properties.catalogs'

			  # Explore the schema interactively
			  scafctl config schema -i

			To enable schema validation in your config file, add this comment
			at the top of ~/.config/scafctl/config.yaml:

			  # yaml-language-server: $schema=~/.config/scafctl/config-schema.json
		`), settings.CliBinaryName, cliParams.BinaryName),
		RunE: func(cCmd *cobra.Command, _ []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Use)
			ctx := settings.IntoContext(cCmd.Context(), cliParams)

			if lgr := logger.FromContext(cCmd.Context()); lgr != nil {
				ctx = logger.WithLogger(ctx, lgr)
			}

			w := writer.FromContext(cCmd.Context())
			if w == nil {
				w = writer.New(ioStreams, cliParams)
			}
			ctx = writer.WithWriter(ctx, w)

			opts.IOStreams = ioStreams
			opts.CliParams = cliParams
			opts.AppName = cliParams.BinaryName + " config schema"

			return opts.Run(ctx)
		},
		SilenceUsage: true,
	}

	flags.AddKvxOutputFlagsToStructWithDefault(cCmd, &opts.KvxOutputFlags, schemaDefaultOutput)

	cCmd.Flags().BoolVar(&opts.Compact, "compact", false,
		"Output compact JSON without indentation (only affects -o json)")

	return cCmd
}

// Run executes the config schema command.
func (o *SchemaOptions) Run(ctx context.Context) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	schemaBytes, err := schema.GenerateConfigSchema()
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	// Unmarshal so kvx can filter, render as YAML, and drive the TUI.
	var schemaMap map[string]any
	if err := json.Unmarshal(schemaBytes, &schemaMap); err != nil {
		wrapped := fmt.Errorf("failed to parse generated config schema: %w", err)
		w.Errorf("%v", wrapped)
		return exitcode.WithCode(wrapped, exitcode.GeneralError)
	}

	kvxOpts := flags.ToKvxOutputOptions(&o.KvxOutputFlags,
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(o.CliParams.NoColor),
		kvx.WithOutputAppName(o.AppName),
	)
	kvxOpts.IOStreams = o.IOStreams

	// -i routes through the TUI, which never serializes JSON, so --compact
	// is meaningless there regardless of the selected format.
	switch {
	case o.Compact && kvxOpts.Interactive:
		w.WarnStderrf("--compact only affects -o json; ignored with -i")
	case o.Compact && kvxOpts.Format == kvx.OutputFormatJSON:
		kvxOpts.PrettyPrint = false
	case o.Compact:
		w.WarnStderrf("--compact only affects -o json; ignored with -o %s", kvxOpts.Format)
	}

	return kvxOpts.Write(schemaMap)
}
