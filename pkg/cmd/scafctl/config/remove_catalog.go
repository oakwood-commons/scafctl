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

// RemoveCatalogOptions holds options for the config remove-catalog command.
type RemoveCatalogOptions struct {
	IOStreams  *terminal.IOStreams
	CliParams  *settings.Run
	ConfigPath string
	Name       string
}

// CommandRemoveCatalog creates the 'config remove-catalog' command.
//
//nolint:dupl // Cobra command boilerplate is intentionally similar across commands
func CommandRemoveCatalog(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &RemoveCatalogOptions{}

	cCmd := &cobra.Command{
		Use:     "remove-catalog <name>",
		Aliases: []string{"rm-catalog"},
		Short:   "Remove a catalog configuration",
		Long: heredoc.Doc(`
			Remove a catalog configuration by name.

			If the removed catalog was the default, the default will be cleared.

			Examples:
			  # Remove a catalog
			  scafctl config remove-catalog old-catalog
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

// Run executes the config remove-catalog command.
func (o *RemoveCatalogOptions) Run(ctx context.Context) error {
	w := writer.MustFromContext(ctx)

	mgr := appconfig.NewManager(o.ConfigPath)
	cfg, err := mgr.Load()
	if err != nil {
		return err
	}

	if err := cfg.RemoveCatalog(o.Name); err != nil {
		return err
	}

	// Clear default if it was the removed catalog
	wasDefault := cfg.Settings.DefaultCatalog == o.Name
	if wasDefault {
		cfg.Settings.DefaultCatalog = ""
	}

	// Update viper and save
	mgr.Set("catalogs", cfg.Catalogs)
	mgr.Set("settings", cfg.Settings)

	if err := mgr.Save(); err != nil {
		return err
	}

	w.Successf("Removed catalog %q", o.Name)
	if wasDefault {
		w.Warning("Cleared default catalog (was the removed catalog)")
	}

	return nil
}
