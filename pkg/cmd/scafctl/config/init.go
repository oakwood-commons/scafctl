package config

import (
	"context"
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

//go:embed templates/*.yaml
var configTemplates embed.FS

// InitOptions holds options for the config init command.
type InitOptions struct {
	IOStreams  *terminal.IOStreams
	CliParams  *settings.Run
	ConfigPath string
	Full       bool
	DryRun     bool
	Output     string
	Force      bool
}

// CommandInit creates the 'config init' command.
//
//nolint:dupl // Cobra command boilerplate is intentionally similar across commands
func CommandInit(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &InitOptions{}

	cCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a configuration file",
		Long: heredoc.Doc(`
			Initialize a scafctl configuration file.

			By default, creates a minimal configuration file at ~/.scafctl/config.yaml.
			Use --full to include all available options with documentation.

			The command will not overwrite an existing config file unless --force is used.

			Examples:
			  # Create minimal config at default location
			  scafctl config init

			  # Create full config with all options documented
			  scafctl config init --full

			  # Preview config without writing (dry run)
			  scafctl config init --dry-run

			  # Write to custom location
			  scafctl config init --output ./my-config.yaml

			  # Overwrite existing config
			  scafctl config init --force
		`),
		Args: cobra.NoArgs,
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

	cCmd.Flags().BoolVar(&opts.Full, "full", false, "Generate full config with all options documented")
	cCmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Preview config without writing to file")
	cCmd.Flags().StringVarP(&opts.Output, "output", "o", "", "Output file path (default: ~/.scafctl/config.yaml)")
	cCmd.Flags().BoolVar(&opts.Force, "force", false, "Overwrite existing config file")

	return cCmd
}

// Run executes the config init command.
func (o *InitOptions) Run(ctx context.Context) error {
	w := writer.MustFromContext(ctx)

	// Determine output path
	outputPath := o.Output
	if outputPath == "" {
		if o.ConfigPath != "" {
			outputPath = o.ConfigPath
		} else {
			var err error
			outputPath, err = paths.ConfigFile()
			if err != nil {
				err = fmt.Errorf("failed to determine config path: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.ConfigError)
			}
		}
	}

	// Check if file exists (unless dry-run or force)
	if !o.DryRun && !o.Force {
		if _, err := os.Stat(outputPath); err == nil {
			err := fmt.Errorf("config file already exists: %s\nUse --force to overwrite or --output to specify a different location", outputPath)
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.ConfigError)
		}
	}

	// Select template
	templateName := "templates/minimal.yaml"
	if o.Full {
		templateName = "templates/full.yaml"
	}

	content, err := configTemplates.ReadFile(templateName)
	if err != nil {
		err = fmt.Errorf("failed to read config template: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	// Dry run - just print
	if o.DryRun {
		w.Infof("# Would write to: %s\n", outputPath)
		w.Plainf("%s", string(content))
		return nil
	}

	// Ensure directory exists
	configDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		err = fmt.Errorf("failed to create config directory: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.ConfigError)
	}

	// Write file
	if err := os.WriteFile(outputPath, content, 0o600); err != nil {
		err = fmt.Errorf("failed to write config file: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.ConfigError)
	}

	w.Successf("Created config file: %s\n", outputPath)
	if o.Full {
		w.Infof("Full config with all options. Edit as needed.\n")
	} else {
		w.Infof("Minimal config created. Use 'scafctl config init --full' for all options.\n")
	}

	return nil
}
