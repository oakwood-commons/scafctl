// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/cache"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/cmd/cmdutil"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/format"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
)

// PullOptions holds options for the pull command.
type PullOptions struct {
	Reference         string // Artifact reference (short name or full remote)
	Catalog           string // Source catalog (URL or config name, --catalog)
	TargetName        string // Optional local name (--as)
	Kind              string // Artifact kind override (--kind)
	VersionConstraint string // Semver version constraint (--version)
	Force             bool   // Overwrite existing (--force)
	DryRun            bool   // Show what would be pulled without pulling (--dry-run)
	Insecure          bool   // Allow HTTP (--insecure)
	NoCache           bool   // Invalidate artifact cache after pull (--no-cache)
	Strict            bool   // Fail if the pulled bundle is incomplete (--strict)
	NoVerify          bool   // Skip pulled-bundle completeness verification (--no-verify)
	CliParams         *settings.Run
	IOStreams         *terminal.IOStreams
}

// CommandPull creates the pull command.
func CommandPull(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &PullOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:   "pull <reference>",
		Short: "Pull an artifact from a remote registry",
		Long: heredoc.Docf(`
			Pull a catalog artifact from a remote OCI registry to the local catalog.

			The root pull command only accepts full remote OCI references.
			Use kind subcommands for short references:
			  - %[1]s catalog pull solution <name[@version]>
			  - %[1]s catalog pull provider <name[@version]>
			  - %[1]s catalog pull auth-handler <name[@version]>

			When using --as, the target must be a concrete name@version.
			Bare names, the latest alias, and digest pins are rejected.

			If no version is specified, the latest version is pulled.

			Examples:
			  # Pull using a full remote reference
			  %[1]s catalog pull ghcr.io/myorg/solutions/my-solution@1.0.0

			  # Pull with a different local name
			  %[1]s catalog pull ghcr.io/myorg/solutions/my-solution@1.0.0 --as local-solution@1.0.0

			  # Force overwrite existing
			  %[1]s catalog pull ghcr.io/myorg/solutions/my-solution@1.0.0 --force
		`, cliParams.BinaryName),
		Args:         rootArgs("pull", cliParams.BinaryName, cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := reportUnknownKind(cmd, "pull", cliParams.BinaryName, args); err != nil {
				return err
			}
			options.Reference = args[0]
			return runPull(cmd.Context(), options)
		},
	}

	cmd.PersistentFlags().StringVar(&options.TargetName, "as", "", "Store with a different local name@version")
	cmd.PersistentFlags().StringVar(&options.VersionConstraint, "version", "", "Semver version constraint (e.g., ^1.0.0, ~2.1, >=1.0 <3.0)")
	cmd.PersistentFlags().BoolVarP(&options.Force, "force", "f", false, "Overwrite existing local artifact")
	cmd.PersistentFlags().BoolVar(&options.DryRun, "dry-run", false, "Show what would be pulled without actually pulling")
	cmd.PersistentFlags().BoolVar(&options.Insecure, "insecure", false, "Allow insecure HTTP connections")
	cmd.PersistentFlags().BoolVar(&options.NoCache, "no-cache", false, "Invalidate the artifact cache for this artifact after pulling")
	cmd.Flags().BoolVar(&options.Strict, "strict", false, "Fail if the pulled bundle is incomplete")
	cmd.Flags().BoolVar(&options.NoVerify, "no-verify", false, "Skip pulled-bundle completeness verification")
	addPullKindCommands(cmd, cliParams, ioStreams, options)

	return cmd
}

func runPull(ctx context.Context, opts *PullOptions) error {
	return runKind(ctx, "pull", opts.Reference, opts,
		validatePullInternal,
		buildPullRemoteReference,
		runPullResolvedBuiltReference)
}

func buildPullRemoteReference(ctx context.Context, opts *PullOptions) (pullReferenceBuild, error) {
	w := writer.FromContext(ctx)
	w.Verbose("Pulling from full OCI reference")

	build, err := newPullReferenceBuilder(ctx, opts).
		parseRemoteReference().
		Build()
	if err != nil {
		return pullReferenceBuild{}, err
	}

	verboseRefInfo(w, build.Reference.Name, string(build.Reference.Kind), build.Tag)

	return build, nil
}

