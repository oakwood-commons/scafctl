// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package load

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
)

const validSolutionYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
  version: 1.0.0
spec:
  resolvers: {}
`

// undefinedDepJSON parses but fails strict spec validation.
const undefinedDepJSON = `{
	"apiVersion": "scafctl.io/v1",
	"kind": "Solution",
	"metadata": {"name": "lenient", "version": "1.0.0"},
	"spec": {"resolvers": {"app": {
		"dependsOn": ["doesNotExist"],
		"resolve": {"with": [{"provider": "static", "inputs": {"value": "x"}}]}
	}}}
}`

const composeRootYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: composed
  version: 1.0.0
compose:
  - part.yaml
spec:
  resolvers: {}
`

const composePartYAML = `spec:
  resolvers:
    app:
      resolve:
        with:
          - provider: static
            inputs:
              value: x
`

func readFileMap(files map[string]string) func(string) ([]byte, error) {
	return func(name string) ([]byte, error) {
		content, ok := files[name]
		if !ok {
			return nil, fs.ErrNotExist
		}
		return []byte(content), nil
	}
}

func TestFileLoader_Load(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	absPath := filepath.Join(cwd, "solution.yaml")
	partPath := filepath.Join(cwd, "part.yaml")

	tests := []struct {
		name          string
		ref           string
		files         map[string]string
		opts          []FileOption
		wantPath      string
		wantResolvers []string
		wantCompose   []string
		wantErr       error
		errMsg        string
	}{
		{
			name:     "relative path resolved against context cwd",
			ref:      "solution.yaml",
			files:    map[string]string{absPath: validSolutionYAML},
			wantPath: absPath,
		},
		{
			name:     "absolute path",
			ref:      absPath,
			files:    map[string]string{absPath: validSolutionYAML},
			wantPath: absPath,
		},
		{
			name:    "missing file",
			ref:     "missing.yaml",
			files:   map[string]string{},
			wantErr: fs.ErrNotExist,
			errMsg:  "reading solution",
		},
		{
			name:   "parse error",
			ref:    "solution.yaml",
			files:  map[string]string{absPath: "not valid yaml: ["},
			errMsg: "loading solution",
		},
		{
			name:   "parse error lenient",
			ref:    "solution.yaml",
			files:  map[string]string{absPath: "not valid yaml: ["},
			opts:   []FileOption{WithLenient()},
			errMsg: "loading solution",
		},
		{
			name:   "strict validation failure",
			ref:    "solution.yaml",
			files:  map[string]string{absPath: undefinedDepJSON},
			errMsg: "loading solution",
		},
		{
			name:          "lenient skips spec validation",
			ref:           "solution.yaml",
			files:         map[string]string{absPath: undefinedDepJSON},
			opts:          []FileOption{WithLenient()},
			wantPath:      absPath,
			wantResolvers: []string{"app"},
		},
		{
			name:          "compose merges parts relative to solution dir",
			ref:           "solution.yaml",
			files:         map[string]string{absPath: composeRootYAML, partPath: composePartYAML},
			wantPath:      absPath,
			wantResolvers: []string{"app"},
		},
		{
			name:    "compose part missing",
			ref:     "solution.yaml",
			files:   map[string]string{absPath: composeRootYAML},
			wantErr: fs.ErrNotExist,
			errMsg:  "composing solution",
		},
		{
			name: "parse only skips compose and lock",
			ref:  "solution.yaml",
			files: map[string]string{
				absPath: composeRootYAML,
				filepath.Join(cwd, bundler.DefaultLockFileName): "version: [",
			},
			opts:        []FileOption{WithParseOnly()},
			wantPath:    absPath,
			wantCompose: []string{"part.yaml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := provider.WithWorkingDirectory(t.Context(), cwd)
			opts := append([]FileOption{WithReadFile(readFileMap(tt.files))}, tt.opts...)
			c := source.Classification{Kind: source.KindLocalFile, Ref: tt.ref}

			got, err := NewFileLoader(opts...).Load(ctx, c)

			if tt.errMsg != "" {
				require.Error(t, err)
				assert.Nil(t, got)
				assert.Contains(t, err.Error(), tt.errMsg)
				if tt.wantErr != nil {
					assert.ErrorIs(t, err, tt.wantErr)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, got)
			require.NotNil(t, got.Solution)
			assert.Equal(t, tt.wantPath, got.Solution.GetPath())
			for _, name := range tt.wantResolvers {
				assert.Contains(t, got.Solution.Spec.Resolvers, name)
			}
			assert.Equal(t, tt.wantCompose, got.Solution.Compose)
			assert.Equal(t, c, got.Source)
			assert.Nil(t, got.Bundle)
			assert.Nil(t, got.Lock)
		})
	}
}

func TestFileLoader_Load_AdjacentLock(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	solPath := filepath.Join(cwd, "solution.yaml")
	lockPath := filepath.Join(cwd, bundler.DefaultLockFileName)

	const validLock = `# This file is auto-generated by scafctl build. Do not edit.
version: 1
plugins:
  - name: echo
    kind: provider
    version: 1.2.3
    digest: sha256:abc
    resolvedFrom: local
`

	readErr := errors.New("permission denied")

	tests := []struct {
		name       string
		readFile   func(string) ([]byte, error)
		wantPlugin string
		wantNil    bool
		wantErr    error
		errMsg     string
	}{
		{
			name:     "no lock file",
			readFile: readFileMap(map[string]string{solPath: validSolutionYAML}),
			wantNil:  true,
		},
		{
			name:       "valid lock is re-encoded as JSON",
			readFile:   readFileMap(map[string]string{solPath: validSolutionYAML, lockPath: validLock}),
			wantPlugin: "echo",
		},
		{
			name:     "malformed lock",
			readFile: readFileMap(map[string]string{solPath: validSolutionYAML, lockPath: "version: ["}),
			errMsg:   "loading lock file",
		},
		{
			name:     "unsupported lock version",
			readFile: readFileMap(map[string]string{solPath: validSolutionYAML, lockPath: "version: 99\n"}),
			errMsg:   "unsupported lock file version",
		},
		{
			name: "unreadable lock",
			readFile: func(name string) ([]byte, error) {
				if name == lockPath {
					return nil, readErr
				}
				return readFileMap(map[string]string{solPath: validSolutionYAML})(name)
			},
			wantErr: readErr,
			errMsg:  "reading lock file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := provider.WithWorkingDirectory(t.Context(), cwd)
			c := source.Classification{Kind: source.KindLocalFile, Ref: "solution.yaml"}

			got, err := NewFileLoader(WithReadFile(tt.readFile)).Load(ctx, c)

			if tt.errMsg != "" {
				require.Error(t, err)
				assert.Nil(t, got)
				assert.Contains(t, err.Error(), tt.errMsg)
				if tt.wantErr != nil {
					assert.ErrorIs(t, err, tt.wantErr)
				}
				return
			}

			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, got.Lock)
				return
			}
			lf, err := bundler.ParseLockJSON(got.Lock)
			require.NoError(t, err)
			require.Len(t, lf.Plugins, 1)
			assert.Equal(t, tt.wantPlugin, lf.Plugins[0].Name)
			assert.Equal(t, "1.2.3", lf.Plugins[0].Version)
		})
	}
}

