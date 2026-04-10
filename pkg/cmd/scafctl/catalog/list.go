// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"
	"sort"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
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

// ListOptions holds options for the list command.
type ListOptions struct {
	Kind        string
	Name        string
	Catalog     string // Remote catalog (registry URL or config name)
	Insecure    bool   // Allow HTTP connections
	AllVersions bool   // Show all versions instead of just latest
	CliParams   *settings.Run
	IOStreams   *terminal.IOStreams
	flags.KvxOutputFlags
}

// ArtifactListItem represents an artifact in list output.
type ArtifactListItem struct {
	Name      string `json:"name" yaml:"name"`
	Version   string `json:"version" yaml:"version"`
	Tag       string `json:"tag" yaml:"tag"`
	Kind      string `json:"kind" yaml:"kind"`
	Digest    string `json:"digest" yaml:"digest"`
	CreatedAt string `json:"createdAt" yaml:"createdAt"`
	Catalog   string `json:"catalog" yaml:"catalog"`
}

// artifactListSchema controls table column display. Columns in the "required" array
// (name, tag, kind, catalog) resist truncation; digest is visible but lower priority.
// version and createdAt are hidden in table view but included in json/yaml output.
var artifactListSchema = []byte(`{
	"type": "array",
	"items": {
		"type": "object",
		"required": ["name", "tag", "kind", "catalog"],
		"properties": {
			"name":      { "type": "string", "title": "Name" },
			"tag":       { "type": "string", "title": "Tag" },
			"kind":      { "type": "string", "title": "Kind" },
			"catalog":   { "type": "string", "title": "Catalog" },
			"version":   { "type": "string", "deprecated": true },
			"digest":    { "type": "string", "title": "Digest" },
			"createdAt": { "type": "string", "deprecated": true }
		}
	}
}`)

// CommandList creates the list command.
func CommandList(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &ListOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List artifacts in the catalog",
		SilenceUsage: true,
		Long: heredoc.Docf(`
			List artifacts stored in the local catalog, or in a remote registry
			when --catalog is specified.

			By default, only the latest version of each artifact is shown.
			Use --name to see all versions of a specific artifact, or
			--all-versions to see everything.

			Filter by kind (solution, provider, auth-handler) to narrow results.
			When listing from a remote registry, --name is required (OCI registries
			do not support registry-wide enumeration).

			Examples:
			  # List latest version of each artifact
			  %[1]s catalog list

			  # List all versions of a specific artifact
			  %[1]s catalog list --name my-solution

			  # List all versions of all artifacts
			  %[1]s catalog list --all-versions

			  # List only solutions
			  %[1]s catalog list --kind solution

			  # List remote versions of an artifact
			  %[1]s catalog list --catalog my-registry --name my-solution

			  # Output as JSON
			  %[1]s catalog list -o json
		`, cliParams.BinaryName),
		RunE: func(cmd *cobra.Command, _ []string) error {
			kvxOpts := flags.ToKvxOutputOptions(&options.KvxOutputFlags,
				kvx.WithIOStreams(ioStreams),
				kvx.WithOutputColumnOrder([]string{"name", "tag", "kind", "digest", "catalog"}),
				kvx.WithOutputSchemaJSON(artifactListSchema),
			)
			return runList(cmd.Context(), options, kvxOpts)
		},
	}

	cmd.Flags().StringVar(&options.Kind, "kind", "", "Filter by artifact kind (solution, provider, auth-handler)")
	cmd.Flags().StringVar(&options.Name, "name", "", "Filter by artifact name (shows all versions when set)")
	cmd.Flags().StringVarP(&options.Catalog, "catalog", "c", "", catalogFlagUsage)
	cmd.Flags().BoolVar(&options.Insecure, "insecure", false, "Allow insecure HTTP connections")
	cmd.Flags().BoolVar(&options.AllVersions, "all-versions", false, "Show all versions instead of just the latest")

	flags.AddKvxOutputFlagsToStruct(cmd, &options.KvxOutputFlags)

	return cmd
}

