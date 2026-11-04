// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/oci"
)

// sharedTestTimeout bounds waits that would otherwise hang if a lock were
// held across a blocked copy.
const sharedTestTimeout = 5 * time.Second

func openLocal(t *testing.T, dir string) *LocalCatalog {
	t.Helper()
	cat, err := NewLocalCatalogAt(dir, logr.Discard())
	require.NoError(t, err)
	return cat
}

func sharedSolutionRef(name string) Reference {
	return Reference{Kind: ArtifactKindSolution, Name: name, Version: semver.MustParse("1.0.0")}
}

// diskTags reads the tags straight from disk through an independent store, so
// it reports what any other process would see.
func diskTags(t *testing.T, dir string) []string {
	t.Helper()
	store, err := oci.New(dir)
	require.NoError(t, err)
	var tags []string
	require.NoError(t, store.Tags(t.Context(), "", func(page []string) error {
		tags = append(tags, page...)
		return nil
	}))
	return tags
}

func TestNewLocalCatalogAt_SharesStorePerDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "catalog")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(dir, link))
	other := filepath.Join(root, "other")

	base := openLocal(t, dir)

	tests := []struct {
		name  string
		path  string
		share bool
	}{
		{name: "same path", path: dir, share: true},
		{name: "unclean path", path: filepath.Join(dir, "..", "catalog") + string(filepath.Separator), share: true},
		{name: "symlink", path: link, share: true},
		{name: "different directory", path: other, share: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cat := openLocal(t, tt.path)
			assert.Equal(t, tt.path, cat.Path(), "Path keeps the caller's spelling")
			if tt.share {
				assert.Same(t, base.localStore, cat.localStore)
			} else {
				assert.NotSame(t, base.localStore, cat.localStore)
			}
		})
	}
}

func TestNewLocalCatalogAt_RelativePathSharesStore(t *testing.T) {
	root := t.TempDir()
	abs := openLocal(t, filepath.Join(root, "catalog"))

	t.Chdir(root)
	rel := openLocal(t, "catalog")

	assert.Same(t, abs.localStore, rel.localStore)
}

func TestLocalCatalog_SharedStore_WritesVisibleAcrossInstances(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	writer := openLocal(t, dir)
	reader := openLocal(t, dir)

	ref := sharedSolutionRef("shared")
	_, err := writer.Store(ctx, ref, []byte("name: shared"), nil, nil, false)
	require.NoError(t, err)

	content, _, err := reader.Fetch(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, "name: shared", string(content))
}

func TestLocalCatalog_SharedStore_ConcurrentWritersKeepAllTags(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	const writers = 8

	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := range writers {
		wg.Go(func() {
			cat, err := NewLocalCatalogAt(dir, logr.Discard())
			if err != nil {
				errs <- err
				return
			}
			ref := sharedSolutionRef(fmt.Sprintf("app-%d", i))
			_, err = cat.Store(ctx, ref, fmt.Appendf(nil, "name: app-%d", i), nil, nil, false)
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	assert.Len(t, diskTags(t, dir), writers, "no writer may overwrite another's tag")
}

func TestNewLocalCatalogAt_ReloadsOnlyWhenIndexChanged(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	first := openLocal(t, dir)
	_, err := first.Store(ctx, sharedSolutionRef("mine"), []byte("name: mine"), nil, nil, false)
	require.NoError(t, err)

	// Opening again re-reads our own last write once, then settles.
	openLocal(t, dir)
	loaded := first.store
	openLocal(t, dir)
	assert.Same(t, loaded, first.store, "an unchanged index.json must not trigger a reload")

	// Another process tags the same manifest under a new name.
	external, err := oci.New(dir)
	require.NoError(t, err)
	desc, err := external.Resolve(ctx, first.tagForRef(sharedSolutionRef("mine")))
	require.NoError(t, err)
	external.AutoSaveIndex = true
	require.NoError(t, external.Tag(ctx, desc, "solution/theirs:1.0.0"))

	second := openLocal(t, dir)
	assert.Same(t, first.localStore, second.localStore)
	assert.NotSame(t, loaded, first.store, "a changed index.json reloads the shared store")

	_, err = first.store.Resolve(ctx, "solution/theirs:1.0.0")
	require.NoError(t, err, "existing instances see the external tag after the reload")
}

func TestLocalCatalog_SharedStore_PruneDoesNotResurrectManifests(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	a := openLocal(t, dir)
	b := openLocal(t, dir)

	oldRef := sharedSolutionRef("old")
	_, err := a.Store(ctx, oldRef, []byte("name: old"), nil, nil, false)
	require.NoError(t, err)
	require.NoError(t, a.Delete(ctx, oldRef), "deleting leaves an orphaned manifest")

	result, err := b.Prune(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, result.RemovedManifests)

	newRef := sharedSolutionRef("new")
	_, err = a.Store(ctx, newRef, []byte("name: new"), nil, nil, false)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ocispec.ImageIndexFile))
	require.NoError(t, err)
	var index ocispec.Index
	require.NoError(t, json.Unmarshal(data, &index))
	require.NotEmpty(t, index.Manifests)
	for _, m := range index.Manifests {
		blob := filepath.Join(dir, ocispec.ImageBlobsDir, m.Digest.Algorithm().String(), m.Digest.Encoded())
		assert.FileExists(t, blob, "index.json lists manifest %s with no blob on disk", m.Digest)
	}

	again, err := b.Prune(ctx)
	require.NoError(t, err)
	assert.Zero(t, again.RemovedManifests, "a pruned manifest must not reappear")

	_, _, err = b.Fetch(ctx, newRef)
	require.NoError(t, err)
}

func TestNewLocalCatalogAt_LoadErrors(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, dir string)
		wantErr string
	}{
		{
			name: "corrupt index",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(dir, ocispec.ImageIndexFile), []byte("{not json"), 0o600))
			},
			wantErr: "failed to create OCI store",
		},
		{
			name: "unreadable index",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.Mkdir(filepath.Join(dir, ocispec.ImageIndexFile), 0o755))
			},
			wantErr: "reading catalog index",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(t, dir)
			_, err := NewLocalCatalogAt(dir, logr.Discard())
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestNewLocalCatalogAt_ReloadErrorOnCachedStore(t *testing.T) {
	dir := t.TempDir()
	openLocal(t, dir)

	require.NoError(t, os.WriteFile(filepath.Join(dir, ocispec.ImageIndexFile), []byte("{not json"), 0o600))
	_, err := NewLocalCatalogAt(dir, logr.Discard())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create OCI store")
}

