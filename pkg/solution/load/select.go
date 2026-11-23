// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package load

import (
	"context"
	"errors"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
)

// ArgPolicy says when Select may take the solution source from the first
// positional argument instead of auto-discovery.
type ArgPolicy int

const (
	// ArgsAreNames treats positional args as names inside the solution
	// (resolvers, actions). args[0] is the source only when it cannot be a
	// name: a URL, a registry reference, or a versioned name ("app@1.2.3").
	ArgsAreNames ArgPolicy = iota
	// ArgsAreNamesOrCatalog is ArgsAreNames, plus a bare name args[0] is
	// selected before discovery when --catalog or --version scopes it, or a
	// valid catalog bare name (catalog.IsValidName) is selected as a fallback
	// when no solution file is discovered.
	ArgsAreNamesOrCatalog
)

// Selection is the solution source chosen for a command invocation and the
// positional arguments it did not consume.
type Selection struct {
	Source source.Classification
	Args   []string
	// Discovered is what auto-discovery found when Source is
	// source.KindAutoDiscover, in priority order.
	Discovered []Discovery
}

// Select decides, once and before anything is loaded, where the solution
// comes from:
//
//  1. explicit (the -f value), when set;
//  2. args[0], when it is a URL, registry reference, or versioned name, or
//     (with ArgsAreNamesOrCatalog, catalog references enabled, and an explicit
//     catalog or version option) a bare name;
//  3. auto-discovery, when a solution file is found;
//  4. with ArgsAreNamesOrCatalog and catalog references enabled, a valid
//     catalog bare name args[0] (matching catalog.IsValidName).
//
// Malformed reference syntax in explicit or args[0] fails immediately with
// source.ErrInvalidRef rather than falling through to auto-discovery.
// A source taken from args[0] is removed from the returned Args. Otherwise
// the discovery error (wrapping ErrNoSolutionFound) is returned. opts scope
// the selected source (see source.WithCatalog, source.WithVersion). Select
// only stats candidate files and peeks at taskfiles; it never fetches.
//
// Load the selection with SelectionLoader so discovery is not repeated.
func (r *Router) Select(ctx context.Context, explicit string, args []string, policy ArgPolicy, opts ...source.Option) (Selection, error) {
	if explicit != "" {
		return selection(explicit, args, opts)
	}
	if len(args) > 0 {
		c, err := source.Classify(args[0])
		if err != nil {
			return Selection{}, err
		}
		if isUnambiguousClass(c) {
			return selection(args[0], args[1:], opts)
		}
		if policy == ArgsAreNamesOrCatalog && r.catalog != nil && len(opts) > 0 && c.Kind == source.KindBareName {
			scoped, err := selection(args[0], args[1:], opts)
			if err != nil {
				return Selection{}, err
			}
			if scoped.Source.Catalog != "" || scoped.Source.Version != "" {
				return scoped, nil
			}
		}
	}
	if r.discover == nil {
		return Selection{}, notEnabled(source.Classification{Kind: source.KindAutoDiscover})
	}

	found, err := r.discover.Discover(ctx)
	if err == nil {
		sel, err := selection("", args, opts)
		if err != nil {
			return Selection{}, err
		}
		sel.Discovered = found
		return sel, nil
	}
	if policy == ArgsAreNamesOrCatalog && errors.Is(err, ErrNoSolutionFound) &&
		r.catalog != nil && len(args) > 0 && isCatalogName(args[0]) {
		return selection(args[0], args[1:], opts)
	}
	return Selection{}, err
}

// DiscoveredPath returns the path of the auto-discovered solution file, for
// commands that need the file itself rather than the loaded solution. With
// DiscoveryRiskHigh, more than one match fails with ErrAmbiguousSolution.
// Returns "" when s was not auto-discovered.
func (s Selection) DiscoveredPath(risk DiscoveryRisk) (string, error) {
	if len(s.Discovered) == 0 {
		return "", nil
	}
	if risk == DiscoveryRiskHigh && len(s.Discovered) > 1 {
		return "", ambiguous(s.Discovered)
	}
	return s.Discovered[0].Path, nil
}

// SelectionLoader returns a Loader that loads an auto-discovered selection
// from s.Discovered instead of searching again. Other classifications, such
// as sub-solutions, go to r.
func (r *Router) SelectionLoader(s Selection) Loader {
	return selectionLoader{router: r, discovered: s.Discovered}
}

type selectionLoader struct {
	router     *Router
	discovered []Discovery
}

func (l selectionLoader) Load(ctx context.Context, c source.Classification) (*MaterializedSolution, error) {
	if c.Kind == source.KindAutoDiscover && len(l.discovered) > 0 {
		return l.router.discover.loadFound(ctx, l.discovered)
	}
	return l.router.Load(ctx, c)
}

func selection(path string, args []string, opts []source.Option) (Selection, error) {
	c, err := source.Classify(path, opts...)
	if err != nil {
		return Selection{}, err
	}
	return Selection{Source: c, Args: args}, nil
}

// nameVersionSeparator separates a catalog name from its version
// ("app@1.2.3").
const nameVersionSeparator = "@"

// isUnambiguousClass reports whether c can only be a solution source: a URL, a
// registry reference, or a versioned name.
func isUnambiguousClass(c source.Classification) bool {
	switch c.Kind {
	case source.KindURL, source.KindRemoteRef:
		return true
	case source.KindBareName:
		return strings.Contains(c.Ref, nameVersionSeparator)
	case source.KindUnknown, source.KindAutoDiscover, source.KindStdin, source.KindLocalFile, source.KindCatalogQualified:
	}
	return false
}

// isCatalogName reports whether arg classifies as an unversioned bare name
// that satisfies catalog artifact naming rules.
func isCatalogName(arg string) bool {
	c, err := source.Classify(arg)
	return err == nil && c.Kind == source.KindBareName && catalog.IsValidName(c.Ref)
}
