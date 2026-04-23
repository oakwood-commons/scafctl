// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// IndexPushOptions holds options for the index push command.
type IndexPushOptions struct {
	Catalog  string // Target catalog (URL or config name, --catalog)
	Insecure bool   // Allow HTTP (--insecure)
	DryRun   bool   // Print the index without pushing (--dry-run)

	CliParams *settings.Run
	IOStreams *terminal.IOStreams
	flags.KvxOutputFlags
}

// IndexShowOptions holds options for the index show command.
type IndexShowOptions struct {
	Catalog  string // Target catalog (URL or config name, --catalog)
	Insecure bool   // Allow HTTP (--insecure)

	CliParams *settings.Run
	IOStreams *terminal.IOStreams
	flags.KvxOutputFlags
}

// IndexListItem represents an artifact entry in index output.
type IndexListItem struct {
	Kind string `json:"kind" yaml:"kind" doc:"Artifact kind (solution, provider, auth-handler)" example:"solution"`
	Name string `json:"name" yaml:"name" doc:"Artifact name" example:"hello-world"`
}

// CommandIndex creates the catalog index command group.
func CommandIndex(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Manage the catalog discovery index",
		Long: heredoc.Doc(`
			Manage the catalog discovery index artifact.

			The catalog index is an OCI artifact that lists all available
			artifacts in a registry. It enables artifact discovery for users
			who do not have permission to enumerate the registry directly.

			Supported artifact kinds: solution, provider, auth-handler.
		`),
		SilenceUsage: true,
	}

	cmd.AddCommand(commandIndexPush(cliParams, ioStreams))
	cmd.AddCommand(commandIndexShow(cliParams, ioStreams))

	return cmd
}

// commandIndexPush creates the index push subcommand.
func commandIndexPush(cliParams *settings.Run, ioStreams *terminal.IOStreams) *cobra.Command {
	opts := &IndexPushOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:   "push",
		Short: "Build and push the catalog index from the registry",
		Long: heredoc.Docf(`
			Discover all artifacts in the target catalog and push a catalog
			index artifact so that users without registry enumeration access
			can discover available packages.

			The command enumerates all solutions, providers, and auth-handlers
			in the registry, builds the index manifest, and pushes it to the
			well-known catalog-index repository.

			Examples:
			  # Push index for the default catalog
			  %[1]s catalog index push

			  # Push index for a named catalog
			  %[1]s catalog index push --catalog myregistry

			  # Preview what would be pushed without pushing
			  %[1]s catalog index push --dry-run

			  # Preview in JSON format
			  %[1]s catalog index push --dry-run -o json
		`, cliParams.BinaryName),
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.AppName = cliParams.BinaryName
			kvxOpts := flags.ToKvxOutputOptions(&opts.KvxOutputFlags,
				kvx.WithIOStreams(ioStreams),
				kvx.WithOutputColumnOrder([]string{"kind", "name"}),
			)
			return runIndexPush(cmd.Context(), opts, kvxOpts)
		},
	}

	cmd.Flags().StringVarP(&opts.Catalog, "catalog", "c", "", catalogFlagUsage)
	cmd.Flags().BoolVar(&opts.Insecure, "insecure", false, "Allow insecure HTTP connections")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Print the index without pushing")
	flags.AddKvxOutputFlagsToStruct(cmd, &opts.KvxOutputFlags)

	return cmd
}

// commandIndexShow creates the index show subcommand.
func commandIndexShow(cliParams *settings.Run, ioStreams *terminal.IOStreams) *cobra.Command {
	opts := &IndexShowOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Display the current catalog index",
		Long: heredoc.Docf(`
			Fetch and display the catalog index artifact from a remote registry.

			Shows the currently published index, which may differ from what
			a fresh 'index push' would generate if new artifacts have been
			added to the registry since the last push.

			Examples:
			  # Show index for the default catalog
			  %[1]s catalog index show

			  # Show index for a named catalog
			  %[1]s catalog index show --catalog myregistry

			  # Show in JSON format
			  %[1]s catalog index show -o json
		`, cliParams.BinaryName),
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.AppName = cliParams.BinaryName
			kvxOpts := flags.ToKvxOutputOptions(&opts.KvxOutputFlags,
				kvx.WithIOStreams(ioStreams),
				kvx.WithOutputColumnOrder([]string{"kind", "name"}),
			)
			return runIndexShow(cmd.Context(), opts, kvxOpts)
		},
	}

	cmd.Flags().StringVarP(&opts.Catalog, "catalog", "c", "", catalogFlagUsage)
	cmd.Flags().BoolVar(&opts.Insecure, "insecure", false, "Allow insecure HTTP connections")
	flags.AddKvxOutputFlagsToStruct(cmd, &opts.KvxOutputFlags)

	return cmd
}

