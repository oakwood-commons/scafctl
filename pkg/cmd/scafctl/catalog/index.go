// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/catalog/search"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

//go:embed index_schema.json
var indexSchemaJSON []byte

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
	Kind     string // Filter by artifact kind (--kind)
	Search   string // Free-text search filter (--search)

	CliParams *settings.Run
	IOStreams *terminal.IOStreams
	flags.KvxOutputFlags
}

// IndexListItem represents an artifact entry in index table output.
type IndexListItem struct {
	Kind          string `json:"kind"          yaml:"kind"          doc:"Artifact kind (solution, provider, auth-handler)" example:"solution"`
	Name          string `json:"name"          yaml:"name"          doc:"Artifact name" example:"hello-world"`
	LatestVersion string `json:"latestVersion" yaml:"latestVersion" doc:"Latest semver version" example:"1.2.0"`
	DisplayName   string `json:"displayName"   yaml:"displayName"   doc:"Human-friendly display name" example:"Hello World"`
	Category      string `json:"category"      yaml:"category"      doc:"Solution category" example:"deployment"`
}

// indexColumnHints controls table column display for index commands.
var indexColumnHints = map[string]tui.ColumnHint{
	"kind":          {MaxWidth: 12, Priority: 10},
	"name":          {MaxWidth: 30, Priority: 10},
	"latestVersion": {MaxWidth: 12, Priority: 8, DisplayName: "version"},
	"displayName":   {MaxWidth: 25, Priority: 6, DisplayName: "display name"},
	"category":      {MaxWidth: 15, Priority: 4},
}

