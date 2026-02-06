package config

import (
	"context"
	"path/filepath"

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
	IOStreams  *terminal.IOStreams
	CliParams  *settings.Run
	ConfigPath string

	flags.KvxOutputFlags
}

// CommandView creates the 'config view' command.
func CommandView(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &ViewOptions{}

	cCmd := &cobra.Command{
		Use:   "view",
		Short: "View current configuration",
		Long: heredoc.Doc(`
			Display the current configuration.

			Shows all settings from the config file merged with environment overrides.

			Examples:
			  # View config as YAML
			  scafctl config view

			  # View config as JSON
			  scafctl config view -o json

			  # View specific section using CEL
			  scafctl config view -e '_.catalogs'
		`),
		RunE: func(cCmd *cobra.Command, _ []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Use)
			ctx := settings.IntoContext(context.Background(), cliParams)

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

			// Get config path from parent command context
			if configFlag := cCmd.Root().Flag("config"); configFlag != nil && configFlag.Value.String() != "" {
				opts.ConfigPath = configFlag.Value.String()
			}

			return opts.Run(ctx)
		},
		SilenceUsage: true,
	}

	flags.AddKvxOutputFlagsToStruct(cCmd, &opts.KvxOutputFlags)
	// Default to yaml for config view
	_ = cCmd.Flags().Set("output", "yaml")

	return cCmd
}

// Run executes the config view command.
func (o *ViewOptions) Run(ctx context.Context) error {
	w := writer.MustFromContext(ctx)

	mgr := appconfig.NewManager(o.ConfigPath)
	cfg, err := mgr.Load()
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.ConfigError)
	}

	// Include config file path in output
	output := map[string]any{
		"configFile": mgr.ConfigPath(),
		"catalogs":   cfg.Catalogs,
		"settings":   cfg.Settings,
	}

	return o.writeOutput(ctx, output)
}

func (o *ViewOptions) writeOutput(ctx context.Context, data any) error {
	kvxOpts := flags.NewKvxOutputOptionsFromFlags(
		o.Output,
		o.Interactive,
		o.Expression,
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(o.CliParams.NoColor),
		kvx.WithOutputAppName("scafctl config view"),
	)
	kvxOpts.IOStreams = o.IOStreams

	return kvxOpts.Write(data)
}
