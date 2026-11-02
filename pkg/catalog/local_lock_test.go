// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/gofrs/flock"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/oci"
)

// Environment for TestLocalCatalog_WriterProcess when re-executed as a child.
const (
	writerProcessDirEnv    = "CATALOG_TEST_WRITER_DIR"
	writerProcessPrefixEnv = "CATALOG_TEST_WRITER_PREFIX"
	writerProcessCountEnv  = "CATALOG_TEST_WRITER_COUNT"
)

// newOtherProcessCatalog opens dir without the in-process store cache, so the
// catalog has its own in-memory index and file lock, as another process would.
func newOtherProcessCatalog(t *testing.T, dir string) *LocalCatalog {
	t.Helper()
	key, err := canonicalCatalogPath(dir)
	require.NoError(t, err)
	s, err := openLocalStore(key)
	require.NoError(t, err)
	return &LocalCatalog{localStore: s, path: dir, logger: logr.Discard()}
}

// requireTagsOnDisk asserts that a fresh open of dir resolves every ref.
func requireTagsOnDisk(t *testing.T, dir string, refs ...Reference) {
	t.Helper()
	ctx := context.Background()
	fresh, err := oci.New(dir)
	require.NoError(t, err)
	for _, ref := range refs {
		_, err := fresh.Resolve(ctx, buildLocalTag(writeRef(ref)))
		assert.NoError(t, err, ref.Name)
	}
}

