// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package aliassave

import (
	"bytes"
	"testing"

	"github.com/adrg/xdg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

func newWriter(t *testing.T) (*writer.Writer, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	io := terminal.NewIOStreams(nil, buf, buf, false)
	return writer.New(io, settings.NewCliParams()), buf
}

func isolateConfig(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	// xdg caches env-derived paths globally. Register the reload before Setenv so
	// the cleanup runs after t.Setenv restores the original env, preventing temp
	// paths from leaking into later tests in this package.
	t.Cleanup(func() { xdg.Reload() })
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_STATE_HOME", tmp)
	xdg.Reload()
}

// setKubeAlias is a reusable mutator that upserts the "prod" kube cluster alias.
func setKubeAlias(server string) Mutator {
	return func(cfg *config.Config) bool {
		if cfg.Kube.Clusters.Aliases == nil {
			cfg.Kube.Clusters.Aliases = map[string]config.ClusterAlias{}
		}
		_, existed := cfg.Kube.Clusters.Aliases["prod"]
		cfg.Kube.Clusters.Aliases["prod"] = config.ClusterAlias{Server: server}
		return existed
	}
}

func TestPersist_NewAlias(t *testing.T) {
	isolateConfig(t)
	w, buf := newWriter(t)

	Persist(w, "", "prod", setKubeAlias("https://api.prod.example.com:6443"))

	assert.Contains(t, buf.String(), `Saved alias "prod"`)
	cfg, err := config.NewManager("").Load()
	require.NoError(t, err)
	assert.Equal(t, "https://api.prod.example.com:6443", cfg.Kube.Clusters.Aliases["prod"].Server)
}

func TestPersist_OverwriteWarns(t *testing.T) {
	isolateConfig(t)
	w, buf := newWriter(t)

	Persist(w, "", "prod", setKubeAlias("https://old.example.com:6443"))
	buf.Reset()
	Persist(w, "", "prod", setKubeAlias("https://new.example.com:6443"))

	assert.Contains(t, buf.String(), "already existed and was updated")
	cfg, err := config.NewManager("").Load()
	require.NoError(t, err)
	assert.Equal(t, "https://new.example.com:6443", cfg.Kube.Clusters.Aliases["prod"].Server)
}

func TestPersist_SaveFailureWarnsNeverPanics(t *testing.T) {
	isolateConfig(t)
	w, buf := newWriter(t)

	// A directory path cannot be loaded/saved as a config file; Persist must
	// degrade to a warning (the login already succeeded), not an error/panic.
	dir := t.TempDir()
	Persist(w, dir, "prod", setKubeAlias("https://x:6443"))

	assert.Contains(t, buf.String(), `could not`)
	assert.Contains(t, buf.String(), `"prod"`)
}

func TestPersist_NilGuards(t *testing.T) {
	isolateConfig(t)
	// Must not panic with a nil writer or nil mutator.
	Persist(nil, "", "prod", setKubeAlias("https://x:6443"))
	w, _ := newWriter(t)
	Persist(w, "", "prod", nil)
}

func TestPersist_EmptyNameWarnsAndSkips(t *testing.T) {
	isolateConfig(t)
	w, buf := newWriter(t)

	mutated := false
	Persist(w, "", "   ", func(*config.Config) bool {
		mutated = true
		return false
	})

	assert.False(t, mutated, "mutate must not run for an empty alias name")
	assert.Contains(t, buf.String(), "no alias name was provided")
}

// TestPersist_StatusGoesToStderr verifies the save status never lands on stdout,
// so it cannot corrupt a command's structured output (e.g. kube login -o json).
func TestPersist_StatusGoesToStderr(t *testing.T) {
	isolateConfig(t)
	var out, errOut bytes.Buffer
	io := terminal.NewIOStreams(nil, &out, &errOut, false)
	w := writer.New(io, settings.NewCliParams())

	Persist(w, "", "prod", setKubeAlias("https://api.prod.example.com:6443"))

	assert.Empty(t, out.String(), "success status must not be written to stdout")
	assert.Contains(t, errOut.String(), `Saved alias "prod"`)
}
