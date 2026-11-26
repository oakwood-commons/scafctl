// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/kvx/pkg/core"
	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/inspect"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const taskRunnerActionsYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: repo-tasks
  version: 1.0.0
spec:
  resolvers:
    broken:
      resolve:
        with:
          - provider: broken-from
  workflow:
    actions:
      build:
        alias: b
        description: Compile the binary
        explicit: true
        provider: message
        inputs:
          message: "building"
      lint:
        alias: l
        description: Run linters
        explicit: true
        provider: message
        inputs:
          message: "linting"
      test:
        description: Run unit tests
        explicit: true
        provider: message
        inputs:
          message: "testing"
`

func TestCommandAction_ListFlag(t *testing.T) {
	t.Parallel()

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cmd := CommandAction(cliParams, streams, "")

	f := cmd.Flags().Lookup("list")
	require.NotNil(t, f)
	assert.Equal(t, "l", f.Shorthand)

	v, err := cmd.Flags().GetBool("list")
	require.NoError(t, err)
	assert.False(t, v)
}

func TestActionOptions_RunList(t *testing.T) {
	t.Parallel()

	t.Run("lists actions as JSON without running resolvers, fetching plugins, or creating output-dir", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		solPath := filepath.Join(tmpDir, "actions.yaml")
		solWithUninstalledPlugin := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: plugin-tasks
  version: 1.0.0
bundle:
  plugins:
    - name: nonexistent-external-plugin
      kind: provider
      version: "^1.0.0"
spec:
  resolvers:
    broken:
      resolve:
        with:
          - provider: broken-from
  workflow:
    actions:
      check:
        description: Default check
        provider: message
        inputs:
          message: "checking"
      release:
        alias: rel
        description: Publish artifacts
        explicit: true
        provider: message
        inputs:
          message: "releasing"
`
		require.NoError(t, os.WriteFile(solPath, []byte(solWithUninstalledPlugin), 0o600))

		nonExistentOutDir := filepath.Join(tmpDir, "must-not-be-created")
		var brokenCalls atomic.Int32
		var stdout, stderr bytes.Buffer
		cliParams := settings.NewCliParams()
		cliParams.ExitOnError = false
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}

		opts := &ActionOptions{
			BinaryName: "mycli",
			List:       true,
			sharedResolverOptions: sharedResolverOptions{
				IOStreams:      streams,
				CliParams:      cliParams,
				File:           solPath,
				OutputDir:      nonExistentOutDir,
				KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
				registry:       actionScopedTestRegistry(newCountingFailResolverProvider(&brokenCalls)),
			},
		}

		ctx := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(streams, cliParams))
		require.NoError(t, opts.Run(ctx))

		assert.Zero(t, brokenCalls.Load(), "resolvers must not run during --list")
		_, statErr := os.Stat(nonExistentOutDir)
		assert.True(t, os.IsNotExist(statErr), "--output-dir must not be created during --list")

		var got []inspect.ActionListEntry
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &got))
		require.Len(t, got, 2)
		assert.Equal(t, "check", got[0].Name)
		assert.Empty(t, got[0].Alias)
		assert.True(t, got[0].Default)
		assert.Equal(t, "Default check", got[0].Description)
		assert.Contains(t, got[0].Command, "mycli run solution -f ")

		assert.Equal(t, "release", got[1].Name)
		assert.Equal(t, "rel", got[1].Alias)
		assert.False(t, got[1].Default)
		assert.Equal(t, "Publish artifacts", got[1].Description)
		assert.Contains(t, got[1].Command, "mycli run action release -f ")
	})

	t.Run("auto-discovers actions.yaml ahead of solution.yaml", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "actions.yaml"), []byte(taskRunnerActionsYAML), 0o600))
		otherSol := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: main-solution
  version: 1.0.0
