// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/kvx/pkg/tui"
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
	ShowAll           bool   // List every configured catalog instead of the default plus official fallback
	CliParams         *settings.Run
	IOStreams         *terminal.IOStreams
	flags.KvxOutputFlags
}

// ArtifactListItem represents an artifact in list output.
type ArtifactListItem struct {
	Name        string `json:"name" yaml:"name"`
	Version     string `json:"version" yaml:"version"`
	Tag         string `json:"tag" yaml:"tag"`
	Kind        string `json:"kind" yaml:"kind"`
	DisplayName string `json:"displayName" yaml:"displayName"`
	Category    string `json:"category" yaml:"category"`
	Digest      string `json:"digest" yaml:"digest"`
	CreatedAt   string `json:"createdAt" yaml:"createdAt"`
	Catalog     string `json:"catalog" yaml:"catalog"`
}

// listColumnHints controls table column display for catalog list.
var listColumnHints = map[string]tui.ColumnHint{
	"name":        {MaxWidth: 30, Priority: 10},
	"tag":         {MaxWidth: 12, Priority: 8},
	"kind":        {MaxWidth: 12, Priority: 10},
	"displayName": {MaxWidth: 25, Priority: 6, DisplayName: "display name"},
	"category":    {MaxWidth: 15, Priority: 4},
	"catalog":     {MaxWidth: 25, Priority: 8},
}

