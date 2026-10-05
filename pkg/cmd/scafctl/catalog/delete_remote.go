// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// remoteDeleter is the part of a remote catalog the delete flow needs.
type remoteDeleter interface {
	Delete(ctx context.Context, ref catalog.Reference) error
	RepositoryPath(ref catalog.Reference) string
}

// deleteFromRemoteCatalog opens the remote catalog resolved from --catalog and
// deletes the target from it.
func deleteFromRemoteCatalog(ctx context.Context, opts *DeleteOptions, build deleteReferenceBuild) error {
	w := writer.FromContext(ctx)

	remoteCatalog, err := newRemoteCatalog(ctx, opts.Catalog, opts.Insecure, build.Registry, build.Repository)
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	return deleteFromRemote(ctx, opts, remoteCatalog, build, build.Registry, build.Repository)
}

// deleteFromRemote deletes build.Reference from the given remote catalog.
//
// Origin does not apply to remote artifacts, so the reference is deleted as
// given: the kind, name and exact version identify one artifact in the registry.
func deleteFromRemote(ctx context.Context, opts *DeleteOptions, remote remoteDeleter, build deleteReferenceBuild, registry, repository string) error {
	w := writer.FromContext(ctx)
	ref := build.Reference
	repoPath := remote.RepositoryPath(ref)

	// Remote existence is not checked in dry-run mode to avoid unnecessary
	// network requests.
	if opts.DryRun {
		w.Infof("Would delete %s@%s from %s (remote existence not verified)", ref.Name, ref.VersionOrDigest(), repoPath)
		return nil
	}

	w.Infof("Deleting %s@%s from %s...", ref.Name, ref.VersionOrDigest(), repoPath)

	if err := remote.Delete(ctx, ref); err != nil {
		if catalog.IsNotFound(err) {
			w.Errorf("artifact %q not found in remote registry", opts.Reference)
			return exitcode.WithCode(err, exitcode.FileNotFound)
		}
		// Some registries do not support DELETE via the API.
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
