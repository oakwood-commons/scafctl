// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package load

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/go-logr/logr"

	"github.com/oakwood-commons/scafctl/pkg/catalog/catalogindex"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
)

// ErrSourceNotEnabled is returned by a Router when a classification names a
// source kind the Router was not configured to load (for example stdin when
// no reader was provided).
var ErrSourceNotEnabled = errors.New("solution source not enabled")

// Router loads a solution from any classified source by dispatching to the
// loader for its kind. It is itself a Loader, so domain code can accept a
// Loader and tests can substitute a fake.
//
// Local files and auto-discovery are enabled unless WithoutLocal is given.
// Stdin, URLs, and catalog references are opt-in via WithStdin, WithURL, and
// WithCatalog (WithoutLocal also overrides WithStdin); a classification for a
// source that is not enabled returns ErrSourceNotEnabled.
type Router struct {
	discover *DiscoverLoader // nil: local sources not enabled
	file     *FileLoader     // nil: local sources not enabled
	stdin    *StdinLoader    // nil: stdin not enabled or local sources not enabled
	url      *URLLoader      // nil: URLs not enabled
	catalog  *CatalogLoader  // nil: catalog references not enabled
}

var _ Loader = (*Router)(nil)

// RouterOption configures NewRouter.
type RouterOption interface {
	applyRouter(*routerConfig)
}

type routerOptionFunc func(*routerConfig)

func (f routerOptionFunc) applyRouter(c *routerConfig) { f(c) }

type routerConfig struct {
	lenient      bool
	noCache      bool
	noLocal      bool
	fileOpts     []FileOption
	discoverOpts []DiscoverOption
	stdin        io.Reader
	urlEnabled   bool
	urlClient    *httpc.Client
	urlOpts      []URLOption
	aliases      CatalogAliases
	fetcher      SolutionFetcher
}

// WithFileOptions passes options to the Router's FileLoader, which also
// serves auto-discovery.
func WithFileOptions(opts ...FileOption) RouterOption {
	return routerOptionFunc(func(c *routerConfig) { c.fileOpts = append(c.fileOpts, opts...) })
}

// WithDiscoverOptions passes options to the Router's DiscoverLoader.
func WithDiscoverOptions(opts ...DiscoverOption) RouterOption {
	return routerOptionFunc(func(c *routerConfig) { c.discoverOpts = append(c.discoverOpts, opts...) })
}

// WithStdin enables loading from stdin, reading from in. A nil reader leaves
// stdin disabled.
func WithStdin(in io.Reader) RouterOption {
	return routerOptionFunc(func(c *routerConfig) { c.stdin = in })
}

// WithURL enables loading from http(s) URLs. A nil client uses the secure
// default (see NewURLLoader).
func WithURL(client *httpc.Client, opts ...URLOption) RouterOption {
	return routerOptionFunc(func(c *routerConfig) {
		c.urlEnabled = true
		c.urlClient = client
		c.urlOpts = append(c.urlOpts, opts...)
	})
}

// WithCatalog enables loading catalog references (remote refs, bare names,
// and catalog-qualified names). A nil fetcher leaves catalog references
// disabled.
func WithCatalog(aliases CatalogAliases, fetcher SolutionFetcher) RouterOption {
	return routerOptionFunc(func(c *routerConfig) {
		c.aliases = aliases
		c.fetcher = fetcher
	})
}

// WithoutLocal disables every source local to the host process -- local
// files, auto-discovery, and stdin (even if WithStdin is given): those kinds
// return ErrSourceNotEnabled, and Select cannot auto-discover. It is for
// callers, such as the API server, whose requests must never read the host
// filesystem or process input. No option re-enables local sources, so it
// holds regardless of option order.
func WithoutLocal() RouterOption {
	return routerOptionFunc(func(c *routerConfig) { c.noLocal = true })
}

