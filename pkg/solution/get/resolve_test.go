// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package get

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindAllSolutions(t *testing.T) {
	t.Parallel()

	t.Run("returns all matching files", func(t *testing.T) {
		t.Parallel()
		existingFiles := map[string]bool{
			"scafctl/solution.yaml": true,
			"solution.yaml":         true,
			"taskfile.yaml":         true,
		}
		customStatFunc := func(path string) (os.FileInfo, error) {
			if existingFiles[path] {
				return nil, nil
			}
			return nil, fmt.Errorf("not found")
		}

		getter := NewGetter(WithStatFunc(customStatFunc))
		results := getter.FindAllSolutions()

		assert.Len(t, results, 3)
		assert.Equal(t, "scafctl/solution.yaml", results[0].Path)
		assert.Equal(t, "solution.yaml", results[1].Path)
		assert.Equal(t, "taskfile.yaml", results[2].Path)
	})

	t.Run("returns empty slice when no files found", func(t *testing.T) {
		t.Parallel()
		customStatFunc := func(path string) (os.FileInfo, error) {
			return nil, fmt.Errorf("not found")
		}

		getter := NewGetter(WithStatFunc(customStatFunc))
		results := getter.FindAllSolutions()

		assert.Empty(t, results)
	})

	t.Run("deduplicates same file from different paths", func(t *testing.T) {
		t.Parallel()
		// When the current folder ("") finds "solution.yaml" which is the
		// same physical file as "./solution.yaml", we should deduplicate.
		existingFiles := map[string]bool{
			"solution.yaml": true,
		}
		customStatFunc := func(path string) (os.FileInfo, error) {
			if existingFiles[path] {
				return nil, nil
			}
			return nil, fmt.Errorf("not found")
		}

		getter := NewGetter(WithStatFunc(customStatFunc))
		results := getter.FindAllSolutions()

		// Should not have duplicates
		assert.Len(t, results, 1)
	})

	t.Run("excludes taskfile in action mode", func(t *testing.T) {
		t.Parallel()
		existingFiles := map[string]bool{
			"actions.yaml":  true,
			"taskfile.yaml": true,
		}
		customStatFunc := func(path string) (os.FileInfo, error) {
			if existingFiles[path] {
				return nil, nil
			}
			return nil, fmt.Errorf("not found")
		}

		getter := NewGetter(
			WithStatFunc(customStatFunc),
			WithDiscoveryMode(settings.DiscoveryModeAction),
		)
		results := getter.FindAllSolutions()

		// taskfile.yaml should NOT appear in action mode
		for _, r := range results {
			assert.NotEqual(t, "taskfile.yaml", r.Path)
		}
		// But actions.yaml should be found
		require.NotEmpty(t, results)
		assert.Equal(t, "actions.yaml", results[0].Path)
		assert.True(t, results[0].IsActionFile)
	})

	t.Run("includes taskfile in solution mode", func(t *testing.T) {
		t.Parallel()
		existingFiles := map[string]bool{
			"taskfile.yaml": true,
		}
		customStatFunc := func(path string) (os.FileInfo, error) {
			if existingFiles[path] {
				return nil, nil
			}
			return nil, fmt.Errorf("not found")
		}

		getter := NewGetter(
			WithStatFunc(customStatFunc),
			WithDiscoveryMode(settings.DiscoveryModeSolution),
		)
		results := getter.FindAllSolutions()

		require.Len(t, results, 1)
		assert.Equal(t, "taskfile.yaml", results[0].Path)
	})

	t.Run("updates lastDiscovery with first result", func(t *testing.T) {
		t.Parallel()
		existingFiles := map[string]bool{
			"scafctl/solution.yaml": true,
			"solution.yaml":         true,
		}
		customStatFunc := func(path string) (os.FileInfo, error) {
			if existingFiles[path] {
				return nil, nil
			}
			return nil, fmt.Errorf("not found")
		}

		getter := NewGetter(WithStatFunc(customStatFunc))
		results := getter.FindAllSolutions()

		require.NotEmpty(t, results)
		disc := getter.LastDiscoveryResult()
		assert.Equal(t, results[0].Path, disc.Path)
	})
}