func runList(ctx context.Context, opts *ListOptions, outputOpts *kvx.OutputOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Parse kind filter
	var kind catalog.ArtifactKind
	if opts.Kind != "" {
		kind = catalog.ArtifactKind(opts.Kind)
		if !kind.IsValid() {
			w.Errorf("invalid kind %q: must be 'solution', 'provider', or 'auth-handler'", opts.Kind)
			return exitcode.Errorf("invalid kind")
		}
	}

	// Remote catalog listing (specific catalog)
	if opts.Catalog != "" {
		return runListRemote(ctx, opts, kind, outputOpts)
	}

	// Create local catalog
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		w.Errorf("failed to open catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// List local artifacts
	artifacts, err := localCatalog.List(ctx, kind, opts.Name)
	if err != nil {
		w.Errorf("failed to list artifacts: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// When --name is specified, also search configured remote catalogs
	// to give a unified view of where the artifact lives.
	if opts.Name != "" {
		if cfg := appconfig.FromContext(ctx); cfg != nil {
			for _, catCfg := range cfg.Catalogs {
				if catCfg.Type != appconfig.CatalogTypeOCI {
					continue
				}
				remoteOpts := &ListOptions{
					Name:      opts.Name,
					Kind:      opts.Kind,
					Catalog:   catCfg.Name,
					Insecure:  opts.Insecure,
					CliParams: opts.CliParams,
					IOStreams: opts.IOStreams,
				}
				remoteArtifacts, remoteErr := listRemoteArtifacts(ctx, remoteOpts, kind)
				if remoteErr != nil {
					w.Warningf("failed to list from remote catalog %q: %v", catCfg.Name, remoteErr)
					if catalogURL, resolveErr := catalog.ResolveCatalogURL(ctx, catCfg.Name); resolveErr == nil {
						registry, _ := catalog.ParseCatalogURL(catalogURL)
						hintOnAuthError(ctx, w, registry, remoteErr)
					}
					continue
				}
				artifacts = append(artifacts, remoteArtifacts...)
			}
		}
	}

	return writeArtifactList(artifacts, opts.Name != "" || opts.AllVersions, outputOpts)
}

func runListRemote(ctx context.Context, opts *ListOptions, kind catalog.ArtifactKind, outputOpts *kvx.OutputOptions) error {
	w := writer.FromContext(ctx)

	if opts.Name == "" {
		w.Error("--name is required when listing from a remote catalog (OCI registries do not support registry-wide enumeration)")
		return exitcode.Errorf("--name required for remote listing")
	}

	artifacts, err := listRemoteArtifacts(ctx, opts, kind)
	if err != nil {
		w.Errorf("failed to list remote artifacts: %v", err)

		// Resolve registry for auth hint
		if catalogURL, resolveErr := catalog.ResolveCatalogURL(ctx, opts.Catalog); resolveErr == nil {
			registry, _ := catalog.ParseCatalogURL(catalogURL)
			hintOnAuthError(ctx, w, registry, err)
		}

		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	return writeArtifactList(artifacts, opts.Name != "" || opts.AllVersions, outputOpts)
}

// listRemoteArtifacts fetches artifacts from a configured remote catalog.
// This is shared between runListRemote (explicit --catalog) and the
// all-catalogs search in runList (--name without --catalog).
func listRemoteArtifacts(ctx context.Context, opts *ListOptions, kind catalog.ArtifactKind) ([]catalog.ArtifactInfo, error) {
	lgr := logger.FromContext(ctx)

	catalogURL, err := catalog.ResolveCatalogURL(ctx, opts.Catalog)
	if err != nil {
		return nil, err
	}

	registry, repository := catalog.ParseCatalogURL(catalogURL)

	credStore, err := catalog.NewCredentialStore(*lgr)
	if err != nil {
		lgr.V(1).Info("failed to create credential store, using anonymous auth", "error", err.Error())
	}

	authHandler := resolveAuthHandler(ctx, registry, opts.Catalog)
	authScope := resolveAuthScope(ctx, opts.Catalog)

	remoteCatalog, err := catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
		Name:            registry,
		Registry:        registry,
		Repository:      repository,
		CredentialStore: credStore,
		AuthHandler:     authHandler,
		AuthScope:       authScope,
		Insecure:        opts.Insecure,
		Logger:          *lgr,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create remote catalog: %w", err)
	}

	return remoteCatalog.List(ctx, kind, opts.Name)
}

func writeArtifactList(artifacts []catalog.ArtifactInfo, showAll bool, outputOpts *kvx.OutputOptions) error {
	// Sort by name, then version descending
	sort.Slice(artifacts, func(i, j int) bool {
		if artifacts[i].Reference.Name != artifacts[j].Reference.Name {
			return artifacts[i].Reference.Name < artifacts[j].Reference.Name
		}
		vi := artifacts[i].Reference.Version
		vj := artifacts[j].Reference.Version
		if vi == nil {
			return false
		}
		if vj == nil {
			return true
		}
		return vi.GreaterThan(vj)
	})

	// When not showing all versions, keep only the latest per name+kind
	if !showAll {
		seen := make(map[string]bool)
		filtered := artifacts[:0]
		for _, a := range artifacts {
			key := string(a.Reference.Kind) + "/" + a.Reference.Name
			if !seen[key] {
				seen[key] = true
				filtered = append(filtered, a)
			}
		}
		artifacts = filtered
	}

	// Convert to output format
	items := make([]ArtifactListItem, len(artifacts))
	for i, a := range artifacts {
		version := ""
		if a.Reference.Version != nil {
			version = a.Reference.Version.String()
		}
		tag := a.Tag
		if tag == "" {
			tag = version
		}
		items[i] = ArtifactListItem{
			Name:      a.Reference.Name,
			Version:   version,
			Tag:       tag,
			Kind:      string(a.Reference.Kind),
			Digest:    a.Digest,
			CreatedAt: a.CreatedAt.Format("2006-01-02 15:04:05"),
			Catalog:   a.Catalog,
		}
	}

	return outputOpts.Write(items)
}