func TestCanonicalCatalogDir_Missing(t *testing.T) {
	_, err := canonicalCatalogDir(filepath.Join(t.TempDir(), "missing"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving catalog directory")
}

// blockingSource holds every Fetch until release is closed, so a test can
// observe what the catalog allows while a copy is in flight.
type blockingSource struct {
	oras.ReadOnlyTarget
	once    sync.Once
	started chan struct{}
	release chan struct{}
}

func (s *blockingSource) Fetch(ctx context.Context, desc ocispec.Descriptor) (io.ReadCloser, error) {
	s.once.Do(func() { close(s.started) })
	select {
	case <-s.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return s.ReadOnlyTarget.Fetch(ctx, desc)
}

// pruneBlockedWindow is how long a test waits to confirm Prune is still
// blocked behind an in-flight ingest.
const pruneBlockedWindow = 100 * time.Millisecond

func TestLocalCatalog_IngestVerbatim_BlocksPruneDuringCopy(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	ingester := openLocal(t, dir)
	pruner := openLocal(t, dir)

	src := &blockingSource{
		ReadOnlyTarget: memSource(t, remoteSolutionBuilder(solutionRef("demo", "1.0.0")), "1.0.0"),
		started:        make(chan struct{}),
		release:        make(chan struct{}),
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(src.release) }) }
	t.Cleanup(release)

	localRef := localRefOf("ghcr.io/myorg/scafctl")
	ingestDone := make(chan error, 1)
	go func() {
		_, err := ingester.IngestVerbatim(ctx, src, "1.0.0", localRef, oras.DefaultCopyOptions)
		ingestDone <- err
	}()

	select {
	case <-src.started:
	case <-time.After(sharedTestTimeout):
		t.Fatal("ingest never started copying")
	}

	pruneDone := make(chan error, 1)
	go func() {
		_, err := pruner.Prune(ctx)
		pruneDone <- err
	}()
	select {
	case err := <-pruneDone:
		release()
		t.Fatalf("Prune ran while IngestVerbatim was still copying (err=%v)", err)
	case <-time.After(pruneBlockedWindow):
	}

	release()
	require.NoError(t, <-ingestDone)
	select {
	case err := <-pruneDone:
		require.NoError(t, err)
	case <-time.After(sharedTestTimeout):
		t.Fatal("Prune never finished after the ingest completed")
	}

	_, _, err := pruner.Fetch(ctx, localRef)
	require.NoError(t, err, "the ingested artifact survives a Prune that raced its copy")
}
