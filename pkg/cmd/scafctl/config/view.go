// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	appconfig "github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// ViewOptions holds options for the config view command.
type ViewOptions struct {
	BinaryName string
	IOStreams  *terminal.IOStreams
	CliParams  *settings.Run
	ConfigPath string

	// ShowOrigin adds per-key source metadata and env-override info to the output.
	ShowOrigin bool
	// SourceFilter, when set, restricts the output to values whose source
	// matches. Valid values: "default", "dropin", "file", "env".
	SourceFilter string

	flags.KvxOutputFlags
}

// CommandView creates the 'config view' command.
//
//nolint:dupl // Cobra command boilerplate is intentionally similar across commands
func CommandView(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &ViewOptions{}

	cCmd := &cobra.Command{
		Use:   "view",
		Short: "View current configuration",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Display the current configuration.

			Shows all settings from the config file merged with environment overrides.

			Use --show-origin to annotate each key with the source it came from
			(default, dropin, file, or env). Use --source=<origin> to restrict
			the output to values from a single source.

			Examples:
			  # View config
			  scafctl config view

			  # View config as YAML
			  scafctl config view -o yaml

			  # View config as JSON
			  scafctl config view -o json

			  # View specific section using CEL
			  scafctl config view -e '_.catalogs'

			  # Annotate every key with its source and list env overrides
			  scafctl config view --show-origin

			  # Show only values coming from the config file
			  scafctl config view --source=file

			  # Show only env-var overrides
			  scafctl config view --source=env
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
			opts.AppName = cliParams.BinaryName
			opts.BinaryName = cliParams.BinaryName

			// Get config path from parent command context
			if configFlag := cCmd.Root().Flag("config"); configFlag != nil && configFlag.Value.String() != "" {
				opts.ConfigPath = configFlag.Value.String()
			}

			return opts.Run(ctx)
		},
		SilenceUsage: true,
	}

	flags.AddKvxOutputFlagsToStruct(cCmd, &opts.KvxOutputFlags)

	cCmd.Flags().BoolVar(&opts.ShowOrigin, "show-origin", false,
		"Annotate each key with its source (default, dropin, file, env) and list env-var overrides")
	cCmd.Flags().StringVar(&opts.SourceFilter, "source", "",
		"Only show values from this source: default, dropin, file, or env")

	return cCmd
}

// Run executes the config view command.
func (o *ViewOptions) Run(ctx context.Context) error {
	if o.BinaryName == "" {
		o.BinaryName = settings.CliBinaryName
	}

	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	if o.SourceFilter != "" && !appconfig.ValidSource(appconfig.Source(o.SourceFilter)) {
		err := fmt.Errorf("invalid --source %q: expected one of default, dropin, file, env", o.SourceFilter)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	mgr := appconfig.NewManager(o.ConfigPath, appconfig.ManagerOptionsFromContext(ctx)...)
	cfg, err := mgr.Load()
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.ConfigError)
	}

	// Emit the full effective config (all top-level sections) via JSON
	// round-trip so CEL expressions can access every field.
	cfgAny, err := kvx.StructToMap(cfg)
	if err != nil {
		return fmt.Errorf("failed to normalize config: %w", err)
	}
	output, _ := cfgAny.(map[string]any)
	if output == nil {
		output = map[string]any{}
	}
	// Redact known sensitive leaves before rendering; view is a human-facing
	// dump so anything shown here can end up in terminal scrollback or logs.
	appconfig.RedactConfigMap(output)

	sources := mgr.Sources()

	if filter := appconfig.Source(o.SourceFilter); filter != "" {
		output = appconfig.FilterMapBySource(output, sources, filter, "")
	}

	output["configFile"] = mgr.ConfigPath()

	if o.ShowOrigin || appconfig.Source(o.SourceFilter) == appconfig.SourceEnv {
		output["envOverrides"] = mgr.EnvOverrides()
	}
	if o.ShowOrigin {
		output["sources"] = sourcesToStringMap(sources)
	}

	return o.writeOutput(ctx, output)
}

// sourcesToStringMap converts a Source map to a plain string map for
// stable YAML/JSON serialization and CEL access.
func sourcesToStringMap(in map[string]appconfig.Source) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = string(v)
	}
	return out
}

func (o *ViewOptions) writeOutput(ctx context.Context, data any) error {
	kvxOpts := flags.NewKvxOutputOptionsFromFlags(
		o.Output,
		o.Interactive,
		o.Expression,
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(o.CliParams.NoColor),
		kvx.WithOutputAppName(o.BinaryName+" config view"),
	)
	kvxOpts.IOStreams = o.IOStreams

	return kvxOpts.Write(data)
}
