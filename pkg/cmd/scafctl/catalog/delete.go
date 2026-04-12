// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
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

// DeleteOptions holds options for the delete command.
type DeleteOptions struct {
	Reference string
	Catalog   string // Target catalog for remote delete (URL or config name, --catalog)
	Kind      string // Artifact kind override (--kind)
	Insecure  bool
	CliParams *settings.Run
	IOStreams *terminal.IOStreams
}

// CommandDelete creates the delete command.
func CommandDelete(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &DeleteOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:          "delete <name@version>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete an artifact from the catalog",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Delete an artifact from the local or remote catalog.

			You must specify the exact version to delete.

			For local artifacts, use the simple name@version format.
			For remote artifacts, use the full registry path or specify --catalog.

			Examples:
			  # Delete from local catalog
			  scafctl catalog delete my-solution@1.0.0

			  # Delete from remote registry (full reference)
			  scafctl catalog delete ghcr.io/myorg/scafctl/solutions/my-solution@1.0.0

			  # Delete from a configured catalog
			  scafctl catalog delete my-solution@1.0.0 --catalog myregistry
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Reference = args[0]
			return runDelete(cmd.Context(), options)
		},
	}

	cmd.Flags().StringVarP(&options.Catalog, "catalog", "c", "", catalogFlagUsage)
	cmd.Flags().StringVar(&options.Kind, "kind", "", "Artifact kind override (solution, provider, auth-handler)")
	cmd.Flags().BoolVar(&options.Insecure, "insecure", false, "Allow insecure HTTP connections")

	return cmd
}

