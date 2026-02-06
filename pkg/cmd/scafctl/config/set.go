package config

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	appconfig "github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// SetOptions holds options for the config set command.
type SetOptions struct {
	IOStreams  *terminal.IOStreams
	CliParams  *settings.Run
	ConfigPath string
	Key        string
	Value      string
}

// CommandSet creates the 'config set' command.
func CommandSet(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &SetOptions{}

	cCmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Long: heredoc.Doc(`
			Set a configuration value by key.

			Uses dot notation for nested values (e.g., settings.noColor).
			Boolean values: true, false
			Integer values: will be parsed as integers where appropriate

			Examples:
			  # Set log level
			  scafctl config set logging.level 1

			  # Enable quiet mode
			  scafctl config set settings.quiet true

			  # Disable colored output
			  scafctl config set settings.noColor true

			  # Set default catalog
			  scafctl config set settings.defaultCatalog my-catalog
		`),
		Args: cobra.ExactArgs(2),
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
			opts.Value = args[1]

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

// Run executes the config set command.
func (o *SetOptions) Run(ctx context.Context) error {
	w := writer.MustFromContext(ctx)

	mgr := appconfig.NewManager(o.ConfigPath)
	_, err := mgr.Load()
	if err != nil {
		return err
	}

	// Parse value to appropriate type
	value := o.parseValue(o.Value)

	mgr.Set(o.Key, value)

	if err := mgr.Save(); err != nil {
		return err
	}

	w.Successf("Set %s = %v", o.Key, value)
	return nil
}

// parseValue attempts to parse a string value to bool or int if applicable.
func (o *SetOptions) parseValue(s string) any {
	// Try bool
	lower := strings.ToLower(s)
	if lower == "true" {
		return true
	}
	if lower == "false" {
		return false
	}

	// Try int
	if i, err := strconv.Atoi(s); err == nil {
		return i
	}

	// Return as string
	return s
}
