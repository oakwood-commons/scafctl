// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package load

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// writeTree creates files (relative slash paths) and directories under dir.
func writeTree(t testing.TB, dir string, files map[string]string, dirs ...string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	}
	for _, rel := range dirs {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, filepath.FromSlash(rel)), 0o750))
	}
}

const (
	// solutionTaskfile is taskfile content discovery treats as a solution.
	solutionTaskfile = "apiVersion: scafctl.io/v1\nkind: Solution\n"
	// goTaskfile is a go-task taskfile, which discovery skips.
	goTaskfile = "version: '3'\ntasks:\n  build:\n    cmds:\n      - echo hi\n"
)

// absDiscoveries rewrites relative slash paths in want to absolute paths under dir.
func absDiscoveries(dir string, want []Discovery) []Discovery {
	out := make([]Discovery, len(want))
	for i, d := range want {
		out[i] = Discovery{Path: filepath.Join(dir, filepath.FromSlash(d.Path)), IsActionFile: d.IsActionFile}
		if d.AlternatePath != "" {
			out[i].AlternatePath = filepath.Join(dir, filepath.FromSlash(d.AlternatePath))
		}
	}
	return out
}

func TestDiscoverLoader_Discover(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		binaryName  string
		opts        []DiscoverOption
		files       map[string]string
		dirs        []string
		want        []Discovery
		wantErr     error
		wantErrText string
	}{
		{
			name:        "nothing found",
			wantErr:     ErrNoSolutionFound,
			wantErrText: filepath.Join("scafctl", "solution.yaml"),
		},
		{
			name:  "root solution file",
			files: map[string]string{"solution.yaml": ""},
			want:  []Discovery{{Path: "solution.yaml"}},
		},
		{
			name: "folder priority before file name priority",
			files: map[string]string{
				"solution.yaml":          "",
				".scafctl/scafctl.yaml":  "",
				"scafctl/taskfile.yml":   solutionTaskfile,
				"scafctl/solution.json":  "",
				"unrelated/solution.yml": "",
			},
			want: []Discovery{
				{Path: "scafctl/solution.json"},
				{Path: "scafctl/taskfile.yml"},
				{Path: ".scafctl/scafctl.yaml"},
				{Path: "solution.yaml"},
			},
		},
		{
			name:  "file name priority within a folder",
			files: map[string]string{"scafctl.yaml": "", "solution.yml": ""},
			want:  []Discovery{{Path: "solution.yml"}, {Path: "scafctl.yaml"}},
		},
		{
			name:    "default mode ignores action files",
			files:   map[string]string{"actions.yaml": ""},
			wantErr: ErrNoSolutionFound,
		},
		{
			name:  "directories are skipped",
			files: map[string]string{"scafctl.yaml": ""},
			dirs:  []string{"solution.yaml"},
			want:  []Discovery{{Path: "scafctl.yaml"}},
		},
		{
			name:       "custom binary name",
			binaryName: "mycli",
			files:      map[string]string{"scafctl/solution.yaml": "", ".mycli/mycli.yml": ""},
			want:       []Discovery{{Path: ".mycli/mycli.yml"}},
		},
		{
			name:  "empty binary name uses default",
			files: map[string]string{settings.CliBinaryName + "/solution.yaml": ""},
			want:  []Discovery{{Path: settings.CliBinaryName + "/solution.yaml"}},
		},
		{
			name:       "duplicate candidates are reported once",
			binaryName: "solution",
			files:      map[string]string{"solution.yaml": ""},
			want:       []Discovery{{Path: "solution.yaml"}},
		},
		{
			name:  "action mode puts action files first with solution alternate",
			opts:  []DiscoverOption{WithDiscoveryMode(settings.DiscoveryModeAction)},
			files: map[string]string{"scafctl/solution.yaml": "", "actions.yml": ""},
			want: []Discovery{
				{Path: "actions.yml", IsActionFile: true, AlternatePath: "scafctl/solution.yaml"},
				{Path: "scafctl/solution.yaml"},
			},
		},
		{
			name:  "action mode without solution file has no alternate",
			opts:  []DiscoverOption{WithDiscoveryMode(settings.DiscoveryModeAction)},
			files: map[string]string{"actions.yaml": ""},
			want:  []Discovery{{Path: "actions.yaml", IsActionFile: true}},
		},
		{
			name:  "action mode falls back to solution file",
			opts:  []DiscoverOption{WithDiscoveryMode(settings.DiscoveryModeAction)},
			files: map[string]string{"solution.yaml": ""},
			want:  []Discovery{{Path: "solution.yaml"}},
		},
		{
			name:    "action mode excludes taskfiles",
			opts:    []DiscoverOption{WithDiscoveryMode(settings.DiscoveryModeAction)},
			files:   map[string]string{"taskfile.yaml": ""},
			wantErr: ErrNoSolutionFound,
		},
		{
			name: "action mode with custom action files",
			opts: []DiscoverOption{
				WithDiscoveryMode(settings.DiscoveryModeAction),
				WithCustomActionFiles([]string{"deploy.yaml"}),
			},
			files: map[string]string{"deploy.yaml": "", "actions.yaml": "", "taskfile.yaml": solutionTaskfile},
			want: []Discovery{
				{Path: "deploy.yaml", IsActionFile: true, AlternatePath: "taskfile.yaml"},
				{Path: "taskfile.yaml"},
			},
		},
		{
			name:  "solution mode reports adjacent actions file",
			opts:  []DiscoverOption{WithDiscoveryMode(settings.DiscoveryModeSolution)},
			files: map[string]string{"scafctl/solution.yaml": "", "scafctl/actions.yml": "", "actions.yaml": ""},
			want:  []Discovery{{Path: "scafctl/solution.yaml", AlternatePath: "scafctl/actions.yml"}},
		},
		{
			name:  "solution mode without actions file has no alternate",
			opts:  []DiscoverOption{WithDiscoveryMode(settings.DiscoveryModeSolution)},
			files: map[string]string{"solution.yaml": ""},
			want:  []Discovery{{Path: "solution.yaml"}},
		},
		{
			name:  "default mode never sets alternate",
			files: map[string]string{"solution.yaml": "", "actions.yaml": ""},
			want:  []Discovery{{Path: "solution.yaml"}},
		},
		{
			name:    "go-task taskfile is skipped",
			files:   map[string]string{"taskfile.yaml": goTaskfile},
			wantErr: ErrNoSolutionFound,
		},
		{
			name:  "go-task taskfile is skipped in favor of a later match",
			files: map[string]string{"scafctl/taskfile.yml": goTaskfile, "solution.yaml": ""},
			want:  []Discovery{{Path: "solution.yaml"}},
		},
		{
			name:  "taskfile with only a kind is kept",
			files: map[string]string{"taskfile.yml": "kind: Solution\n"},
			want:  []Discovery{{Path: "taskfile.yml"}},
		},
		{
			name:  "taskfile with only a spec is kept",
			files: map[string]string{"taskfile.yml": "spec:\n  resolvers: {}\n"},
			want:  []Discovery{{Path: "taskfile.yml"}},
		},
		{
			name:  "unparseable taskfile is kept so loading reports the error",
			files: map[string]string{"taskfile.yaml": "apiVersion: [\n"},
			want:  []Discovery{{Path: "taskfile.yaml"}},
		},
		{
			name:  "non-taskfile names are not inspected",
			files: map[string]string{"solution.yaml": goTaskfile},
			want:  []Discovery{{Path: "solution.yaml"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, tt.files, tt.dirs...)
			ctx := provider.WithWorkingDirectory(t.Context(), dir)

			got, err := NewDiscoverLoader(nil, tt.binaryName, tt.opts...).Discover(ctx)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, got)
				assert.Contains(t, err.Error(), dir)
				if tt.wantErrText != "" {
					assert.Contains(t, err.Error(), tt.wantErrText)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, absDiscoveries(dir, tt.want), got)
		})
	}
}

