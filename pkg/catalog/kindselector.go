package catalog

import "context"

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
// The first error from any per-kind List is returned (partial results are
// discarded) so callers get deterministic failure behavior identical to a
// single List call.
func ListAcrossKinds(ctx context.Context, cat Catalog, kinds []ArtifactKind, name string) ([]ArtifactInfo, error) {
	if len(kinds) == 0 {
		return cat.List(ctx, "", name)
	}

	var results []ArtifactInfo
	for _, kind := range kinds {
		infos, err := cat.List(ctx, kind, name)
		if err != nil {
			return nil, err
		}
		results = append(results, infos...)
	}
	return results, nil
}
