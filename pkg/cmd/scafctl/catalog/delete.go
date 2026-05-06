// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/input"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// DeleteOptions holds options for the delete command.
type DeleteOptions struct {
	Reference string
	All       bool   // Delete all local artifacts (--all)
	Force     bool   // Skip confirmation prompt (--force)
	DryRun    bool   // Show what would be deleted without deleting (--dry-run)
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
		Long: strings.ReplaceAll(heredoc.Doc(`
			Delete an artifact from the local or remote catalog.

			You must specify the exact version to delete.

			For local artifacts, use the simple name@version format.
			For remote artifacts, use the full registry path or specify --catalog.

			Use --all to delete all artifacts from the local catalog.

			Examples:
			  # Delete from local catalog
			  scafctl catalog delete my-solution@1.0.0

			  # Delete all local artifacts
			  scafctl catalog delete --all

			  # Delete all local artifacts (skip confirmation)
			  scafctl catalog delete --all --force

			  # Delete from remote registry (full reference)
			  scafctl catalog delete ghcr.io/myorg/scafctl/solutions/my-solution@1.0.0

			  # Delete from a configured catalog
			  scafctl catalog delete my-solution@1.0.0 --catalog myregistry
		`), settings.CliBinaryName, cliParams.BinaryName),
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.All {
				if len(args) > 0 {
					return exitcode.Errorf("--all cannot be used with positional arguments")
				}
				if options.Catalog != "" {
					return exitcode.Errorf("--all only applies to the local catalog; cannot be combined with --catalog")
				}
				return runDeleteAll(cmd.Context(), options)
			}
			if len(args) != 1 {
				return exitcode.Errorf("requires exactly 1 argument: %s catalog delete my-solution@1.0.0", cliParams.BinaryName)
			}
			options.Reference = args[0]
			return runDelete(cmd.Context(), options)
		},
	}

	cmd.Flags().BoolVar(&options.All, "all", false, "Delete all artifacts from the local catalog")
	cmd.Flags().BoolVarP(&options.Force, "force", "f", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&options.DryRun, "dry-run", false, "Show what would be deleted without actually deleting")
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

	// Dry-run mode: show what would be deleted and return
	if opts.DryRun {
		// Verify the artifact exists before reporting
		exists, getErr := localCatalog.Exists(ctx, ref)
		if getErr != nil {
			w.Errorf("failed to check artifact: %v", getErr)
			return exitcode.WithCode(getErr, exitcode.CatalogError)
		}
		if !exists {
			w.Errorf("artifact %q not found in catalog", opts.Reference)
			return exitcode.WithCode(fmt.Errorf("artifact %q not found in catalog", opts.Reference), exitcode.FileNotFound)
		}
		w.Infof("Would delete %s from local catalog", ref.String())
		return nil
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

// runDeleteAll deletes all artifacts from the local catalog.
func runDeleteAll(ctx context.Context, opts *DeleteOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Confirm action (skip in force, dry-run, or quiet mode)
	if !opts.Force && !opts.DryRun && !opts.CliParams.IsQuiet {
		in := input.FromContext(ctx)
		if in == nil {
			return fmt.Errorf("input not initialized in context")
		}
		confirmed, err := in.Confirm(input.NewConfirmOptions().
			WithPrompt("Delete all artifacts from the local catalog?").
			WithDefault(false))
		if err != nil {
			err := fmt.Errorf("failed to read confirmation: %w", err)
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.GeneralError)
		}
		if !confirmed {
			w.Info("Delete cancelled")
			return nil
		}
	}

	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		w.Errorf("failed to open catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	allKinds := []catalog.ArtifactKind{
		catalog.ArtifactKindSolution,
		catalog.ArtifactKindProvider,
		catalog.ArtifactKindAuthHandler,
	}

	var deleted, failed int
	for _, kind := range allKinds {
		artifacts, listErr := localCatalog.List(ctx, kind, "")
		if listErr != nil {
			lgr.V(1).Info("failed to list artifacts", "kind", kind, "error", listErr)
			failed++
			continue
		}
		for _, info := range artifacts {
			if opts.DryRun {
				w.Infof("Would delete %s", info.Reference.String())
				deleted++
				continue
			}
			if delErr := localCatalog.Delete(ctx, info.Reference); delErr != nil {
				lgr.V(1).Info("failed to delete artifact", "ref", info.Reference.String(), "error", delErr)
				failed++
				continue
			}
			deleted++
		}
	}

	if opts.DryRun {
		if failed > 0 {
			w.Warningf("Failed to list %d artifact kind(s); preview may be incomplete", failed)
		}
		if deleted == 0 && failed == 0 {
			w.Infof("No artifacts in local catalog")
		} else if deleted > 0 {
			w.Infof("Would delete %d artifact(s) from local catalog", deleted)
		}
		return nil
	}

	if failed > 0 {
		w.Warningf("Failed to delete %d artifact(s); check logs for details", failed)
	}

	if deleted == 0 && failed == 0 {
		w.Infof("No artifacts in local catalog")
	} else if deleted > 0 {
		w.Successf("Deleted %d artifact(s) from local catalog", deleted)
	}

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
		w.Verbose("Deleting from full OCI reference")

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

		verboseRefInfo(w, remoteRef.Name, string(remoteRef.Kind), remoteRef.Tag)
	} else {
		// Short reference with --catalog flag: my-solution@1.0.0 --catalog myregistry
		w.Verbosef("Deleting from catalog %q", opts.Catalog)

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
	authScope := resolveAuthScope(ctx, opts.Catalog)

	verboseRemoteInfo(ctx, w, registry, repository, authHandler, authScope)

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

	// Dry-run mode: show what would be deleted and return.
	// Remote existence is not checked to avoid unnecessary network requests.
	if opts.DryRun {
		w.Infof("Would delete %s@%s from %s (remote existence not verified)", ref.Name, ref.VersionOrDigest(), repoPath)
		return nil
	}

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
