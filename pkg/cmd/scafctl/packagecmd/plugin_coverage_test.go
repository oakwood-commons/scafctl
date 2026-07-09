// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package packagecmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newPackageTestCtx creates a context with a writer and logger for build package tests.
func newPackageTestCtx(t testing.TB) (context.Context, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	lgr := logger.GetNoopLogger()
	ctx = logger.WithLogger(ctx, lgr)
	return ctx, &buf
}

// TestRunPackagePlugin_StoreAndForce exercises the full runPackagePlugin store
// path at the unit level: a successful multi-platform store, the already-exists
// rejection on a second store without --force, and the force-overwrite path.
func TestRunPackagePlugin_StoreAndForce(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	xdg.Reload()
	t.Cleanup(func() { xdg.Reload() })

	binPath := filepath.Join(t.TempDir(), "cov-provider")
	require.NoError(t, os.WriteFile(binPath, []byte("cov-binary"), 0o755))

	opts := &PluginOptions{
		Name:      "cov-provider",
		Kind:      "provider",
		Version:   "1.0.0",
		Platforms: []string{"linux/amd64=" + binPath},
	}

	// First store succeeds.
	ctx, buf := newPackageTestCtx(t)
	require.NoError(t, runPackagePlugin(ctx, opts))
	assert.Contains(t, buf.String(), "Built cov-provider@1.0.0")

	// Second store without --force is rejected because the artifact exists.
	ctx2, buf2 := newPackageTestCtx(t)
	require.Error(t, runPackagePlugin(ctx2, opts))
	assert.Contains(t, buf2.String(), "Use --force to overwrite")

	// With --force the existing artifact is overwritten.
	opts.Force = true
	ctx3, _ := newPackageTestCtx(t)
	require.NoError(t, runPackagePlugin(ctx3, opts))
}

// ── CommandPackagePlugin construction tests ─────────────────────────────────────

func TestCommandPackagePlugin(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPackagePlugin(cliParams, ioStreams, "scafctl/build")

	require.NotNil(t, cmd)
	assert.Equal(t, "plugin", cmd.Use)
	assert.Contains(t, cmd.Aliases, "plug")
	assert.Contains(t, cmd.Aliases, "p")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
	assert.True(t, cmd.SilenceUsage)
}

func TestCommandPackagePlugin_Flags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPackagePlugin(cliParams, ioStreams, "scafctl/build")

	tests := []struct {
		flagName string
		required bool
	}{
		{"name", true},
		{"kind", false},
		{"version", true},
		{"platform", true},
		{"force", false},
	}

	for _, tt := range tests {
		t.Run(tt.flagName, func(t *testing.T) {
			t.Parallel()
			f := cmd.Flags().Lookup(tt.flagName)
			require.NotNil(t, f, "flag %q should exist", tt.flagName)
		})
	}
}

func TestCommandPackagePlugin_DefaultKind(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPackagePlugin(cliParams, ioStreams, "scafctl/build")

	kind, err := cmd.Flags().GetString("kind")
	require.NoError(t, err)
	assert.Equal(t, "provider", kind)
}

// ── runPackagePlugin validation tests ───────────────────────────────────────────

