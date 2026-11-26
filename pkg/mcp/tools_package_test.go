// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/adrg/xdg"
	"github.com/go-logr/logr"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandlePackageSolution(t *testing.T) {
	xdgDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdgDir)
	t.Setenv("XDG_CACHE_HOME", xdgDir)
	xdg.Reload()
	t.Cleanup(func() { xdg.Reload() })

	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("defaults to dry_run=true and verifies bundle discovery", func(t *testing.T) {
		solDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(solDir, "data.txt"), []byte("ok"), 0o600))
		solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: mcp-pkg-test
  version: 1.0.0
spec:
  resolvers:
    data:
      description: Read local file
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: ./data.txt
  workflow:
    actions:
      print:
        description: Print data
        provider: message
        inputs:
          message:
            rslvr: data
`
		solPath := filepath.Join(solDir, "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(solYAML), 0o600))

		req := mcp.CallToolRequest{}
		req.Params.Name = "package_solution"
		req.Params.Arguments = map[string]any{
			"cwd": solDir, // auto-discovers solution.yaml when path is omitted
		}

		res, err := srv.handlePackageSolution(context.Background(), req)
		require.NoError(t, err)
		require.False(t, res.IsError, "expected success, got: %+v", res.Content)

		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &parsed))
		assert.Equal(t, true, parsed["dryRun"])
		assert.Equal(t, "mcp-pkg-test@1.0.0", parsed["reference"])
		bundle := parsed["bundle"].(map[string]any)
		assert.Equal(t, float64(1), bundle["fileCount"])
	})

	t.Run("returns structured VALIDATION_ERROR when path escapes bundle root", func(t *testing.T) {
		outer := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(outer, "parent.txt"), []byte("x"), 0o600))
		inner := filepath.Join(outer, "nested")
		require.NoError(t, os.MkdirAll(inner, 0o700))

		solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: escape-mcp
  version: 1.0.0
spec:
  resolvers:
    bad:
      description: Parent path read
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: ../parent.txt
`
		solPath := filepath.Join(inner, "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(solYAML), 0o600))

		req := mcp.CallToolRequest{}
		req.Params.Name = "package_solution"
		req.Params.Arguments = map[string]any{
			"path": solPath,
		}

		res, err := srv.handlePackageSolution(context.Background(), req)
		require.NoError(t, err)
		assert.True(t, res.IsError)
		text := res.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "VALIDATION_ERROR")
		assert.Contains(t, text, "path escapes bundle root")
		assert.Contains(t, text, "base_dir")

		// Passing base_dir enclosing parent.txt succeeds.
		okReq := mcp.CallToolRequest{}
		okReq.Params.Name = "package_solution"
		okReq.Params.Arguments = map[string]any{
			"path":     solPath,
			"base_dir": outer,
		}
		// Note: relative to base_dir=outer, ../parent.txt would still escape outer,
		// whereas ./parent.txt inside outer succeeds when base_dir=outer.
		fixedYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: base-dir-mcp
  version: 1.0.0
