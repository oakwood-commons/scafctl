// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package prepare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/load"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stageTestBundle builds a bundle tar from relPath -> content.
func stageTestBundle(t testing.TB, files map[string]string) []byte {
	t.Helper()
	root := t.TempDir()
	entries := make([]bundler.FileEntry, 0, len(files))
	for rel, content := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o600))
		entries = append(entries, bundler.FileEntry{RelPath: rel})
	}
	snap, err := bundler.ReadFileSnapshot(root, entries)
	require.NoError(t, err)
	data, _, err := bundler.CreateBundleTar(snap, nil)
	require.NoError(t, err)
	return data
}

// isolateTempDir points os.TempDir at a fresh directory so a test can assert
// what Stage created (or failed to clean up). Not usable with t.Parallel.
func isolateTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	return dir
}

func requireEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "Stage must not leave anything behind in %s", dir)
}

func TestStage_NilSolution(t *testing.T) {
	t.Parallel()
	_, err := Stage(context.Background(), nil, StageOptions{})
	require.Error(t, err)

	_, err = Stage(context.Background(), &load.MaterializedSolution{}, StageOptions{})
	require.Error(t, err)
}

func TestStage_NoBundle(t *testing.T) {
	tmp := isolateTempDir(t)

	staged, err := Stage(context.Background(), &load.MaterializedSolution{Solution: minimalSolution()}, StageOptions{})
	require.NoError(t, err)
	t.Cleanup(staged.Cleanup)

	assert.Empty(t, staged.SolutionDir)
	assert.Nil(t, staged.Lock)
	requireEmptyDir(t, tmp)
}