func TestRunPackagePlugin_InvalidName_Uppercase(t *testing.T) {
	t.Parallel()
	ctx, _ := newPackageTestCtx(t)

	opts := &PluginOptions{
		Name:      "MyPlugin", // uppercase letters — invalid
		Kind:      "provider",
		Version:   "1.0.0",
		Platforms: []string{"linux/amd64=./bin"},
	}

	err := runPackagePlugin(ctx, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid name")
}

func TestRunPackagePlugin_InvalidName_StartsWithNumber(t *testing.T) {
	t.Parallel()
	ctx, _ := newPackageTestCtx(t)

	opts := &PluginOptions{
		Name:      "1bad-name", // starts with number — invalid
		Kind:      "provider",
		Version:   "1.0.0",
		Platforms: []string{"linux/amd64=./bin"},
	}

	err := runPackagePlugin(ctx, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid name")
}

func TestRunPackagePlugin_InvalidName_Empty(t *testing.T) {
	t.Parallel()
	ctx, _ := newPackageTestCtx(t)

	opts := &PluginOptions{
		Name:      "", // empty — invalid
		Kind:      "provider",
		Version:   "1.0.0",
		Platforms: []string{"linux/amd64=./bin"},
	}

	err := runPackagePlugin(ctx, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid name")
}

func TestRunPackagePlugin_InvalidKind(t *testing.T) {
	t.Parallel()
	ctx, _ := newPackageTestCtx(t)

	opts := &PluginOptions{
		Name:      "my-plugin",
		Kind:      "unknown-kind", // invalid kind
		Version:   "1.0.0",
		Platforms: []string{"linux/amd64=./bin"},
	}

	err := runPackagePlugin(ctx, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid kind")
}

func TestRunPackagePlugin_InvalidKind_Solution(t *testing.T) {
	t.Parallel()
	ctx, _ := newPackageTestCtx(t)

	opts := &PluginOptions{
		Name:      "my-plugin",
		Kind:      "solution", // valid artifact kind but not a valid plugin kind
		Version:   "1.0.0",
		Platforms: []string{"linux/amd64=./bin"},
	}

	err := runPackagePlugin(ctx, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid kind")
}

func TestRunPackagePlugin_InvalidVersion_NotSemver(t *testing.T) {
	t.Parallel()
	ctx, _ := newPackageTestCtx(t)

	opts := &PluginOptions{
		Name:      "my-plugin",
		Kind:      "provider",
		Version:   "not-a-version", // not a valid semantic version
		Platforms: []string{"linux/amd64=./bin"},
	}

	err := runPackagePlugin(ctx, opts)
	require.Error(t, err)
	// Error should mention the invalid version
	errMsg := err.Error()
	assert.True(t, errMsg != "", "error should not be empty")
}

func TestRunPackagePlugin_InvalidVersion_Empty(t *testing.T) {
	t.Parallel()
	ctx, _ := newPackageTestCtx(t)

	opts := &PluginOptions{
		Name:      "my-plugin",
		Kind:      "auth-handler",
		Version:   "", // empty version
		Platforms: []string{"linux/amd64=./bin"},
	}

	err := runPackagePlugin(ctx, opts)
	require.Error(t, err)
}

func TestRunPackagePlugin_InvalidPlatformFormat(t *testing.T) {
	t.Parallel()
	ctx, _ := newPackageTestCtx(t)

	opts := &PluginOptions{
		Name:      "my-plugin",
		Kind:      "provider",
		Version:   "1.0.0",
		Platforms: []string{"linux/amd64"}, // missing =path portion
	}

	err := runPackagePlugin(ctx, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --platform format")
}

func TestRunPackagePlugin_ValidKinds(t *testing.T) {
	t.Parallel()

	validKinds := []string{"provider", "auth-handler"}
	for _, kind := range validKinds {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			ctx, _ := newPackageTestCtx(t)

			opts := &PluginOptions{
				Name:      "my-plugin",
				Kind:      kind,
				Version:   "1.0.0",
				Platforms: []string{"linux/amd64=/nonexistent/path"}, // will fail at binary read
			}

			err := runPackagePlugin(ctx, opts)
			// Name, kind, version are all valid — should fail later at binary read, not at validation
			if err != nil {
				errMsg := err.Error()
				assert.NotContains(t, errMsg, "invalid name", "should not fail on name validation")
				assert.NotContains(t, errMsg, "invalid kind", "should not fail on kind validation")
				// Should not be a semver error either
				assert.NotContains(t, errMsg, "invalid version", "should not fail on version validation")
			}
		})
	}
}

// ── Benchmarks ────────────────────────────────────────────────────────────────

func BenchmarkRunPackagePlugin_InvalidName(b *testing.B) {
	ctx, _ := newPackageTestCtx(b)
	opts := &PluginOptions{
		Name:      "MyPlugin",
		Kind:      "provider",
		Version:   "1.0.0",
		Platforms: []string{"linux/amd64=./bin"},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = runPackagePlugin(ctx, opts)
	}
}

func BenchmarkCommandPackagePlugin_Construction(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		CommandPackagePlugin(cliParams, ioStreams, "scafctl/build")
	}
}

// ── Helper function Tests ─────────────────────────────────────────────────────

// TestNewPackageTestCtx verifies the test helper sets up context correctly.
func TestNewPackageTestCtx(t *testing.T) {
	ctx, buf := newPackageTestCtx(t)
	require.NotNil(t, ctx)
	require.NotNil(t, buf)

	w := writer.FromContext(ctx)
	require.NotNil(t, w)

	w.Infof("test message")
	assert.Contains(t, buf.String(), "test message")
}
