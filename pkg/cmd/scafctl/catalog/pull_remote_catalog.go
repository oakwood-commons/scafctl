// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// newPullRemoteCatalog creates the remote catalog used by pull flows after the
// registry and repository have been resolved.
func newPullRemoteCatalog(ctx context.Context, opts *PullOptions, registry, repository string) (*catalog.RemoteCatalog, error) {
	return newRemoteCatalog(ctx, opts.Catalog, opts.Insecure, registry, repository)
}

// newRemoteCatalog creates a remote catalog for an already-resolved registry and
// repository. catalogFlag is the user's --catalog value (possibly empty); it
// selects the auth handler and scope configured for that catalog.
func newRemoteCatalog(ctx context.Context, catalogFlag string, insecure bool, registry, repository string) (*catalog.RemoteCatalog, error) {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	credStore, err := catalog.NewCredentialStore(*lgr)
	if err != nil {
		lgr.V(1).Info("failed to create credential store, using anonymous auth", "error", err.Error())
	}

	authHandler := resolveAuthHandler(ctx, registry, catalogFlag)
	authScope := resolveAuthScope(ctx, registry, catalogFlag)

	verboseRemoteInfo(ctx, w, registry, repository, authHandlerName(authHandler), authScope)

	remoteCatalog, err := catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
		Name:            registry,
		Registry:        registry,
		Repository:      repository,
		CredentialStore: credStore,
		AuthHandler:     authHandler,
		AuthScope:       authScope,
		Insecure:        insecure,
		Logger:          *lgr,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create remote catalog: %w", err)
	}

	return remoteCatalog, nil
}

// resolvePullFetchReference applies any version constraint and resolves the
// final concrete remote reference that pull will fetch.
func resolvePullFetchReference(ctx context.Context, opts *PullOptions, remoteCatalog catalog.Catalog, ref catalog.Reference) (catalog.Reference, error) {
	w := writer.FromContext(ctx)

	if opts.VersionConstraint != "" {
		resolved, err := resolveVersionConstraint(ctx, remoteCatalog, ref, opts.VersionConstraint)
		if err != nil {
			return catalog.Reference{}, err
		}
		w.Verbosef("Version constraint %q resolved to %s", opts.VersionConstraint, resolved.Version.Original())
		ref = resolved
	}

	info, err := remoteCatalog.Resolve(ctx, ref)
	if err != nil {
		return catalog.Reference{}, err
	}

	return info.Reference, nil
}