func TestDiscoverLoader_Discover_StatFunc(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"solution.yaml": ""})
	ctx := provider.WithWorkingDirectory(t.Context(), dir)

	var calls []string
	stat := func(name string) (os.FileInfo, error) {
		calls = append(calls, name)
		return nil, os.ErrNotExist
	}

	dl := NewDiscoverLoader(nil, "scafctl", WithStatFunc(stat))
	_, err := dl.Discover(ctx)
	require.ErrorIs(t, err, ErrNoSolutionFound)
	assert.Len(t, calls, len(dl.folders)*len(dl.fileNames))
	assert.Equal(t, filepath.Join(dir, "scafctl", "solution.yaml"), calls[0])
}

func TestDiscoverLoader_Load(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		files     map[string]string
		fileOpts  []FileOption
		opts      []DiscoverOption
		wantPath  string
		wantName  string
		wantOther []string
		wantErr   error
		wantErrIn []string
	}{
		{
			name:     "single match",
			files:    map[string]string{"solution.yaml": validSolutionYAML},
			wantPath: "solution.yaml",
			wantName: "test-solution",
		},
		{
			name:      "low risk loads first match",
			files:     map[string]string{"scafctl/solution.yaml": validSolutionYAML, "solution.yaml": "not: [valid"},
			wantPath:  "scafctl/solution.yaml",
			wantName:  "test-solution",
			wantOther: []string{"solution.yaml"},
		},
		{
			name:      "high risk rejects multiple matches",
			files:     map[string]string{"scafctl/solution.yaml": validSolutionYAML, "solution.yaml": validSolutionYAML},
			opts:      []DiscoverOption{WithDiscoveryRisk(DiscoveryRiskHigh)},
			wantErr:   ErrAmbiguousSolution,
			wantErrIn: []string{filepath.Join("scafctl", "solution.yaml"), "-f/--file"},
		},
		{
			name:     "high risk allows single match",
			files:    map[string]string{"solution.yaml": validSolutionYAML},
			opts:     []DiscoverOption{WithDiscoveryRisk(DiscoveryRiskHigh)},
			wantPath: "solution.yaml",
			wantName: "test-solution",
		},
		{
			name:    "nothing found",
			wantErr: ErrNoSolutionFound,
		},
		{
			name:      "file loader error is returned",
			files:     map[string]string{"solution.json": undefinedDepJSON},
			wantErrIn: []string{"loading solution"},
		},
		{
			name:     "file loader options are honoured",
			files:    map[string]string{"solution.json": undefinedDepJSON},
			fileOpts: []FileOption{WithLenient()},
			wantPath: "solution.json",
			wantName: "lenient",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, tt.files)
			ctx := provider.WithWorkingDirectory(t.Context(), dir)

			dl := NewDiscoverLoader(NewFileLoader(tt.fileOpts...), "scafctl", tt.opts...)
			got, err := dl.Load(ctx, source.Classification{Kind: source.KindAutoDiscover})
			if tt.wantErr != nil || len(tt.wantErrIn) > 0 {
				require.Error(t, err)
				if tt.wantErr != nil {
					require.ErrorIs(t, err, tt.wantErr)
				}
				for _, s := range tt.wantErrIn {
					assert.Contains(t, err.Error(), s)
				}
				assert.Nil(t, got)
				return
			}
			require.NoError(t, err)

			wantPath := filepath.Join(dir, filepath.FromSlash(tt.wantPath))
			assert.Equal(t, tt.wantName, got.Solution.Metadata.Name)
			assert.Equal(t, wantPath, got.Solution.GetPath())
			assert.Equal(t, source.Classification{Kind: source.KindLocalFile, Ref: wantPath}, got.Source)
			require.NotNil(t, got.Discovery)
			assert.Equal(t, wantPath, got.Discovery.Path)

			var wantOther []string
			for _, p := range tt.wantOther {
				wantOther = append(wantOther, filepath.Join(dir, filepath.FromSlash(p)))
			}
			assert.Equal(t, wantOther, got.OtherMatches)
		})
	}
}