func TestStage_ExtractsBundle(t *testing.T) {
	t.Parallel()
	ms := &load.MaterializedSolution{
		Solution: minimalSolution(),
		Bundle:   stageTestBundle(t, map[string]string{"data/greeting.txt": "hello from bundle"}),
	}

	staged, err := Stage(context.Background(), ms, StageOptions{})
	require.NoError(t, err)
	t.Cleanup(staged.Cleanup)

	require.NotEmpty(t, staged.SolutionDir)
	assert.True(t, filepath.IsAbs(staged.SolutionDir))
	got, err := os.ReadFile(filepath.Join(staged.SolutionDir, "data", "greeting.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello from bundle", string(got))
}

func TestStage_DoesNotWriteSolutionOrLock(t *testing.T) {
	t.Parallel()
	ms := &load.MaterializedSolution{
		Solution: minimalSolution(),
		Bundle:   stageTestBundle(t, map[string]string{"a.txt": "a"}),
		Lock:     []byte(`{"version":1}`),
	}

	staged, err := Stage(context.Background(), ms, StageOptions{})
	require.NoError(t, err)
	t.Cleanup(staged.Cleanup)

	assert.NoFileExists(t, filepath.Join(staged.SolutionDir, "solution.yaml"))
	assert.NoFileExists(t, filepath.Join(staged.SolutionDir, bundler.DefaultLockFileName))
}

func TestStage_BundledSolutionYAMLNotOverwritten(t *testing.T) {
	t.Parallel()
	const bundled = "bundled file named solution.yaml"
	ms := &load.MaterializedSolution{
		Solution: minimalSolution(),
		Bundle:   stageTestBundle(t, map[string]string{"solution.yaml": bundled}),
	}

	staged, err := Stage(context.Background(), ms, StageOptions{})
	require.NoError(t, err)
	t.Cleanup(staged.Cleanup)

	got, err := os.ReadFile(filepath.Join(staged.SolutionDir, "solution.yaml"))
	require.NoError(t, err)
	assert.Equal(t, bundled, string(got))
}

func TestStage_LockParsedInMemory(t *testing.T) {
	t.Parallel()
	ms := &load.MaterializedSolution{
		Solution: minimalSolution(),
		Lock:     []byte(`{"version":1,"plugins":[{"name":"p","kind":"provider","version":"1.0.0","digest":"sha256:abc","resolvedFrom":"c"}]}`),
	}

	staged, err := Stage(context.Background(), ms, StageOptions{})
	require.NoError(t, err)
	t.Cleanup(staged.Cleanup)

	require.NotNil(t, staged.Lock)
	assert.Equal(t, bundler.LockFileVersion, staged.Lock.Version)
	require.Len(t, staged.Lock.Plugins, 1)
	assert.Equal(t, "p", staged.Lock.Plugins[0].Name)
}

func TestStage_MalformedLock_NoTempDir(t *testing.T) {
	tmp := isolateTempDir(t)
	ms := &load.MaterializedSolution{
		Solution: minimalSolution(),
		Bundle:   stageTestBundle(t, map[string]string{"a.txt": "a"}),
		Lock:     []byte("{not json"),
	}

	staged, err := Stage(context.Background(), ms, StageOptions{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidLock)
	assert.Nil(t, staged)
	requireEmptyDir(t, tmp)
}

func TestStage_BundleTooLarge_CleansUp(t *testing.T) {
	tmp := isolateTempDir(t)
	ms := &load.MaterializedSolution{
		Solution: minimalSolution(),
		Bundle:   stageTestBundle(t, map[string]string{"big.txt": strings.Repeat("x", 4096)}),
	}

	staged, err := Stage(context.Background(), ms, StageOptions{MaxBundleSize: 1024})
	require.Error(t, err)
	assert.ErrorIs(t, err, bundler.ErrBundleTooLarge)
	assert.ErrorIs(t, err, ErrInvalidBundle)
	assert.Nil(t, staged)
	requireEmptyDir(t, tmp)
}

func TestStage_CorruptBundle_CleansUp(t *testing.T) {
	tmp := isolateTempDir(t)
	ms := &load.MaterializedSolution{
		Solution: minimalSolution(),
		Bundle:   []byte("definitely not a tar archive"),
	}

	staged, err := Stage(context.Background(), ms, StageOptions{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidBundle)
	assert.Nil(t, staged)
	requireEmptyDir(t, tmp)
}

func TestExtractBundleLimited_HostFilesystemFailure_NotInvalidBundle(t *testing.T) {
	tmp := isolateTempDir(t)
	diskFull := errors.New("no space left on device")
	failingExtract := func(_ []byte, destDir string, _ int64) (*bundler.BundleManifest, error) {
		// Simulate a partial write followed by a host I/O failure, wrapped the
		// way the bundler wraps os.WriteFile errors.
		partial := filepath.Join(destDir, "partial.txt")
		require.NoError(t, os.WriteFile(partial, []byte("x"), 0o600))
		return nil, fmt.Errorf("failed to write partial.txt: %w", &fs.PathError{Op: "write", Path: partial, Err: diskFull})
	}

	dir, err := extractBundleLimited(context.Background(), []byte("bundle"), 0, failingExtract)
	require.Error(t, err)
	assert.Empty(t, dir)
	assert.NotErrorIs(t, err, ErrInvalidBundle, "a host failure must not be reported as a bad bundle")
	assert.ErrorIs(t, err, diskFull)
	requireEmptyDir(t, tmp)
}

func TestExtractBundleLimited_ArchiveError_IsInvalidBundle(t *testing.T) {
	tmp := isolateTempDir(t)
	badArchive := func(_ []byte, _ string, _ int64) (*bundler.BundleManifest, error) {
		return nil, errors.New("failed to read tar entry: unexpected EOF")
	}

	_, err := extractBundleLimited(context.Background(), []byte("bundle"), 0, badArchive)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidBundle)
	requireEmptyDir(t, tmp)
}

func TestStage_MergesPluginDefaults(t *testing.T) {
	t.Parallel()
	defaultVal := &spec.ValueRef{Literal: "from-default"}
	inlineVal := &spec.ValueRef{Literal: "inline"}

	sol := minimalSolution()
	sol.Bundle.Plugins = []solution.PluginDependency{{
		Name:     "myprovider",
		Kind:     solution.PluginKindProvider,
		Version:  "^1.0.0",
		Defaults: map[string]*spec.ValueRef{"a": defaultVal, "b": defaultVal},
	}}
	sol.Spec.Workflow = &action.Workflow{Actions: map[string]*action.Action{
		"step": {Provider: "myprovider", Inputs: map[string]*spec.ValueRef{"a": inlineVal}},
	}}

	staged, err := Stage(context.Background(), &load.MaterializedSolution{Solution: sol}, StageOptions{})
	require.NoError(t, err)
	t.Cleanup(staged.Cleanup)

	inputs := staged.Solution.Spec.Workflow.Actions["step"].Inputs
	assert.Equal(t, inlineVal, inputs["a"], "inline input wins over plugin default")
	assert.Equal(t, defaultVal, inputs["b"], "missing input gets plugin default")
}

func TestStage_Context_SolutionDirectory(t *testing.T) {
	t.Parallel()
	ms := &load.MaterializedSolution{
		Solution: minimalSolution(),
		Bundle:   stageTestBundle(t, map[string]string{"a.txt": "a"}),
	}

	staged, err := Stage(context.Background(), ms, StageOptions{})
	require.NoError(t, err)
	t.Cleanup(staged.Cleanup)

	dir, ok := provider.SolutionDirectoryFromContext(staged.Context(context.Background()))
	require.True(t, ok)
	assert.Equal(t, staged.SolutionDir, dir)
}

func TestStage_Context_NoBundle_NoSolutionDirectory(t *testing.T) {
	t.Parallel()
	staged, err := Stage(context.Background(), &load.MaterializedSolution{Solution: minimalSolution()}, StageOptions{})
	require.NoError(t, err)
	t.Cleanup(staged.Cleanup)

	_, ok := provider.SolutionDirectoryFromContext(staged.Context(context.Background()))
	assert.False(t, ok, "without a bundle there is no solution directory to scope paths to")
}

func TestStage_Context_ExecutionSettings(t *testing.T) {
	t.Parallel()
	staged, err := Stage(context.Background(), &load.MaterializedSolution{Solution: minimalSolution()},
		StageOptions{Entrypoint: EntrypointAPI})
	require.NoError(t, err)
	t.Cleanup(staged.Cleanup)

	execSettings, ok := provider.ExecutionSettingsFromContext(staged.Context(context.Background()))
	require.True(t, ok)
	raw, ok := execSettings[hostMetadataSettingsKey]
	require.True(t, ok)

	var meta struct {
		Entrypoint string `json:"entrypoint"`
		Solution   struct {
			Name string `json:"name"`
		} `json:"solution"`
	}
	require.NoError(t, json.Unmarshal(raw, &meta))
	assert.Equal(t, EntrypointAPI, meta.Entrypoint)
	assert.Equal(t, "test-solution", meta.Solution.Name)
}

func TestStage_Cleanup(t *testing.T) {
	t.Parallel()
	ms := &load.MaterializedSolution{
		Solution: minimalSolution(),
		Bundle:   stageTestBundle(t, map[string]string{"a.txt": "a"}),
	}

	staged, err := Stage(context.Background(), ms, StageOptions{})
	require.NoError(t, err)
	require.DirExists(t, staged.SolutionDir)

	staged.Cleanup()
	assert.NoDirExists(t, staged.SolutionDir)
	staged.Cleanup() // idempotent

	var nilStaged *Staged
	nilStaged.Cleanup() // nil-safe
}

func TestStage_NoChdir(t *testing.T) {
	t.Parallel()
	before, err := os.Getwd()
	require.NoError(t, err)

	ms := &load.MaterializedSolution{
		Solution: minimalSolution(),
		Bundle:   stageTestBundle(t, map[string]string{"a.txt": "a"}),
	}
	staged, err := Stage(context.Background(), ms, StageOptions{})
	require.NoError(t, err)
	t.Cleanup(staged.Cleanup)

	after, err := os.Getwd()
	require.NoError(t, err)
	assert.Equal(t, before, after, "Stage must not change the process working directory")
}

func TestStage_Concurrent(t *testing.T) {
	t.Parallel()
	const workers = 8

	bundles := make([][]byte, workers)
	for i := range bundles {
		bundles[i] = stageTestBundle(t, map[string]string{"id.txt": fmt.Sprintf("bundle-%d", i)})
	}

	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			staged, err := Stage(context.Background(), &load.MaterializedSolution{
				Solution: minimalSolution(),
				Bundle:   bundles[i],
			}, StageOptions{})
			if err != nil {
				errs[i] = err
				return
			}
			defer staged.Cleanup()
			got, err := os.ReadFile(filepath.Join(staged.SolutionDir, "id.txt"))
			if err != nil {
				errs[i] = err
				return
			}
			if want := fmt.Sprintf("bundle-%d", i); string(got) != want {
				errs[i] = fmt.Errorf("worker %d read %q, want %q", i, got, want)
			}
		}()
	}
	wg.Wait()
	for _, err := range errs {
		assert.NoError(t, err)
	}
}

func TestStage_TempDirUsesAppName(t *testing.T) {
	orig := paths.AppName()
	paths.SetAppName("mycli")
	t.Cleanup(func() { paths.SetAppName(orig) })

	ms := &load.MaterializedSolution{
		Solution: minimalSolution(),
		Bundle:   stageTestBundle(t, map[string]string{"a.txt": "a"}),
	}
	staged, err := Stage(context.Background(), ms, StageOptions{})
	require.NoError(t, err)
	t.Cleanup(staged.Cleanup)

	assert.True(t, strings.HasPrefix(filepath.Base(staged.SolutionDir), "mycli-bundle-"),
		"temp dir %q should use the embedder's app name", staged.SolutionDir)
}

func BenchmarkStage(b *testing.B) {
	ms := &load.MaterializedSolution{
		Solution: minimalSolution(),
		Bundle: stageTestBundle(b, map[string]string{
			"templates/a.tmpl": strings.Repeat("a", 4096),
			"data/b.json":      `{"k":"v"}`,
		}),
	}
	for b.Loop() {
		staged, err := Stage(context.Background(), ms, StageOptions{Entrypoint: EntrypointAPI})
		if err != nil {
			b.Fatal(err)
		}
		staged.Cleanup()
	}
}
