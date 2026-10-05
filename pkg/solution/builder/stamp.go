// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"errors"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// ErrNameMismatch is returned when the resolved artifact name differs from
// metadata.name and StampOptions.AllowRename is not set.
var ErrNameMismatch = errors.New("artifact name does not match metadata.name")

// buildDirtyValue is the AnnotationBuildDirty value for a dirty working tree.
const buildDirtyValue = "true"

// StampOptions controls StampSolution.
type StampOptions struct {
	// AllowRename permits packaging under a name other than metadata.name
	// (today: --force).
	AllowRename bool
}

// StampResult reports which metadata fields StampSolution changed.
type StampResult struct {
	// NameChanged is true when metadata.name was replaced.
	NameChanged bool
	// VersionChanged is true when metadata.version was replaced.
	VersionChanged bool
}

// IdentityChanged reports whether name or version changed, i.e. whether a
// stored raw document would no longer match the artifact identity. Build
// annotations are not counted: they reach the artifact as OCI annotations.
func (r StampResult) IdentityChanged() bool {
	return r.NameChanged || r.VersionChanged
}

// StampSolution writes the resolved identity and git metadata into sol so the
// stored artifact carries the authoritative values. It checks the name
// before changing anything, so on error sol is untouched.
//
// The build annotations (catalog.AnnotationBuildCommit,
// catalog.AnnotationBuildDirty) are owned by scafctl: any value already in
// the document is removed, then they are set from git when recorded.
func StampSolution(sol *solution.Solution, id *Identity, git GitMetadata, opts StampOptions) (StampResult, error) {
	meta := &sol.Metadata
	name, version := id.Ref.Name, id.Ref.Version

	var result StampResult
	if meta.Name != name {
		if !opts.AllowRename {
			return StampResult{}, fmt.Errorf("%w: %q vs %q", ErrNameMismatch, name, meta.Name)
		}
		meta.Name = name
		result.NameChanged = true
	}
	// Compare the full string, not semver.Equal: Equal ignores build metadata,
	// and the stored document must carry the exact version used for the tag.
	if meta.Version == nil || meta.Version.String() != version.String() {
		meta.Version = version
		result.VersionChanged = true
	}

	delete(meta.Annotations, catalog.AnnotationBuildCommit)
	delete(meta.Annotations, catalog.AnnotationBuildDirty)
	if git.Recorded() {
		if meta.Annotations == nil {
			meta.Annotations = make(map[string]string)
		}
		meta.Annotations[catalog.AnnotationBuildCommit] = git.Commit
		if git.Dirty {
			meta.Annotations[catalog.AnnotationBuildDirty] = buildDirtyValue
		}
	}
	return result, nil
}