spec:
  workflow:
    actions:
      deploy:
        provider: message
        inputs:
          message: "deploy"
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "solution.yaml"), []byte(otherSol), 0o600))

		var stdout, stderr bytes.Buffer
		cliParams := settings.NewCliParams()
		cliParams.ExitOnError = false
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}

		opts := &ActionOptions{
			List: true,
			sharedResolverOptions: sharedResolverOptions{
				IOStreams:      streams,
				CliParams:      cliParams,
				KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
			},
		}

		ctx := provider.WithWorkingDirectory(
			writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(streams, cliParams)),
			tmpDir,
		)
		require.NoError(t, opts.Run(ctx))

		var got []inspect.ActionListEntry
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &got))
		require.Len(t, got, 3)
		assert.Equal(t, "build", got[0].Name)
		assert.Equal(t, "lint", got[1].Name)
		assert.Equal(t, "test", got[2].Name)
	})

	t.Run("supports stdin -f - and CEL -w / -e filters without missing-key errors", func(t *testing.T) {
		t.Parallel()
		var stdout, stderr bytes.Buffer
		cliParams := settings.NewCliParams()
		cliParams.ExitOnError = false
		streams := &terminal.IOStreams{
			In:     io.NopCloser(strings.NewReader(taskRunnerActionsYAML)),
			Out:    &stdout,
			ErrOut: &stderr,
		}

		opts := &ActionOptions{
			List: true,
			sharedResolverOptions: sharedResolverOptions{
				IOStreams: streams,
				CliParams: cliParams,
				File:      "-",
				KvxOutputFlags: flags.KvxOutputFlags{
					Output: "json",
					Where:  `!_.default && _.alias != ""`,
				},
			},
		}

		ctx := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(streams, cliParams))
		require.NoError(t, opts.Run(ctx))

		var got []inspect.ActionListEntry
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &got))
		require.Len(t, got, 2)
		assert.Equal(t, "build", got[0].Name)
		assert.Equal(t, "scafctl run action build -f -", got[0].Command)
		assert.Equal(t, "lint", got[1].Name)
	})

	t.Run("supports positional URL reference with --list", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/x-yaml")
			_, _ = w.Write([]byte(taskRunnerActionsYAML))
		}))
		defer srv.Close()

		var stdout, stderr bytes.Buffer
		cliParams := settings.NewCliParams()
		cliParams.ExitOnError = false
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}

		opts := &ActionOptions{
			List:  true,
			Names: []string{srv.URL + "/actions.yaml"},
			sharedResolverOptions: sharedResolverOptions{
				IOStreams:      streams,
				CliParams:      cliParams,
				KvxOutputFlags: flags.KvxOutputFlags{Output: "yaml"},
			},
		}

		allowPriv := true
		appCfg := &config.Config{
			HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &allowPriv},
		}
		ctx := config.WithConfig(
			writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(streams, cliParams)),
			appCfg,
		)
		require.NoError(t, opts.Run(ctx))

		var got []inspect.ActionListEntry
		require.NoError(t, yaml.Unmarshal(stdout.Bytes(), &got))
		require.Len(t, got, 3)
		assert.Equal(t, "scafctl run action build -s "+srv.URL+"/actions.yaml", got[0].Command)
	})

	t.Run("empty action list emits [] in structured output and stderr notice in human mode", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		solPath := filepath.Join(tmpDir, "finally-only.yaml")
		finallyOnly := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: finally-only
  version: 1.0.0
spec:
  workflow:
    finally:
      cleanup:
        provider: message
        inputs:
          message: "cleaning"
`
		require.NoError(t, os.WriteFile(solPath, []byte(finallyOnly), 0o600))

		// JSON -> "[]"
		var jsonOut, jsonErr bytes.Buffer
		cliParams := settings.NewCliParams()
		cliParams.ExitOnError = false
		jsonStreams := &terminal.IOStreams{Out: &jsonOut, ErrOut: &jsonErr}
		jsonOpts := &ActionOptions{
			List: true,
			sharedResolverOptions: sharedResolverOptions{
				IOStreams:      jsonStreams,
				CliParams:      cliParams,
				File:           solPath,
				KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
			},
		}
		ctxJSON := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(jsonStreams, cliParams))
		require.NoError(t, jsonOpts.Run(ctxJSON))
		assert.Equal(t, "[]\n", jsonOut.String())

		// Human default -> clean stdout, warning on stderr.
		var humOut, humErr bytes.Buffer
		humStreams := &terminal.IOStreams{Out: &humOut, ErrOut: &humErr}
		humOpts := &ActionOptions{
			List: true,
			sharedResolverOptions: sharedResolverOptions{
				IOStreams: humStreams,
				CliParams: cliParams,
				File:      solPath,
			},
		}
		ctxHum := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(humStreams, cliParams))
		require.NoError(t, humOpts.Run(ctxHum))
		assert.Empty(t, humOut.String())
		assert.Contains(t, humErr.String(), `No actions defined in solution "finally-only"`)
	})
}

func TestActionOptions_RunList_Rejections(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solPath := filepath.Join(tmpDir, "actions.yaml")
	require.NoError(t, os.WriteFile(solPath, []byte(taskRunnerActionsYAML), 0o600))
	emptyDir := t.TempDir()

	tests := []struct {
		name      string
		workDir   string
		configure func(*ActionOptions)
		wantErr   string
	}{
		{
			name:    "rejects --dry-run",
			workDir: tmpDir,
			configure: func(o *ActionOptions) {
				o.File = solPath
				o.DryRun = true
			},
			wantErr: "--list cannot be combined with --dry-run",
		},
		{
			name:    "rejects -r resolver params",
			workDir: tmpDir,
			configure: func(o *ActionOptions) {
				o.File = solPath
				o.ResolverParams = []string{"env=prod"}
			},
			wantErr: "--list does not accept resolver parameters",
		},
		{
			name:    "rejects positional key=value params",
			workDir: tmpDir,
			configure: func(o *ActionOptions) {
				o.File = solPath
				o.DynamicArgs = []string{"env=prod"}
			},
			wantErr: "--list does not accept resolver parameters",
		},
		{
			name:    "rejects action names when solution file is present",
			workDir: tmpDir,
			configure: func(o *ActionOptions) {
				o.Names = []string{"build"}
			},
			wantErr: "--list does not accept action names",
		},
		{
			name:    "rejects action names even in empty directory",
			workDir: emptyDir,
			configure: func(o *ActionOptions) {
				o.Names = []string{"build"}
			},
			wantErr: "--list does not accept action names",
		},
		{
			name:    "rejects -o test before loading source",
			workDir: tmpDir,
			configure: func(o *ActionOptions) {
				o.File = filepath.Join(tmpDir, "nonexistent.yaml")
				o.Output = "test"
			},
			wantErr: "not supported with --list",
		},
		{
			name:    "rejects invalid output format",
			workDir: tmpDir,
			configure: func(o *ActionOptions) {
				o.File = solPath
				o.Output = "bogus"
			},
			wantErr: "invalid output format",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			cliParams := settings.NewCliParams()
			cliParams.ExitOnError = false
			streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}

			opts := &ActionOptions{
				List: true,
				sharedResolverOptions: sharedResolverOptions{
					IOStreams: streams,
					CliParams: cliParams,
				},
			}
			tc.configure(opts)

			ctx := provider.WithWorkingDirectory(
				writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(streams, cliParams)),
				tc.workDir,
			)
			err := opts.Run(ctx)
			require.Error(t, err)
			assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestActionOptions_RunList_KvxSnapshotAndEmbedder(t *testing.T) {
	t.Parallel()

	_, ds, err := tui.ParseSchemaWithDisplay(actionListSchemaJSON)
	require.NoError(t, err)
	require.NotNil(t, ds)
	assert.Equal(t, "name", ds.List.TitleField)
	assert.Equal(t, "description", ds.List.SubtitleField)
	assert.Empty(t, ds.List.BadgeFields, "empty alias strings and bools must not render unlabeled badge pills")

	entries := []inspect.ActionListEntry{
		{
			Name:        "build",
			Alias:       "b",
			Default:     false,
			Description: "Compile the binary for the current platform",
			Command:     "mycli run action build",
		},
		{
			Name:        "check",
			Alias:       "",
			Default:     true,
			Description: "Run fast formatting and lint checks",
			Command:     "mycli run solution",
		},
	}

	root, err := core.LoadObject(entries)
	require.NoError(t, err)

	for _, noColor := range []bool{true, false} {
		for _, width := range []int{95, 120} {
			snap := tui.RenderSnapshot(root, tui.Config{
				AppName:       "mycli run action",
				Width:         width,
				Height:        24,
				NoColor:       noColor,
				DisplaySchema: ds,
			})
			assert.Contains(t, snap, "build")
			assert.Contains(t, snap, "Compile the binary")
			assert.NotContains(t, snap, `[{"alias"`, "must render card list rather than raw JSON blob")
		}
	}

	rootItems, ok := root.([]any)
	require.True(t, ok)
	detailSnap := tui.RenderDetailView(rootItems[0], tui.DetailViewOptions{
		Width:   100,
		Height:  30,
		Schema:  ds,
		NoColor: true,
	})
	assert.Contains(t, detailSnap, "build")
	assert.Contains(t, detailSnap, "Properties")
	assert.Contains(t, detailSnap, "mycli run action build")

	opts := &ActionOptions{
		BinaryName:            "mycli",
		sharedResolverOptions: sharedResolverOptions{CliParams: settings.NewCliParams()},
	}
	kvxOpts := opts.actionListOutputOptions(context.Background())
	assert.Equal(t, "mycli run action", kvxOpts.AppName)
	assert.Equal(t, []string{"name", "alias", "default", "description"}, kvxOpts.ColumnOrder)
	assert.True(t, kvxOpts.ColumnHints["command"].Hidden)
	assert.True(t, kvxOpts.ColumnHints["description"].Flex)
}

func TestActionOptions_Run_AllExplicitBareInvocation(t *testing.T) {
	t.Parallel()

	t.Run("human output lists actions on stderr, skips resolvers and output-dir creation, exits InvalidInput", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		solPath := filepath.Join(tmpDir, "actions.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(taskRunnerActionsYAML), 0o600))
		outDir := filepath.Join(tmpDir, "uncreated-out")

		var brokenCalls atomic.Int32
		var stdout, stderr bytes.Buffer
		cliParams := settings.NewCliParams()
		cliParams.ExitOnError = false
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}

		opts := &ActionOptions{
			ActionTimeout: 10 * time.Second,
			sharedResolverOptions: sharedResolverOptions{
				IOStreams:       streams,
				CliParams:       cliParams,
				File:            solPath,
				OutputDir:       outDir,
				ResolverTimeout: 10 * time.Second,
				PhaseTimeout:    30 * time.Second,
				registry:        actionScopedTestRegistry(newCountingFailResolverProvider(&brokenCalls)),
			},
		}

		ctx := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(streams, cliParams))
		err := opts.Run(ctx)
		require.Error(t, err)
		assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
		assert.Contains(t, err.Error(), "no default actions; specify one of: build, lint, test")
		assert.Empty(t, stdout.String())
		assert.Contains(t, stderr.String(), "Available actions:")
		assert.Contains(t, stderr.String(), "  build (b)  Compile the binary")
		assert.Contains(t, stderr.String(), "  lint (l)   Run linters")
		assert.Contains(t, stderr.String(), "  test       Run unit tests")
		assert.Zero(t, brokenCalls.Load(), "resolvers must not run when all actions are explicit")

		_, statErr := os.Stat(outDir)
		assert.True(t, os.IsNotExist(statErr), "--output-dir must not be created on disk when run fails with no default actions")
	})

	t.Run("json output emits structured failure envelope and quiet suppresses Available actions list", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		solPath := filepath.Join(tmpDir, "actions.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(taskRunnerActionsYAML), 0o600))

		// JSON mode
		var brokenCalls atomic.Int32
		var jsonOut, jsonErr bytes.Buffer
		cliParams := settings.NewCliParams()
		cliParams.ExitOnError = false
		jsonStreams := &terminal.IOStreams{Out: &jsonOut, ErrOut: &jsonErr}
		jsonOpts := &ActionOptions{
			sharedResolverOptions: sharedResolverOptions{
				IOStreams:      jsonStreams,
				CliParams:      cliParams,
				File:           solPath,
				KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
				registry:       actionScopedTestRegistry(newCountingFailResolverProvider(&brokenCalls)),
			},
		}
		ctxJSON := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(jsonStreams, cliParams))
		err := jsonOpts.Run(ctxJSON)
		require.Error(t, err)
		assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
		assert.Contains(t, jsonErr.String(), "Available actions:")
		var envelope map[string]any
		require.NoError(t, json.Unmarshal(jsonOut.Bytes(), &envelope))
		assert.Equal(t, "failed", envelope["status"])

		// Quiet mode (-o quiet)
		var quietOut, quietErr bytes.Buffer
		quietStreams := &terminal.IOStreams{Out: &quietOut, ErrOut: &quietErr}
		quietOpts := &ActionOptions{
			sharedResolverOptions: sharedResolverOptions{
				IOStreams:      quietStreams,
				CliParams:      cliParams,
				File:           solPath,
				KvxOutputFlags: flags.KvxOutputFlags{Output: "quiet"},
				registry:       actionScopedTestRegistry(newCountingFailResolverProvider(&brokenCalls)),
			},
		}
		ctxQuiet := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(quietStreams, cliParams))
		err = quietOpts.Run(ctxQuiet)
		require.Error(t, err)
		assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
		assert.Empty(t, quietOut.String())
		assert.NotContains(t, quietErr.String(), "Available actions:")
	})

	t.Run("preserves finally-only workflows and relative --output-dir resolution against invoking CWD", func(t *testing.T) {
		t.Parallel()
		solDir := t.TempDir()
		invokeDir := t.TempDir()
		solPath := filepath.Join(solDir, "finally.yaml")
		finallySol := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: finally-only
  version: 1.0.0
spec:
  workflow:
    finally:
      cleanup:
        provider: file
        inputs:
          operation: write
          path: marker.txt
          content: "done"
`
		require.NoError(t, os.WriteFile(solPath, []byte(finallySol), 0o600))

		var stdout, stderr bytes.Buffer
		cliParams := settings.NewCliParams()
		cliParams.ExitOnError = false
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}

		opts := &ActionOptions{
			ActionTimeout: 10 * time.Second,
			sharedResolverOptions: sharedResolverOptions{
				IOStreams:       streams,
				CliParams:       cliParams,
				File:            solPath,
				OutputDir:       "rel-out",
				KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
				ResolverTimeout: 10 * time.Second,
				PhaseTimeout:    30 * time.Second,
				registry:        actionScopedTestRegistry(),
			},
		}

		ctx := provider.WithWorkingDirectory(
			writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(streams, cliParams)),
			invokeDir,
		)
		require.NoError(t, opts.Run(ctx), stderr.String())

		// Relative --output-dir resolved against invokeDir, not solDir.
		_, err := os.Stat(filepath.Join(invokeDir, "rel-out", "marker.txt"))
		require.NoError(t, err)
		_, solDirErr := os.Stat(filepath.Join(solDir, "rel-out"))
		assert.True(t, os.IsNotExist(solDirErr))
	})
}

