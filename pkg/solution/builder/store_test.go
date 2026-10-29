// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testPulledOrigin = "ghcr.io/example/solutions"

// seedPulledSolution stores a remote-origin copy of name@version, the shape a
// `catalog pull` leaves behind, so builds of the same name@version must not
// collide with it.
func seedPulledSolution(t *testing.T, cat *catalog.LocalCatalog, name, version string, content []byte) catalog.Reference {
	t.Helper()
	ref := catalog.Reference{
		Kind:    catalog.ArtifactKindSolution,
		Name:    name,
		Version: semver.MustParse(version),
		Remote:  testPulledOrigin,
	}
	_, err := cat.Store(t.Context(), ref, content, nil,
		map[string]string{catalog.AnnotationSourceCanonical: testPulledOrigin}, false)
	require.NoError(t, err)
	return ref
}

// dedupBuildResult builds a real v2 (deduplicated) bundle with one small and
// one large file so the stored layer layout exercises every positional slot.
func dedupBuildResult(t *testing.T) *BuildResult {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "small.txt"), []byte("small"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "large.bin"),
		bytes.Repeat([]byte("x"), int(bundler.DefaultDedupeThreshold)+1), 0o600))

	files, err := bundler.ReadFileSnapshot(root, []bundler.FileEntry{
		{RelPath: "small.txt"},
		{RelPath: "large.bin"},
	})
	require.NoError(t, err)
	dr, err := bundler.CreateDeduplicatedBundle(files, nil)
	require.NoError(t, err)
	return &BuildResult{Dedup: dr}
}

// storeBuilt stages and publishes a built solution artifact at version 1.0.0,
// the way the Packager's stage and publish stages do.
func storeBuilt(t *testing.T, cat *catalog.LocalCatalog, name string, content []byte, br *BuildResult, annotations map[string]string, force bool) (catalog.ArtifactInfo, error) {
	t.Helper()
	staged, err := stageArtifact(t.Context(), solutionArtifact(BuiltSolutionRef(name, semver.MustParse("1.0.0")), content, br, annotations))
	require.NoError(t, err)
	return publishArtifact(t.Context(), cat, staged, len(content), force)
}

func TestPublishArtifact_PulledCopySameVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		br   func(t *testing.T) *BuildResult
	}{
		{name: "no bundle", br: func(*testing.T) *BuildResult { return nil }},
		{name: "tar bundle", br: func(*testing.T) *BuildResult { return &BuildResult{TarData: []byte("tar")} }},
		{name: "dedup bundle", br: dedupBuildResult},
		{name: "with lock layer", br: func(*testing.T) *BuildResult {
			return &BuildResult{TarData: []byte("tar"), LockData: []byte("lock")}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			cat, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
			require.NoError(t, err)

			pulled := seedPulledSolution(t, cat, "foo", "1.0.0", []byte("pulled"))

			info, err := storeBuilt(t, cat, "foo", []byte("built"), tt.br(t), nil, false)
			require.NoError(t, err, "a pulled copy of the same version must not block a build")

			ref := info.Reference
			assert.True(t, ref.Built, "stored artifact is built")
			assert.Empty(t, ref.Remote)
			assert.Equal(t, "solution/foo:1.0.0", info.Tag)
			assert.Equal(t, "built", info.Annotations[catalog.AnnotationOrigin])

			// Verify fetches through the returned reference: it must land on the
			// built copy, never the pulled one.
			content, _, fetched, err := cat.FetchWithBundle(ctx, ref)
			require.NoError(t, err)
			assert.Equal(t, "built", string(content))
			assert.Equal(t, info.Digest, fetched.Digest)

			// The pulled copy is untouched.
			pulledContent, _, err := cat.Fetch(ctx, pulled)
			require.NoError(t, err)
			assert.Equal(t, "pulled", string(pulledContent))
		})
	}
}

func TestPublishArtifact_ExistsOnlyAgainstBuiltCopy(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cat, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)

	pulled := seedPulledSolution(t, cat, "foo", "1.0.0", []byte("pulled"))
	_, err = storeBuilt(t, cat, "foo", []byte("v1"), nil, nil, false)
	require.NoError(t, err)

	_, err = storeBuilt(t, cat, "foo", []byte("v2"), nil, nil, false)
	require.Error(t, err)
	assert.True(t, catalog.IsExists(err), "second build without --force collides with the built copy: %v", err)

	info, err := storeBuilt(t, cat, "foo", []byte("v3"), nil, nil, true)
	require.NoError(t, err)

	content, _, err := cat.Fetch(ctx, info.Reference)
	require.NoError(t, err)
	assert.Equal(t, "v3", string(content))

	pulledContent, _, err := cat.Fetch(ctx, pulled)
	require.NoError(t, err)
	assert.Equal(t, "pulled", string(pulledContent), "--force must only overwrite the built copy")
}

func TestPublishArtifact_DedupLayoutReassembles(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cat, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)

	br := dedupBuildResult(t)
	br.LockData = []byte("lock")
	info, err := storeBuilt(t, cat, "dd", []byte("solution"), br, nil, false)
	require.NoError(t, err)

	_, bundleData, _, err := cat.FetchWithBundle(ctx, info.Reference)
	require.NoError(t, err)
	require.NotEmpty(t, bundleData, "dedup layers must reassemble into a bundle tar")

	_, layers, _, err := cat.FetchWithLayer(ctx, info.Reference, catalog.MediaTypeSolutionLock)
	require.NoError(t, err)
	assert.Equal(t, "lock", string(layers[catalog.MediaTypeSolutionLock]))
}

