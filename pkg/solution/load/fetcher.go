// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package load

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-logr/logr"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/catalog/catalogindex"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/paths"
)

// FetcherOption configures SolutionFetcherFromContext.
type FetcherOption func(*fetcherConfig)

type fetcherConfig struct {
	onDemandRemotes bool
}

// WithOnDemandRemotes lets the fetcher serve a reference naming a registry that
// is not a configured catalog, using a remote built on demand with the
// process's credentials (see catalog.ContextRemoteFactory). Only use it where
// the caller chooses the reference -- never where it comes from the network,
// or a request could make the process contact any host with its credentials.
func WithOnDemandRemotes() FetcherOption {
	return func(c *fetcherConfig) { c.onDemandRemotes = true }
}

// SolutionFetcherFromContext returns a fetcher over the local catalog and the
// OCI remote catalogs configured in ctx. By default a reference naming any
// other registry is rejected without contacting it; WithOnDemandRemotes lifts
// that. Nothing is opened until the first fetch, so callers that never load a
// catalog reference pay nothing; a failure to open the local catalog is
// returned from that fetch and from every later one.
func SolutionFetcherFromContext(ctx context.Context, lgr logr.Logger, opts ...FetcherOption) SolutionFetcher {
	var cfg fetcherConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return newLazyFetcher(func() (SolutionFetcher, error) {
		return newSolutionFetcher(ctx, lgr, paths.CatalogDir(), cfg)
	})
}

// lazyFetcher builds its SolutionFetcher on the first fetch.
type lazyFetcher struct {
	build func() (SolutionFetcher, error)
}

var _ SolutionFetcher = (*lazyFetcher)(nil)

func newLazyFetcher(build func() (SolutionFetcher, error)) *lazyFetcher {
	return &lazyFetcher{build: sync.OnceValues(build)}
}

// FetchSolution builds the underlying fetcher if needed and delegates to it.
func (l *lazyFetcher) FetchSolution(ctx context.Context, ref catalog.Reference, opts ...catalog.FetchOption) (*catalog.SolutionParts, error) {
	f, err := l.build()
	if err != nil {
		return nil, fmt.Errorf("catalog not available: %w", err)
	}
	return f.FetchSolution(ctx, ref, opts...)
}

// newSolutionFetcher opens the local catalog at localDir and registers the OCI
// remotes configured in ctx. Unconfigured remotes are built on demand only
// when cfg.onDemandRemotes is set.
func newSolutionFetcher(ctx context.Context, lgr logr.Logger, localDir string, cfg fetcherConfig) (SolutionFetcher, error) {
	// TODO: share one LocalCatalog per process instead of opening a new one
	// here. Each instance wraps its own OCI store, which reads index.json once
	// at creation, so a second instance in the same command (e.g. a pull
	// followed by a load) won't see tags written through the first. Prefer a
	// context-carried instance (catalog.WithLocalCatalog/LocalCatalogFromContext,
	// set once at root) or catalogs.Set.Local() once catalogs are wired in.
	// The same applies to the ~15 other catalog.NewLocalCatalog call sites.
	local, err := catalog.NewLocalCatalogAt(localDir, lgr)
	if err != nil {
		return nil, fmt.Errorf("opening local catalog: %w", err)
	}

	var remotes []catalog.SolutionRemote
	for _, c := range catalog.RemoteCatalogsFromContext(ctx, lgr) {
		if r, ok := c.(catalog.SolutionRemote); ok {
			remotes = append(remotes, r)
		}
	}

	var fetcherOpts []catalog.SolutionFetcherOption
	if cfg.onDemandRemotes {
		fetcherOpts = append(fetcherOpts, catalog.WithRemoteFactory(catalog.ContextRemoteFactory(lgr)))
	}

	return catalog.NewSolutionFetcher(
		catalog.NewLocalSolutionStore(local),
		defaultRemoteID(ctx),
		remotes,
		fetcherOpts...,
	), nil
}

// defaultRemoteID returns the canonical origin of the configured default
// catalog, or "" when it is unset, local, or not a remote catalog.
func defaultRemoteID(ctx context.Context) string {
	cfg := config.FromContext(ctx)
	if cfg == nil {
		return ""
	}
	alias := cfg.Settings.DefaultCatalog
	if alias == "" || alias == config.CatalogNameLocal {
		return ""
	}
	id, _ := catalogindex.FromContext(ctx).RegistryForAlias(alias)
	return id
}
