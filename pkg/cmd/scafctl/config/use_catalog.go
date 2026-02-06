package config

import (
	"context"
	"path/filepath"

	"github.com/MakeNowJust/heredoc/v2"
	appconfig "github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// UseCatalogOptions holds options for the config use-catalog command.
type UseCatalogOptions struct {
	IOStreams  *terminal.IOStreams
	CliParams  *settings.Run
	ConfigPath string
	Name       string
}

// CommandUseCatalog creates the 'config use-catalog' command.
//
//nolint:dupl // Cobra command boilerplate is intentionally similar across commands
func CommandUseCatalog(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &UseCatalogOptions{}

	cCmd := &cobra.Command{
		Use:   "use-catalog <name>",
		Short: "Set the default catalog",
		Long: heredoc.Doc(`
			Set a catalog as the default.

			The default catalog is used when no --catalog flag is specified.

			Examples:
			  # Set default catalog
			  scafctl config use-catalog my-catalog

			  # Clear default catalog (use empty string)
			  scafctl config use-catalog ""
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cCmd *cobra.Command, args []string) error {
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
			opts.Name = args[0]

			// Get config path from parent command context
			if configFlag := cCmd.Root().Flag("config"); configFlag != nil && configFlag.Value.String() != "" {
				opts.ConfigPath = configFlag.Value.String()
			}

			return opts.Run(ctx)
		},
		SilenceUsage: true,
	}

	return cCmd
}

// Run executes the config use-catalog command.
func (o *UseCatalogOptions) Run(ctx context.Context) error {
	w := writer.MustFromContext(ctx)

	mgr := appconfig.NewManager(o.ConfigPath)
	cfg, err := mgr.Load()
	if err != nil {
		return err
	}

	// Validate catalog exists (unless clearing)
	if err := cfg.SetDefaultCatalog(o.Name); err != nil {
		return err
	}

	// Update viper and save
	mgr.Set("settings.defaultCatalog", o.Name)

	if err := mgr.Save(); err != nil {
		return err
	}

	if o.Name == "" {
		w.Success("Cleared default catalog")
	} else {
		w.Successf("Set default catalog to %q", o.Name)
	}

	return nil
}