func TestActionOptions_RunList_CatalogAndVersionSelectors(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	localCat, err := catalog.NewLocalCatalogAt(filepath.Join(tmpDir, "scafctl", "catalog"), logr.Discard())
	require.NoError(t, err)

	catalogSolYAML := []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: scoped-tasks
  version: 1.2.0-beta.1
spec:
  workflow:
    actions:
      check:
        description: Default check
        provider: message
        inputs:
          message: "check"
      release:
        alias: rel
        description: Explicit release
        explicit: true
        provider: message
        inputs:
          message: "release"
`)
	ref := catalog.Reference{
		Kind:    catalog.ArtifactKindSolution,
		Name:    "scoped-tasks",
		Version: semver.MustParse("1.2.0-beta.1"),
	}
	_, err = localCat.Store(context.Background(), ref, catalogSolYAML, nil, nil, false)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false
	streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}

	opts := &ActionOptions{
		List: true,
		sharedResolverOptions: sharedResolverOptions{
			IOStreams:         streams,
			CliParams:         cliParams,
			Solution:          "scoped-tasks",
			Catalog:           "local",
			VersionConstraint: "^1.0.0-0",
			PreRelease:        true,
			KvxOutputFlags:    flags.KvxOutputFlags{Output: "json"},
		},
	}

	ctx := provider.WithWorkingDirectory(
		writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(streams, cliParams)),
		t.TempDir(),
	)
	require.NoError(t, opts.Run(ctx), stderr.String())

	var got []inspect.ActionListEntry
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &got))
	require.Len(t, got, 2)
	assert.Equal(t, "scafctl run solution scoped-tasks --catalog local --version '^1.0.0-0' --pre-release", got[0].Command)
	assert.Equal(t, "scafctl run action release -s scoped-tasks --catalog local --version '^1.0.0-0' --pre-release", got[1].Command)
}

func TestActionOptions_RunList_QuotesFilePathWithSpaces(t *testing.T) {
	t.Parallel()
	spaceDir := filepath.Join(t.TempDir(), "task files")
	require.NoError(t, os.MkdirAll(spaceDir, 0o755))
	solPath := filepath.Join(spaceDir, "actions.yaml")
	mixedYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: mixed-tasks
  version: 1.0.0
spec:
  resolvers:
    mode:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: mode
              default: dev
  workflow:
    actions:
      check:
        description: Default check
        provider: message
        inputs:
          message: "check"
      deploy:
        description: Gated deploy
        when:
          expr: '_.mode == "prod"'
        provider: message
        inputs:
          message: "deploy"
      release:
        description: Explicit release
        explicit: true
        provider: message
        inputs:
          message: "release"
`
	require.NoError(t, os.WriteFile(solPath, []byte(mixedYAML), 0o600))

	var stdout, stderr bytes.Buffer
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false
	streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}

	opts := &ActionOptions{
		List: true,
		sharedResolverOptions: sharedResolverOptions{
			IOStreams:      streams,
			CliParams:      cliParams,
			File:           solPath,
			KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
		},
	}

	ctx := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(streams, cliParams))
	require.NoError(t, opts.Run(ctx), stderr.String())

	var got []inspect.ActionListEntry
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &got))
	require.Len(t, got, 3)

	quotedPath := "'" + strings.ReplaceAll(solPath, "'", `'\''`) + "'"
	assert.Equal(t, "scafctl run solution -f "+quotedPath, got[0].Command)
	assert.Equal(t, "scafctl run solution -f "+quotedPath+" -r mode=prod", got[1].Command)
	assert.Equal(t, "scafctl run action release -f "+quotedPath, got[2].Command)
}

func BenchmarkActionOptions_RunList(b *testing.B) {
	tmpDir := b.TempDir()
	solPath := filepath.Join(tmpDir, "actions.yaml")
	require.NoError(b, os.WriteFile(solPath, []byte(taskRunnerActionsYAML), 0o600))

	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var stdout, stderr bytes.Buffer
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
		opts := &ActionOptions{
			List: true,
			sharedResolverOptions: sharedResolverOptions{
				IOStreams:      streams,
				CliParams:      cliParams,
				File:           solPath,
				KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
			},
		}
		ctx := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(streams, cliParams))
		_ = opts.Run(ctx)
	}
}