func TestLocalCatalog_CrossProcess(t *testing.T) {
	ctx := context.Background()

	t.Run("a write keeps tags another process added", func(t *testing.T) {
		dir := t.TempDir()
		a := newOtherProcessCatalog(t, dir)
		b := newOtherProcessCatalog(t, dir)

		_, err := a.Store(ctx, solutionRef("from-a", "1.0.0"), []byte("a"), nil, nil, false)
		require.NoError(t, err)
		_, err = b.Store(ctx, solutionRef("from-b", "1.0.0"), []byte("b"), nil, nil, false)
		require.NoError(t, err)

		requireTagsOnDisk(t, dir, solutionRef("from-a", "1.0.0"), solutionRef("from-b", "1.0.0"))

		// b reloaded before writing, so it now also sees a's artifact.
		got, _, err := b.Fetch(ctx, solutionRef("from-a", "1.0.0"))
		require.NoError(t, err)
		assert.Equal(t, []byte("a"), got)
	})

	t.Run("a write keeps tags an external oras writer added", func(t *testing.T) {
		dir := t.TempDir()
		cat := newOtherProcessCatalog(t, dir)

		external, err := oci.New(dir)
		require.NoError(t, err)
		desc, err := oras.PackManifest(ctx, external, oras.PackManifestVersion1_1, "application/vnd.test.external", oras.PackManifestOptions{})
		require.NoError(t, err)
		require.NoError(t, external.Tag(ctx, desc, "external"))

		_, err = cat.Store(ctx, solutionRef("ours", "1.0.0"), []byte("ours"), nil, nil, false)
		require.NoError(t, err)

		fresh, err := oci.New(dir)
		require.NoError(t, err)
		var tags []string
		require.NoError(t, fresh.Tags(ctx, "", func(page []string) error {
			tags = append(tags, page...)
			return nil
		}))
		assert.Contains(t, tags, "external")
		assert.Contains(t, tags, buildLocalTag(writeRef(solutionRef("ours", "1.0.0"))))
	})

	t.Run("concurrent writers in two stores lose no tags", func(t *testing.T) {
		dir := t.TempDir()
		stores := []*LocalCatalog{newOtherProcessCatalog(t, dir), newOtherProcessCatalog(t, dir)}
		const perStore = 10

		var wg sync.WaitGroup
		errs := make(chan error, len(stores)*perStore)
		refs := make([]Reference, 0, perStore*len(stores))
		for s, cat := range stores {
			for i := range perStore {
				ref := solutionRef(fmt.Sprintf("s%d-n%d", s, i), "1.0.0")
				refs = append(refs, ref)
				wg.Go(func() {
					_, err := cat.Store(ctx, ref, []byte(ref.Name), nil, nil, false)
					errs <- err
				})
			}
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}

		requireTagsOnDisk(t, dir, refs...)
	})

	t.Run("opening never sees an index another process is writing", func(t *testing.T) {
		dir := t.TempDir()
		writer := newOtherProcessCatalog(t, dir)

		done := make(chan struct{})
		go func() {
			defer close(done)
			for i := range 20 {
				_, err := writer.Store(ctx, solutionRef(fmt.Sprintf("w%d", i), "1.0.0"), []byte(strconv.Itoa(i)), nil, nil, false)
				assert.NoError(t, err)
			}
		}()

		key, err := canonicalCatalogPath(dir)
		require.NoError(t, err)
		for {
			select {
			case <-done:
				return
			default:
			}
			_, err := openLocalStore(key)
			require.NoError(t, err)
		}
	})

	t.Run("prune is not undone by a later write", func(t *testing.T) {
		dir := t.TempDir()
		cat := newOtherProcessCatalog(t, dir)

		old, err := cat.Store(ctx, solutionRef("app", "1.0.0"), []byte("v1"), nil, nil, false)
		require.NoError(t, err)
		// Overwriting leaves the first manifest untagged.
		_, err = cat.Store(ctx, solutionRef("app", "1.0.0"), []byte("v2"), nil, nil, true)
		require.NoError(t, err)

		res, err := cat.Prune(ctx)
		require.NoError(t, err)
		require.Positive(t, res.RemovedManifests)

		_, err = cat.Store(ctx, solutionRef("other", "1.0.0"), []byte("other"), nil, nil, false)
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(dir, ocispec.ImageIndexFile))
		require.NoError(t, err)
		assert.NotContains(t, string(data), old.Digest)
	})

	// Prune edits index.json directly and then reloads. If that reload fails,
	// the write's release still records the edited index's digest, so the
	// in-memory store must be marked stale or it would be trusted and its
	// pruned tags written back.
	for _, tc := range []struct {
		name  string
		check func(t *testing.T, dir string, cat *LocalCatalog, gone Reference)
	}{
		{
			name: "a read after a failed reload does not trust the old store",
			check: func(t *testing.T, _ string, cat *LocalCatalog, gone Reference) {
				exists, err := cat.Exists(ctx, gone)
				require.NoError(t, err)
				assert.False(t, exists)
			},
		},
		{
			name: "a write after a failed reload does not restore pruned tags",
			check: func(t *testing.T, dir string, cat *LocalCatalog, gone Reference) {
				_, err := cat.Store(ctx, solutionRef("other", "1.0.0"), []byte("other"), nil, nil, false)
				require.NoError(t, err)
				data, err := os.ReadFile(filepath.Join(dir, ocispec.ImageIndexFile))
				require.NoError(t, err)
				assert.NotContains(t, string(data), buildLocalTag(writeRef(gone)))
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cat := newOtherProcessCatalog(t, dir)
			gone := solutionRef("gone", "1.0.0")
			_, err := cat.Store(ctx, gone, []byte("gone"), nil, nil, false)
			require.NoError(t, err)
			indexPath := filepath.Join(dir, ocispec.ImageIndexFile)

			unlock, err := cat.lockWrite(ctx)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(indexPath, []byte("not json"), 0o600))
			require.Error(t, cat.reload(ctx))
			require.True(t, cat.stale)
			require.NoError(t, os.WriteFile(indexPath, []byte(`{"schemaVersion":2,"manifests":[]}`), 0o600))
			unlock()

			tc.check(t, dir, cat, gone)
			assert.False(t, cat.stale)
		})
	}
}