func TestFileLoader_Load_BaseDir(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	solDir := filepath.Join(cwd, "sol")
	baseDir := filepath.Join(cwd, "base")
	solPath := filepath.Join(solDir, "solution.yaml")

	const baseLock = "version: 1\nplugins:\n  - name: from-base\n    kind: provider\n    version: 1.0.0\n    digest: sha256:abc\n    resolvedFrom: local\n"
	const adjacentLock = "version: 1\nplugins:\n  - name: from-adjacent\n    kind: provider\n    version: 1.0.0\n    digest: sha256:def\n    resolvedFrom: local\n"

	tests := []struct {
		name          string
		baseDir       string
		files         map[string]string
		wantResolvers []string
		wantPlugin    string
		wantNoLock    bool
		wantErr       error
		errMsg        string
	}{
		{
			name:          "compose resolves parts against base dir",
			baseDir:       baseDir,
			files:         map[string]string{solPath: composeRootYAML, filepath.Join(baseDir, "part.yaml"): composePartYAML},
			wantResolvers: []string{"app"},
			wantNoLock:    true,
		},
		{
			name:    "compose ignores parts next to the solution file",
			baseDir: baseDir,
			files:   map[string]string{solPath: composeRootYAML, filepath.Join(solDir, "part.yaml"): composePartYAML},
			wantErr: fs.ErrNotExist,
			errMsg:  "composing solution",
		},
		{
			name:    "lock is read from base dir, not next to the solution file",
			baseDir: baseDir,
			files: map[string]string{
				solPath: validSolutionYAML,
				filepath.Join(baseDir, bundler.DefaultLockFileName): baseLock,
				filepath.Join(solDir, bundler.DefaultLockFileName):  adjacentLock,
			},
			wantPlugin: "from-base",
		},
		{
			name:    "no lock in base dir ignores adjacent lock",
			baseDir: baseDir,
			files: map[string]string{
				solPath: validSolutionYAML,
				filepath.Join(solDir, bundler.DefaultLockFileName): adjacentLock,
			},
			wantNoLock: true,
		},
		{
			name:    "relative base dir resolves against working directory",
			baseDir: "base",
			files: map[string]string{
				solPath: validSolutionYAML,
				filepath.Join(baseDir, bundler.DefaultLockFileName): baseLock,
			},
			wantPlugin: "from-base",
		},
		{
			name:    "empty base dir keeps solution-dir semantics",
			baseDir: "",
			files: map[string]string{
				solPath: validSolutionYAML,
				filepath.Join(solDir, bundler.DefaultLockFileName): adjacentLock,
			},
			wantPlugin: "from-adjacent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := provider.WithWorkingDirectory(t.Context(), cwd)
			c := source.Classification{Kind: source.KindLocalFile, Ref: solPath}

			got, err := NewFileLoader(WithReadFile(readFileMap(tt.files)), WithBaseDir(tt.baseDir)).Load(ctx, c)

			if tt.errMsg != "" {
				require.Error(t, err)
				assert.Nil(t, got)
				assert.Contains(t, err.Error(), tt.errMsg)
				if tt.wantErr != nil {
					assert.ErrorIs(t, err, tt.wantErr)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, solPath, got.Solution.GetPath())
			for _, name := range tt.wantResolvers {
				assert.Contains(t, got.Solution.Spec.Resolvers, name)
			}
			if tt.wantNoLock {
				assert.Nil(t, got.Lock)
				return
			}
			lf, err := bundler.ParseLockJSON(got.Lock)
			require.NoError(t, err)
			require.Len(t, lf.Plugins, 1)
			assert.Equal(t, tt.wantPlugin, lf.Plugins[0].Name)
		})
	}
}