func TestDiscoverLoader_Load_UnsupportedKinds(t *testing.T) {
	t.Parallel()

	dl := NewDiscoverLoader(nil, "scafctl")
	for _, kind := range allKinds {
		if kind == source.KindAutoDiscover {
			continue
		}
		t.Run(kind.String(), func(t *testing.T) {
			t.Parallel()
			got, err := dl.Load(t.Context(), source.Classification{Kind: kind, Ref: "ref"})
			require.ErrorIs(t, err, ErrUnsupportedKind)
			assert.Contains(t, err.Error(), kind.String())
			assert.Nil(t, got)
		})
	}
}

func TestNewDiscoverLoader(t *testing.T) {
	t.Parallel()

	t.Run("defaults", func(t *testing.T) {
		t.Parallel()
		dl := NewDiscoverLoader(nil, "")
		assert.NotNil(t, dl.files)
		assert.NotNil(t, dl.statFunc)
		assert.Equal(t, settings.DiscoveryModeDefault, dl.mode)
		assert.Equal(t, DiscoveryRiskLow, dl.risk)
		assert.Equal(t, settings.SolutionFoldersFor(settings.CliBinaryName), dl.folders)
		assert.Equal(t, settings.SolutionFileNamesFor(settings.CliBinaryName), dl.fileNames)
	})

	t.Run("options", func(t *testing.T) {
		t.Parallel()
		files := NewFileLoader()
		custom := []string{"deploy.yaml"}
		dl := NewDiscoverLoader(files, "mycli",
			WithDiscoveryMode(settings.DiscoveryModeAction),
			WithCustomActionFiles(custom),
			WithDiscoveryRisk(DiscoveryRiskHigh),
		)
		assert.Same(t, files, dl.files)
		assert.Equal(t, settings.DiscoveryModeAction, dl.mode)
		assert.Equal(t, DiscoveryRiskHigh, dl.risk)
		assert.Equal(t, custom, dl.customActionFiles)
		assert.Equal(t, settings.SolutionFoldersFor("mycli"), dl.folders)
		assert.Equal(t, settings.FileNamesForMode(settings.DiscoveryModeAction, "mycli", custom), dl.fileNames)
	})
}

