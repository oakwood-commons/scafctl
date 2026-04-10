// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"

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
		Long: heredoc.Doc(`
			Create an alias tag for an existing catalog artifact.

			Tags are freeform aliases that point to a specific version of an artifact.
			Common uses include marking releases as "stable" or "production".

			Note: "latest" is reserved and auto-resolves to the highest semver version.
			It cannot be used as a manual alias.

			The source artifact must exist and have a version specified.
			The alias must not be a valid semver version (use 'scafctl build' for that).

			By default, tags the artifact in the local catalog. Use --catalog to
			tag an artifact in a remote registry.

			Examples:
			  # Tag a solution as stable
			  scafctl catalog tag my-solution@1.0.0 stable

			  # Tag for production
			  scafctl catalog tag my-solution@1.0.0 production

			  # Tag in a remote registry
			  scafctl catalog tag my-solution@1.0.0 production --catalog ghcr.io/myorg

			  # Tag with explicit kind
			  scafctl catalog tag echo@1.0.0 stable --kind provider
		`),
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
	name, version := catalog.ParseNameVersion(opts.Reference)
	if version == "" {
		w.Error("version required: use format 'name@version' (e.g., 'my-solution@1.0.0')")
		return exitcode.Errorf("version required")
	}

	// Reject digest references — tagging requires a semver source version.
	if catalog.IsValidDigest(version) {
		w.Error("digest references cannot be tagged; use a semver version (e.g., 'my-solution@1.0.0')")
		return exitcode.Errorf("digest not supported for tagging")
	}

	// Determine artifact kind from --kind flag if provided.
	var artifactKind catalog.ArtifactKind
	if opts.Kind != "" {
		kind, ok := catalog.ParseArtifactKind(opts.Kind)
		if !ok {
			w.Errorf("invalid kind %q: must be 'solution', 'provider', or 'auth-handler'", opts.Kind)
			return exitcode.Errorf("invalid kind")
		}
		artifactKind = kind
	}

	// Build reference
	ref, err := catalog.ParseReference(artifactKind, opts.Reference)
	if err != nil {
		w.Errorf("invalid reference %q: %v", opts.Reference, err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Remote tag operation — no local catalog needed.
	if opts.Catalog != "" {
		return runTagRemote(ctx, opts, ref)
	}

	// Local tagging: create local catalog and infer kind if not specified.
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		err = fmt.Errorf("failed to open local catalog: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	if artifactKind == "" {
		artifactKind, err = catalog.InferKindFromLocalCatalog(ctx, localCatalog, name, version)
		if err != nil {
			w.Errorf("failed to infer artifact kind: %v", err)
			w.Infof("Hint: use --kind to specify the artifact kind explicitly")
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
		// Re-parse reference with the inferred kind.
		ref, err = catalog.ParseReference(artifactKind, opts.Reference)
		if err != nil {
			w.Errorf("invalid reference %q: %v", opts.Reference, err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
	}

	// Tag locally
	oldVersion, err := localCatalog.Tag(ctx, ref, opts.Alias)
	if err != nil {
		if catalog.IsNotFound(err) {
			w.Errorf("artifact %q not found in local catalog", opts.Reference)
			return exitcode.WithCode(err, exitcode.FileNotFound)
		}
		w.Errorf("failed to tag artifact: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	if oldVersion != "" {
		w.Warningf("Moved %q from %s → %s", opts.Alias, oldVersion, ref.Version.String())
	}
	w.Successf("Tagged %s@%s as %q", ref.Name, ref.Version.String(), opts.Alias)

	return nil
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

	// Resolve auth handler for automatic token bridging
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