func runDelete(ctx context.Context, opts *DeleteOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Check if this is a remote delete: explicit --catalog flag or remote-looking reference
	if opts.Catalog != "" || looksLikeRemoteReference(opts.Reference) {
		return runDeleteRemote(ctx, opts)
	}

	// Parse reference to get name and version
	name, version := catalog.ParseNameVersion(opts.Reference)
	if version == "" {
		w.Error("version required: use format 'name@version' (e.g., 'my-solution@1.0.0')")
		return exitcode.Errorf("version required")
	}

	// Create local catalog
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		w.Errorf("failed to open catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Determine artifact kind - first try --kind flag, then infer from local catalog
	var artifactKind catalog.ArtifactKind
	if opts.Kind != "" {
		kind, ok := catalog.ParseArtifactKind(opts.Kind)
		if !ok {
			w.Errorf("invalid kind %q: must be 'solution', 'provider', or 'auth-handler'", opts.Kind)
			return exitcode.Errorf("invalid kind")
		}
		artifactKind = kind
	} else {
		// Infer kind from local catalog by trying each kind
		artifactKind, err = catalog.InferKindFromLocalCatalog(ctx, localCatalog, name, version)
		if err != nil {
			w.Errorf("failed to infer artifact kind: %v", err)
			w.Infof("Hint: use --kind to specify the artifact kind explicitly")
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
	}

	// Build reference
	ref, err := catalog.ParseReference(artifactKind, opts.Reference)
	if err != nil {
		w.Errorf("invalid reference %q: %v", opts.Reference, err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Delete artifact
	if err := localCatalog.Delete(ctx, ref); err != nil {
		if catalog.IsNotFound(err) {
			w.Errorf("artifact %q not found in catalog", opts.Reference)
			return exitcode.WithCode(err, exitcode.FileNotFound)
		}
		w.Errorf("failed to delete artifact: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	w.Successf("Deleted %s", ref.String())

	return nil
}

// runDeleteRemote deletes an artifact from a remote registry.
func runDeleteRemote(ctx context.Context, opts *DeleteOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	var registry, repository string
	var ref catalog.Reference

	if looksLikeRemoteReference(opts.Reference) {
		// Full remote reference: ghcr.io/myorg/scafctl/solutions/my-solution@1.0.0
		remoteRef, err := catalog.ParseRemoteReference(opts.Reference)
		if err != nil {
			w.Errorf("invalid remote reference: %v", err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}

		// Override kind if specified
		if opts.Kind != "" {
			kind, ok := catalog.ParseArtifactKind(opts.Kind)
			if !ok {
				w.Errorf("invalid kind %q: must be 'solution', 'provider', or 'auth-handler'", opts.Kind)
				return exitcode.Errorf("invalid kind")
			}
			remoteRef.Kind = kind
		}

		// Require version/tag for deletion
		if remoteRef.Tag == "" {
			w.Error("version required: use format 'registry/repo/kind/name@version'")
			return exitcode.Errorf("version required")
		}

		registry = remoteRef.Registry
		repository = remoteRef.Repository
		localRef, err := remoteRef.ToReference()
		if err != nil {
			w.Errorf("invalid reference: %v", err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
		ref = localRef
	} else {
		// Short reference with --catalog flag: my-solution@1.0.0 --catalog myregistry
		name, version := catalog.ParseNameVersion(opts.Reference)
		if version == "" {
			w.Error("version required: use format 'name@version' (e.g., 'my-solution@1.0.0')")
			return exitcode.Errorf("version required")
		}

		// Resolve catalog URL
		catalogURL, err := catalog.ResolveCatalogURL(ctx, opts.Catalog)
		if err != nil {
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
		registry, repository = catalog.ParseCatalogURL(catalogURL)

		// Determine artifact kind
		var artifactKind catalog.ArtifactKind
		if opts.Kind != "" {
			kind, ok := catalog.ParseArtifactKind(opts.Kind)
			if !ok {
				w.Errorf("invalid kind %q: must be 'solution', 'provider', or 'auth-handler'", opts.Kind)
				return exitcode.Errorf("invalid kind")
			}
			artifactKind = kind
		} else {
			// Try to infer from local catalog first, then fall back to remote
			localCatalog, localErr := catalog.NewLocalCatalog(*lgr)
			if localErr == nil {
				artifactKind, err = catalog.InferKindFromLocalCatalog(ctx, localCatalog, name, version)
			}
			if artifactKind == "" {
				// Local inference failed or unavailable; defer to remote inference
				// after the remote catalog is created (see below).
				lgr.V(1).Info("local kind inference failed, will try remote",
					"localErr", localErr, "inferErr", err)
			}
		}

		if artifactKind != "" {
			ref, err = catalog.ParseReference(artifactKind, opts.Reference)
			if err != nil {
				w.Errorf("invalid reference: %v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}
		}
		// When artifactKind is empty, ref will be set after remote inference below.
	}

	// Create credential store
	credStore, err := catalog.NewCredentialStore(*lgr)
	if err != nil {
		lgr.V(1).Info("failed to create credential store, using anonymous auth", "error", err.Error())
	}

	// Resolve auth handler for automatic token bridging
	authHandler := resolveAuthHandler(ctx, registry, opts.Catalog)

	// Create remote catalog
	remoteCatalog, err := catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
		Name:            registry,
		Registry:        registry,
		Repository:      repository,
		CredentialStore: credStore,
		AuthHandler:     authHandler,
		AuthScope:       resolveAuthScope(ctx, opts.Catalog),
		Insecure:        opts.Insecure,
		Logger:          *lgr,
	})
	if err != nil {
		w.Errorf("failed to create remote catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// If kind is still unknown (short-reference path where ParseReference was
	// skipped because no --kind was provided and local inference failed),
	// infer from the remote catalog by probing each artifact kind.
	if ref.Kind == "" {
		// Use already-parsed ref fields when available (full remote ref path),
		// otherwise parse from the raw reference string (short ref path).
		infName, infVersion := ref.Name, ""
		if ref.Version != nil {
			infVersion = ref.Version.String()
		} else if ref.Digest != "" {
			infVersion = ref.Digest
		}
		if infName == "" {
			infName, infVersion = catalog.ParseNameVersion(opts.Reference)
		}
		inferredKind, inferErr := catalog.InferKindFromRemote(ctx, remoteCatalog, infName, infVersion)
		if inferErr != nil {
			w.Errorf("could not infer artifact kind: %v", inferErr)
			w.Infof("Hint: use --kind to specify the artifact kind explicitly")
			return exitcode.WithCode(inferErr, exitcode.InvalidInput)
		}
		ref, err = catalog.ParseReference(inferredKind, opts.Reference)
		if err != nil {
			w.Errorf("invalid reference: %v", err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
	}

	// Delete from remote
	repoPath := remoteCatalog.RepositoryPath(ref)
	w.Infof("Deleting %s@%s from %s...", ref.Name, ref.VersionOrDigest(), repoPath)

	if err := remoteCatalog.Delete(ctx, ref); err != nil {
		if catalog.IsNotFound(err) {
			w.Errorf("artifact not found in remote registry")
			return exitcode.WithCode(err, exitcode.FileNotFound)
		}
		// Check for unsupported operation (some registries don't support DELETE)
		errStr := err.Error()
		if strings.Contains(errStr, "405") || strings.Contains(errStr, "unsupported") {
			w.Errorf("registry does not support deletion via API")
			w.Infof("For GitHub (ghcr.io), delete packages at: https://github.com/orgs/%s/packages", repository)
			return exitcode.WithCode(err, exitcode.CatalogError)
		}
		w.Errorf("failed to delete artifact: %v", err)
		hintOnAuthError(ctx, w, registry, err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	w.Successf("Deleted %s@%s from %s", ref.Name, ref.VersionOrDigest(), repoPath)

	return nil
}

// looksLikeRemoteReference returns true if the reference appears to be a remote registry URL.
// Remote references contain a registry host with a dot (e.g., "ghcr.io", "docker.io")
// or start with "oci://", "localhost:", or contain a port.
func looksLikeRemoteReference(ref string) bool {
	return catalog.LooksLikeRemoteReference(ref)
}
