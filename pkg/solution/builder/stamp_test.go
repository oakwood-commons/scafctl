// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"maps"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

func TestStampSolution(t *testing.T) {
	t.Parallel()

	const commit = "abc1234"
	staleBuild := map[string]string{
		catalog.AnnotationBuildCommit: "forged",
		catalog.AnnotationBuildDirty:  "true",
		"team":                        "platform",
	}

	tests := []struct {
		name            string
		metaName        string
		metaVersion     string
		annotations     map[string]string
		idName          string
		idVersion       string
		git             GitMetadata
		opts            StampOptions
		want            StampResult
		wantAnnotations map[string]string
		wantErr         error
	}{
		{
			name: "identity unchanged", metaName: "demo", metaVersion: "1.0.0",
			idName: "demo", idVersion: "1.0.0",
		},
		{
			name: "version stamped", metaName: "demo", metaVersion: "1.0.0",
			idName: "demo", idVersion: "2.0.0",
			want: StampResult{VersionChanged: true},
		},
		{
			name: "build metadata stamped", metaName: "demo", metaVersion: "1.0.0+build1",
			idName: "demo", idVersion: "1.0.0+build2",
			want: StampResult{VersionChanged: true},
		},
		{
			name: "build metadata unchanged", metaName: "demo", metaVersion: "1.0.0+build1",
			idName: "demo", idVersion: "1.0.0+build1",
		},
		{
			name: "missing version stamped", metaName: "demo",
			idName: "demo", idVersion: "0.1.0",
			want: StampResult{VersionChanged: true},
		},
		{
			name: "rename allowed", metaName: "demo", metaVersion: "1.0.0",
			idName: "other", idVersion: "1.0.0", opts: StampOptions{AllowRename: true},
			want: StampResult{NameChanged: true},
		},
		{
			name: "rename refused", metaName: "demo", metaVersion: "1.0.0", annotations: staleBuild,
			idName: "other", idVersion: "2.0.0", git: GitMetadata{Commit: commit},
			wantErr: ErrNameMismatch,
		},
		{
			name: "clean git recorded", metaName: "demo", metaVersion: "1.0.0",
			idName: "demo", idVersion: "1.0.0", git: GitMetadata{Commit: commit},
			wantAnnotations: map[string]string{catalog.AnnotationBuildCommit: commit},
		},
		{
			name: "dirty git recorded", metaName: "demo", metaVersion: "1.0.0",
			idName: "demo", idVersion: "1.0.0", git: GitMetadata{Commit: commit, Dirty: true},
			wantAnnotations: map[string]string{catalog.AnnotationBuildCommit: commit, catalog.AnnotationBuildDirty: buildDirtyValue},
		},
		{
			name: "clean git replaces stale build annotations", metaName: "demo", metaVersion: "1.0.0", annotations: staleBuild,
			idName: "demo", idVersion: "1.0.0", git: GitMetadata{Commit: commit},
			wantAnnotations: map[string]string{catalog.AnnotationBuildCommit: commit, "team": "platform"},
		},
		{
			name: "no git strips stale build annotations", metaName: "demo", metaVersion: "1.0.0", annotations: staleBuild,
			idName: "demo", idVersion: "1.0.0",
			wantAnnotations: map[string]string{"team": "platform"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sol := &solution.Solution{Metadata: solution.Metadata{Name: tt.metaName, Annotations: maps.Clone(tt.annotations)}}
			if tt.metaVersion != "" {
				sol.Metadata.Version = semver.MustParse(tt.metaVersion)
			}
			before := sol.Metadata
			before.Annotations = maps.Clone(sol.Metadata.Annotations)
			id := &Identity{Ref: catalog.Reference{Name: tt.idName, Version: semver.MustParse(tt.idVersion)}}

			got, err := StampSolution(sol, id, tt.git, tt.opts)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, before, sol.Metadata, "metadata must be untouched on error")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.want.NameChanged || tt.want.VersionChanged, got.IdentityChanged())
			assert.Equal(t, tt.idName, sol.Metadata.Name)
			assert.Equal(t, tt.idVersion, sol.Metadata.Version.String())
			assert.Equal(t, tt.wantAnnotations, sol.Metadata.Annotations)
		})
	}
}
