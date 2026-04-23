// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import "sort"

// IndexDiffChange represents the type of change for an artifact in the index.
type IndexDiffChange string

const (
	// IndexDiffAdded indicates a new artifact not present in the current index.
	IndexDiffAdded IndexDiffChange = "added"
	// IndexDiffRemoved indicates an artifact present in the current index but
	// not in the new set.
	IndexDiffRemoved IndexDiffChange = "removed"
	// IndexDiffVersionChanged indicates an artifact whose latest version changed.
	IndexDiffVersionChanged IndexDiffChange = "version-changed"
	// IndexDiffUnchanged indicates an artifact with no changes.
	IndexDiffUnchanged IndexDiffChange = "unchanged"
)

// IndexDiffEntry represents a single artifact in the index diff.
type IndexDiffEntry struct {
	Change        IndexDiffChange `json:"change"        yaml:"change"        doc:"Type of change" example:"added"`
	Kind          ArtifactKind    `json:"kind"           yaml:"kind"           doc:"Artifact kind" example:"solution"`
	Name          string          `json:"name"           yaml:"name"           doc:"Artifact name" example:"hello-world"`
	LatestVersion string          `json:"latestVersion"  yaml:"latestVersion"  doc:"New latest version" example:"1.2.0"`
	PrevVersion   string          `json:"prevVersion"    yaml:"prevVersion"    doc:"Previous latest version (empty if added)" example:"1.1.0"`
}

// IndexDiffSummary summarizes the diff result.
type IndexDiffSummary struct {
	Entries []IndexDiffEntry `json:"entries"  yaml:"entries"  doc:"All diff entries"`
	Added   int              `json:"added"    yaml:"added"    doc:"Number of added artifacts"`
	Removed int              `json:"removed"  yaml:"removed"  doc:"Number of removed artifacts"`
	Changed int              `json:"changed"  yaml:"changed"  doc:"Number of version-changed artifacts"`
	Total   int              `json:"total"    yaml:"total"    doc:"Total artifacts in new index"`
}

// DiffIndex compares a new set of discovered artifacts against the currently
// published index and returns a summary of changes. Both slices may be nil/empty.
func DiffIndex(current, next []DiscoveredArtifact) IndexDiffSummary {
	type artifactKey struct {
		kind ArtifactKind
		name string
	}

	currentMap := make(map[artifactKey]DiscoveredArtifact, len(current))
	for _, a := range current {
		currentMap[artifactKey{kind: a.Kind, name: a.Name}] = a
	}

	seen := make(map[artifactKey]bool, len(next))
	var entries []IndexDiffEntry
	var added, removed, changed int

	// Process new artifacts: added or changed.
	for _, a := range next {
		key := artifactKey{kind: a.Kind, name: a.Name}
		seen[key] = true

		if prev, ok := currentMap[key]; ok {
			if a.LatestVersion != prev.LatestVersion {
				changed++
				entries = append(entries, IndexDiffEntry{
					Change:        IndexDiffVersionChanged,
					Kind:          a.Kind,
					Name:          a.Name,
					LatestVersion: a.LatestVersion,
					PrevVersion:   prev.LatestVersion,
				})
			} else {
				entries = append(entries, IndexDiffEntry{
					Change:        IndexDiffUnchanged,
					Kind:          a.Kind,
					Name:          a.Name,
					LatestVersion: a.LatestVersion,
				})
			}
		} else {
			added++
			entries = append(entries, IndexDiffEntry{
				Change:        IndexDiffAdded,
				Kind:          a.Kind,
				Name:          a.Name,
				LatestVersion: a.LatestVersion,
			})
		}
	}

	// Process removed artifacts: in current but not in next.
	for _, a := range current {
		key := artifactKey{kind: a.Kind, name: a.Name}
		if !seen[key] {
			removed++
			entries = append(entries, IndexDiffEntry{
				Change:        IndexDiffRemoved,
				Kind:          a.Kind,
				Name:          a.Name,
				LatestVersion: a.LatestVersion,
			})
		}
	}

	// Sort: changes first (added > version-changed > removed > unchanged),
	// then by kind, then by name.
	sort.Slice(entries, func(i, j int) bool {
		oi, oj := changeOrder(entries[i].Change), changeOrder(entries[j].Change)
		if oi != oj {
			return oi < oj
		}
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind < entries[j].Kind
		}
		return entries[i].Name < entries[j].Name
	})

	return IndexDiffSummary{
		Entries: entries,
		Added:   added,
		Removed: removed,
		Changed: changed,
		Total:   len(next),
	}
}

// changeOrder returns a sort priority for each change type.
func changeOrder(c IndexDiffChange) int {
	switch c {
	case IndexDiffAdded:
		return 0
	case IndexDiffVersionChanged:
		return 1
	case IndexDiffRemoved:
		return 2
	case IndexDiffUnchanged:
		return 3
	default:
		return 4
	}
}