func TestLocalCatalog_ReadsSeeOtherProcesses(t *testing.T) {
	ctx := context.Background()
	app := solutionRef("app", "1.0.0")

	t.Run("a read sees an artifact another process stored", func(t *testing.T) {
		dir := t.TempDir()
		reader := newOtherProcessCatalog(t, dir)
		writer := newOtherProcessCatalog(t, dir)

		exists, err := reader.Exists(ctx, app)
		require.NoError(t, err)
		require.False(t, exists)

		_, err = writer.Store(ctx, app, []byte("app"), nil, nil, false)
		require.NoError(t, err)

		got, _, err := reader.Fetch(ctx, app)
		require.NoError(t, err)
		assert.Equal(t, []byte("app"), got)
		listed, err := reader.List(ctx, "", "")
		require.NoError(t, err)
		assert.Len(t, listed, 1)
	})

	t.Run("a read stops seeing an artifact another process deleted", func(t *testing.T) {
		dir := t.TempDir()
		reader := newOtherProcessCatalog(t, dir)
		writer := newOtherProcessCatalog(t, dir)
		_, err := writer.Store(ctx, app, []byte("app"), nil, nil, false)
		require.NoError(t, err)
		exists, err := reader.Exists(ctx, app)
		require.NoError(t, err)
		require.True(t, exists)

		require.NoError(t, writer.Delete(ctx, app))

		exists, err = reader.Exists(ctx, app)
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("a read of an unchanged index still waits for a writer", func(t *testing.T) {
		dir := t.TempDir()
		cat := newOtherProcessCatalog(t, dir)
		_, err := cat.Store(ctx, app, []byte("app"), nil, nil, false)
		require.NoError(t, err)
		cat.lockTimeout = 100 * time.Millisecond

		holder := flock.New(filepath.Join(dir, catalogLockFile))
		require.NoError(t, holder.Lock())
		_, _, err = cat.Fetch(ctx, app)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		require.True(t, cat.mu.TryLock())
		cat.mu.Unlock()

		require.NoError(t, holder.Unlock())
		_, _, err = cat.Fetch(ctx, app)
		require.NoError(t, err)
	})

	t.Run("another process cannot overwrite and prune during a read", func(t *testing.T) {
		dir := t.TempDir()
		reader := newOtherProcessCatalog(t, dir)
		writer := newOtherProcessCatalog(t, dir)
		_, err := writer.Store(ctx, app, []byte("v1"), nil, nil, false)
		require.NoError(t, err)

		unlock, err := reader.lockRead(ctx)
		require.NoError(t, err)
		desc, err := reader.store.Resolve(ctx, buildLocalTag(writeRef(app)))
		require.NoError(t, err)

		writer.lockTimeout = 100 * time.Millisecond
		_, err = writer.Store(ctx, app, []byte("v2"), nil, nil, true)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		_, err = writer.Prune(ctx)
		assert.ErrorIs(t, err, context.DeadlineExceeded)

		// What the read resolved is still there to read.
		_, err = reader.fetchBlob(ctx, desc)
		require.NoError(t, err)
		unlock()

		_, err = writer.Store(ctx, app, []byte("v2"), nil, nil, true)
		require.NoError(t, err)
		res, err := writer.Prune(ctx)
		require.NoError(t, err)
		require.Positive(t, res.RemovedManifests)
		got, _, err := reader.Fetch(ctx, app)
		require.NoError(t, err)
		assert.Equal(t, []byte("v2"), got)
	})

	t.Run("concurrent readers share the file lock until the last releases", func(t *testing.T) {
		dir := t.TempDir()
		cat := newOtherProcessCatalog(t, dir)
		other := flock.New(filepath.Join(dir, catalogLockFile))

		first, err := cat.lockRead(ctx)
		require.NoError(t, err)
		second, err := cat.lockRead(ctx)
		require.NoError(t, err)

		first()
		locked, err := other.TryLock()
		require.NoError(t, err)
		assert.False(t, locked, "the second reader still holds the shared lock")

		second()
		locked, err = other.TryLock()
		require.NoError(t, err)
		assert.True(t, locked)
		require.NoError(t, other.Unlock())
	})

	t.Run("a read of a changed index waits for the writer", func(t *testing.T) {
		dir := t.TempDir()
		reader := newOtherProcessCatalog(t, dir)
		writer := newOtherProcessCatalog(t, dir)
		_, err := writer.Store(ctx, app, []byte("app"), nil, nil, false)
		require.NoError(t, err)
		reader.lockTimeout = 100 * time.Millisecond

		holder := flock.New(filepath.Join(dir, catalogLockFile))
		require.NoError(t, holder.Lock())
		_, err = reader.List(ctx, "", "")
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		require.True(t, reader.mu.TryLock())
		reader.mu.Unlock()

		require.NoError(t, holder.Unlock())
		listed, err := reader.List(ctx, "", "")
		require.NoError(t, err)
		assert.Len(t, listed, 1)
	})

	t.Run("a read fails if the changed index cannot be reloaded", func(t *testing.T) {
		dir := t.TempDir()
		cat := newOtherProcessCatalog(t, dir)
		require.NoError(t, os.WriteFile(filepath.Join(dir, ocispec.ImageIndexFile), []byte("not json"), 0o600))

		_, err := cat.List(ctx, "", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "opening OCI store")
		require.True(t, cat.mu.TryLock())
		cat.mu.Unlock()
	})
}

func TestLocalStore_FileLockErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("write gives up after lockTimeout", func(t *testing.T) {
		dir := t.TempDir()
		cat := newOtherProcessCatalog(t, dir)
		cat.lockTimeout = 100 * time.Millisecond

		holder := flock.New(filepath.Join(dir, catalogLockFile))
		require.NoError(t, holder.Lock())
		t.Cleanup(func() { _ = holder.Unlock() })

		_, err := cat.Store(ctx, solutionRef("blocked", "1.0.0"), []byte("x"), nil, nil, false)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Contains(t, err.Error(), "locking catalog")

		// The failed write released the in-process lock.
		require.True(t, cat.mu.TryLock())
		cat.mu.Unlock()
	})

	t.Run("write stops waiting when ctx is cancelled", func(t *testing.T) {
		dir := t.TempDir()
		cat := newOtherProcessCatalog(t, dir)

		holder := flock.New(filepath.Join(dir, catalogLockFile))
		require.NoError(t, holder.Lock())
		t.Cleanup(func() { _ = holder.Unlock() })

		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		_, err := cat.Store(cancelled, solutionRef("blocked", "1.0.0"), []byte("x"), nil, nil, false)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("write fails if the changed index cannot be reloaded", func(t *testing.T) {
		dir := t.TempDir()
		cat := newOtherProcessCatalog(t, dir)
		require.NoError(t, os.WriteFile(filepath.Join(dir, ocispec.ImageIndexFile), []byte("not json"), 0o600))

		_, err := cat.Store(ctx, solutionRef("x", "1.0.0"), []byte("x"), nil, nil, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "opening OCI store")
		require.True(t, cat.mu.TryLock())
		cat.mu.Unlock()
	})
}

func TestLocalStore_ReadOnlyDir(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions that apply to this user")
	}
	ctx := context.Background()

	// readOnlyCatalog returns a catalog directory holding one artifact, made
	// read-only, with or without its lock file.
	readOnlyCatalog := func(t *testing.T, keepLockFile bool) string {
		t.Helper()
		dir := t.TempDir()
		cat := newOtherProcessCatalog(t, dir)
		_, err := cat.Store(ctx, solutionRef("app", "1.0.0"), []byte("app"), nil, nil, false)
		require.NoError(t, err)
		if !keepLockFile {
			require.NoError(t, os.Remove(filepath.Join(dir, catalogLockFile)))
		}
		require.NoError(t, os.Chmod(dir, 0o555))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		key, err := canonicalCatalogPath(dir)
		require.NoError(t, err)
		return key
	}

	for _, tc := range []struct {
		name         string
		keepLockFile bool
	}{
		{name: "without a lock file opens unlocked", keepLockFile: false},
		{name: "with a lock file opens locked", keepLockFile: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := readOnlyCatalog(t, tc.keepLockFile)

			s, err := openLocalStore(dir)
			require.NoError(t, err)
			_, err = s.store.Resolve(ctx, buildLocalTag(writeRef(solutionRef("app", "1.0.0"))))
			require.NoError(t, err)

			if !tc.keepLockFile {
				// The fallback is for opening only: a write still needs the
				// lock, so it fails rather than writing unlocked.
				_, err = s.lockWrite(ctx)
				require.ErrorIs(t, err, fs.ErrPermission)
			}
		})
	}

	t.Run("a read reloads without a lock file", func(t *testing.T) {
		dir := t.TempDir()
		reader := newOtherProcessCatalog(t, dir)
		writer := newOtherProcessCatalog(t, dir)
		app := solutionRef("app", "1.0.0")
		_, err := writer.Store(ctx, app, []byte("app"), nil, nil, false)
		require.NoError(t, err)
		require.NoError(t, os.Remove(filepath.Join(dir, catalogLockFile)))
		require.NoError(t, os.Chmod(dir, 0o555))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

		got, _, err := reader.Fetch(ctx, app)
		require.NoError(t, err)
		assert.Equal(t, []byte("app"), got)
	})
}

func TestLockFileUnwritable(t *testing.T) {
	pathErr := func(errno syscall.Errno) error {
		return fmt.Errorf("locking: %w", &fs.PathError{Op: "open", Path: "/x/.lock", Err: errno})
	}
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "permission denied", err: pathErr(syscall.EACCES), want: true},
		{name: "read-only filesystem", err: pathErr(syscall.EROFS), want: true},
		{name: "timeout", err: fmt.Errorf("locking: %w", context.DeadlineExceeded), want: false},
		{name: "missing directory", err: pathErr(syscall.ENOENT), want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, lockFileUnwritable(tc.err))
		})
	}
}

