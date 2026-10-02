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
	Origin    string // Select the local copy by where it came from (--origin)
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

			The local catalog can hold several copies with the same name and
			version (built locally, pulled from different catalogs). Delete
			never picks one silently: when the reference matches more than
			one, it fails and prints a command for each copy. Select one with
			--origin, --kind, or a digest.

			Use --all to delete all artifacts from the local catalog.

			Examples:
			  # Delete from local catalog
			  scafctl catalog delete my-solution@1.0.0

			  # Delete the locally built copy when a pulled copy also exists
			  scafctl catalog delete my-solution@1.0.0 --origin built

			  # Delete the copy pulled from a specific registry
			  scafctl catalog delete my-solution@1.0.0 --origin ghcr.io/myorg

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
				if options.Origin != "" {
					return exitcode.Errorf("--origin only applies to a single local delete; cannot be combined with --all")
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
	cmd.Flags().StringVar(&options.Origin, "origin", "", pushOriginFlagUsage)
	cmd.Flags().BoolVar(&options.Insecure, "insecure", false, "Allow insecure HTTP connections")

	return cmd
}

func runDelete(ctx context.Context, opts *DeleteOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Check if this is a remote delete: explicit --catalog flag or remote-looking reference
	if opts.Catalog != "" || looksLikeRemoteReference(opts.Reference) {
		if opts.Origin != "" {
			w.Error("--origin only applies to local deletes; it has no effect on a remote delete")
			return exitcode.Errorf("--origin not supported for remote delete")
		}
		return runDeleteRemote(ctx, opts)
	}

	// Parse reference to get name and version
	_, version := catalog.ParseNameVersion(opts.Reference)
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

	sel, err := catalog.ParseExactSelector(opts.Reference, opts.Kind, opts.Origin)
	if err != nil {
		w.Errorf("invalid reference %q: %v", opts.Reference, err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// The local catalog can hold several copies with the same name and
	// version (built locally, pulled from different catalogs). Resolve
	// selects exactly one, failing with a disambiguation hint rather than
	// picking one silently when the reference is ambiguous.
	info, err := localCatalog.ResolveExact(ctx, sel)
	if err != nil {
		return deleteResolveError(w, opts.CliParams.BinaryName, err)
	}
	ref := info.Reference

	// Dry-run mode: show what would be deleted and return
	if opts.DryRun {
		w.Infof("Would delete %s and any aliases of it from local catalog", ref.String())
		return nil
	}

	// Delete the resolved identity -- its version tag and every alias of it --
	// never a tag re-derived from the reference (which could belong to a
	// different digest or origin).
	removed, err := localCatalog.DeleteResolved(ctx, info)
	if err != nil {
		if catalog.IsNotFound(err) {
			w.Errorf("artifact %q not found in catalog", opts.Reference)
			return exitcode.WithCode(err, exitcode.FileNotFound)
		}
		w.Errorf("failed to delete artifact: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	if aliases := removedAliases(removed, ref); len(aliases) > 0 {
		w.Successf("Deleted %s (and aliases: %s)", ref.String(), strings.Join(aliases, ", "))
	} else {
		w.Successf("Deleted %s", ref.String())
	}

	// A different copy (e.g. one left behind under an alias by an earlier
	// same-version rebuild) can still match the reference; say so rather than
	// letting the user assume the version is gone.
	if remaining, err := localCatalog.ResolveExact(ctx, sel); err == nil {
		w.Warningf("%s still resolves to another local copy (digest %s); run delete again to remove it",
			opts.Reference, remaining.Digest)
	}

	return nil
}

// removedAliases returns the removed tag labels other than ref's version tag.
func removedAliases(removed []string, ref catalog.Reference) []string {
	version := ""
	if ref.Version != nil {
		version = ref.Version.String()
	}
	aliases := make([]string, 0, len(removed))
	for _, label := range removed {
		if label != version {
			aliases = append(aliases, label)
		}
	}
	return aliases
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
	// Several listed entries (a version tag and its aliases) can share one
	// identity; DeleteResolved removes them together, so count each once.
	seen := make(map[string]bool)
	for _, kind := range allKinds {
		artifacts, listErr := localCatalog.List(ctx, kind, "")
		if listErr != nil {
			lgr.V(1).Info("failed to list artifacts", "kind", kind, "error", listErr)
			failed++
			continue
		}
		for _, info := range artifacts {
			key := strings.Join([]string{info.Reference.Kind.String(), info.Reference.Name, info.Canonical, info.Digest}, "\x00")
			if seen[key] {
				continue
			}
			seen[key] = true
			if opts.DryRun {
				w.Infof("Would delete %s", info.Reference.String())
				deleted++
				continue
			}
			if _, delErr := localCatalog.DeleteResolved(ctx, info); delErr != nil {
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

	// Resolve auth provider for automatic token bridging
	authHandler := resolveAuthHandler(ctx, registry, opts.Catalog)
	authScope := resolveAuthScope(ctx, opts.Catalog)

	verboseRemoteInfo(ctx, w, registry, repository, authHandlerName(authHandler), authScope)

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

// deleteResolveError reports a ResolveExact failure for delete with its exit
// code. Ambiguity errors are followed by one copy-pasteable command per
// candidate.
func deleteResolveError(w *writer.Writer, binaryName string, err error) error {
	var ambArtifact *catalog.AmbiguousArtifactError
	var ambKind *catalog.AmbiguousKindError
	var hints []string
	switch {
	case errors.As(err, &ambArtifact):
		hints = ambArtifact.DeleteHints(binaryName)
	case errors.As(err, &ambKind):
		hints = ambKind.DeleteHints(binaryName)
	}
	return exactResolveError(w, err, hints)
}

// exactResolveError reports a ResolveExact failure with its exit code,
// followed by the given disambiguation hints (if any).
func exactResolveError(w *writer.Writer, err error, hints []string) error {
	w.Errorf("%v", err)
	if len(hints) > 0 {
		w.Infof("Select one with:")
		for _, h := range hints {
			w.Infof("  %s", h)
		}
	}

	switch {
	case catalog.IsAmbiguous(err), catalog.IsInvalidReference(err):
		return exitcode.WithCode(err, exitcode.InvalidInput)
	case catalog.IsNotFound(err):
		return exitcode.WithCode(err, exitcode.FileNotFound)
	default:
		return exitcode.WithCode(fmt.Errorf("failed to resolve artifact: %w", err), exitcode.CatalogError)
	}
}

// looksLikeRemoteReference returns true if the reference appears to be a remote registry URL.
// Remote references contain a registry host with a dot (e.g., "ghcr.io", "docker.io")
// or start with "oci://", "localhost:", or contain a port.
func looksLikeRemoteReference(ref string) bool {
	return catalog.LooksLikeRemoteReference(ref)
}
