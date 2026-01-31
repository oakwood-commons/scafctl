package config

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/MakeNowJust/heredoc/v2"
	appconfig "github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// UnsetOptions holds options for the config unset command.
type UnsetOptions struct {
	IOStreams  *terminal.IOStreams
	CliParams  *settings.Run
	ConfigPath string
	Key        string
}

// CommandUnset creates the 'config unset' command.
//
//nolint:dupl // Cobra command boilerplate is intentionally similar across commands
func CommandUnset(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &UnsetOptions{}

	cCmd := &cobra.Command{
		Use:   "unset <key>",
		Short: "Unset a configuration value",
		Long: heredoc.Doc(`
			Unset (remove) a configuration value by key.

			This resets the value to its default. Uses dot notation for nested values.

			Examples:
			  # Unset log level (resets to default 0)
			  scafctl config unset logging.level

			  # Unset default catalog
			  scafctl config unset settings.defaultCatalog
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
			opts.Key = args[0]

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

// Run executes the config unset command.
func (o *UnsetOptions) Run(ctx context.Context) error {
	w := writer.MustFromContext(ctx)

	mgr := appconfig.NewManager(o.ConfigPath)
	_, err := mgr.Load()
	if err != nil {
		return err
	}

	// Check if key exists
	if !mgr.IsSet(o.Key) {
		return fmt.Errorf("key %q is not set in configuration", o.Key)
	}

	// Get default value based on key
	defaultValue := o.getDefaultValue(o.Key)
	mgr.Set(o.Key, defaultValue)

	if err := mgr.Save(); err != nil {
		return err
	}

	w.Successf("Unset %s (reset to default: %v)", o.Key, defaultValue)
	return nil
}

// getDefaultValue returns the default value for a configuration key.
func (o *UnsetOptions) getDefaultValue(key string) any {
	defaults := map[string]any{
		"settings.defaultCatalog": "local",
		"settings.noColor":        false,
		"settings.quiet":          false,
		"logging.level":           0,
		"logging.format":          "json",
		"logging.timestamps":      true,
		"logging.enableProfiling": false,
	}

	if v, ok := defaults[key]; ok {
		return v
	}
	return nil
}
