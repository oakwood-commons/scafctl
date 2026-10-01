// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	appconfig "github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// ValidateOptions holds options for the config validate command.
type ValidateOptions struct {
	IOStreams      *terminal.IOStreams
	CliParams      *settings.Run
	KvxOutputFlags flags.KvxOutputFlags
	ConfigPath     string
	File           string
}

// ValidateResult is the structured result emitted for data-format output (-o
// json/yaml/csv/toml/text). The human-formatted path uses writer Successf/Infof
// lines instead.
type ValidateResult struct {
	File           string `json:"file" yaml:"file" toml:"file" doc:"Config file path that was validated" maxLength:"4096"`
	Valid          bool   `json:"valid" yaml:"valid" toml:"valid" doc:"Whether the file passed validation"`
	Version        int    `json:"version,omitempty" yaml:"version,omitempty" toml:"version,omitempty" doc:"Config schema version" maximum:"100"`
	Catalogs       int    `json:"catalogs" yaml:"catalogs" toml:"catalogs" doc:"Number of configured catalogs" maximum:"1000"`
	DefaultCatalog string `json:"defaultCatalog,omitempty" yaml:"defaultCatalog,omitempty" toml:"defaultCatalog,omitempty" doc:"Default catalog name" maxLength:"255"`
	Error          string `json:"error,omitempty" yaml:"error,omitempty" toml:"error,omitempty" doc:"Validation error, if any" maxLength:"4096"`
}

// toMap flattens ValidateResult into a map[string]any so kvx's CSV writer
// (which only decomposes maps and slices) can emit a proper header + row.
// Keys match the json/yaml tags, and every field is always present so the
// CSV schema is stable across success and failure rows.
func (r ValidateResult) toMap() map[string]any {
	return map[string]any{
		"file":           r.File,
		"valid":          r.Valid,
		"version":        r.Version,
		"catalogs":       r.Catalogs,
		"defaultCatalog": r.DefaultCatalog,
		"error":          r.Error,
	}
}

// validateCSVColumnOrder pins the column order for -o csv so the header row
// matches the struct's logical order instead of the writer's alphabetical
// fallback.
var validateCSVColumnOrder = []string{"file", "valid", "version", "catalogs", "defaultCatalog", "error"}

// validateOutputFormats is the subset of kvx output formats that produce
// meaningful output for a scalar validator result. Formats whose rendering
// adds no value for a single object (table/list/tree) or whose shape is
// nonsensical (mermaid/test) are intentionally excluded so --help, PreRunE
// rejection, and runtime routing stay in sync.
var validateOutputFormats = []string{
	string(kvx.OutputFormatAuto),
	string(kvx.OutputFormatJSON),
	string(kvx.OutputFormatYAML),
	string(kvx.OutputFormatCSV),
	string(kvx.OutputFormatTOML),
	string(kvx.OutputFormatText),
	string(kvx.OutputFormatQuiet),
}

