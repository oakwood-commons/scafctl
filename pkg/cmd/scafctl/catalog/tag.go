// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// TagOptions holds options for the tag command.
type TagOptions struct {
	Reference string // Source artifact reference (name@version)
	Alias     string // Alias tag to create (e.g., "stable", "latest")
	Catalog   string // Target catalog for remote tagging (URL or config name, --catalog)
	Kind      string // Artifact kind override (--kind)
	Origin    string // Select the local copy by where it came from (--origin)
	Insecure  bool   // Allow HTTP (--insecure)
	CliParams *settings.Run
	IOStreams *terminal.IOStreams
}

// CommandTag creates the tag command.
func CommandTag(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &TagOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:   "tag <name@version> <alias>",
		Short: "Create an alias tag for an artifact",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Create an alias tag for an existing catalog artifact.

			Tags are freeform aliases that point to a specific version of an artifact.
			Common uses include marking releases as "stable" or "production".

			Note: "latest" is reserved and auto-resolves to the highest semver version.
			It cannot be used as a manual alias.

			The source artifact must exist and have a version (or, for the
			local catalog, a digest) specified.
			The alias must not be a valid semver version (use 'scafctl build' for that).

			By default, tags the artifact in the local catalog. Use --catalog to
			tag an artifact in a remote registry.

			The local catalog can hold several copies with the same name and
			version (built locally, pulled from different catalogs). Tag never
			picks one silently: when the reference matches more than one, it
			fails and prints a command for each copy. Select one with --origin,
			--kind, or a digest. The alias is scoped to the selected copy's origin.

			Examples:
			  # Tag a solution as stable
			  scafctl catalog tag my-solution@1.0.0 stable

			  # Tag for production
			  scafctl catalog tag my-solution@1.0.0 production

			  # Tag the copy pulled from a specific registry
			  scafctl catalog tag my-solution@1.0.0 stable --origin ghcr.io/myorg

			  # Tag the locally built copy when a pulled copy also exists
			  scafctl catalog tag my-solution@1.0.0 stable --origin built

			  # Tag in a remote registry
			  scafctl catalog tag my-solution@1.0.0 production --catalog ghcr.io/myorg

			  # Tag with explicit kind
			  scafctl catalog tag echo@1.0.0 stable --kind provider
		`), settings.CliBinaryName, cliParams.BinaryName),
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Reference = args[0]
			options.Alias = args[1]
			return runTag(cmd.Context(), options)
		},
	}

	cmd.Flags().StringVarP(&options.Catalog, "catalog", "c", "", catalogFlagUsage)
	cmd.Flags().StringVar(&options.Kind, "kind", "", "Artifact kind override (solution, provider, auth-handler)")
	cmd.Flags().StringVar(&options.Origin, "origin", "", pushOriginFlagUsage)
	cmd.Flags().BoolVar(&options.Insecure, "insecure", false, "Allow insecure HTTP connections")

	return cmd
}

func runTag(ctx context.Context, opts *TagOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Validate alias - must not be a valid semver version
	if err := catalog.ValidateAlias(opts.Alias); err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Parse reference - require version
	_, version := catalog.ParseNameVersion(opts.Reference)
	if version == "" {
		w.Error("version required: use format 'name@version' (e.g., 'my-solution@1.0.0')")
		return exitcode.Errorf("version required")
	}

	// Remote tag operation — no local catalog needed.
	if opts.Catalog != "" {
		// A remote tag needs a semver source version to read the manifest by.
		if catalog.IsValidDigest(version) {
			w.Error("digest references cannot be tagged remotely; use a semver version (e.g., 'my-solution@1.0.0')")
			return exitcode.Errorf("digest not supported for remote tagging")
		}
		if opts.Origin != "" {
			w.Error("--origin only applies to local tagging; it has no effect with --catalog")
			return exitcode.Errorf("--origin not supported for remote tag")
		}
		var artifactKind catalog.ArtifactKind
		if opts.Kind != "" {
			kind, ok := catalog.ParseArtifactKind(opts.Kind)
			if !ok {
				w.Errorf("invalid kind %q: must be 'solution', 'provider', or 'auth-handler'", opts.Kind)
				return exitcode.Errorf("invalid kind")
			}
			artifactKind = kind
		}
		ref, err := catalog.ParseReference(artifactKind, opts.Reference)
		if err != nil {
			w.Errorf("invalid reference %q: %v", opts.Reference, err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
		return runTagRemote(ctx, opts, ref)
	}

	sel, err := catalog.ParseExactSelector(opts.Reference, opts.Kind, opts.Origin)
	if err != nil {
		w.Errorf("invalid reference %q: %v", opts.Reference, err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		err = fmt.Errorf("failed to open local catalog: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// The local catalog can hold several copies with the same name and
	// version (built locally, pulled from different catalogs). Select exactly
	// one, failing with a disambiguation hint rather than picking silently.
	info, err := localCatalog.ResolveExact(ctx, sel)
	if err != nil {
		return tagResolveError(w, opts.CliParams.BinaryName, opts.Alias, err)
	}
	ref := info.Reference

	oldVersion, err := localCatalog.TagResolved(ctx, info, opts.Alias)
	if err != nil {
		if catalog.IsNotFound(err) {
			w.Errorf("artifact %q not found in local catalog", opts.Reference)
			return exitcode.WithCode(err, exitcode.FileNotFound)
		}
		w.Errorf("failed to tag artifact: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	if oldVersion != "" {
		w.Warningf("Moved %q from %s → %s", opts.Alias, oldVersion, ref.VersionOrDigest())
	}
	w.Successf("Tagged %s@%s as %q", ref.Name, ref.VersionOrDigest(), opts.Alias)

	return nil
}

// tagResolveError reports a ResolveExact failure for tag with its exit code.
// Ambiguity errors are followed by one copy-pasteable command per candidate.
func tagResolveError(w *writer.Writer, binaryName, alias string, err error) error {
	var ambArtifact *catalog.AmbiguousArtifactError
	var ambKind *catalog.AmbiguousKindError
	var hints []string
	switch {
	case errors.As(err, &ambArtifact):
		hints = ambArtifact.TagHints(binaryName, alias)
	case errors.As(err, &ambKind):
		hints = ambKind.TagHints(binaryName, alias)
	}
	return exactResolveError(w, err, hints)
}

// runTagRemote tags an artifact in a remote registry.
func runTagRemote(ctx context.Context, opts *TagOptions, ref catalog.Reference) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Resolve catalog URL
	catalogURL, err := catalog.ResolveCatalogURL(ctx, opts.Catalog)
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	registry, repository := catalog.ParseCatalogURL(catalogURL)

	// Create credential store
	credStore, err := catalog.NewCredentialStore(*lgr)
	if err != nil {
		lgr.V(1).Info("failed to create credential store, using anonymous auth", "error", err.Error())
	}

	// Resolve auth provider for automatic token bridging
	authHandler := resolveAuthHandler(ctx, registry, opts.Catalog)
	authScope := resolveAuthScope(ctx, opts.Catalog)

	// Create remote catalog
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

	// Tag in remote
	w.Infof("Tagging %s@%s as %q in %s...", ref.Name, ref.Version.String(), opts.Alias, catalogURL)

	oldVersion, err := remoteCatalog.Tag(ctx, ref, opts.Alias)
	if err != nil {
		if catalog.IsNotFound(err) {
			w.Errorf("artifact not found in remote registry")
			return exitcode.WithCode(err, exitcode.FileNotFound)
		}
		w.Errorf("failed to tag artifact: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	if oldVersion != "" {
		w.Warningf("Moved %q from previous artifact to %s", opts.Alias, ref.Version.String())
	}
	w.Successf("Tagged %s@%s as %q in %s", ref.Name, ref.Version.String(), opts.Alias, catalogURL)

	return nil
}