// TestLocalCatalog_MultiProcessNoLostTags runs real concurrent processes, each
// storing its own tags, and checks every tag reaches disk.
func TestLocalCatalog_MultiProcessNoLostTags(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns child processes")
	}
	dir := t.TempDir()
	const processes, perProcess = 3, 8

	cmds := make([]*exec.Cmd, processes)
	outs := make([]*bytes.Buffer, processes)
	for p := range processes {
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestLocalCatalog_WriterProcess$", "-test.count=1")
		cmd.Env = append(os.Environ(),
			writerProcessDirEnv+"="+dir,
			writerProcessPrefixEnv+"="+fmt.Sprintf("p%d", p),
			writerProcessCountEnv+"="+strconv.Itoa(perProcess),
		)
		// One comparable writer for both streams, so exec writes it serially.
		outs[p] = &bytes.Buffer{}
		cmd.Stdout, cmd.Stderr = outs[p], outs[p]
		require.NoError(t, cmd.Start())
		cmds[p] = cmd
	}
	for p, cmd := range cmds {
		require.NoError(t, cmd.Wait(), outs[p].String())
	}

	refs := make([]Reference, 0, perProcess*processes)
	for p := range processes {
		for i := range perProcess {
			refs = append(refs, solutionRef(fmt.Sprintf("p%d-n%d", p, i), "1.0.0"))
		}
	}
	requireTagsOnDisk(t, dir, refs...)
}

// TestLocalCatalog_WriterProcess is the child half of
// TestLocalCatalog_MultiProcessNoLostTags; it does nothing when run directly.
func TestLocalCatalog_WriterProcess(t *testing.T) {
	dir := os.Getenv(writerProcessDirEnv)
	if dir == "" {
		t.Skip("child process helper")
	}
	count, err := strconv.Atoi(os.Getenv(writerProcessCountEnv))
	require.NoError(t, err)
	prefix := os.Getenv(writerProcessPrefixEnv)

	cat, err := NewLocalCatalogAt(dir, logr.Discard())
	require.NoError(t, err)
	for i := range count {
		name := fmt.Sprintf("%s-n%d", prefix, i)
		_, err := cat.Store(t.Context(), solutionRef(name, "1.0.0"), []byte(name), nil, nil, false)
		require.NoError(t, err)
	}
}
