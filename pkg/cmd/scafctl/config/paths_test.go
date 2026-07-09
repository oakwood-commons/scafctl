// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
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

// TestPathsOptions_ConfigSources_JSON verifies the config.d fragments appear in
// the structured output so nothing is hidden from tooling.
func TestPathsOptions_ConfigSources_JSON(t *testing.T) {
	cfgDir := setupConfigDir(t)
	fragPath := filepath.Join(cfgDir, "config.d", "50-clusters.yaml")
	require.NoError(t, os.WriteFile(fragPath, []byte("telemetry:\n  serviceName: x\n"), 0o600))

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	opts := &PathsOptions{IOStreams: ioStreams, CliParams: cliParams, BinaryName: "scafctl"}
	opts.KvxOutputFlags.Output = "json"
	opts.KvxOutputFlags.AppName = "scafctl"

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	require.NoError(t, opts.Run(ctx))

	out := stdout.String()
	assert.Contains(t, out, "50-clusters.yaml")
	assert.Contains(t, out, "config.d")
}

// TestPathsOptions_ConfigSources_Table verifies the merge-order section is
// rendered in human output, including the fragment and the built-in/env layers.
func TestPathsOptions_ConfigSources_Table(t *testing.T) {
	cfgDir := setupConfigDir(t)
	fragPath := filepath.Join(cfgDir, "config.d", "50-clusters.yaml")
	require.NoError(t, os.WriteFile(fragPath, []byte("telemetry:\n  serviceName: x\n"), 0o600))

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()

	opts := &PathsOptions{IOStreams: ioStreams, CliParams: cliParams, BinaryName: "scafctl"}
	opts.KvxOutputFlags.AppName = "scafctl"

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	require.NoError(t, opts.Run(ctx))

	out := stdout.String()
	assert.Contains(t, out, "Config sources (merge order)")
	assert.Contains(t, out, "built-in defaults")
	assert.Contains(t, out, "50-clusters.yaml")
	assert.Contains(t, out, "environment variables")
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