// IndexDiffItem represents an artifact entry in diff output.
type IndexDiffItem struct {
	Change        string `json:"change"        yaml:"change"        doc:"Change type (added, removed, version-changed, unchanged)" example:"added"`
	Kind          string `json:"kind"          yaml:"kind"          doc:"Artifact kind" example:"solution"`
	Name          string `json:"name"          yaml:"name"          doc:"Artifact name" example:"hello-world"`
	LatestVersion string `json:"latestVersion" yaml:"latestVersion" doc:"New latest version" example:"1.2.0"`
	PrevVersion   string `json:"prevVersion"   yaml:"prevVersion"   doc:"Previous version (empty if added)" example:"1.1.0"`
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
			kvxOpts := flags.ToKvxOutputOptions(&opts.KvxOutputFlags,
				kvx.WithIOStreams(ioStreams),
				kvx.WithOutputColumnOrder([]string{"kind", "name", "latestVersion", "displayName", "category"}),
				kvx.WithOutputColumnHints(indexColumnHints),
				kvx.WithOutputDisplaySchemaJSON(indexSchemaJSON),
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

			  # Show only solutions
			  %[1]s catalog index show --kind solution

			  # Search by name or description
			  %[1]s catalog index show --search hello

			  # Show in JSON format
			  %[1]s catalog index show -o json
		`, cliParams.BinaryName),
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			kvxOpts := flags.ToKvxOutputOptions(&opts.KvxOutputFlags,
				kvx.WithIOStreams(ioStreams),
				kvx.WithOutputColumnOrder([]string{"kind", "name", "latestVersion", "displayName", "category"}),
				kvx.WithOutputColumnHints(indexColumnHints),
				kvx.WithOutputDisplaySchemaJSON(indexSchemaJSON),
			)
			return runIndexShow(cmd.Context(), opts, kvxOpts)
		},
	}

	cmd.Flags().StringVarP(&opts.Catalog, "catalog", "c", "", catalogFlagUsage)
	cmd.Flags().BoolVar(&opts.Insecure, "insecure", false, "Allow insecure HTTP connections")
	cmd.Flags().StringVarP(&opts.Kind, "kind", "k", "", "Filter by artifact kind (solution, provider, auth-handler)")
	cmd.Flags().StringVarP(&opts.Search, "search", "s", "", "Free-text search filter (matches name, display name, description, category)")
	flags.AddKvxOutputFlagsToStruct(cmd, &opts.KvxOutputFlags)

	return cmd
}

// runIndexPush discovers all artifacts via registry enumeration and pushes
// the catalog index. With --dry-run it prints the index without pushing.
func runIndexPush(ctx context.Context, opts *IndexPushOptions, outputOpts *kvx.OutputOptions) error {
	w := writer.FromContext(ctx)
	lgr := logger.FromContext(ctx)

	remoteCatalog, err := createIndexRemoteCatalog(ctx, opts.Catalog, opts.Insecure)
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Use catalog name as table header instead of binary name.
	outputOpts.AppName = remoteCatalog.Name()

	artifacts, err := remoteCatalog.ListRepositories(ctx)
	if err != nil {
		w.Errorf("failed to discover artifacts: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	if len(artifacts) == 0 {
		w.WarnStderrf("No artifacts found in catalog -- nothing to push.")
		return writeIndexList(nil, outputOpts)
	}

	// Enrich artifacts with their latest semver version.
	w.Verbosef("Resolving latest versions for %d artifact(s)...", len(artifacts))
	remoteCatalog.ResolveLatestVersions(ctx, artifacts)

	// Fetch the current published index (if any) so we can skip enrichment
	// for unchanged artifacts and use it for dry-run diffing.
	existingIndex, fetchErr := remoteCatalog.FetchIndex(ctx)
	if fetchErr != nil {
		w.Verbosef("No existing index found, will enrich all artifacts: %v", fetchErr)
	}

	// Enrich solution artifacts with metadata from their YAML.
	// Unchanged artifacts (same name+version in existing index) reuse cached metadata.
	w.Verbosef("Enriching solution metadata...")
	search.EnrichArtifacts(ctx, *lgr, remoteCatalog, artifacts, existingIndex)

	if opts.DryRun {
		if fetchErr != nil {
			w.Verbosef("Dry run: %d artifact(s) would be indexed to catalog %q (%s)",
				len(artifacts), remoteCatalog.Name(), remoteCatalog.Registry())
			return writeIndexList(artifacts, outputOpts)
		}

		diff := catalog.DiffIndex(existingIndex, artifacts)
		w.Verbosef("Dry run: %d artifact(s) would be indexed to catalog %q (%s)",
			diff.Total, remoteCatalog.Name(), remoteCatalog.Registry())
		w.Verbosef("Changes: %d added, %d removed, %d version-changed, %d unchanged",
			diff.Added, diff.Removed, diff.Changed,
			diff.Total-diff.Added-diff.Changed)
		return writeIndexDiff(diff, outputOpts)
	}

	w.Verbosef("Pushing catalog index with %d artifact(s)...", len(artifacts))

	if err := remoteCatalog.PushIndex(ctx, artifacts); err != nil {
		err = fmt.Errorf("failed to push catalog index: %w", err)
		w.Errorf("%v", err)
		hintOnAuthError(ctx, w, remoteCatalog.Registry(), err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	w.Verbosef("Pushed catalog index with %d artifact(s)", len(artifacts))
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

	// Use catalog name as table header instead of binary name.
	outputOpts.AppName = remoteCatalog.Name()

	artifacts, err := remoteCatalog.FetchIndex(ctx)
	if err != nil {
		err = fmt.Errorf("failed to fetch catalog index: %w", err)
		w.Errorf("%v", err)
		hintOnAuthError(ctx, w, remoteCatalog.Registry(), err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Warn if stale credentials were detected during fetch.
	warnStaleCredentials(ctx, w, remoteCatalog)
	verboseCredentialSource(w, remoteCatalog)

	if len(artifacts) == 0 {
		w.Verbosef("Catalog index is empty.")
		return writeIndexList(nil, outputOpts)
	}

	artifacts = filterIndexArtifacts(artifacts, opts.Kind, opts.Search)

	w.Verbosef("Catalog index contains %d artifact(s)", len(artifacts))
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

	// Resolve a human-readable catalog name for display purposes.
	catalogName := catalog.ResolveCatalogDisplayName(ctx, catalogFlag)

	registry, repository := catalog.ParseCatalogURL(catalogURL)

	credStore, err := catalog.NewCredentialStore(*lgr)
	if err != nil {
		lgr.V(1).Info("failed to create credential store, using anonymous auth", "error", err.Error())
	}

	authHandler := resolveAuthHandler(ctx, registry, catalogFlag)
	authScope := resolveAuthScope(ctx, catalogFlag)

	verboseRemoteInfo(ctx, w, registry, repository, authHandler, authScope)

	return catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
		Name:            catalogName,
		Registry:        registry,
		Repository:      repository,
		CredentialStore: credStore,
		AuthHandler:     authHandler,
		AuthScope:       authScope,
		Insecure:        insecure,
		Logger:          *lgr,
	})
}

// filterIndexArtifacts filters artifacts by kind and free-text search.
func filterIndexArtifacts(artifacts []catalog.DiscoveredArtifact, kind, search string) []catalog.DiscoveredArtifact {
	if kind == "" && search == "" {
		return artifacts
	}

	kindLower := strings.ToLower(kind)
	searchLower := strings.ToLower(search)

	filtered := make([]catalog.DiscoveredArtifact, 0, len(artifacts))
	for _, a := range artifacts {
		if kindLower != "" && strings.ToLower(string(a.Kind)) != kindLower {
			continue
		}
		if searchLower != "" && !matchesSearch(a, searchLower) {
			continue
		}
		filtered = append(filtered, a)
	}
	return filtered
}

// matchesSearch returns true if any of the artifact's text fields contain the query.
func matchesSearch(a catalog.DiscoveredArtifact, query string) bool {
	return strings.Contains(strings.ToLower(a.Name), query) ||
		strings.Contains(strings.ToLower(a.DisplayName), query) ||
		strings.Contains(strings.ToLower(a.Description), query) ||
		strings.Contains(strings.ToLower(a.Category), query)
}

// writeIndexList writes the index artifact list using kvx output options.
// Structured formats (json/yaml) get the full DiscoveredArtifact with all
// metadata; table output gets a flat IndexListItem without array fields.
func writeIndexList(artifacts []catalog.DiscoveredArtifact, outputOpts *kvx.OutputOptions) error {
	if kvx.IsStructuredFormat(outputOpts.Format) {
		return outputOpts.Write(artifacts)
	}

	items := make([]IndexListItem, len(artifacts))
	for i, a := range artifacts {
		items[i] = IndexListItem{
			Kind:          string(a.Kind),
			Name:          a.Name,
			LatestVersion: a.LatestVersion,
			DisplayName:   a.DisplayName,
			Category:      a.Category,
		}
	}
	return outputOpts.Write(items)
}

// writeIndexDiff writes the index diff entries using kvx output options.
func writeIndexDiff(diff catalog.IndexDiffSummary, outputOpts *kvx.OutputOptions) error {
	items := make([]IndexDiffItem, len(diff.Entries))
	for i, e := range diff.Entries {
		items[i] = IndexDiffItem{
			Change:        string(e.Change),
			Kind:          string(e.Kind),
			Name:          e.Name,
			LatestVersion: e.LatestVersion,
			PrevVersion:   e.PrevVersion,
		}
	}

	outputOpts.ColumnOrder = []string{"change", "kind", "name", "latestVersion", "prevVersion"}
	return outputOpts.Write(items)
}
