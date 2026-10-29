// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/sbom"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/terminal/format"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// pushKindOptions holds the options of the push commands: the root command
// (full remote reference) and the `catalog push <kind>` subcommands. It extends
// PushOptions with --origin.
type pushKindOptions struct {
	PushOptions
	Origin string // Select the local artifact by origin: "built" or a registry URL (--origin)
}

// runPushResolvedBuiltReference opens the local catalog and pushes the parsed
// request.
func runPushResolvedBuiltReference(ctx context.Context, opts *pushKindOptions, build pushReferenceBuild) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		err = fmt.Errorf("failed to open local catalog: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	info, err := resolvePushSource(ctx, opts, localCatalog, build)
	if err != nil {
		return err
	}
	ref := info.Reference

	if opts.DryRun {
		writePushDryRun(w, opts, ref, build)
		return nil
	}

	remoteCatalog, err := newRemoteCatalog(ctx, opts.Catalog, opts.Insecure, build.Registry, build.Repository)
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	return pushToRemote(ctx, opts, localCatalog, remoteCatalog, ref, build)
}

// resolvePushSource finds the local artifact to push. A reference with a
// version and an origin is a direct lookup. Otherwise the catalog searches, and
// a name and version held under several origins is reported with a command that
// selects each one, instead of guessing.
func resolvePushSource(ctx context.Context, opts *pushKindOptions, localCatalog *catalog.LocalCatalog, build pushReferenceBuild) (catalog.ArtifactInfo, error) {
	w := writer.FromContext(ctx)

	info, err := localCatalog.Resolve(ctx, build.Reference)
	if err == nil {
		return info, nil
	}

	var ambiguous *catalog.AmbiguousArtifactError
	switch {
	case errors.As(err, &ambiguous):
		w.Errorf("%v\n%s", err, originHint(ambiguous))
		return catalog.ArtifactInfo{}, exitcode.WithCode(err, exitcode.InvalidInput)
	case catalog.IsNotFound(err):
		w.Errorf("artifact %q not found in local catalog", opts.Reference)
		return catalog.ArtifactInfo{}, exitcode.WithCode(err, exitcode.FileNotFound)
	case errors.Is(err, catalog.ErrInvalidQuery):
		w.Errorf("%v", err)
		return catalog.ArtifactInfo{}, exitcode.WithCode(err, exitcode.InvalidInput)
	default:
		err = fmt.Errorf("failed to resolve artifact: %w", err)
		w.Errorf("%v", err)
		return catalog.ArtifactInfo{}, exitcode.WithCode(err, exitcode.CatalogError)
	}
}

// pushSourceLabel names where the local copy came from.
func pushSourceLabel(ref catalog.Reference) string {
	if ref.Remote != "" {
		return ref.Remote
	}
	return catalog.SourceBuiltKeyword
}

func writePushDryRun(w *writer.Writer, opts *pushKindOptions, ref catalog.Reference, build pushReferenceBuild) {
	target := build.remoteRef(ref)
	dest := fmt.Sprintf("%s/%s/%s", catalog.CanonicalRemoteCatalogID(build.Registry, build.Repository), ref.Kind.Plural(), target.Name)
	w.Infof("Would push %s@%s (origin: %s) to %s", ref.Name, ref.VersionOrDigest(), pushSourceLabel(ref), dest)
	if opts.TargetName != "" {
		w.Infof("  --as: would publish as %s@%s", target.Name, target.VersionOrDigest())
	}
	if opts.Force {
		w.Infof("  --force: would overwrite if exists")
	}
	if opts.Latest {
		w.Infof("  --latest: would tag as \"latest\"")
	}
}