// runIndexPush discovers all artifacts via registry enumeration and pushes
// the catalog index. With --dry-run it prints the index without pushing.
func runIndexPush(ctx context.Context, opts *IndexPushOptions, outputOpts *kvx.OutputOptions) error {
	w := writer.FromContext(ctx)

	remoteCatalog, err := createIndexRemoteCatalog(ctx, opts.Catalog, opts.Insecure)
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	artifacts, err := remoteCatalog.ListRepositories(ctx)
	if err != nil {
		w.Errorf("failed to discover artifacts: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	if len(artifacts) == 0 {
		w.WarnStderrf("No artifacts found in catalog — nothing to push.")
		return nil
	}

	if opts.DryRun {
		w.PlainStderrf("Dry run: %d artifact(s) would be indexed to catalog %q (%s):\n",
			len(artifacts), opts.Catalog, remoteCatalog.Registry())
		return writeIndexList(artifacts, outputOpts)
	}

	w.Infof("Pushing catalog index with %d artifact(s)...", len(artifacts))

	if err := remoteCatalog.PushIndex(ctx, artifacts); err != nil {
		err = fmt.Errorf("failed to push catalog index: %w", err)
		w.Errorf("%v", err)
		hintOnAuthError(ctx, w, remoteCatalog.Registry(), err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	w.Successf("Pushed catalog index with %d artifact(s)", len(artifacts))
	return writeIndexList(artifacts, outputOpts)
}

// runIndexShow fetches and displays the currently published catalog index.
func runIndexShow(ctx context.Context, opts *IndexShowOptions, outputOpts *kvx.OutputOptions) error {
	w := writer.FromContext(ctx)

	remoteCatalog, err := createIndexRemoteCatalog(ctx, opts.Catalog, opts.Insecure)
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	artifacts, err := remoteCatalog.FetchIndex(ctx)
	if err != nil {
		err = fmt.Errorf("failed to fetch catalog index: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	if len(artifacts) == 0 {
		w.Infof("Catalog index is empty.")
		return nil
	}

	w.PlainStderrf("Catalog index contains %d artifact(s):\n", len(artifacts))
	return writeIndexList(artifacts, outputOpts)
}

// createIndexRemoteCatalog creates a RemoteCatalog for index operations.
// It does not set a discovery strategy — index push uses ListRepositories
// (which enumerates via the registry API) and index show uses FetchIndex
// directly.
func createIndexRemoteCatalog(ctx context.Context, catalogFlag string, insecure bool) (*catalog.RemoteCatalog, error) {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	catalogURL, err := catalog.ResolveCatalogURL(ctx, catalogFlag)
	if err != nil {
		return nil, err
	}

	registry, repository := catalog.ParseCatalogURL(catalogURL)

	credStore, err := catalog.NewCredentialStore(*lgr)
	if err != nil {
		lgr.V(1).Info("failed to create credential store, using anonymous auth", "error", err.Error())
	}

	authHandler := resolveAuthHandler(ctx, registry, catalogFlag)
	authScope := resolveAuthScope(ctx, catalogFlag)

	verboseRemoteInfo(ctx, w, registry, repository, authHandler, authScope)

	return catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
		Name:            catalogFlag,
		Registry:        registry,
		Repository:      repository,
		CredentialStore: credStore,
		AuthHandler:     authHandler,
		AuthScope:       authScope,
		Insecure:        insecure,
		Logger:          *lgr,
	})
}

// writeIndexList writes the index artifact list using kvx output options.
func writeIndexList(artifacts []catalog.DiscoveredArtifact, outputOpts *kvx.OutputOptions) error {
	items := make([]IndexListItem, len(artifacts))
	for i, a := range artifacts {
		items[i] = IndexListItem{
			Kind: string(a.Kind),
			Name: a.Name,
		}
	}
	return outputOpts.Write(items)
}
