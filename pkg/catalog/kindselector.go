// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"

	"github.com/go-logr/logr"
)

// KindSelectorPlugin is a user-facing kind *selector* (not a stored artifact
// kind) that expands to every plugin artifact kind -- providers and auth
// handlers. Plugins are the artifacts that get fetched from a catalog and run
// as separate processes; users think in terms of "plugins" rather than the
// internal provider/auth-handler split, so `catalog list --kind plugin` and the
// MCP catalog_list_plugins tool accept this selector and list both underlying
// kinds. It is deliberately NOT a valid ArtifactKind (IsValid returns false)
// because no artifact is ever stored under the type "plugin".
const KindSelectorPlugin = "plugin"

// PluginKinds returns the concrete artifact kinds that make up a "plugin":
// providers and auth handlers. Solutions are intentionally excluded.
func PluginKinds() []ArtifactKind {
	return []ArtifactKind{ArtifactKindProvider, ArtifactKindAuthHandler}
}

// ExpandKindSelector resolves a user-supplied --kind value into the concrete
// artifact kinds to list. It accepts:
//
//   - ""                -> nil, true  (no filter: list all kinds)
//   - "plugin"          -> [provider, auth-handler], true
//   - a concrete kind   -> [that kind], true  (solution/provider/auth-handler)
//
// A nil result with ok==true means "no kind filter" (all kinds), matching the
// existing List(ctx, "", name) semantics. ok==false indicates the selector is
// not a recognized kind or alias, and callers should surface a validation
// error.
func ExpandKindSelector(sel string) (kinds []ArtifactKind, ok bool) {
	switch sel {
	case "":
		return nil, true
	case KindSelectorPlugin:
		return PluginKinds(), true
	default:
		kind := ArtifactKind(sel)
		if !kind.IsValid() {
			return nil, false
		}
		return []ArtifactKind{kind}, true
	}
}

// ListAcrossKinds lists artifacts from a catalog across a set of concrete kinds
// and concatenates the results in the order the kinds are given. It is the
// multi-kind counterpart to Catalog.List: when kinds is empty it performs a
// single List(ctx, "", name) (all kinds); otherwise it calls List once per kind
// and appends the results.
//
// Error handling depends on how many kinds are requested:
//
//   - Single kind: the error from List is returned directly (strict), so
//     `catalog list --kind provider --name X` still surfaces a real failure.
//   - Multiple kinds (e.g. the "plugin" selector expanding to provider +
//     auth-handler): per-kind errors are tolerated and skipped. A plugin
//     generally exists under only ONE of providers/<name> or
//     auth-handlers/<name>, and a remote catalog returns an error for the
//     absent kind's repository; failing the whole listing on that would drop
//     the kind that DOES exist. This mirrors RemoteCatalog's own
//     listAcrossKinds name-search behavior. Errors are surfaced to the provided
//     logger at V(1) for diagnosis.
//
// When every kind in a multi-kind request fails, the last error is returned so
// the caller is not left with a silently empty result masking a systemic
// failure (e.g. the whole catalog being unreachable).
func ListAcrossKinds(ctx context.Context, cat Catalog, kinds []ArtifactKind, name string, logger logr.Logger) ([]ArtifactInfo, error) {
	if len(kinds) == 0 {
		return cat.List(ctx, "", name)
	}

	if len(kinds) == 1 {
		return cat.List(ctx, kinds[0], name)
	}

	var results []ArtifactInfo
	var lastErr error
	var okCount int
	for _, kind := range kinds {
		infos, err := cat.List(ctx, kind, name)
		if err != nil {
			// Tolerate a per-kind failure (typically the absent kind's
			// repository) but remember it in case every kind fails.
			logger.V(1).Info("listing kind failed, skipping",
				"kind", kind, "name", name, "error", err.Error())
			lastErr = err
			continue
		}
		okCount++
		results = append(results, infos...)
	}

	// Only surface an error when NO kind succeeded -- otherwise a partial
	// success (the common case for a name present under one kind) is returned.
	if okCount == 0 && lastErr != nil {
		return nil, lastErr
	}
	return results, nil
}
