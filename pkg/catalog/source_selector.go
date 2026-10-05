// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"fmt"
	"strings"
)

// SourceBuiltKeyword is the reserved --origin value selecting locally built artifacts.
const SourceBuiltKeyword = "built"

// ParseSourceSelector converts a user-supplied origin ("built" or a registry
// URL) into a SourceSelector. An empty value selects any origin. Registry URLs
// are normalized to the canonical form stored in AnnotationSourceCanonical;
// whether the origin actually exists is decided at lookup time.
func ParseSourceSelector(origin string) (SourceSelector, error) {
	origin = strings.TrimSpace(origin)
	switch origin {
	case "":
		return SourceSelector{Kind: SourceAny}, nil
	case SourceBuiltKeyword:
		return SourceSelector{Kind: SourceBuilt}, nil
	}

	// An empty registry would normalize to Remote "", which matches built
	// artifacts, so it must be rejected here.
	registry, repository := ParseCatalogURL(origin)
	if registry == "" {
		return SourceSelector{}, fmt.Errorf("origin %q must be %q or a registry URL (e.g. ghcr.io/org/plugins)", origin, SourceBuiltKeyword)
	}

	return SourceSelector{
		Kind:   SourceRemote,
		Remote: CanonicalRemoteCatalogID(registry, repository),
	}, nil
}