// CommandValidate creates the 'config validate' command.
func CommandValidate(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &ValidateOptions{}

	cCmd := &cobra.Command{
		Use:   "validate [file]",
		Short: "Validate a configuration file",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Validate a scafctl configuration file.

			Checks that the configuration file is valid YAML and conforms
			to the expected schema. Reports any errors found.

			By default, validates the config file at
			$XDG_CONFIG_HOME/scafctl/config.yaml (or ~/.config/scafctl/config.yaml).
			Specify a file path to validate a different file.

			Examples:
			  # Validate default config
			  scafctl config validate

			  # Validate a specific file
			  scafctl config validate ./my-config.yaml

			  # Emit a structured result for scripting
			  scafctl config validate -o json

			  # Validate with verbose output
			  scafctl config validate --log-level -1
		`), settings.CliBinaryName, cliParams.BinaryName),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cCmd *cobra.Command, args []string) error {
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

			if len(args) > 0 {
				opts.File = args[0]
			}

			// Get config path from parent command context
			if configFlag := cCmd.Root().Flag("config"); configFlag != nil && configFlag.Value.String() != "" {
				opts.ConfigPath = configFlag.Value.String()
			}

			return opts.Run(ctx)
		},
		SilenceUsage: true,
	}

	flags.AddKvxOutputFormatFlagToStructWithFormats(cCmd, &opts.KvxOutputFlags, validateOutputFormats)

	return cCmd
}

// Run executes the config validate command.
func (o *ValidateOptions) Run(ctx context.Context) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}
	lgr := logger.FromContext(ctx)

	kvxOpts := flags.ToKvxOutputOptions(&o.KvxOutputFlags,
		kvx.WithIOStreams(o.IOStreams),
		kvx.WithOutputColumnOrder(validateCSVColumnOrder),
	)
	// Of the allowed formats (validateOutputFormats), auto is the only one that
	// renders through the hand-rolled Successf/Infof block; every other allowed
	// format is a serialization that kvx should emit.
	structured := kvxOpts.Format != kvx.OutputFormatAuto

	// Determine file to validate
	filePath := o.File
	if filePath == "" {
		if o.ConfigPath != "" {
			filePath = o.ConfigPath
		} else {
			var err error
			filePath, err = paths.ConfigFile()
			if err != nil {
				err = fmt.Errorf("failed to determine config path: %w", err)
				o.emitFailure(w, kvxOpts, structured, "", err)
				return exitcode.WithCode(err, exitcode.ConfigError)
			}
		}
	}

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		err := fmt.Errorf("config file not found: %s", filePath)
		o.emitFailure(w, kvxOpts, structured, filePath, err)
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	if lgr != nil {
		lgr.V(1).Info("Validating config file", "path", filePath)
	}

	// Load and validate using the manager
	mgr := appconfig.NewManager(filePath)
	cfg, err := mgr.Load()
	if err != nil {
		o.emitFailure(w, kvxOpts, structured, filePath, err)
		return exitcode.WithCode(err, exitcode.ConfigError)
	}

	// Run additional validation
	if err := cfg.Validate(); err != nil {
		wrapped := fmt.Errorf("validation error: %w", err)
		o.emitFailure(w, kvxOpts, structured, filePath, wrapped)
		return exitcode.WithCode(wrapped, exitcode.ValidationFailed)
	}

	result := ValidateResult{
		File:           filePath,
		Valid:          true,
		Version:        cfg.Version,
		Catalogs:       len(cfg.Catalogs),
		DefaultCatalog: cfg.Settings.DefaultCatalog,
	}

	if structured {
		return kvxOpts.Write(resultForFormat(result, kvxOpts.Format))
	}

	w.Successf("Valid: %s\n", filePath)
	w.Infof("  Version: %d\n", result.Version)
	w.Infof("  Catalogs: %d\n", result.Catalogs)
	if result.DefaultCatalog != "" {
		w.Infof("  Default catalog: %s\n", result.DefaultCatalog)
	}

	return nil
}

// emitFailure writes the failure in the correct format: a ValidateResult via
// kvx for data formats, a red Errorf line otherwise. The caller still returns
// the exit-coded error so CI pass/fail is driven by the exit code.
func (o *ValidateOptions) emitFailure(w *writer.Writer, kvxOpts *kvx.OutputOptions, structured bool, filePath string, err error) {
	if structured {
		result := ValidateResult{
			File:  filePath,
			Valid: false,
			Error: err.Error(),
		}
		_ = kvxOpts.Write(resultForFormat(result, kvxOpts.Format))
		return
	}
	if filePath != "" {
		w.Errorf("Validation failed: %s\n", filePath)
	}
	w.Errorf("%v", err)
}

// resultForFormat returns the shape kvx needs to render the given format.
// kvx.writeCSV only decomposes maps and slices, so a struct would collapse to
// a single %v cell with no header; every other kvx format consumes the struct
// natively (with its json/yaml tags preserved) so we only remap for CSV.
func resultForFormat(r ValidateResult, f kvx.OutputFormat) any {
	if f == kvx.OutputFormatCSV {
		return r.toMap()
	}
	return r
}
