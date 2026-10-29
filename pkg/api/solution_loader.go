// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"

	"github.com/oakwood-commons/scafctl/pkg/catalog/catalogindex"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/solution/load"
)

// SolutionLoaderFactory builds the solution loader for one API request. opts
// extend the factory's defaults, for example load.WithLenient() for lint.
//
// A loader is built per request rather than once per server so each request
// sees the current local catalog and its own request context.
type SolutionLoaderFactory func(ctx context.Context, opts ...load.RouterOption) load.Loader

// NewSolutionLoaderFactory returns the default SolutionLoaderFactory. Its
// loaders read only remote sources -- URLs and catalog references -- and never
// the server's filesystem or stdin: local files, auto-discovery, and stdin
// return load.ErrSourceNotEnabled whatever opts a caller passes.
//
// Catalog references are served only from the local catalog and the catalogs
// configured on the server. A reference naming any other registry is rejected
// without contacting it, so a request cannot make the server dial an
// arbitrary host or send it the server's registry credentials.
//
// idx translates catalog aliases; a nil idx uses the index in the request
// context (see catalogindex.FromContext).
func NewSolutionLoaderFactory(idx *catalogindex.Index) SolutionLoaderFactory {
	return func(ctx context.Context, opts ...load.RouterOption) load.Loader {
		lgr := logger.FromContext(ctx)
		aliases := idx
		if aliases == nil {
			aliases = catalogindex.FromContext(ctx)
		}
		routerOpts := make([]load.RouterOption, 0, 2+len(opts))
		routerOpts = append(routerOpts,
			load.WithoutLocal(),
			load.WithCatalog(aliases, load.SolutionFetcherFromContext(ctx, *lgr)),
		)
		routerOpts = append(routerOpts, opts...)
		return load.NewRouterFromContext(ctx, *lgr, routerOpts...)
	}
}

// SolutionLoader returns the solution loader for one request, built by
// NewSolutionLoader or, when that is nil, by the remote-only default factory.
func (hc *HandlerContext) SolutionLoader(ctx context.Context, opts ...load.RouterOption) load.Loader {
	factory := hc.NewSolutionLoader
	if factory == nil {
		factory = NewSolutionLoaderFactory(hc.CatalogIndex)
	}
	return factory(ctx, opts...)
}
