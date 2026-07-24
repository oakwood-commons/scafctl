package catalog

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// FilterByVersionConstraint filters artifacts by a semver constraint (e.g.
// ">=1.2.0", "~1.4"). An empty constraint returns the input unchanged. Matching
// artifacts are returned sorted by version descending (newest first). Artifacts
// without a parsed version are excluded when a constraint is supplied.
//
// This is the shared implementation used by both the CLI (`catalog list
// --name X <constraint>`) and the MCP catalog_list_plugins tool so version
// filtering behaves identically across surfaces.
func FilterByVersionConstraint(artifacts []ArtifactInfo, constraint string) ([]ArtifactInfo, error) {
	if constraint == "" {
		return artifacts, nil
	}

	c, err := semver.NewConstraint(constraint)
	if err != nil {
		return nil, fmt.Errorf("invalid version constraint %q: %w", constraint, err)
	}

	result := make([]ArtifactInfo, 0, len(artifacts))
	for _, a := range artifacts {
		if a.Reference.Version == nil {
			continue
		}
		if c.Check(a.Reference.Version) {
			result = append(result, a)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		vi := result[i].Reference.Version
		vj := result[j].Reference.Version
		if vi == nil {
			return false
		}
		if vj == nil {
			return true
		}
		return vj.LessThan(vi) // descending
	})
	return result, nil
}

// DeduplicateArtifacts merges rows with the same name+tag+kind across catalogs.
// When duplicates exist, the row with richer metadata (digest, createdAt) is
// preferred and the catalog names are combined into a comma-separated string.
// Order of first appearance is preserved.
//
// Shared by the CLI list rendering and the MCP catalog_list_plugins tool so a
// plugin present in multiple catalogs is reported once with a combined catalog
// list rather than duplicated.
func DeduplicateArtifacts(artifacts []ArtifactInfo) []ArtifactInfo {
	type dedupKey struct {
		name string
		tag  string
		kind ArtifactKind
	}

	keyFor := func(a ArtifactInfo) dedupKey {
		tag := a.Tag
		if tag == "" && a.Reference.Version != nil {
			tag = a.Reference.Version.String()
		}
		return dedupKey{name: a.Reference.Name, tag: tag, kind: a.Reference.Kind}
	}

	catalogSet := func(csv string) map[string]struct{} {
		set := make(map[string]struct{})
		for _, name := range strings.Split(csv, ", ") {
			if name != "" {
				set[name] = struct{}{}
			}
		}
		return set
	}

	seen := make(map[dedupKey]int, len(artifacts)) // key -> index in result
	result := make([]ArtifactInfo, 0, len(artifacts))

	for _, a := range artifacts {
		k := keyFor(a)
		if idx, ok := seen[k]; ok {
			existing := &result[idx]

			names := catalogSet(existing.Catalog)
			if _, found := names[a.Catalog]; !found {
				existing.Catalog = existing.Catalog + ", " + a.Catalog
			}
			if existing.Digest == "" && a.Digest != "" {
				existing.Digest = a.Digest
			}
			if existing.CreatedAt.IsZero() && !a.CreatedAt.IsZero() {
				existing.CreatedAt = a.CreatedAt
			}
			// Merge annotations: keep existing values, fill gaps from the other.
			if len(a.Annotations) > 0 {
				if existing.Annotations == nil {
					existing.Annotations = make(map[string]string, len(a.Annotations))
				}
				for ak, av := range a.Annotations {
					if _, found := existing.Annotations[ak]; !found {
						existing.Annotations[ak] = av
					}
				}
			}
			continue
		}
		seen[k] = len(result)
		result = append(result, a)
	}
	return result
}
