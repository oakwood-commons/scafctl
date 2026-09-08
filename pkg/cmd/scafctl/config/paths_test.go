// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/adrg/xdg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// setupConfigDir points XDG_CONFIG_HOME at a temp dir and returns the resolved
// scafctl config directory (its parent is the temp dir). Uses t.Setenv, so the
// caller must not be parallel.
func setupConfigDir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	xdg.Reload()
	t.Cleanup(xdg.Reload)
	cfgDir := filepath.Join(tmp, "scafctl")
	require.NoError(t, os.MkdirAll(filepath.Join(cfgDir, "config.d"), 0o755))
	return cfgDir
}

// runPaths wires an isolated writer + IOStreams and invokes PathsOptions.Run.
func runPaths(t *testing.T, opts *PathsOptions) (stdout, stderr *bytes.Buffer) {
	t.Helper()
	stdout = &bytes.Buffer{}
	stderr = &bytes.Buffer{}
	ioStreams := terminal.NewIOStreams(nil, stdout, stderr, false)
	cliParams := settings.NewCliParams()

	opts.IOStreams = ioStreams
	opts.CliParams = cliParams
	if opts.BinaryName == "" {
		opts.BinaryName = "scafctl"
	}
	opts.KvxOutputFlags.AppName = opts.BinaryName

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	require.NoError(t, opts.Run(ctx))
	return stdout, stderr
}

// TestPaths_JSON_ContainsPathsAndNoStderrNote verifies structured output is a
// valid JSON array of rows and that no merge-order note leaks to stderr.
func TestPaths_JSON_ContainsPathsAndNoStderrNote(t *testing.T) {
	setupConfigDir(t)

	opts := &PathsOptions{}
	opts.KvxOutputFlags.Output = "json"
	stdout, stderr := runPaths(t, opts)

	var rows []pathRow
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &rows), "stdout should be a JSON array")
	require.NotEmpty(t, rows)

	var config *pathRow
	for i := range rows {
		if rows[i].Name == "Config" {
			config = &rows[i]
			break
		}
	}
	require.NotNil(t, config, "expected a Config row")
	assert.NotEmpty(t, config.Path)
	assert.Equal(t, runtime.GOOS, config.Platform)
	assert.False(t, config.Illustrative)

	assert.Empty(t, stderr.String(), "structured output must not leak the merge-order note to stderr")
}

// TestPaths_YAML_NoStderrNote verifies YAML output stays clean of the note.
func TestPaths_YAML_NoStderrNote(t *testing.T) {
	setupConfigDir(t)

	opts := &PathsOptions{}
	opts.KvxOutputFlags.Output = "yaml"
	_, stderr := runPaths(t, opts)

	assert.Empty(t, stderr.String())
}

// TestPaths_Quiet_NoStderrNote verifies quiet output stays clean of the note.
func TestPaths_Quiet_NoStderrNote(t *testing.T) {
	setupConfigDir(t)

	opts := &PathsOptions{}
	opts.KvxOutputFlags.Output = "quiet"
	stdout, stderr := runPaths(t, opts)

	assert.Empty(t, stdout.String())
	assert.Empty(t, stderr.String())
}

// TestPaths_Human_RendersMergeOrderNote verifies the merge-order note appears
// on stderr (and only stderr) in the default human format on the real platform.
func TestPaths_Human_RendersMergeOrderNote(t *testing.T) {
	cfgDir := setupConfigDir(t)
	fragPath := filepath.Join(cfgDir, "config.d", "50-clusters.yaml")
	require.NoError(t, os.WriteFile(fragPath, []byte("telemetry:\n  serviceName: x\n"), 0o600))

	opts := &PathsOptions{}
	stdout, stderr := runPaths(t, opts)

	// The kvx table lands on stdout; the note lands on stderr.
	assert.NotEmpty(t, stdout.String(), "expected kvx output on stdout")
	errStr := stderr.String()
	assert.Contains(t, errStr, "Config sources (merge order)")
	assert.Contains(t, errStr, "built-in defaults")
	assert.Contains(t, errStr, "50-clusters.yaml")
	assert.Contains(t, errStr, "environment variables")
	assert.Contains(t, errStr, "Later sources override earlier ones.")

	// Nothing merge-order-related should leak to stdout.
	assert.NotContains(t, stdout.String(), "Config sources (merge order)")
}