func TestResolve(t *testing.T) {
	t.Parallel()

	newCtx := func() context.Context {
		ios := &terminal.IOStreams{
			Out:    &discardWriter{},
			ErrOut: &discardWriter{},
			In:     os.Stdin,
		}
		cliParams := settings.NewCliParams()
		w := writer.New(ios, cliParams)
		return writer.WithWriter(context.Background(), w)
	}

	t.Run("returns file flag when provided", func(t *testing.T) {
		t.Parallel()
		ctx := newCtx()
		getter := NewGetter()

		path, err := Resolve(ctx, getter, "./my-solution.yaml", "", ResolveOptions{})
		require.NoError(t, err)
		assert.Equal(t, "./my-solution.yaml", path)
	})

	t.Run("returns positional arg when provided", func(t *testing.T) {
		t.Parallel()
		ctx := newCtx()
		getter := NewGetter()

		path, err := Resolve(ctx, getter, "", "my-catalog-ref@1.0.0", ResolveOptions{})
		require.NoError(t, err)
		assert.Equal(t, "my-catalog-ref@1.0.0", path)
	})

	t.Run("file flag takes priority over positional", func(t *testing.T) {
		t.Parallel()
		ctx := newCtx()
		getter := NewGetter()

		path, err := Resolve(ctx, getter, "./explicit.yaml", "ignored-ref", ResolveOptions{})
		require.NoError(t, err)
		assert.Equal(t, "./explicit.yaml", path)
	})

	t.Run("auto-discovers when no file or positional", func(t *testing.T) {
		t.Parallel()
		ctx := newCtx()
		existingFiles := map[string]bool{
			"solution.yaml": true,
		}
		customStatFunc := func(path string) (os.FileInfo, error) {
			if existingFiles[path] {
				return nil, nil
			}
			return nil, fmt.Errorf("not found")
		}

		getter := NewGetter(WithStatFunc(customStatFunc))

		path, err := Resolve(ctx, getter, "", "", ResolveOptions{Risk: DiscoveryRiskLow})
		require.NoError(t, err)
		assert.Equal(t, "solution.yaml", path)
	})

	t.Run("returns error when nothing found", func(t *testing.T) {
		t.Parallel()
		ctx := newCtx()
		customStatFunc := func(path string) (os.FileInfo, error) {
			return nil, fmt.Errorf("not found")
		}

		getter := NewGetter(WithStatFunc(customStatFunc))

		_, err := Resolve(ctx, getter, "", "", ResolveOptions{Risk: DiscoveryRiskLow})
		assert.ErrorIs(t, err, ErrNoSolutionFound)
	})

	t.Run("low risk warns on multiple matches", func(t *testing.T) {
		t.Parallel()
		ctx := newCtx()
		existingFiles := map[string]bool{
			"scafctl/solution.yaml": true,
			"solution.yaml":         true,
		}
		customStatFunc := func(path string) (os.FileInfo, error) {
			if existingFiles[path] {
				return nil, nil
			}
			return nil, fmt.Errorf("not found")
		}

		getter := NewGetter(WithStatFunc(customStatFunc))

		path, err := Resolve(ctx, getter, "", "", ResolveOptions{Risk: DiscoveryRiskLow})
		require.NoError(t, err)
		// Returns first match
		assert.Equal(t, "scafctl/solution.yaml", path)
	})

	t.Run("high risk errors on multiple matches", func(t *testing.T) {
		t.Parallel()
		ctx := newCtx()
		existingFiles := map[string]bool{
			"scafctl/solution.yaml": true,
			"solution.yaml":         true,
		}
		customStatFunc := func(path string) (os.FileInfo, error) {
			if existingFiles[path] {
				return nil, nil
			}
			return nil, fmt.Errorf("not found")
		}

		getter := NewGetter(WithStatFunc(customStatFunc))

		_, err := Resolve(ctx, getter, "", "", ResolveOptions{Risk: DiscoveryRiskHigh})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "multiple solution files found")
		assert.Contains(t, err.Error(), "use -f/--file")
	})
}

func TestSearchedPaths(t *testing.T) {
	t.Parallel()

	t.Run("returns expected search paths", func(t *testing.T) {
		t.Parallel()
		getter := NewGetter()
		paths := getter.SearchedPaths()

		assert.Contains(t, paths, "scafctl/solution.yaml")
		assert.Contains(t, paths, "solution.yaml")
		assert.Contains(t, paths, "taskfile.yaml")
		assert.Contains(t, paths, "scafctl/taskfile.yaml")
	})

	t.Run("custom binary name", func(t *testing.T) {
		t.Parallel()
		getter := NewGetter(WithSolutionDiscovery(
			settings.SolutionFoldersFor("mycli"),
			settings.SolutionFileNamesFor("mycli"),
		))
		paths := getter.SearchedPaths()

		assert.Contains(t, paths, "mycli/solution.yaml")
		assert.Contains(t, paths, "mycli/mycli.yaml")
		assert.Contains(t, paths, "taskfile.yaml")
		assert.NotContains(t, paths, "scafctl/solution.yaml")
	})
}

// discardWriter implements io.Writer and discards all writes.
type discardWriter struct{}

func (d *discardWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}