func TestFileLoader_Load_UnsupportedKinds(t *testing.T) {
	t.Parallel()

	for _, kind := range allKinds {
		if kind == source.KindLocalFile {
			continue
		}
		t.Run(kind.String(), func(t *testing.T) {
			t.Parallel()
			got, err := NewFileLoader().Load(t.Context(), source.Classification{Kind: kind, Ref: "ref"})
			require.ErrorIs(t, err, ErrUnsupportedKind)
			assert.Nil(t, got)
			assert.Contains(t, err.Error(), kind.String())
		})
	}
}

func TestNewFileLoader(t *testing.T) {
	t.Parallel()

	t.Run("defaults", func(t *testing.T) {
		t.Parallel()
		l := NewFileLoader()
		assert.NotNil(t, l.readFile)
		assert.False(t, l.lenient)
	})

	t.Run("options", func(t *testing.T) {
		t.Parallel()
		errSentinel := errors.New("sentinel")
		l := NewFileLoader(WithReadFile(func(string) ([]byte, error) { return nil, errSentinel }), WithLenient())
		_, err := l.readFile("x")
		require.ErrorIs(t, err, errSentinel)
		assert.True(t, l.lenient)
	})
}

func BenchmarkFileLoader_Load(b *testing.B) {
	cwd := b.TempDir()
	path := filepath.Join(cwd, "solution.yaml")
	l := NewFileLoader(WithReadFile(readFileMap(map[string]string{path: validSolutionYAML})))
	ctx := provider.WithWorkingDirectory(b.Context(), cwd)
	c := source.Classification{Kind: source.KindLocalFile, Ref: "solution.yaml"}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := l.Load(ctx, c); err != nil {
			b.Fatal(err)
		}
	}
}
