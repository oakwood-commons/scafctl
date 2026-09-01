// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	upstream "github.com/oakwood-commons/celexp"

	celexp "github.com/oakwood-commons/scafctl/pkg/celexp"
	celexpenv "github.com/oakwood-commons/scafctl/pkg/celexp/env"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// TestMain registers the env/cache factories so EvaluateExpression resolves the
// full extension set (built-in + custom), mirroring the CLI's startup wiring in
// pkg/cmd/scafctl/defaults.go. Without this, the core falls back to a bare env.
func TestMain(m *testing.M) {
	celexp.SetEnvFactory(celexpenv.New)
	celexp.SetCacheFactory(celexpenv.GlobalCache)
	os.Exit(m.Run())
}

// TestBridge_SettingsParity confirms the adapter's init() seeded the library's
// mutable defaults from scafctl's settings, so scafctl remains the source of
// truth for the CEL cache size and cost limit.
//
// To be meaningful the assertions must be able to fail, so it first sets the
// library values to sentinels that differ from what the bridge should install,
// re-runs the wiring, and asserts the bridge overwrote them.
func TestBridge_SettingsParity(t *testing.T) {
	// Save and restore so the sentinel poke does not leak into other tests.
	origCache := upstream.DefaultCacheSize
	origCost := celexp.GetDefaultCostLimit()
	t.Cleanup(func() {
		upstream.DefaultCacheSize = origCache
		celexp.SetDefaultCostLimit(origCost)
	})

	// Poke sentinels that differ from scafctl's settings, then re-run the wiring.
	upstream.DefaultCacheSize = settings.DefaultCELCacheSize + 12345
	celexp.SetDefaultCostLimit(uint64(settings.DefaultCELCostLimit) + 12345)
	celexp.WireBridge()

	assert.Equal(t, settings.DefaultCELCacheSize, upstream.DefaultCacheSize,
		"WireBridge must seed the library cache size from scafctl settings")
	assert.Equal(t, settings.DefaultCELCacheSize, celexp.GetDefaultCacheSize(),
		"adapter getter must reflect the same value")
	assert.Equal(t, uint64(settings.DefaultCELCostLimit), celexp.GetDefaultCostLimit(),
		"WireBridge must seed the library cost limit from scafctl settings")
}

// TestBridge_HostConfigDirBranding confirms the config-dir seam is wired to
// scafctl's paths.ConfigDir, so host.configDir() honors paths.SetAppName.
func TestBridge_HostConfigDirBranding(t *testing.T) {
	const appName = "mycli-celexp-test"
	orig := paths.AppName()
	paths.SetAppName(appName)
	t.Cleanup(func() { paths.SetAppName(orig) })

	ctx := context.Background()
	out, err := celexp.EvaluateExpression(ctx, `host.configDir()`, nil, nil)
	require.NoError(t, err)
	got, ok := out.(string)
	require.True(t, ok, "host.configDir() must return a string")
	assert.Equal(t, paths.ConfigDir(), got,
		"host.configDir() must resolve via scafctl's paths.ConfigDir")
	assert.Contains(t, got, appName,
		"host.configDir() must honor paths.SetAppName branding")
}

// TestBridge_DebugSinkRouting confirms the debug-sink seam routes debug.out
// output through scafctl's *writer.Writer discovered from context.
func TestBridge_DebugSinkRouting(t *testing.T) {
	ioStreams, _, errBuf := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, &settings.Run{NoColor: true, MinLogLevel: "debug"})
	ctx := writer.WithWriter(context.Background(), w)

	// env.New auto-discovers the sink from context via the wired provider.
	env, err := celexpenv.New(ctx)
	require.NoError(t, err)

	ast, iss := env.Compile(`debug.out("hello", 42)`)
	require.NoError(t, iss.Err())
	prg, err := env.Program(ast)
	require.NoError(t, err)
	out, _, err := prg.Eval(map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, int64(42), out.Value(), "debug.out returns its passthrough value")
	assert.Contains(t, errBuf.String(), "hello",
		"debug.out output must reach scafctl's writer")
}

// TestBridge_DebugSinkNilContext confirms that with no writer in context, env
// construction and debug.out evaluation do not panic (the typed-nil hazard).
func TestBridge_DebugSinkNilContext(t *testing.T) {
	ctx := context.Background() // no writer

	env, err := celexpenv.New(ctx)
	require.NoError(t, err)

	require.NotPanics(t, func() {
		ast, iss := env.Compile(`debug.out("x", 1)`)
		require.NoError(t, iss.Err())
		prg, err := env.Program(ast)
		require.NoError(t, err)
		_, _, err = prg.Eval(map[string]any{})
		require.NoError(t, err)
	})
}

// TestEnv_NewWithWriter_NilWriter confirms the adapter's NewWithWriter tolerates
// a nil *writer.Writer (normalized to a nil Sink) without panicking.
func TestEnv_NewWithWriter_NilWriter(t *testing.T) {
	env, err := celexpenv.NewWithWriter(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, env)
}

// TestEvalAs covers the generic EvalAs/EvalAsWithContext wrappers (real code,
// not var aliases).
func TestEvalAs(t *testing.T) {
	res, err := celexp.Expression("1 + 2").Compile(nil)
	require.NoError(t, err)

	got, err := celexp.EvalAs[int64](res, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(3), got)

	gotCtx, err := celexp.EvalAsWithContext[int64](context.Background(), res, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(3), gotCtx)
}

// TestBuildDataContext_ErrorWording confirms the adapter restores scafctl's
// flag-accurate error message (--data / --file) rather than the library's
// parameter-named wording.
func TestBuildDataContext_ErrorWording(t *testing.T) {
	_, err := celexp.BuildDataContext(`{"a":1}`, "some.json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--data")
	assert.Contains(t, err.Error(), "--file")

	// Happy path still delegates to the library (nil when both empty).
	out, err := celexp.BuildDataContext("", "")
	require.NoError(t, err)
	assert.Nil(t, out)
}
