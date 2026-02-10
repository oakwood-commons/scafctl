// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	appconfig "github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// AddCatalogOptions holds options for the config add-catalog command.
type AddCatalogOptions struct {
	IOStreams  *terminal.IOStreams
	CliParams  *settings.Run
	ConfigPath string

	Name       string
	Type       string
	Path       string
	URL        string
	SetDefault bool
}

// CommandAddCatalog creates the 'config add-catalog' command.
//
//nolint:dupl // Cobra command boilerplate is intentionally similar across commands
func CommandAddCatalog(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &AddCatalogOptions{}

	cCmd := &cobra.Command{
		Use:   "add-catalog <name>",
		Short: "Add a catalog configuration",
		Long: heredoc.Docf(`
			Add a new catalog configuration.

			Supported catalog types: %s

			For filesystem catalogs, use --path to specify the local directory.
			For remote catalogs (oci, http), use --url to specify the endpoint.

			Examples:
			  # Add a filesystem catalog
			  scafctl config add-catalog local --type filesystem --path ./catalogs

			  # Add an OCI registry catalog
			  scafctl config add-catalog registry --type oci --url oci://registry.example.com/catalogs

			  # Add catalog and set as default
			  scafctl config add-catalog main --type filesystem --path ./main --default
		`, strings.Join(appconfig.ValidCatalogTypes(), ", ")),
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

	cCmd.Flags().StringVarP(&opts.Type, "type", "t", appconfig.CatalogTypeFilesystem,
		fmt.Sprintf("Catalog type (%s)", strings.Join(appconfig.ValidCatalogTypes(), ", ")))
	cCmd.Flags().StringVarP(&opts.Path, "path", "p", "",
		"Path for filesystem catalogs")
	cCmd.Flags().StringVarP(&opts.URL, "url", "u", "",
		"URL for remote catalogs (oci, http)")
	cCmd.Flags().BoolVarP(&opts.SetDefault, "default", "d", false,
		"Set as default catalog")

	return cCmd
}

// Run executes the config add-catalog command.
func (o *AddCatalogOptions) Run(ctx context.Context) error {
	w := writer.MustFromContext(ctx)

	// Validate type
	validType := false
	for _, t := range appconfig.ValidCatalogTypes() {
		if o.Type == t {
			validType = true
			break
		}
	}
	if !validType {
		err := fmt.Errorf("invalid catalog type %q, must be one of: %s",
			o.Type, strings.Join(appconfig.ValidCatalogTypes(), ", "))
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Validate path/url based on type
	if o.Type == appconfig.CatalogTypeFilesystem {
		if o.Path == "" {
			err := fmt.Errorf("--path is required for filesystem catalogs")
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
	} else {
		if o.URL == "" {
			err := fmt.Errorf("--url is required for %s catalogs", o.Type)
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
	}

	mgr := appconfig.NewManager(o.ConfigPath)
	cfg, err := mgr.Load()
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.ConfigError)
	}

	catalog := appconfig.CatalogConfig{
		Name: o.Name,
		Type: o.Type,
		Path: o.Path,
		URL:  o.URL,
	}

	if err := cfg.AddCatalog(catalog); err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	if o.SetDefault {
		cfg.Settings.DefaultCatalog = o.Name
	}

	// Update viper and save
	mgr.Set("catalogs", cfg.Catalogs)
	mgr.Set("settings", cfg.Settings)

	if err := mgr.Save(); err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.ConfigError)
	}

	w.Successf("Added catalog %q", o.Name)
	if o.SetDefault {
		w.Infof("Set %q as default catalog", o.Name)
	}

	return nil
}