func TestPublishArtifact_RecordsMetadataAnnotations(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cat, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)

	sol := &solution.Solution{Metadata: solution.Metadata{
		Source:      "https://example.com/repo",
		DisplayName: "Meta",
		Description: "desc",
		Annotations: map[string]string{
			"custom.key":                   "v",
			catalog.AnnotationArtifactName: "spoofed",
		},
	}}
	stored, err := storeBuilt(t, cat, "meta", []byte("solution"), nil, solutionAnnotations(sol, "fallback.yaml"), false)
	require.NoError(t, err)

	info, err := cat.Resolve(ctx, stored.Reference)
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/repo", info.Annotations[catalog.AnnotationSource])
	assert.Equal(t, "Meta", info.Annotations[catalog.AnnotationDisplayName])
	assert.Equal(t, "desc", info.Annotations[catalog.AnnotationDescription])
	assert.Equal(t, "v", info.Annotations["custom.key"])
	assert.Equal(t, "meta", info.Annotations[catalog.AnnotationArtifactName], "identity is stamped from the reference")
	assert.Equal(t, int64(len("solution")), stored.Size, "size reports the solution content")
}

func TestResolveBuiltSolution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		seedPulled bool
		seedBuilt  bool
		wantFound  bool
	}{
		{name: "nothing stored"},
		{name: "only pulled copy", seedPulled: true},
		{name: "only built copy", seedBuilt: true, wantFound: true},
		{name: "built and pulled copies", seedPulled: true, seedBuilt: true, wantFound: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			cat, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
			require.NoError(t, err)

			if tt.seedPulled {
				seedPulledSolution(t, cat, "foo", "1.0.0", []byte("pulled"))
			}
			var builtDigest string
			if tt.seedBuilt {
				stored, err := storeBuilt(t, cat, "foo", []byte("built"), nil, nil, false)
				require.NoError(t, err)
				builtDigest = stored.Digest
			}

			info, err := ResolveBuiltSolution(ctx, cat, "foo", semver.MustParse("1.0.0"))
			if !tt.wantFound {
				require.Error(t, err)
				assert.True(t, catalog.IsNotFound(err), "got %v", err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, builtDigest, info.Digest)
			assert.True(t, info.Reference.Built)
		})
	}
}

func TestResolveBuiltSolution_NilVersion(t *testing.T) {
	t.Parallel()
	cat, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)

	_, err = ResolveBuiltSolution(t.Context(), cat, "foo", nil)
	require.Error(t, err)
}

func TestStagedArtifact_Read(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		br         func(t *testing.T) *BuildResult
		wantBundle bool
	}{
		{name: "document only", br: func(*testing.T) *BuildResult { return nil }},
		{name: "bundled without files", br: func(*testing.T) *BuildResult { return &BuildResult{LockData: []byte("{}")} }},
		{name: "dedup bundle", br: dedupBuildResult, wantBundle: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ref := BuiltSolutionRef("foo", semver.MustParse("1.0.0"))
			staged, err := stageArtifact(t.Context(), solutionArtifact(ref, []byte("doc"), tt.br(t), nil))
			require.NoError(t, err)

			doc, bundle, err := staged.read(t.Context())
			require.NoError(t, err)
			assert.Equal(t, "doc", string(doc))
			assert.Equal(t, tt.wantBundle, len(bundle) > 0)
		})
	}
}

func TestSolutionAnnotations_SourceFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "metadata source wins", source: "https://example.com/repo", want: "https://example.com/repo"},
		{name: "fallback when unset", want: "solution.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sol := &solution.Solution{Metadata: solution.Metadata{Source: tt.source}}
			got := solutionAnnotations(sol, "solution.yaml")
			assert.Equal(t, tt.want, got[catalog.AnnotationSource])
		})
	}
}

func TestWriteBuildCacheEntry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		dir  func(t *testing.T) string
		want bool
	}{
		{name: "writes entry", dir: func(t *testing.T) string { return t.TempDir() }, want: true},
		{name: "unwritable dir", dir: func(t *testing.T) string {
			file := filepath.Join(t.TempDir(), "file")
			require.NoError(t, os.WriteFile(file, nil, 0o600))
			return file
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := tt.dir(t)
			build := &BuildResult{BuildFingerprint: "fp", InputFileCount: 2}
			info := catalog.ArtifactInfo{Reference: BuiltSolutionRef("foo", semver.MustParse("1.2.3")), Digest: "sha256:abc"}

			assert.Equal(t, tt.want, writeBuildCacheEntry(t.Context(), dir, build, info))
			if !tt.want {
				return
			}
			entry, hit := bundler.CheckBuildCache(dir, "fp")
			require.True(t, hit)
			assert.Equal(t, "foo", entry.ArtifactName)
			assert.Equal(t, "1.2.3", entry.ArtifactVersion)
			assert.Equal(t, "sha256:abc", entry.ArtifactDigest)
			assert.Equal(t, 2, entry.InputFiles)
		})
	}
}