// NewRouter builds a Router and its loaders once. binaryName drives the
// auto-discovery folder names; empty uses settings.CliBinaryName.
//
// WithLenient applies to every loader (file, discovery, stdin, URL, catalog).
// WithNoCache applies to catalog references.
func NewRouter(binaryName string, opts ...RouterOption) *Router {
	cfg := &routerConfig{}
	for _, opt := range opts {
		opt.applyRouter(cfg)
	}

	fileOpts := cfg.fileOpts
	urlOpts := cfg.urlOpts
	var catalogOpts []CatalogOption
	var stdinOpts []StdinOption
	if cfg.lenient {
		fileOpts = append(fileOpts, WithLenient())
		urlOpts = append(urlOpts, WithLenient())
		catalogOpts = append(catalogOpts, WithLenient())
		stdinOpts = append(stdinOpts, WithLenient())
	}
	if cfg.noCache {
		catalogOpts = append(catalogOpts, WithNoCache())
	}

	r := &Router{}
	if !cfg.noLocal {
		r.file = NewFileLoader(fileOpts...)
		r.discover = NewDiscoverLoader(r.file, binaryName, cfg.discoverOpts...)
	}
	if cfg.stdin != nil && !cfg.noLocal {
		r.stdin = NewStdinLoader(cfg.stdin, stdinOpts...)
	}
	if cfg.urlEnabled {
		r.url = NewURLLoader(cfg.urlClient, urlOpts...)
	}
	if cfg.fetcher != nil {
		r.catalog = NewCatalogLoader(cfg.aliases, cfg.fetcher, catalogOpts...)
	}
	return r
}

// NewRouterFromContext builds a Router wired from ctx: auto-discovery uses the
// binary name and custom action file names in settings.Run, URLs use a client that enforces the app
// config's HTTP policy, and catalog references use the catalog index plus the
// local and configured remote catalogs, building unconfigured remotes on
// demand (see WithOnDemandRemotes). The catalog is only opened if a catalog
// reference is loaded. Stdin stays disabled unless opts enable it.
//
// The on-demand remotes make it unsuitable as-is for loading references that
// arrive over the network: such callers must override it with a WithCatalog
// whose fetcher does not use WithOnDemandRemotes.
//
// opts are applied after the context defaults, so callers can extend or
// override them.
func NewRouterFromContext(ctx context.Context, lgr logr.Logger, opts ...RouterOption) *Router {
	var httpCfg *config.HTTPClientConfig
	if cfg := config.FromContext(ctx); cfg != nil {
		httpCfg = &cfg.HTTPClient
	}

	defaults := []RouterOption{
		WithURL(httpc.NewClientFromAppConfig(httpCfg, logr.Discard())),
		WithCatalog(catalogindex.FromContext(ctx), SolutionFetcherFromContext(ctx, lgr, WithOnDemandRemotes())),
	}
	if s, ok := settings.FromContext(ctx); ok && len(s.ActionDiscoveryFileNames) > 0 {
		defaults = append(defaults, WithDiscoverOptions(WithCustomActionFiles(s.ActionDiscoveryFileNames)))
	}
	return NewRouter(settings.BinaryNameFromContext(ctx), append(defaults, opts...)...)
}

// CLIRouterOptions returns the router options shared by CLI commands: stdin
// (nil leaves it disabled), the auto-discovery mode, and WithNoCache when
// noCache is set.
func CLIRouterOptions(stdin io.Reader, noCache bool, mode settings.DiscoveryMode) []RouterOption {
	opts := []RouterOption{
		WithStdin(stdin),
		WithDiscoverOptions(WithDiscoveryMode(mode)),
	}
	if noCache {
		opts = append(opts, WithNoCache())
	}
	return opts
}

// Load dispatches c to the loader for its kind.
func (r *Router) Load(ctx context.Context, c source.Classification) (*MaterializedSolution, error) {
	switch c.Kind {
	case source.KindLocalFile:
		if r.file != nil {
			return r.file.Load(ctx, c)
		}
		return nil, notEnabled(c)
	case source.KindAutoDiscover:
		if r.discover != nil {
			return r.discover.Load(ctx, c)
		}
		return nil, notEnabled(c)
	case source.KindStdin:
		if r.stdin != nil {
			return r.stdin.Load(ctx, c)
		}
		return nil, notEnabled(c)
	case source.KindURL:
		if r.url != nil {
			return r.url.Load(ctx, c)
		}
		return nil, notEnabled(c)
	case source.KindRemoteRef, source.KindCatalogQualified, source.KindBareName:
		if r.catalog != nil {
			return r.catalog.Load(ctx, c)
		}
		return nil, notEnabled(c)
	case source.KindUnknown:
	}
	return nil, fmt.Errorf("router: %w: %s", ErrUnsupportedKind, c.Kind)
}

// notEnabled reports that the Router was not configured to load c's kind.
func notEnabled(c source.Classification) error {
	return fmt.Errorf("router: %w: %s source %q", ErrSourceNotEnabled, c.Kind, c.Ref)
}