// TestPaths_Illustrative_NoStderrNote verifies --platform suppresses the note
// regardless of format because config sources are current-system-only.
func TestPaths_Illustrative_NoStderrNote(t *testing.T) {
	setupConfigDir(t)

	target := "linux"
	if runtime.GOOS == "linux" {
		target = "darwin"
	}

	opts := &PathsOptions{Platform: target}
	_, stderr := runPaths(t, opts)

	assert.NotContains(t, stderr.String(), "Config sources (merge order)")
}

// TestPaths_Illustrative_PlatformField verifies illustrative rows carry the
// requested platform and the illustrative flag.
func TestPaths_Illustrative_PlatformField(t *testing.T) {
	setupConfigDir(t)

	target := "linux"
	if runtime.GOOS == "linux" {
		target = "darwin"
	}

	opts := &PathsOptions{Platform: target}
	opts.KvxOutputFlags.Output = "json"
	stdout, _ := runPaths(t, opts)

	var rows []pathRow
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &rows))
	require.NotEmpty(t, rows)
	for _, r := range rows {
		assert.Equal(t, target, r.Platform, "row %q should be tagged with the requested platform", r.Name)
		assert.True(t, r.Illustrative, "row %q should be marked illustrative", r.Name)
	}
}

// TestPaths_PlatformMacosAlias verifies "macos" is normalized to "darwin".
func TestPaths_PlatformMacosAlias(t *testing.T) {
	setupConfigDir(t)

	opts := &PathsOptions{Platform: "macos"}
	opts.KvxOutputFlags.Output = "json"
	stdout, _ := runPaths(t, opts)

	var rows []pathRow
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &rows))
	require.NotEmpty(t, rows)
	assert.Equal(t, "darwin", rows[0].Platform)
}

// TestPaths_UnsupportedPlatform verifies a bad --platform yields an error.
func TestPaths_UnsupportedPlatform(t *testing.T) {
	setupConfigDir(t)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	ioStreams := terminal.NewIOStreams(nil, stdout, stderr, false)
	cliParams := settings.NewCliParams()

	opts := &PathsOptions{
		BinaryName: "scafctl",
		IOStreams:  ioStreams,
		CliParams:  cliParams,
		Platform:   "plan9",
	}
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	err := opts.Run(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported platform")
	assert.Contains(t, stderr.String(), "unsupported platform")
}

// TestPaths_WhereFilter verifies per-item CEL filtering narrows the JSON array.
func TestPaths_WhereFilter(t *testing.T) {
	setupConfigDir(t)

	opts := &PathsOptions{}
	opts.KvxOutputFlags.Output = "json"
	opts.KvxOutputFlags.Where = `_.name == "Config"`
	stdout, _ := runPaths(t, opts)

	var rows []pathRow
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &rows))
	require.Len(t, rows, 1)
	assert.Equal(t, "Config", rows[0].Name)
}

// TestPaths_TableFormat verifies -o table renders without the hand-rolled
// "<binary> Paths" header the old implementation printed.
func TestPaths_TableFormat(t *testing.T) {
	setupConfigDir(t)

	opts := &PathsOptions{}
	opts.KvxOutputFlags.Output = "table"
	stdout, _ := runPaths(t, opts)

	out := stdout.String()
	require.NotEmpty(t, out)
	assert.NotContains(t, out, "scafctl Paths", "old hand-rolled header must be gone")
	assert.Contains(t, strings.ToLower(out), "config")
}

// TestConfigSourceInfos_MarksMissingConfigFile verifies the user config file is
// reported as not present when it does not exist on disk.
func TestConfigSourceInfos_MarksMissingConfigFile(t *testing.T) {
	setupConfigDir(t)

	opts := &PathsOptions{BinaryName: "scafctl"}
	infos := opts.configSourceInfos()
	require.NotEmpty(t, infos)

	last := infos[len(infos)-1]
	assert.Equal(t, "config.yaml", last.Info.Name)
	assert.False(t, last.Exists)
	assert.Contains(t, last.Info.Description, "not present")
}
