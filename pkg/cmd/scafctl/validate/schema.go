// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/schema"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"sigs.k8s.io/yaml"

	"github.com/spf13/cobra"
)

// schemaOptions holds flags for the 'validate schema' command.
type schemaOptions struct {
	SchemaFile string
	DataFile   string
}

// CommandValidateSchema creates the 'validate schema' subcommand. It validates
// arbitrary data (JSON or YAML) against a JSON Schema (JSON or YAML). It does
// NOT run lint -- it operates on arbitrary data, not on a scafctl solution.
func CommandValidateSchema(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &schemaOptions{}

	binaryName := cliParams.BinaryName
	if binaryName == "" {
		binaryName = settings.CliBinaryName
	}

	cCmd := &cobra.Command{
		Use:   "schema --schema <schema> --data <data>",
		Short: fmt.Sprintf("Validate data against a JSON Schema (used by %s as a low-level gate)", binaryName),
		Long: strings.ReplaceAll(`Validate arbitrary data against a JSON Schema and fail on any violation.

Both the schema and the data may be supplied as JSON or YAML. Pass '-' to
--data to read the data from stdin. Unlike 'validate solution', this command
does NOT run lint: it checks raw data conformance only, which makes it useful
for validating configuration files, API payloads, or any document against a
schema of your choosing.

EXIT CODES:
  0  Data conforms to the schema
  2  Data violates the schema
  3  The schema is invalid, or the data could not be parsed as JSON/YAML
  4  A schema or data file was not found

Examples:
  # Validate a data file against a schema (both JSON)
  scafctl validate schema --schema schema.json --data data.json

  # Schema and data as YAML
  scafctl validate schema --schema schema.yaml --data data.yaml

  # Read the data from stdin
  cat data.yaml | scafctl validate schema --schema schema.json --data -`, settings.CliBinaryName, binaryName),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Bare invocation (no flags at all): show help rather than a raw
			// "required flag(s) not set" error (grammar Rule 8).
			if opts.SchemaFile == "" && opts.DataFile == "" {
				return cmd.Help()
			}
			// Only one of the two provided: give a clear, actionable message
			// naming exactly what is missing and how to supply it.
			var missing []string
			if opts.SchemaFile == "" {
				missing = append(missing, "--schema <file>")
			}
			if opts.DataFile == "" {
				missing = append(missing, "--data <file|->")
			}
			if len(missing) > 0 {
				w := writer.FromContext(cmd.Context())
				if w == nil {
					w = writer.New(ioStreams, cliParams)
				}
				w.Errorf("missing required flag(s): %s", strings.Join(missing, ", "))
				w.Plainlnf("Usage: %s validate schema --schema <schema> --data <data|->", binaryName)
				w.Plainlnf("Run '%s validate schema --help' for details.", binaryName)
				return exitcode.WithCode(fmt.Errorf("missing required flags: %s", strings.Join(missing, ", ")), exitcode.InvalidInput)
			}

			cliParams.EntryPointSettings.Path = filepath.Join(path, cmd.Name())
			ctx := settings.IntoContext(cmd.Context(), cliParams)
			if lgr := logger.FromContext(cmd.Context()); lgr != nil {
				ctx = logger.WithLogger(ctx, lgr)
			}
			w := writer.FromContext(cmd.Context())
			if w == nil {
				w = writer.New(ioStreams, cliParams)
			}
			ctx = writer.WithWriter(ctx, w)

			return runValidateSchema(ctx, opts, ioStreams)
		},
		SilenceUsage: true,
	}

	cCmd.Flags().StringVar(&opts.SchemaFile, "schema", "", "Path to the JSON Schema file (JSON or YAML) (required)")
	cCmd.Flags().StringVar(&opts.DataFile, "data", "", "Path to the data file to validate (JSON or YAML); use '-' for stdin (required)")

	return cCmd
}

// runValidateSchema reads the schema and data, validates, renders violations,
// and returns an exit-coded error on any failure. Failures are printed to the
// writer so the user sees an actionable message, not a silent non-zero exit.
func runValidateSchema(ctx context.Context, opts *schemaOptions, ioStreams *terminal.IOStreams) error {
	w := writer.FromContext(ctx)

	fail := func(err error, code int) error {
		if w != nil {
			w.Errorf("%v", err)
		}
		return exitcode.WithCode(err, code)
	}

	schemaBytes, err := os.ReadFile(opts.SchemaFile)
	if err != nil {
		return fail(fmt.Errorf("reading schema file %q: %w", opts.SchemaFile, err), exitcode.FileNotFound)
	}

	dataBytes, err := readData(opts.DataFile, ioStreams)
	if err != nil {
		if w != nil {
			w.Errorf("%v", err)
		}
		return err
	}

	var data any
	if err := yaml.Unmarshal(dataBytes, &data); err != nil {
		return fail(fmt.Errorf("parsing data as JSON/YAML: %w", err), exitcode.InvalidInput)
	}

	violations, err := schema.ValidateDataAgainstSchema(schemaBytes, data)
	if err != nil {
		return fail(fmt.Errorf("invalid schema: %w", err), exitcode.InvalidInput)
	}

	if len(violations) == 0 {
		if w != nil {
			w.Success("Data is valid against the schema.")
		}
		return nil
	}

	if w != nil {
		w.WarnStderrf("%d schema violation(s):", len(violations))
		for _, v := range violations {
			if v.Path != "" {
				w.PlainStderrf("  - %s: %s", v.Path, v.Message)
			} else {
				w.PlainStderrf("  - %s", v.Message)
			}
		}
	}

	return exitcode.WithCode(fmt.Errorf("data failed schema validation with %d violation(s)", len(violations)), exitcode.ValidationFailed)
}

// readData reads the data document from a file, or from stdin when the path is
// '-'. Errors are already exit-coded: a stdin read failure maps to GeneralError
// (it is not a missing file), while a missing/unreadable file maps to
// FileNotFound.
func readData(dataFile string, ioStreams *terminal.IOStreams) ([]byte, error) {
	if dataFile == "-" {
		var in io.Reader = os.Stdin
		if ioStreams != nil && ioStreams.In != nil {
			in = ioStreams.In
		}
		b, err := io.ReadAll(in)
		if err != nil {
			return nil, exitcode.WithCode(fmt.Errorf("reading data from stdin: %w", err), exitcode.GeneralError)
		}
		return b, nil
	}

	b, err := os.ReadFile(dataFile)
	if err != nil {
		return nil, exitcode.WithCode(fmt.Errorf("reading data file %q: %w", dataFile, err), exitcode.FileNotFound)
	}
	return b, nil
}