// verboseWriterCtx returns ctx with a verbose, colorless writer whose stderr
// is captured in the returned buffer.
func verboseWriterCtx(ctx context.Context) (context.Context, *bytes.Buffer) {
	errOut := &bytes.Buffer{}
	cliParams := settings.NewCliParams()
	cliParams.Verbose = true
	cliParams.NoColor = true
	w := writer.New(&terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: errOut}, cliParams)
	return writer.WithWriter(ctx, w), errOut
}

func TestReportDiscovery(t *testing.T) {
	t.Parallel()

	t.Run("single match prints only the chosen path", func(t *testing.T) {
		t.Parallel()
		ctx, errOut := verboseWriterCtx(context.Background())
		ReportDiscovery(ctx, &MaterializedSolution{Discovery: &Discovery{Path: "/work/solution.yaml"}})
		assert.Contains(t, errOut.String(), "Using /work/solution.yaml")
		assert.NotContains(t, errOut.String(), "Multiple solution files found")
	})

	t.Run("multiple matches prints the others first", func(t *testing.T) {
		t.Parallel()
		ctx, errOut := verboseWriterCtx(context.Background())
		ReportDiscovery(ctx, &MaterializedSolution{
			Discovery:    &Discovery{Path: "/work/solution.yaml"},
			OtherMatches: []string{"/work/scafctl.yaml"},
		})
		assert.Contains(t, errOut.String(), "Multiple solution files found (also: /work/scafctl.yaml); using first match")
		assert.Contains(t, errOut.String(), "Using /work/solution.yaml")
	})

	t.Run("explicit source prints nothing", func(t *testing.T) {
		t.Parallel()
		ctx, errOut := verboseWriterCtx(context.Background())
		ReportDiscovery(ctx, &MaterializedSolution{})
		ReportDiscovery(ctx, nil)
		assert.Empty(t, errOut.String())
	})

	t.Run("no writer in context", func(t *testing.T) {
		t.Parallel()
		assert.NotPanics(t, func() {
			ReportDiscovery(context.Background(), &MaterializedSolution{
				Discovery:    &Discovery{Path: "/work/solution.yaml"},
				OtherMatches: []string{"/work/scafctl.yaml"},
			})
		})
	})
}

func TestDiscoverPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		binaryName string
		risk       DiscoveryRisk
		files      map[string]string
		want       string
		wantErr    error
		wantOut    []string
	}{
		{
			name:    "single match",
			files:   map[string]string{"solution.yaml": ""},
			want:    "solution.yaml",
			wantOut: []string{"Using "},
		},
		{
			name:    "low risk uses first match",
			files:   map[string]string{"scafctl/solution.yaml": "", "solution.yaml": ""},
			want:    "scafctl/solution.yaml",
			wantOut: []string{"Multiple solution files found (also: ", "using first match"},
		},
		{
			name:    "high risk rejects multiple matches",
			risk:    DiscoveryRiskHigh,
			files:   map[string]string{"scafctl/solution.yaml": "", "solution.yaml": ""},
			wantErr: ErrAmbiguousSolution,
		},
		{
			name:  "high risk allows single match",
			risk:  DiscoveryRiskHigh,
			files: map[string]string{"solution.yaml": ""},
			want:  "solution.yaml",
		},
		{
			name:    "nothing found",
			wantErr: ErrNoSolutionFound,
		},
		{
			name:       "binary name from context",
			binaryName: "mycli",
			files:      map[string]string{"scafctl/solution.yaml": "", "mycli/solution.yaml": ""},
			want:       "mycli/solution.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, tt.files)
			ctx, errOut := verboseWriterCtx(provider.WithWorkingDirectory(t.Context(), dir))
			if tt.binaryName != "" {
				ctx = settings.IntoContext(ctx, &settings.Run{BinaryName: tt.binaryName})
			}

			got, err := DiscoverPath(ctx, tt.risk)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Empty(t, got)
				assert.Empty(t, errOut.String())
				return
			}
			require.NoError(t, err)
			want := filepath.Join(dir, filepath.FromSlash(tt.want))
			assert.Equal(t, want, got)
			assert.Contains(t, errOut.String(), "Using "+want)
			for _, s := range tt.wantOut {
				assert.Contains(t, errOut.String(), s)
			}
		})
	}
}

func TestReportSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		c    source.Classification
		want string
	}{
		{name: "stdin", c: source.Classification{Kind: source.KindStdin, Ref: "-"}, want: "Loading solution from stdin"},
		{name: "local file", c: source.Classification{Kind: source.KindLocalFile, Ref: "./sol.yaml"}, want: "Loading solution from: ./sol.yaml"},
		{name: "catalog", c: source.Classification{Kind: source.KindBareName, Ref: "my-app@1.0.0"}, want: "Loading solution from: my-app@1.0.0"},
		{name: "auto-discover", c: source.Classification{Kind: source.KindAutoDiscover}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, errOut := verboseWriterCtx(context.Background())
			ReportSource(ctx, tc.c)
			if tc.want == "" {
				assert.Empty(t, errOut.String())
				return
			}
			assert.Contains(t, errOut.String(), tc.want)
		})
	}

	t.Run("no writer in context", func(t *testing.T) {
		t.Parallel()
		assert.NotPanics(t, func() {
			ReportSource(context.Background(), source.Classification{Kind: source.KindLocalFile, Ref: "x.yaml"})
		})
	})
}

func BenchmarkDiscoverLoader_Load(b *testing.B) {
	dir := b.TempDir()
	writeTree(b, dir, map[string]string{"solution.yaml": validSolutionYAML})
	ctx := provider.WithWorkingDirectory(b.Context(), dir)
	dl := NewDiscoverLoader(nil, "scafctl")
	c := source.Classification{Kind: source.KindAutoDiscover}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := dl.Load(ctx, c); err != nil {
			b.Fatal(err)
		}
	}
}
