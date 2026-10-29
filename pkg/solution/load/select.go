// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package load

import (
	"context"
	"errors"
	"strings"

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
	// ArgsAreNamesOrCatalog is ArgsAreNames, plus a bare args[0] ("app") is a
	// catalog name when no solution file is discovered.
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
//  2. args[0], when it is a URL, registry reference, or versioned name;
//  3. auto-discovery, when a solution file is found;
//  4. with ArgsAreNamesOrCatalog and catalog references enabled, a bare
//     name args[0].
//
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
	if len(args) > 0 && isUnambiguousSource(args[0]) {
		return selection(args[0], args[1:], opts)
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

// isUnambiguousSource reports whether arg can only be a solution source: a
// URL, a registry reference, or a versioned name.
func isUnambiguousSource(arg string) bool {
	c, err := source.Classify(arg)
	if err != nil {
		return false
	}
	switch c.Kind {
	case source.KindURL, source.KindRemoteRef:
		return true
	case source.KindBareName:
		return strings.Contains(c.Ref, nameVersionSeparator)
	case source.KindUnknown, source.KindAutoDiscover, source.KindStdin, source.KindLocalFile, source.KindCatalogQualified:
	}
	return false
}

// isCatalogName reports whether arg, on its own, classifies as a catalog name.
func isCatalogName(arg string) bool {
	c, err := source.Classify(arg)
	return err == nil && c.Kind == source.KindBareName
}
