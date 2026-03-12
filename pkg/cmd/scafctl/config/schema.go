// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/schema"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// SchemaOptions holds options for the config schema command.
type SchemaOptions struct {
	IOStreams *terminal.IOStreams
	CliParams *settings.Run
	Compact   bool
}

// CommandSchema creates the 'config schema' command.
func CommandSchema(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &SchemaOptions{}

	cCmd := &cobra.Command{
		Use:   "schema",
		Short: "Output JSON Schema for config file",
		Long: heredoc.Doc(`
			Output the JSON Schema for the scafctl configuration file.

			The schema can be used by editors and IDEs for autocompletion
			and validation of config files.

			Examples:
			  # Output schema to stdout
			  scafctl config schema

			  # Save schema to a file
			  scafctl config schema > ~/.scafctl/config-schema.json

			  # Output compact schema (no indentation)
			  scafctl config schema --compact

			To enable schema validation in your config file, add this comment
			at the top of ~/.scafctl/config.yaml:

			  # yaml-language-server: $schema=~/.scafctl/config-schema.json
		`),
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

			return opts.Run(ctx)
		},
		SilenceUsage: true,
	}

	cCmd.Flags().BoolVar(&opts.Compact, "compact", false, "Output compact JSON without indentation")

	return cCmd
}

// Run executes the config schema command.
func (o *SchemaOptions) Run(ctx context.Context) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	var schemaBytes []byte
	var err error

	if o.Compact {
		schemaBytes, err = schema.GenerateConfigSchemaCompact()
	} else {
		schemaBytes, err = schema.GenerateConfigSchema()
	}
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	w.Plainln(string(schemaBytes))
	return nil
}