// pushToRemote copies the resolved local artifact to the destination, then
// attaches the SBOM and applies the latest tag when requested.
func pushToRemote(ctx context.Context, opts *pushKindOptions, localCatalog *catalog.LocalCatalog, remoteCatalog *catalog.RemoteCatalog, ref catalog.Reference, build pushReferenceBuild) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	remoteRef := build.remoteRef(ref)
	copyOpts := catalog.CopyOptions{
		TargetName: opts.TargetName,
		Force:      opts.Force,
		OnProgress: func(desc ocispec.Descriptor) {
			lgr.V(1).Info("copying blob",
				"digest", desc.Digest.String(),
				"size", desc.Size)
		},
	}

	repoPath := remoteCatalog.RepositoryPath(remoteRef)
	w.Infof("Pushing %s@%s (origin: %s) to %s...", ref.Name, ref.VersionOrDigest(), pushSourceLabel(ref), repoPath)

	result, err := remoteCatalog.CopyFromLocal(ctx, ref, localCatalog, copyOpts)
	if err != nil {
		if catalog.IsExists(err) {
			w.Errorf("artifact already exists in remote (use --force to overwrite)")
			return exitcode.WithCode(err, exitcode.CatalogError)
		}
		err = fmt.Errorf("failed to push artifact: %w", err)
		w.Errorf("%v", err)
		hintOnAuthError(ctx, w, build.Registry, err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	w.Successf("Pushed %s@%s (%s)",
		remoteRef.Name,
		remoteRef.VersionOrDigest(),
		format.Bytes(result.Size))

	// Auto-generate and attach SBOM for solutions (unless --no-sbom)
	if shouldAttachSBOM(ref.Kind, opts.NoSBOM) {
		if err := attachSBOMAs(ctx, opts, localCatalog, remoteCatalog, ref, remoteRef); err != nil {
			w.Warningf("SBOM attachment failed: %v", err)
			// Non-fatal: the push itself succeeded
		}
	}

	// Tag as "latest" in the remote registry if requested.
	if opts.Latest {
		w.Infof("Tagging %s@%s as \"latest\"...", remoteRef.Name, remoteRef.VersionOrDigest())
		_, tagErr := remoteCatalog.Tag(ctx, remoteRef, "latest")
		if tagErr != nil {
			w.Warningf("failed to tag as latest: %v", tagErr)
			// Non-fatal: the push itself succeeded
		} else {
			w.Successf("Tagged %s@%s as \"latest\"", remoteRef.Name, remoteRef.VersionOrDigest())
		}
	}

	return nil
}

// attachSBOMAs generates an SPDX SBOM from the local solution content and
// attaches it as a referrer to the pushed remote artifact. ref locates the
// content in the local catalog; remoteRef is the artifact's name in the
// destination, which differs from ref when --as renames it.
func attachSBOMAs(ctx context.Context, opts *pushKindOptions, localCatalog *catalog.LocalCatalog, remoteCatalog *catalog.RemoteCatalog, ref, remoteRef catalog.Reference) error {
	w := writer.FromContext(ctx)

	contentData, _, err := localCatalog.Fetch(ctx, ref)
	if err != nil {
		return fmt.Errorf("failed to fetch local content for SBOM: %w", err)
	}

	var sol solution.Solution
	if err := sol.UnmarshalFromBytes(contentData); err != nil {
		return fmt.Errorf("failed to parse solution for SBOM: %w", err)
	}

	sbomData, err := sbom.Generate(&sol, sbom.GenerateOptions{
		BinaryName: cliBinaryName(opts.CliParams),
	})
	if err != nil {
		return fmt.Errorf("failed to generate SBOM: %w", err)
	}

	w.Infof("Attaching SBOM to %s@%s...", remoteRef.Name, remoteRef.VersionOrDigest())

	desc, err := remoteCatalog.Attach(ctx, remoteRef, sbom.MediaType, sbomData, map[string]string{
		"org.opencontainers.image.title": fmt.Sprintf("%s-%s.spdx.json", remoteRef.Name, remoteRef.VersionOrDigest()),
	})
	if err != nil {
		return fmt.Errorf("failed to attach SBOM: %w", err)
	}

	w.Successf("SBOM attached (%s)", desc.Digest.String())
	return nil
}