// artifactListSchema controls table column display. Columns in the "required" array
// (name, tag, kind, catalog) resist truncation.
// version, createdAt, and digest are hidden in table view but included in json/yaml output.
var artifactListSchema = []byte(`{
	"type": "array",
	"items": {
		"type": "object",
		"required": ["name", "tag", "kind", "catalog"],
		"properties": {
			"name":        { "type": "string", "title": "Name" },
			"tag":         { "type": "string", "title": "Tag" },
			"kind":        { "type": "string", "title": "Kind" },
			"displayName": { "type": "string", "title": "Display Name" },
			"category":    { "type": "string", "title": "Category" },
			"catalog":     { "type": "string", "title": "Catalog" },
			"version":     { "type": "string", "deprecated": true },
			"digest":      { "type": "string", "deprecated": true },
			"createdAt":   { "type": "string", "deprecated": true }
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
			List artifacts from the local catalog, the default catalog, and
			the built-in official catalog as an ordered fallback (the same
			chain the resolver uses), so official artifacts still appear when
			the default catalog is private or unreachable. Set
			settings.disableOfficialCatalog to drop the official fallback.
			Use --catalog to list from exactly one catalog, or --all to
			list from every configured catalog.

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

			  # List only plugins (providers and auth handlers)
			  %[1]s catalog list --kind plugin

			  # List all versions of a plugin
			  %[1]s catalog list --kind plugin --name github --all-versions

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
				kvx.WithOutputColumnOrder([]string{"name", "tag", "kind", "displayName", "category", "digest", "catalog"}),
				kvx.WithOutputSchemaJSON(artifactListSchema),
				kvx.WithOutputColumnHints(listColumnHints),
			)
			return runList(cmd.Context(), options, kvxOpts)
		},
	}

	cmd.Flags().StringVar(&options.Kind, "kind", "", "Filter by artifact kind (solution, provider, auth-handler, or 'plugin' for providers + auth handlers)")
	cmd.Flags().StringVar(&options.Name, "name", "", "Filter by artifact name (shows all versions when set)")
	cmd.Flags().StringVarP(&options.Search, "search", "s", "", "Filter artifacts by name (substring match)")
	cmd.Flags().StringVarP(&options.Catalog, "catalog", "c", "", catalogFlagUsage)
	cmd.Flags().BoolVar(&options.Insecure, "insecure", false, "Allow insecure HTTP connections")
	cmd.Flags().BoolVar(&options.AllVersions, "all-versions", false, "Show all versions instead of just the latest")
	cmd.Flags().BoolVar(&options.PreRelease, "pre-release", false, "Include pre-release versions (e.g. 1.0.0-beta.1)")
	cmd.Flags().BoolVar(&options.ShowAll, "all", false, "List every configured catalog instead of the default plus official fallback")
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

	// Parse kind filter. The empty string means "all kinds"; the "plugin"
	// selector expands to provider + auth-handler (see catalog.ExpandKindSelector).
	kinds, ok := catalog.ExpandKindSelector(opts.Kind)
	if !ok {
		w.Errorf("invalid kind %q: must be 'solution', 'provider', 'auth-handler', or 'plugin'", opts.Kind)
		return exitcode.Errorf("invalid kind")
	}

	// When --catalog names a catalog that is NOT in the user's config (e.g., a
	// bare registry URL), delegate to the direct remote listing path.
	if opts.Catalog != "" && !isConfiguredCatalog(ctx, opts.Catalog) {
		return runListRemote(ctx, opts, kinds, outputOpts)
	}

	// Create local catalog
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		w.Errorf("failed to open catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// List local artifacts
	artifacts, err := catalog.ListAcrossKinds(ctx, localCatalog, kinds, opts.Name, *lgr)
	if err != nil {
		w.Errorf("failed to list artifacts: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// aggregateDegraded records the first remote catalog (if any) that fell back
	// to anonymous access due to rejected credentials. It is only rendered as an
	// error when the combined result is empty (see writeArtifactList).
	var aggregateDegraded *catalog.AuthDegradedError

	// Determine which remote catalogs to query.
	// --catalog: only the named catalog (used as post-filter below).
	// --all: all configured catalogs.
	// default: the default catalog plus the built-in official catalog as an
	//   ordered fallback (catalog.DefaultListCatalogs).
	if cfg := appconfig.FromContext(ctx); cfg != nil {
		var remoteCatalogs []appconfig.CatalogConfig
		switch {
		case opts.Catalog != "":
			// --catalog names a specific configured catalog; query only that one.
			// The reserved official catalog is treated as unavailable when
			// settings.disableOfficialCatalog is set, mirroring the resolver
			// chain (which omits official entirely when disabled).
			officialDisabled := opts.Catalog == appconfig.CatalogNameOfficial && cfg.Settings.DisableOfficialCatalog
			if cat, ok := cfg.GetCatalog(opts.Catalog); ok && cat.Type == appconfig.CatalogTypeOCI && !officialDisabled {
				remoteCatalogs = append(remoteCatalogs, *cat)
			}
		case opts.ShowAll:
			for _, catCfg := range cfg.Catalogs {
				if catCfg.Type == appconfig.CatalogTypeOCI {
					remoteCatalogs = append(remoteCatalogs, catCfg)
				}
			}
		default:
			// Bare `catalog list` (no --catalog, no --all) queries the default
			// catalog as the primary, then falls back to the built-in official
			// catalog -- mirroring the ordered chain the resolver consumes so a
			// private/unauthenticated default does not hide anonymously-available
			// official artifacts. See issue #692.
			remoteCatalogs = catalog.DefaultListCatalogs(cfg)
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
			remoteArtifacts, remoteDegraded, remoteErr := listRemoteArtifacts(ctx, remoteOpts, kinds)
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
			// Remember the first catalog that degraded to anonymous access. If
			// the aggregate result ends up empty, this makes the emptiness
			// attributable to a rejected credential rather than a truly empty
			// catalog.
			if remoteDegraded != nil && aggregateDegraded == nil {
				aggregateDegraded = remoteDegraded
			}
			artifacts = append(artifacts, remoteArtifacts...)
		}
	}

	// Raw count across local + all remote catalogs, before user filters. Used
	// to distinguish a genuinely empty (auth-degraded) listing from one emptied
	// by pre-release/version/catalog filtering.
	rawResultCount := len(artifacts)

	// Filter pre-release versions unless --pre-release flag is set.
	if !opts.PreRelease {
		before := len(artifacts)
		artifacts = filterPreReleaseArtifacts(artifacts)
		if hidden := before - len(artifacts); hidden > 0 {
			bin := settings.BinaryNameFromContext(ctx)
			w.WarnStderrf("%d artifact(s) hidden (pre-release) -- use --pre-release to show them, or: %s catalog list --pre-release --all-versions", hidden, bin)
		}
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

	return writeArtifactList(ctx, w, artifacts, opts.AllVersions || opts.VersionConstraint != "", outputOpts, aggregateDegraded, rawResultCount)
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

	verboseRemoteInfo(ctx, w, registry, repository, authHandlerName(authHandler), authScope)

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

	// Resolve the kind selector. An explicit --kind wins (and may be the
	// "plugin" alias expanding to provider + auth-handler); otherwise fall back
	// to the kind embedded in the remote reference, if any.
	kinds, ok := catalog.ExpandKindSelector(opts.Kind)
	if !ok {
		w.Errorf("invalid kind %q: must be 'solution', 'provider', 'auth-handler', or 'plugin'", opts.Kind)
		return exitcode.Errorf("invalid kind")
	}
	if opts.Kind == "" && remoteRef.Kind != "" {
		kinds = []catalog.ArtifactKind{remoteRef.Kind}
	}

	w.Verbosef("Listing tags for %s...", remoteRef.Name)

	artifacts, err := catalog.ListAcrossKinds(ctx, remoteCatalog, kinds, remoteRef.Name, *lgr)
	rawResultCount := len(artifacts)
	if err != nil {
		w.Errorf("failed to list remote artifacts: %v", err)
		hintOnAuthError(ctx, w, registry, err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	w.Verbosef("Found %d artifact(s)", len(artifacts))

	// Filter pre-release versions unless --pre-release flag is set.
	if !opts.PreRelease {
		before := len(artifacts)
		artifacts = filterPreReleaseArtifacts(artifacts)
		if hidden := before - len(artifacts); hidden > 0 {
			bin := settings.BinaryNameFromContext(ctx)
			w.WarnStderrf("%d artifact(s) hidden (pre-release) -- use --pre-release to show them, or: %s catalog list --pre-release --all-versions", hidden, bin)
		}
	}

	if opts.VersionConstraint != "" {
		artifacts, err = filterArtifactsByConstraint(artifacts, opts.VersionConstraint)
		if err != nil {
			w.Errorf("version filter: %v", err)
			return exitcode.Errorf("invalid version constraint")
		}
		w.Verbosef("After version filter %q: %d artifact(s)", opts.VersionConstraint, len(artifacts))
	}

	return writeArtifactList(ctx, w, artifacts, opts.AllVersions || opts.VersionConstraint != "", outputOpts, catalog.NewAuthDegradedError(remoteCatalog), rawResultCount)
}

func runListRemote(ctx context.Context, opts *ListOptions, kinds []catalog.ArtifactKind, outputOpts *kvx.OutputOptions) error {
	w := writer.FromContext(ctx)

	if opts.Name == "" {
		w.Verbose("Enumerating all artifacts in catalog (this may take a moment)...")
	}

	artifacts, degraded, err := listRemoteArtifacts(ctx, opts, kinds)
	rawResultCount := len(artifacts)
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
		before := len(artifacts)
		artifacts = filterPreReleaseArtifacts(artifacts)
		if hidden := before - len(artifacts); hidden > 0 {
			bin := settings.BinaryNameFromContext(ctx)
			w.WarnStderrf("%d artifact(s) hidden (pre-release) -- use --pre-release to show them, or: %s catalog list --pre-release --all-versions", hidden, bin)
		}
	}

	if opts.VersionConstraint != "" {
		artifacts, err = filterArtifactsByConstraint(artifacts, opts.VersionConstraint)
		if err != nil {
			w.Errorf("version filter: %v", err)
			return exitcode.Errorf("invalid version constraint")
		}
	}

	return writeArtifactList(ctx, w, artifacts, opts.AllVersions || opts.VersionConstraint != "", outputOpts, degraded, rawResultCount)
}

// listRemoteArtifacts fetches artifacts from a configured remote catalog.
// This is shared between runListRemote (explicit --catalog) and the
// unified all-catalogs search in runList.
func listRemoteArtifacts(ctx context.Context, opts *ListOptions, kinds []catalog.ArtifactKind) ([]catalog.ArtifactInfo, *catalog.AuthDegradedError, error) {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	w.Verbosef("Resolving catalog %q", opts.Catalog)

	catalogURL, err := catalog.ResolveCatalogURL(ctx, opts.Catalog)
	if err != nil {
		return nil, nil, err
	}

	registry, repository := catalog.ParseCatalogURL(catalogURL)

	credStore, err := catalog.NewCredentialStore(*lgr)
	if err != nil {
		lgr.V(1).Info("failed to create credential store, using anonymous auth", "error", err.Error())
	}

	authHandler := resolveAuthHandler(ctx, registry, opts.Catalog)
	authScope := resolveAuthScope(ctx, opts.Catalog)
	discoveryStrategy := resolveDiscoveryStrategy(ctx, opts.Catalog)

	verboseRemoteInfo(ctx, w, registry, repository, authHandlerName(authHandler), authScope)

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
		return nil, nil, fmt.Errorf("failed to create remote catalog: %w", err)
	}

	result, listErr := catalog.ListAcrossKinds(ctx, remoteCatalog, kinds, opts.Name, *lgr)

	// If stored credentials were rejected the catalog fell back to anonymous
	// access, so the listing is degraded/incomplete rather than authoritative.
	// Surface that as a typed value so the render layer can decide whether it is
	// a fatal error (empty result) or a non-fatal warning (partial result).
	degraded := catalog.NewAuthDegradedError(remoteCatalog)
	verboseCredentialSource(w, remoteCatalog)

	return result, degraded, listErr
}

func writeArtifactList(ctx context.Context, w *writer.Writer, artifacts []catalog.ArtifactInfo, showAll bool, outputOpts *kvx.OutputOptions, degraded *catalog.AuthDegradedError, rawResultCount int) error {
	structured := kvx.IsStructuredFormat(outputOpts.Format)
	quiet := kvx.IsQuietFormat(outputOpts.Format)

	// Handle empty results.
	if len(artifacts) == 0 {
		// Empty + degraded is treated as fatal ONLY when the raw listing itself
		// was empty (rawResultCount == 0) -- i.e. rejected credentials produced
		// a genuinely empty anonymous listing. If the raw listing had artifacts
		// but user filters (pre-release, version constraint, --catalog) removed
		// them all, the emptiness is due to filtering, not the auth failure, so
		// we do not fail the command; we still surface the degraded warning.
		fatalDegraded := degraded != nil && rawResultCount == 0
		if fatalDegraded {
			// For structured output, still write an empty array to stdout so
			// JSON/YAML consumers get a parseable document even on non-zero
			// exit; the degraded/authError marker goes to stderr.
			var writeErr error
			if structured {
				writeErr = outputOpts.Write([]ArtifactListItem{})
			}
			renderDegradedSignal(ctx, w, degraded, structured, quiet, true /* fatal/empty */)
			authErr := fmt.Errorf("%w; run %s", degraded, authLoginHint(ctx, degraded.Registry, degraded.Handler))
			// Surface a stdout write failure alongside the auth-degraded error
			// (the degraded state remains the primary signal / exit code).
			if writeErr != nil {
				authErr = fmt.Errorf("%w (failed to write empty result: %w)", authErr, writeErr)
			}
			return exitcode.WithCode(authErr, exitcode.CatalogError)
		}
		// Non-fatal: emptiness came from filtering a degraded-but-non-empty
		// listing (still warn), or there was no degradation at all.
		if degraded != nil {
			renderDegradedSignal(ctx, w, degraded, structured, quiet, false /* non-fatal */)
		}
		if structured {
			return outputOpts.Write([]ArtifactListItem{})
		}
		w.Infof("No artifacts found in catalog.")
		return nil
	}

	// Non-empty + degraded is a partial success: real results were returned
	// from anonymous access, but some content may be hidden. Print the results
	// (exit 0) and warn that the listing is incomplete.
	if degraded != nil {
		renderDegradedSignal(ctx, w, degraded, structured, quiet, false /* partial/non-empty */)
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
			Name:        a.Reference.Name,
			Version:     version,
			Tag:         tag,
			Kind:        string(a.Reference.Kind),
			DisplayName: a.Annotations[catalog.AnnotationDisplayName],
			Category:    a.Annotations[catalog.AnnotationCategory],
			Digest:      a.Digest,
			CreatedAt:   createdAt,
			Catalog:     a.Catalog,
		}
	}

	return outputOpts.Write(items)
}

// authLoginHint returns the "<bin> auth login <handler>" (or "<bin> catalog
// login <registry>") fix command for a degraded registry, using the binary
// name from context. It prefers the handler that actually supplied the rejected
// credentials (which RemoteCatalog gives precedence over credential-store
// entries), falling back to inferring a handler from the registry host only
// when no handler was recorded.
func authLoginHint(ctx context.Context, registry, handler string) string {
	bin := settings.BinaryNameFromContext(ctx)
	if handler == "" {
		var customHandlers []appconfig.CustomOAuth2Config
		if cfg := appconfig.FromContext(ctx); cfg != nil {
			customHandlers = cfg.Auth.CustomOAuth2
		}
		handler = catalog.InferAuthHandler(registry, customHandlers)
	}
	if handler != "" {
		return fmt.Sprintf("%s auth login %s", bin, handler)
	}
	return fmt.Sprintf("%s catalog login %s", bin, registry)
}

// renderDegradedSignal emits a human-facing message about the degraded listing
// and, for structured output, a machine-readable {"degraded":true,...} marker on
// stderr so -o json consumers can detect the condition without changing the
// stdout array contract. When fatal is true (an empty result on rejected
// credentials) it emits an error-framed line, since the command exits non-zero
// and the returned Cobra error is silenced; otherwise it emits an
// incomplete-listing warning for a partial (non-empty) result. It emits nothing
// when quiet output is requested (quiet suppresses all output; the exit code
// still conveys the failure).
func renderDegradedSignal(ctx context.Context, w *writer.Writer, degraded *catalog.AuthDegradedError, structured, quiet, fatal bool) {
	if quiet {
		return
	}
	if fatal {
		w.Errorf("Cannot list catalog %s: rejected %s, and anonymous access found nothing.",
			degraded.Registry, credentialSourceDescription(degraded))
	} else {
		w.WarnStderrf("Catalog listing is incomplete: rejected %s for %s — showing anonymous results only.",
			credentialSourceDescription(degraded), degraded.Registry)
	}
	w.PlainStderrf("  To fix: %s", authLoginHint(ctx, degraded.Registry, degraded.Handler))

	if structured {
		marker := map[string]any{
			"degraded": true,
			"authError": map[string]any{
				"registry":         degraded.Registry,
				"handler":          degraded.Handler,
				"credentialSource": degraded.CredentialSource,
			},
		}
		if data, err := json.Marshal(marker); err == nil {
			w.PlainStderrf("%s", string(data))
		}
	}
}

// credentialSourceDescription returns a human-readable description of the
// rejected credential source for a degraded error.
func credentialSourceDescription(degraded *catalog.AuthDegradedError) string {
	if degraded.CredentialSource != "" {
		return "your " + degraded.CredentialSource
	}
	if degraded.Handler != "" {
		return fmt.Sprintf("your %s auth handler credentials", degraded.Handler)
	}
	return "your stored credentials"
}