spec:
  resolvers:
    good:
      description: Shared root read
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: ./parent.txt
`
		require.NoError(t, os.WriteFile(solPath, []byte(fixedYAML), 0o600))
		okRes, err := srv.handlePackageSolution(context.Background(), okReq)
		require.NoError(t, err)
		assert.False(t, okRes.IsError)
	})

	t.Run("strict mode fails on unmatched bundle.include glob", func(t *testing.T) {
		solDir := t.TempDir()
		solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: strict-glob-mcp
  version: 1.0.0
bundle:
  include:
    - nonexistent/**
spec:
  resolvers:
    val:
      description: Static value
      resolve:
        with:
          - provider: static
            inputs:
              value: ok
`
		solPath := filepath.Join(solDir, "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(solYAML), 0o600))

		req := mcp.CallToolRequest{}
		req.Params.Name = "package_solution"
		req.Params.Arguments = map[string]any{
			"path":   solPath,
			"strict": true,
		}

		res, err := srv.handlePackageSolution(context.Background(), req)
		require.NoError(t, err)
		assert.True(t, res.IsError)
		text := res.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "matches no bundled files")
	})

	t.Run("stores into isolated local catalog when dry_run=false and handles error cases", func(t *testing.T) {
		solDir := t.TempDir()
		solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: stored-mcp-sol
  version: 1.0.0
spec:
  resolvers:
    val:
      description: Static value
      resolve:
        with:
          - provider: static
            inputs:
              value: stored
  workflow:
    actions:
      msg:
        description: Emit message
        provider: message
        inputs:
          message:
            rslvr: val
`
		solPath := filepath.Join(solDir, "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(solYAML), 0o600))

		storeReq := mcp.CallToolRequest{}
		storeReq.Params.Name = "package_solution"
		storeReq.Params.Arguments = map[string]any{
			"path":      solPath,
			"dry_run":   false,
			"no_bundle": true,
			"no_vendor": true,
		}
		storeRes, err := srv.handlePackageSolution(context.Background(), storeReq)
		require.NoError(t, err)
		require.False(t, storeRes.IsError, "expected store success, got: %+v", storeRes.Content)

		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(storeRes.Content[0].(mcp.TextContent).Text), &parsed))
		assert.Equal(t, false, parsed["dryRun"])
		assert.NotEmpty(t, parsed["digest"])

		// Duplicate store without force fails with suggestion to pass force: true.
		dupRes, err := srv.handlePackageSolution(context.Background(), storeReq)
		require.NoError(t, err)
		assert.True(t, dupRes.IsError)
		assert.Contains(t, dupRes.Content[0].(mcp.TextContent).Text, "force: true")

		// Name override without force fails with INVALID_INPUT.
		renameReq := mcp.CallToolRequest{}
		renameReq.Params.Name = "package_solution"
		renameReq.Params.Arguments = map[string]any{
			"path": solPath,
			"name": "other-name",
		}
		renameRes, err := srv.handlePackageSolution(context.Background(), renameReq)
		require.NoError(t, err)
		assert.True(t, renameRes.IsError)
		assert.Contains(t, renameRes.Content[0].(mcp.TextContent).Text, "INVALID_INPUT")

		// Stdin '-' rejected cleanly.
		stdinReq := mcp.CallToolRequest{}
		stdinReq.Params.Name = "package_solution"
		stdinReq.Params.Arguments = map[string]any{"path": "-"}
		stdinRes, err := srv.handlePackageSolution(context.Background(), stdinReq)
		require.NoError(t, err)
		assert.True(t, stdinRes.IsError)
		assert.Contains(t, stdinRes.Content[0].(mcp.TextContent).Text, "stdin")

		// Invalid cwd rejected cleanly.
		badCwdReq := mcp.CallToolRequest{}
		badCwdReq.Params.Name = "package_solution"
		badCwdReq.Params.Arguments = map[string]any{"cwd": filepath.Join(solDir, "missing-dir")}
		badCwdRes, err := srv.handlePackageSolution(context.Background(), badCwdReq)
		require.NoError(t, err)
		assert.True(t, badCwdRes.IsError)

		// Invalid base_dir rejected cleanly with field base_dir.
		badBaseReq := mcp.CallToolRequest{}
		badBaseReq.Params.Name = "package_solution"
		badBaseReq.Params.Arguments = map[string]any{
			"path":     solPath,
			"base_dir": filepath.Join(solDir, "missing-base"),
		}
		badBaseRes, err := srv.handlePackageSolution(context.Background(), badBaseReq)
		require.NoError(t, err)
		assert.True(t, badBaseRes.IsError)
		assert.Contains(t, badBaseRes.Content[0].(mcp.TextContent).Text, "base_dir")

		// Invalid semver version maps to INVALID_INPUT.
		badVerReq := mcp.CallToolRequest{}
		badVerReq.Params.Name = "package_solution"
		badVerReq.Params.Arguments = map[string]any{
			"path":    solPath,
			"version": "not-a-semver",
		}
		badVerRes, err := srv.handlePackageSolution(context.Background(), badVerReq)
		require.NoError(t, err)
		assert.True(t, badVerRes.IsError)
		assert.Contains(t, badVerRes.Content[0].(mcp.TextContent).Text, "INVALID_INPUT")

		// Explicit development placeholder version maps ErrDevVersion -> INVALID_INPUT.
		noVerFile := filepath.Join(t.TempDir(), "solution.yaml")
		require.NoError(t, os.WriteFile(noVerFile, []byte("apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: unversioned-mcp\nspec: {}\n"), 0o600))
		noVerReq := mcp.CallToolRequest{}
		noVerReq.Params.Name = "package_solution"
		noVerReq.Params.Arguments = map[string]any{
			"path":    noVerFile,
			"version": "0.0.0-dev",
		}
		noVerRes, err := srv.handlePackageSolution(context.Background(), noVerReq)
		require.NoError(t, err)
		assert.True(t, noVerRes.IsError)
		assert.Contains(t, noVerRes.Content[0].(mcp.TextContent).Text, "version")

		// Lint error maps ErrPreflightBlocked -> VALIDATION_ERROR with lint error count.
		lintErrFile := filepath.Join(t.TempDir(), "solution.yaml")
		lintErrYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: lint-fail-mcp
  version: 1.0.0
spec:
  resolvers:
    selfRef:
      description: Self reference lint error
      resolve:
        with:
          - provider: static
            inputs:
              value: ok
      transform:
        with:
          - provider: cel
            inputs:
              expression:
                expr: "_.selfRef"
`
		require.NoError(t, os.WriteFile(lintErrFile, []byte(lintErrYAML), 0o600))
		lintReq := mcp.CallToolRequest{}
		lintReq.Params.Name = "package_solution"
		lintReq.Params.Arguments = map[string]any{"path": lintErrFile}
		lintRes, err := srv.handlePackageSolution(context.Background(), lintReq)
		require.NoError(t, err)
		assert.True(t, lintRes.IsError)
		assert.Contains(t, lintRes.Content[0].(mcp.TextContent).Text, "lint error(s)")

		// Missing file maps to LOAD_FAILED.
		missingReq := mcp.CallToolRequest{}
		missingReq.Params.Name = "package_solution"
		missingReq.Params.Arguments = map[string]any{"path": filepath.Join(solDir, "does-not-exist.yaml")}
		missingRes, err := srv.handlePackageSolution(context.Background(), missingReq)
		require.NoError(t, err)
		assert.True(t, missingRes.IsError)
		assert.Contains(t, missingRes.Content[0].(mcp.TextContent).Text, "LOAD_FAILED")

		// Ambiguous auto-discovery maps ErrAmbiguousSolution -> INVALID_INPUT with list_solutions hint.
		ambigDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(ambigDir, "solution.yaml"), []byte(solYAML), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(ambigDir, "scafctl.yaml"), []byte(solYAML), 0o600))
		ambigReq := mcp.CallToolRequest{}
		ambigReq.Params.Name = "package_solution"
		ambigReq.Params.Arguments = map[string]any{"cwd": ambigDir}
		ambigRes, err := srv.handlePackageSolution(context.Background(), ambigReq)
		require.NoError(t, err)
		assert.True(t, ambigRes.IsError)
		assert.Contains(t, ambigRes.Content[0].(mcp.TextContent).Text, "Multiple solution files")
	})

	t.Run("embedder binary name appears in package_solution description", func(t *testing.T) {
		embedSrv, err := NewServer(WithServerName("mycli"), WithServerVersion("test"))
		require.NoError(t, err)

		infoBytes, err := embedSrv.Info()
		require.NoError(t, err)
		assert.Contains(t, string(infoBytes), "mycli package solution")
	})

	t.Run("preserves request cancellation and refreshed config via freshConfigContext", func(t *testing.T) {
		solDir := t.TempDir()
		solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: ctx-cancel-mcp
  version: 1.0.0
spec: {}
`
		solPath := filepath.Join(solDir, "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(solYAML), 0o600))

		cancelCtx, cancel := context.WithCancel(context.Background())
		cancel()

		req := mcp.CallToolRequest{}
		req.Params.Name = "package_solution"
		req.Params.Arguments = map[string]any{
			"path":      solPath,
			"dry_run":   false,
			"no_bundle": true,
			"no_vendor": true,
			"skip_lint": true,
		}
		res, err := srv.handlePackageSolution(cancelCtx, req)
		require.NoError(t, err)
		require.True(t, res.IsError)
		assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "context canceled")

		// Startup config has signatures off; reloaded config enables enforce without
		// trusted issuers, which must override startup s.ctx during plugin vendoring.
		cfgSrv, err := NewServer(WithServerVersion("test"), WithServerConfig(&config.Config{}))
		require.NoError(t, err)
		cfgSrv.cfgReloader = newConfigReloader(&config.Config{
			Plugins: config.PluginsConfig{
				Signatures: config.PluginSignaturesConfig{Mode: "enforce"},
			},
		}, time.Minute, logr.Discard())

		pluginSolYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: sig-policy-mcp
  version: 1.0.0
bundle:
  plugins:
    - name: demo
      kind: provider
      version: "^1.0.0"
spec: {}
`
		pluginSolPath := filepath.Join(t.TempDir(), "solution.yaml")
		require.NoError(t, os.WriteFile(pluginSolPath, []byte(pluginSolYAML), 0o600))

		sigReq := mcp.CallToolRequest{}
		sigReq.Params.Name = "package_solution"
		sigReq.Params.Arguments = map[string]any{
			"path":      pluginSolPath,
			"skip_lint": true,
		}
		sigRes, err := cfgSrv.handlePackageSolution(context.Background(), sigReq)
		require.NoError(t, err)
		require.True(t, sigRes.IsError)
		assert.Contains(t, sigRes.Content[0].(mcp.TextContent).Text, "plugin signature policy")
	})
}
