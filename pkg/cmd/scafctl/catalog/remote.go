// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	appconfig "github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"

	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
)

// CommandRemote creates the 'catalog remote' command group.
func CommandRemote(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "remote",
		Short:        "Manage remote catalog registries",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Manage remote catalog registries.

			Add, remove, and list configured remote registries for pushing and
			pulling artifacts.
		`),
	}

	cmdPath := fmt.Sprintf("%s/%s", path, cmd.Use)
	cmd.AddCommand(commandRemoteAdd(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(commandRemoteRemove(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(commandRemoteSetDefault(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(commandRemoteList(cliParams, ioStreams, cmdPath))

	return cmd
}

// --- remote add ---

// RemoteAddOptions holds options for the remote add command.
type RemoteAddOptions struct {
	IOStreams  *terminal.IOStreams
	CliParams  *settings.Run
	ConfigPath string

	Name         string
	Type         string
	Path         string
	URL          string
	SetDefault   bool
	Force        bool
	AuthProvider string
	AuthScope    string
}

func commandRemoteAdd(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	opts := &RemoteAddOptions{
		IOStreams: ioStreams,
		CliParams: cliParams,
	}

	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a remote catalog",
		Long: heredoc.Docf(`
			Add a new remote catalog configuration.

			Supported catalog types: %s

			For filesystem catalogs, use --path to specify the local directory.
			For remote catalogs (oci, http), use --url to specify the endpoint.

			Examples:
			  # Add a filesystem catalog
			  %[2]s catalog remote add local --type filesystem --path ./catalogs

			  # Add an OCI registry catalog
			  %[2]s catalog remote add registry --type oci --url oci://ghcr.io/myorg

			  # Add catalog and set as default
			  %[2]s catalog remote add main --type oci --url oci://ghcr.io/myorg --default

			  # Add OCI catalog with auth provider (no separate catalog login needed)
			  %[2]s catalog remote add registry --type oci --url oci://ghcr.io/myorg --auth-provider github
		`, strings.Join(appconfig.ValidCatalogTypes(), ", "), settings.CliBinaryName),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Name = args[0]

			if configFlag := cmd.Root().Flag("config"); configFlag != nil && configFlag.Value.String() != "" {
				opts.ConfigPath = configFlag.Value.String()
			}

			return runRemoteAdd(cmd.Context(), opts)
		},
		SilenceUsage: true,
	}

	cmd.Flags().StringVarP(&opts.Type, "type", "t", appconfig.CatalogTypeFilesystem,
		fmt.Sprintf("Catalog type (%s)", strings.Join(appconfig.ValidCatalogTypes(), ", ")))
	cmd.Flags().StringVarP(&opts.Path, "path", "p", "", "Path for filesystem catalogs")
	cmd.Flags().StringVarP(&opts.URL, "url", "u", "", "URL for remote catalogs (oci, http)")
	cmd.Flags().BoolVar(&opts.SetDefault, "default", false, "Set as default catalog")
	cmd.Flags().BoolVarP(&opts.Force, "force", "f", false, "Overwrite if catalog already exists")
	cmd.Flags().StringVar(&opts.AuthProvider, "auth-provider", "", "Auth handler for automatic token injection (e.g. github, gcp, entra)")
	cmd.Flags().StringVar(&opts.AuthScope, "auth-scope", "", "OAuth scope for auth provider token requests")

	return cmd
}

func runRemoteAdd(ctx context.Context, opts *RemoteAddOptions) error {
	w := writer.FromContext(ctx)

	if !appconfig.IsValidCatalogType(opts.Type) {
		err := fmt.Errorf("invalid catalog type %q, must be one of: %s",
			opts.Type, strings.Join(appconfig.ValidCatalogTypes(), ", "))
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	if opts.Type == appconfig.CatalogTypeFilesystem {
		if opts.Path == "" {
			err := fmt.Errorf("--path is required for filesystem catalogs")
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
	} else {
		if opts.URL == "" {
			err := fmt.Errorf("--url is required for %s catalogs", opts.Type)
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
	}

	mgr := appconfig.NewManager(opts.ConfigPath, appconfig.ManagerOptionsFromContext(ctx)...)
	cfg, err := mgr.Load()
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.ConfigError)
	}

	cat := appconfig.CatalogConfig{
		Name:         opts.Name,
		Type:         opts.Type,
		Path:         opts.Path,
		URL:          opts.URL,
		AuthProvider: opts.AuthProvider,
		AuthScope:    opts.AuthScope,
	}

	if err := cfg.AddCatalog(cat); err != nil {
		if !opts.Force {
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.CatalogError)
		}
		// Force: remove existing and re-add
		_ = cfg.RemoveCatalog(opts.Name)
		if err := cfg.AddCatalog(cat); err != nil {
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.CatalogError)
		}
	}

	if opts.SetDefault {
		cfg.Settings.DefaultCatalog = opts.Name
	}

	mgr.Set("catalogs", cfg.Catalogs)
	mgr.Set("settings", cfg.Settings)

	if err := mgr.Save(); err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.ConfigError)
	}

	w.Successf("Added catalog %q", opts.Name)
	if opts.SetDefault {
		w.Infof("Set %q as default catalog", opts.Name)
	}

	return nil
}

// --- remote remove ---

// RemoteRemoveOptions holds options for the remote remove command.
type RemoteRemoveOptions struct {
	IOStreams  *terminal.IOStreams
	CliParams  *settings.Run
	ConfigPath string
	Name       string
}

func commandRemoteRemove(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	opts := &RemoteRemoveOptions{
		IOStreams: ioStreams,
		CliParams: cliParams,
	}

	cmd := &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm"},
		Short:   "Remove a remote catalog",
		Long: heredoc.Docf(`
			Remove a catalog configuration by name.

			If the removed catalog was the default, the default will be cleared.

			Examples:
			  %s catalog remote remove old-registry
		`, settings.CliBinaryName),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Name = args[0]

			if configFlag := cmd.Root().Flag("config"); configFlag != nil && configFlag.Value.String() != "" {
				opts.ConfigPath = configFlag.Value.String()
			}

			return runRemoteRemove(cmd.Context(), opts)
		},
		SilenceUsage: true,
	}

	return cmd
}

func runRemoteRemove(ctx context.Context, opts *RemoteRemoveOptions) error {
	w := writer.FromContext(ctx)

	mgr := appconfig.NewManager(opts.ConfigPath, appconfig.ManagerOptionsFromContext(ctx)...)
	cfg, err := mgr.Load()
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.ConfigError)
	}

	if err := cfg.RemoveCatalog(opts.Name); err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	wasDefault := cfg.Settings.DefaultCatalog == opts.Name
	if wasDefault {
		cfg.Settings.DefaultCatalog = ""
	}

	mgr.Set("catalogs", cfg.Catalogs)
	mgr.Set("settings", cfg.Settings)

	if err := mgr.Save(); err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.ConfigError)
	}

	w.Successf("Removed catalog %q", opts.Name)
	if wasDefault {
		w.Warning("Cleared default catalog (was the removed catalog)")
	}

	return nil
}

// --- remote set-default ---

// RemoteSetDefaultOptions holds options for the remote set-default command.
type RemoteSetDefaultOptions struct {
	IOStreams  *terminal.IOStreams
	CliParams  *settings.Run
	ConfigPath string
	Name       string
}

func commandRemoteSetDefault(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	opts := &RemoteSetDefaultOptions{
		IOStreams: ioStreams,
		CliParams: cliParams,
	}

	cmd := &cobra.Command{
		Use:   "set-default <name>",
		Short: "Set the default catalog",
		Long: heredoc.Docf(`
			Set a catalog as the default.

			The default catalog is used when no --catalog flag is specified.

			Examples:
			  # Set default catalog
			  %[1]s catalog remote set-default my-registry

			  # Clear default catalog
			  %[1]s catalog remote set-default ""
		`, settings.CliBinaryName),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Name = args[0]

			if configFlag := cmd.Root().Flag("config"); configFlag != nil && configFlag.Value.String() != "" {
				opts.ConfigPath = configFlag.Value.String()
			}

			return runRemoteSetDefault(cmd.Context(), opts)
		},
		SilenceUsage: true,
	}

	return cmd
}

func runRemoteSetDefault(ctx context.Context, opts *RemoteSetDefaultOptions) error {
	w := writer.FromContext(ctx)

	mgr := appconfig.NewManager(opts.ConfigPath, appconfig.ManagerOptionsFromContext(ctx)...)
	cfg, err := mgr.Load()
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.ConfigError)
	}

	if err := cfg.SetDefaultCatalog(opts.Name); err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	mgr.Set("settings.defaultCatalog", opts.Name)

	if err := mgr.Save(); err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.ConfigError)
	}

	if opts.Name == "" {
		w.Success("Cleared default catalog")
	} else {
		w.Successf("Set default catalog to %q", opts.Name)
	}

	return nil
}

// --- remote list ---

// RemoteListItem represents a catalog entry in list output.
// JSON fields must not use omitempty for fields in the table schema -- kvx
// requires consistent keys across all array items for column rendering.
type RemoteListItem struct {
	Name         string `json:"name" yaml:"name"`
	Type         string `json:"type" yaml:"type"`
	URL          string `json:"url" yaml:"url"`
	Path         string `json:"path" yaml:"path,omitempty"`
	AuthProvider string `json:"authProvider" yaml:"authProvider"`
	AuthScope    string `json:"authScope" yaml:"authScope,omitempty"`
	Default      bool   `json:"default" yaml:"default"`
}

// remoteListSchema controls table display for catalog remote list.
var remoteListSchema = []byte(`{
	"type": "array",
	"items": {
		"type": "object",
		"properties": {
			"name":         { "type": "string", "title": "Name" },
			"type":         { "type": "string", "title": "Type" },
			"url":          { "type": "string", "title": "URL" },
			"authProvider": { "type": "string", "title": "Auth" },
			"default":      { "type": "boolean", "title": "Default" },
			"path":         { "type": "string", "deprecated": true },
			"authScope":    { "type": "string", "deprecated": true }
		}
	}
}`)

func commandRemoteList(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	var kvxFlags flags.KvxOutputFlags

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List configured catalogs",
		Long: heredoc.Docf(`
			List all configured remote catalogs.

			Examples:
			  %[1]s catalog remote list
			  %[1]s catalog remote list -o json
		`, cliParams.BinaryName),
		RunE: func(cmd *cobra.Command, _ []string) error {
			kvxFlags.AppName = cliParams.BinaryName
			outputOpts := flags.ToKvxOutputOptions(&kvxFlags,
				kvx.WithIOStreams(ioStreams),
				kvx.WithOutputColumnOrder([]string{"name", "type", "url", "authProvider", "default"}),
				kvx.WithOutputSchemaJSON(remoteListSchema),
			)

			configPath := ""
			if configFlag := cmd.Root().Flag("config"); configFlag != nil && configFlag.Value.String() != "" {
				configPath = configFlag.Value.String()
			}

			return runRemoteList(cmd.Context(), configPath, outputOpts)
		},
		SilenceUsage: true,
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &kvxFlags)

	return cmd
}

func runRemoteList(ctx context.Context, configPath string, outputOpts *kvx.OutputOptions) error {
	w := writer.FromContext(ctx)

	mgr := appconfig.NewManager(configPath, appconfig.ManagerOptionsFromContext(ctx)...)
	cfg, err := mgr.Load()
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.ConfigError)
	}
	items := make([]RemoteListItem, 0, len(cfg.Catalogs))
	for _, c := range cfg.Catalogs {
		items = append(items, RemoteListItem{
			Name:         c.Name,
			Type:         c.Type,
			URL:          c.URL,
			Path:         c.Path,
			AuthProvider: c.AuthProvider,
			AuthScope:    c.AuthScope,
			Default:      c.Name == cfg.Settings.DefaultCatalog,
		})
	}
	return outputOpts.Write(items)
}