func runPullResolvedBuiltReference(ctx context.Context, opts *PullOptions, build pullReferenceBuild) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)
	ref := build.Reference
	registry := build.Registry
	repository := build.Repository

	remoteCatalog, err := newPullRemoteCatalog(ctx, opts, registry, repository)
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Resolve the final concrete remote reference to fetch.
	ref, err = resolvePullFetchReference(ctx, opts, remoteCatalog, ref)
	if err != nil {
		if catalog.IsNotFound(err) {
			w.Errorf("artifact %q not found in remote registry", opts.Reference)
			return exitcode.WithCode(err, exitcode.FileNotFound)
		}
		err = fmt.Errorf("failed to resolve artifact: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Dry-run mode: show what would be pulled and return
	if opts.DryRun {
		writePullDryRun(w, opts, ref, registry, repository)
		return nil
	}

	// Create local catalog
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		err = fmt.Errorf("failed to open local catalog: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	copyOpts := newPullCopyOptions(lgr, opts)

	if ref.Kind == "" {
		err = fmt.Errorf("artifact kind is required after pull reference resolution")
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Pull from remote
	repoPath := remoteCatalog.RepositoryPath(ref)
	w.Infof("Pulling %s@%s from %s...", ref.Name, ref.VersionOrDigest(), repoPath)

	result, err := remoteCatalog.CopyToLocal(ctx, ref, localCatalog, copyOpts)
	if err != nil {
		return handlePullCopyError(w, err)
	}

	return postPull(ctx, opts, ref, result, localCatalog, remoteCatalog)
}

func newPullCopyOptions(lgr *logr.Logger, opts *PullOptions) catalog.CopyOptions {
	return catalog.CopyOptions{
		TargetName: opts.TargetName,
		Force:      opts.Force,
		OnProgress: func(desc ocispec.Descriptor) {
			lgr.V(1).Info("copying blob",
				"digest", desc.Digest.String(),
				"size", desc.Size)
		},
	}
}

func handlePullCopyError(w *writer.Writer, err error) error {
	if catalog.IsExists(err) {
		w.Errorf("artifact already exists locally (use --force to overwrite)")
		return exitcode.WithCode(err, exitcode.CatalogError)
	}
	err = fmt.Errorf("failed to pull artifact: %w", err)
	w.Errorf("%v", err)
	return exitcode.WithCode(err, exitcode.CatalogError)
}

func postPull(
	ctx context.Context,
	opts *PullOptions,
	ref catalog.Reference,
	result catalog.ArtifactInfo,
	localCatalog *catalog.LocalCatalog,
	remoteCatalog *catalog.RemoteCatalog,
) error {
	w := writer.FromContext(ctx)
	lgr := logger.FromContext(ctx)
	// The reference that was actually stored locally: with --as the artifact
	// lives under the renamed target (name and version), not the source ref.
	storedRef := result.Reference

	w.Successf("Pulled %s@%s (%s)",
		storedRef.Name,
		storedRef.VersionOrDigest(),
		format.Bytes(result.Size))

	// Warn about stale credentials if anonymous fallback was used.
	warnStaleCredentials(ctx, w, remoteCatalog)
	verboseCredentialSource(w, remoteCatalog)

	// === Pulled-bundle completeness verification (solution kind only) ===
	//
	// Verify against the FETCHED bundle reassembled by the local catalog's
	// FetchWithBundle (avoids a second network round-trip). Consumer policy is
	// warn-by-default; only --strict makes incompleteness fatal.
	if !opts.NoVerify && ref.Kind == catalog.ArtifactKindSolution {
		// Verify against the stored ref: the source ref may not exist in the
		// local catalog at all.
		if verr := verifyPulledBundle(ctx, localCatalog, storedRef, opts, w, lgr); verr != nil {
			return verr
		}
	}

	// When --no-cache is set, invalidate any stale artifact cache entry so that
	// subsequent run/render/get commands fetch the freshly pulled artifact from
	// the local catalog rather than a cached copy.
	if opts.NoCache {
		targetName := storedRef.Name
		version := ""
		if storedRef.Version != nil {
			version = storedRef.Version.String()
		}
		if err := cache.InvalidateArtifact(paths.ArtifactCacheDir(), settings.DefaultArtifactCacheTTL, string(ref.Kind), targetName, version); err != nil {
			lgr.V(1).Info("failed to invalidate artifact cache (ignoring)", "error", err)
		} else {
			lgr.V(1).Info("artifact cache invalidated", "kind", ref.Kind, "name", targetName, "version", version)
		}
	}

	return nil
}

func writePullDryRun(w *writer.Writer, opts *PullOptions, ref catalog.Reference, registry, repository string) {
	targetName := ref.Name
	if opts.TargetName != "" {
		targetName = opts.TargetName
	}
	src := fmt.Sprintf("%s/%s/%s/%s", registry, repository, ref.Kind.Plural(), ref.Name)
	w.Infof("Would pull %s@%s from %s to local catalog as %s", ref.Name, ref.VersionOrDigest(), src, targetName)
}

// verifyPulledBundle fetches the pulled bundle from the local catalog, verifies
// its completeness, renders the result, and enforces consumer policy:
// warn-by-default on incompleteness; only --strict makes it fatal.
func verifyPulledBundle(ctx context.Context, localCatalog catalog.Catalog, ref catalog.Reference, opts *PullOptions, w *writer.Writer, lgr *logr.Logger) error {
	content, bundleData, _, ferr := localCatalog.FetchWithBundle(ctx, ref)
	if ferr != nil {
		return failOrWarnPull(w, opts.Strict, fmt.Errorf("fetching pulled bundle for verification: %w", ferr))
	}

	var sol solution.Solution
	if err := sol.LoadFromBytes(content); err != nil {
		return failOrWarnPull(w, opts.Strict, fmt.Errorf("parsing pulled solution for verification: %w", err))
	}

	// Pass the (possibly empty) bundleData through to VerifyBundle: the empty
	// case is handled by verifyNoBundleCase, which surfaces warnings for local
	// files and unvendored catalog deps. Skipping it here would let --strict
	// wrongly succeed on a bundle-less-but-incomplete artifact.
	vr, err := bundler.VerifyBundle(ctx, &sol, bundleData, *lgr)
	if err != nil {
		return failOrWarnPull(w, opts.Strict, fmt.Errorf("verifying pulled bundle: %w", err))
	}

	// Consumer: in non-strict mode incompleteness is a warning (the pull still
	// succeeds), so render missing items as warnings rather than errors to avoid
	// contradicting the success result. Under --strict they render as errors.
	cmdutil.RenderVerifyResult(w, vr, !opts.Strict)

	warnMsg, failErr := pullVerifyDecision(vr, opts.Strict)
	if warnMsg != "" {
		w.Warningf("%s", warnMsg)
	}
	if failErr != nil {
		w.Errorf("%v", failErr)
	}
	return failErr
}

// failOrWarnPull applies the consumer fail-closed policy for infrastructure,
// parse, and verify errors. Under --strict these fail closed with a printed,
// exit-coded error; otherwise they warn and return nil so a transient infra
// hiccup does not break the pull.
func failOrWarnPull(w *writer.Writer, strict bool, err error) error {
	if strict {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}
	w.Warningf("%v (skipping verification; run with --strict to fail)", err)
	return nil
}

// pullVerifyDecision applies the consumer verification policy to a verify
// result. Consumer policy is warn-by-default: incompleteness only becomes fatal
// under --strict, and warnings become fatal only under --strict. It returns a
// warning message to emit (empty for none) and the error to fail the pull with
// (nil to continue). The two returns are mutually exclusive.
func pullVerifyDecision(vr *bundler.VerifyResult, strict bool) (warnMsg string, failErr error) {
	switch {
	case !vr.Passed() && strict:
		return "", exitcode.Errorf("pulled bundle is incomplete: %d error(s) (strict)", len(vr.Errors))
	case !vr.Passed():
		return fmt.Sprintf("pulled bundle is incomplete: %d error(s) -- run with --strict to fail", len(vr.Errors)), nil
	case len(vr.Warnings) > 0 && strict:
		return "", exitcode.Errorf("pulled bundle has %d warning(s) (strict)", len(vr.Warnings))
	}
	return "", nil
}

func validatePullInternal(opts *PullOptions) error {
	if err := validateVersionConstraint(opts.Reference, opts.VersionConstraint); err != nil {
		return err
	}

	if !looksLikeRemoteReference(opts.Reference) {
		return fmt.Errorf("%w; use 'catalog pull <kind> <name[@version]>' for short references", ErrRemoteReferenceRequired)
	}

	remoteRef, err := catalog.ParseRemoteReference(opts.Reference)
	if err != nil {
		return fmt.Errorf("invalid reference: %w", err)
	}

	parsedRef, err := remoteRef.ToReference()
	if err != nil {
		return fmt.Errorf("invalid reference: %w", err)
	}

	if err := validateTargetName(parsedRef, opts.TargetName); err != nil {
		return err
	}

	return nil
}
