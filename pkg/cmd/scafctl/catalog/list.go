// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"
	"sort"
	"strings"

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
	Kind              string
	Name              string
	Search            string // Case-insensitive substring to filter artifact names (e.g. "starter")
	Catalog           string // Remote catalog (registry URL or config name)
	VersionConstraint string // Semver version constraint (e.g., "^1.0.0", ">= 1.0, < 2.0")
	Insecure          bool   // Allow HTTP connections
	AllVersions       bool   // Show all versions instead of just latest
	PreRelease        bool   // Include pre-release versions
	ShowAll           bool   // List all configured catalogs instead of just the default
	CliParams         *settings.Run
	IOStreams         *terminal.IOStreams
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
			List artifacts from the default catalog and local catalog.
			Use --catalog to list from a specific catalog, or --all to
			list from all configured catalogs.

			By default, only the latest version of each artifact is shown.
			Use --name to see all versions of a specific artifact, or
			--all-versions to see everything.

			Filter by kind (solution, provider, auth-handler) to narrow results.
			When listing from a remote registry without --name, all artifacts
			are enumerated via the OCI _catalog endpoint (requires registry support).

			You can also pass a full OCI reference to --name to list directly from
			a remote registry without --catalog:
			  %[1]s catalog list --name ghcr.io/myorg/solutions/my-solution

			Examples:
			  # List latest version of each artifact
			  %[1]s catalog list

			  # List all versions of a specific artifact
			  %[1]s catalog list --name my-solution

			  # List all versions of all artifacts
			  %[1]s catalog list --all-versions

			  # List only solutions
			  %[1]s catalog list --kind solution

			  # List all artifacts in a remote catalog
			  %[1]s catalog list --catalog my-registry

			  # List all configured catalogs
			  %[1]s catalog list --all

			  # List remote versions of a specific artifact
			  %[1]s catalog list --catalog my-registry --name my-solution

			  # List via full OCI reference
			  %[1]s catalog list --name ghcr.io/myorg/solutions/my-solution

			  # Output as JSON
			  %[1]s catalog list -o json
		`, cliParams.BinaryName),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			options.AppName = cliParams.BinaryName
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
	cmd.Flags().StringVarP(&options.Search, "search", "s", "", "Filter artifacts by name (substring match)")
	cmd.Flags().StringVarP(&options.Catalog, "catalog", "c", "", catalogFlagUsage)
	cmd.Flags().BoolVar(&options.Insecure, "insecure", false, "Allow insecure HTTP connections")
	cmd.Flags().BoolVar(&options.AllVersions, "all-versions", false, "Show all versions instead of just the latest")
	cmd.Flags().BoolVar(&options.PreRelease, "pre-release", false, "Include pre-release versions (e.g. 1.0.0-beta.1)")
	cmd.Flags().BoolVar(&options.ShowAll, "all", false, "List all configured catalogs instead of just the default")
	cmd.Flags().StringVar(&options.VersionConstraint, "version", "", "Filter by semver version constraint (e.g., \"^1.0.0\", \">= 1.0, < 2.0\")")

	flags.AddKvxOutputFlagsToStruct(cmd, &options.KvxOutputFlags)

	return cmd
}

func runList(ctx context.Context, opts *ListOptions, outputOpts *kvx.OutputOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Wire pre-release context flag
	if opts.PreRelease {
		ctx = catalog.WithIncludePreRelease(ctx)
	}

	// Wire search pattern for pre-filtering before tag fetches
	if opts.Search != "" {
		ctx = catalog.WithSearchPattern(ctx, opts.Search)
	}

	// Validate --version vs @version in --name.
	if err := validateVersionConstraint(opts.Name, opts.VersionConstraint); err != nil {
		w.Errorf("%v", err)
		// Distinguish conflict (both @version and --version) from invalid syntax.
		if strings.Contains(err.Error(), "cannot use --version") {
			return exitcode.Errorf("conflicting options")
		}
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Full OCI reference in --name: list directly from a remote registry.
	if opts.Name != "" && looksLikeRemoteReference(opts.Name) {
		return runListFromRemoteRef(ctx, opts, outputOpts)
	}

	// Strip @version from --name (e.g. "email-notifier@1.0.0" → name="email-notifier")
	name := opts.Name
	if idx := strings.LastIndex(name, "@"); idx > 0 {
		name = name[:idx]
	}
	opts.Name = name

	// Parse kind filter
	var kind catalog.ArtifactKind
	if opts.Kind != "" {
		kind = catalog.ArtifactKind(opts.Kind)
		if !kind.IsValid() {
			w.Errorf("invalid kind %q: must be 'solution', 'provider', or 'auth-handler'", opts.Kind)
			return exitcode.Errorf("invalid kind")
		}
	}

	// When --catalog names a catalog that is NOT in the user's config (e.g., a
	// bare registry URL), delegate to the direct remote listing path.
	if opts.Catalog != "" && !isConfiguredCatalog(ctx, opts.Catalog) {
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

	// Determine which remote catalogs to query.
	// --catalog: only the named catalog (used as post-filter below).
	// --all: all configured catalogs.
	// default: only the configured defaultCatalog.
	if cfg := appconfig.FromContext(ctx); cfg != nil {
		var remoteCatalogs []appconfig.CatalogConfig
		if opts.Catalog != "" {
			// --catalog names a specific configured catalog; query only that one.
			if cat, ok := cfg.GetCatalog(opts.Catalog); ok && cat.Type == appconfig.CatalogTypeOCI {
				remoteCatalogs = append(remoteCatalogs, *cat)
			}
		} else if opts.ShowAll {
			for _, catCfg := range cfg.Catalogs {
				if catCfg.Type == appconfig.CatalogTypeOCI {
					remoteCatalogs = append(remoteCatalogs, catCfg)
				}
			}
		} else if defCat, ok := cfg.GetDefaultCatalog(); ok && defCat.Type == appconfig.CatalogTypeOCI {
			remoteCatalogs = append(remoteCatalogs, *defCat)
		}

		for _, catCfg := range remoteCatalogs {
			w.Verbosef("Searching remote catalog %q...", catCfg.Name)
			remoteOpts := &ListOptions{
				Name:      opts.Name,
				Kind:      opts.Kind,
				Search:    opts.Search,
				Catalog:   catCfg.Name,
				Insecure:  opts.Insecure,
				CliParams: opts.CliParams,
				IOStreams: opts.IOStreams,
			}
			remoteArtifacts, remoteErr := listRemoteArtifacts(ctx, remoteOpts, kind)
			if remoteErr != nil {
				if catalog.IsEnumerationNotSupported(remoteErr) {
					w.Verbosef("Catalog %q does not support enumeration, skipping.", catCfg.Name)
				} else {
					// In multi-catalog mode (--all), demote errors to verbose.
					// In single-default mode, show as warnings.
					if opts.ShowAll {
						w.Verbosef("Skipping catalog %q: %v", catCfg.Name, remoteErr)
					} else {
						w.WarnStderrf("failed to list from remote catalog %q: %v", catCfg.Name, remoteErr)
						if catalogURL, resolveErr := catalog.ResolveCatalogURL(ctx, catCfg.Name); resolveErr == nil {
							registry, _ := catalog.ParseCatalogURL(catalogURL)
							hintOnAuthError(ctx, w, registry, remoteErr)
						}
					}
				}
				continue
			}
			artifacts = append(artifacts, remoteArtifacts...)
		}
	}

	// Filter pre-release versions unless --pre-release flag is set.
	if !opts.PreRelease {
		artifacts = filterPreReleaseArtifacts(artifacts)
	}

	// Apply version constraint filter if set.
	if opts.VersionConstraint != "" {
		artifacts, err = filterArtifactsByConstraint(artifacts, opts.VersionConstraint)
		if err != nil {
			w.Errorf("version filter: %v", err)
			return exitcode.Errorf("invalid version constraint")
		}
	}

	// Post-filter by catalog name when --catalog is set.
	if opts.Catalog != "" {
		artifacts = filterArtifactsByCatalog(artifacts, opts.Catalog)
	}

	return writeArtifactList(w, artifacts, opts.AllVersions || opts.VersionConstraint != "", outputOpts)
}

// runListFromRemoteRef lists artifacts from a full OCI reference
// (e.g. "ghcr.io/myorg/solutions/email-notifier@1.0.0").
func runListFromRemoteRef(ctx context.Context, opts *ListOptions, outputOpts *kvx.OutputOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	if opts.Catalog != "" {
		w.Error("cannot use --catalog with a full remote reference in --name")
		return exitcode.Errorf("conflicting options")
	}

	w.Verbose("Parsing full OCI reference from --name")

	remoteRef, err := catalog.ParseRemoteReference(opts.Name)
	if err != nil {
		w.Errorf("invalid reference: %v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	registry := remoteRef.Registry
	repository := remoteRef.Repository

	verboseRefInfo(w, remoteRef.Name, string(remoteRef.Kind), remoteRef.Tag)

	credStore, err := catalog.NewCredentialStore(*lgr)
	if err != nil {
		lgr.V(1).Info("failed to create credential store, using anonymous auth", "error", err.Error())
	}

	authHandler := resolveAuthHandler(ctx, registry, "")
	authScope := resolveAuthScopeForRegistry(ctx, registry)

	verboseRemoteInfo(ctx, w, registry, repository, authHandler, authScope)

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
		w.Errorf("failed to create remote catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	var kind catalog.ArtifactKind
	if opts.Kind != "" {
		kind = catalog.ArtifactKind(opts.Kind)
		if !kind.IsValid() {
			w.Errorf("invalid kind %q: must be 'solution', 'provider', or 'auth-handler'", opts.Kind)
			return exitcode.Errorf("invalid kind")
		}
	} else if remoteRef.Kind != "" {
		kind = remoteRef.Kind
	}

	w.Verbosef("Listing tags for %s...", remoteRef.Name)

	artifacts, err := remoteCatalog.List(ctx, kind, remoteRef.Name)
	if err != nil {
		w.Errorf("failed to list remote artifacts: %v", err)
		hintOnAuthError(ctx, w, registry, err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	w.Verbosef("Found %d artifact(s)", len(artifacts))

	// Filter pre-release versions unless --pre-release flag is set.
	if !opts.PreRelease {
		artifacts = filterPreReleaseArtifacts(artifacts)
	}

	if opts.VersionConstraint != "" {
		artifacts, err = filterArtifactsByConstraint(artifacts, opts.VersionConstraint)
		if err != nil {
			w.Errorf("version filter: %v", err)
			return exitcode.Errorf("invalid version constraint")
		}
		w.Verbosef("After version filter %q: %d artifact(s)", opts.VersionConstraint, len(artifacts))
	}

	return writeArtifactList(w, artifacts, opts.AllVersions || opts.VersionConstraint != "", outputOpts)
}

func runListRemote(ctx context.Context, opts *ListOptions, kind catalog.ArtifactKind, outputOpts *kvx.OutputOptions) error {
	w := writer.FromContext(ctx)

	if opts.Name == "" {
		w.Verbose("Enumerating all artifacts in catalog (this may take a moment)...")
	}

	artifacts, err := listRemoteArtifacts(ctx, opts, kind)
	if err != nil {
		if catalog.IsEnumerationNotSupported(err) {
			if opts.Name == "" {
				bin := settings.BinaryNameFromContext(ctx)
				w.WarnStderrf("Catalog %q does not support repository enumeration.", opts.Catalog)
				w.PlainStderrf("To list versions of a specific artifact:")
				w.PlainStderrf("  %s catalog list --catalog %s --name <artifact>", bin, opts.Catalog)
				w.PlainStderrf("To pull an artifact directly:")
				w.PlainStderrf("  %s catalog pull <artifact> --catalog %s", bin, opts.Catalog)
				if kvx.IsStructuredFormat(outputOpts.Format) {
					return outputOpts.Write([]ArtifactListItem{})
				}
				return nil
			}
			w.Errorf("%v", err)
			w.Verbose("Use --name to list versions of a specific artifact.")
			return exitcode.WithCode(err, exitcode.CatalogError)
		}

		w.Errorf("failed to list remote artifacts: %v", err)

		// Resolve registry for auth hint
		if catalogURL, resolveErr := catalog.ResolveCatalogURL(ctx, opts.Catalog); resolveErr == nil {
			registry, _ := catalog.ParseCatalogURL(catalogURL)
			hintOnAuthError(ctx, w, registry, err)
		}

		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Filter pre-release versions unless --pre-release flag is set.
	if !opts.PreRelease {
		artifacts = filterPreReleaseArtifacts(artifacts)
	}

	if opts.VersionConstraint != "" {
		artifacts, err = filterArtifactsByConstraint(artifacts, opts.VersionConstraint)
		if err != nil {
			w.Errorf("version filter: %v", err)
			return exitcode.Errorf("invalid version constraint")
		}
	}

	return writeArtifactList(w, artifacts, opts.AllVersions || opts.VersionConstraint != "", outputOpts)
}

// listRemoteArtifacts fetches artifacts from a configured remote catalog.
// This is shared between runListRemote (explicit --catalog) and the
// unified all-catalogs search in runList.
func listRemoteArtifacts(ctx context.Context, opts *ListOptions, kind catalog.ArtifactKind) ([]catalog.ArtifactInfo, error) {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	w.Verbosef("Resolving catalog %q", opts.Catalog)

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
	discoveryStrategy := resolveDiscoveryStrategy(ctx, opts.Catalog)

	verboseRemoteInfo(ctx, w, registry, repository, authHandler, authScope)

	remoteCatalog, err := catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
		Name:              opts.Catalog,
		Registry:          registry,
		Repository:        repository,
		CredentialStore:   credStore,
		AuthHandler:       authHandler,
		AuthScope:         authScope,
		DiscoveryStrategy: discoveryStrategy,
		Insecure:          opts.Insecure,
		Logger:            *lgr,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create remote catalog: %w", err)
	}

	return remoteCatalog.List(ctx, kind, opts.Name)
}

func writeArtifactList(w *writer.Writer, artifacts []catalog.ArtifactInfo, showAll bool, outputOpts *kvx.OutputOptions) error {
	// Handle empty results: structured formats get an empty array,
	// table/interactive formats get a human-friendly message.
	if len(artifacts) == 0 {
		if kvx.IsStructuredFormat(outputOpts.Format) {
			return outputOpts.Write([]ArtifactListItem{})
		}
		w.Infof("No artifacts found in catalog.")
		return nil
	}

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

	// Deduplicate across catalogs: merge rows with same name+tag+kind,
	// combining catalog names and preferring richer metadata (digest, createdAt).
	artifacts = deduplicateArtifacts(artifacts)

	// When not showing all versions, keep only the latest per name+kind
	// (after dedup, catalog names are merged so we key on kind+name only).
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
		createdAt := ""
		if !a.CreatedAt.IsZero() {
			createdAt = a.CreatedAt.Format("2006-01-02 15:04:05")
		}
		items[i] = ArtifactListItem{
			Name:      a.Reference.Name,
			Version:   version,
			Tag:       tag,
			Kind:      string(a.Reference.Kind),
			Digest:    a.Digest,
			CreatedAt: createdAt,
			Catalog:   a.Catalog,
		}
	}

	return outputOpts.Write(items)
}
