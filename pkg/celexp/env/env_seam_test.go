// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package env_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	env "github.com/oakwood-commons/scafctl/pkg/celexp/env"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// This test file deliberately imports ONLY pkg/celexp/env (plus scafctl helpers),
// NOT the pkg/celexp root, to prove the adapter's seam-wiring init() still runs
// via the env adapter's blank import of the root. Without that blank import, the
// config-dir and debug-sink seams would be unwired here and these tests would
// fail (wrong config dir, dropped debug output) -- the C1 regression guard.
//
// Note: there is intentionally no TestMain registering the env/cache factory
// here. These tests call env.New directly (not EvaluateExpression), so they do
// not need the factory -- and importing the root to register it would defeat the
// C1 guard by pulling in the root's init() through the test binary.

// TestSeamWired_HostConfigDir proves host.configDir() resolves via scafctl's
// paths.ConfigDir (config-dir seam) when importing only the env adapter.
func TestSeamWired_HostConfigDir(t *testing.T) {
	const appName = "env-seam-probe"
	orig := paths.AppName()
	paths.SetAppName(appName)
	t.Cleanup(func() { paths.SetAppName(orig) })

	e, err := env.New(context.Background())
	require.NoError(t, err)

	ast, iss := e.Compile(`host.configDir()`)
	require.NoError(t, iss.Err())
	prg, err := e.Program(ast)
	require.NoError(t, err)
	out, _, err := prg.Eval(map[string]any{})
	require.NoError(t, err)

	assert.Equal(t, paths.ConfigDir(), out.Value(),
		"host.configDir() must resolve via scafctl paths.ConfigDir (seam wired)")
	assert.Contains(t, out.Value().(string), appName,
		"host.configDir() must honor paths.SetAppName (seam wired)")
}

// TestSeamWired_DebugSink proves debug.out output reaches scafctl's writer
// (debug-sink seam) when importing only the env adapter.
func TestSeamWired_DebugSink(t *testing.T) {
	ioStreams, _, errBuf := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, &settings.Run{NoColor: true, MinLogLevel: "debug"})
	ctx := writer.WithWriter(context.Background(), w)

	e, err := env.New(ctx)
	require.NoError(t, err)

	ast, iss := e.Compile(`debug.out("seam-check", 7)`)
	require.NoError(t, iss.Err())
	prg, err := e.Program(ast)
	require.NoError(t, err)
	_, _, err = prg.Eval(map[string]any{})
	require.NoError(t, err)

	assert.Contains(t, errBuf.String(), "seam-check",
		"debug.out output must reach scafctl's writer (seam wired)")
}

// TestDebugOutEnvOptions covers the adapter's DebugOutEnvOptions wrapper with
// both a real writer and a nil writer (typed-nil normalization).
func TestDebugOutEnvOptions(t *testing.T) {
	ioStreams, _, errBuf := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, &settings.Run{NoColor: true, MinLogLevel: "debug"})

	opts := env.DebugOutEnvOptions(w)
	require.NotEmpty(t, opts)

	e, err := env.New(context.Background(), opts...)
	require.NoError(t, err)
	ast, iss := e.Compile(`debug.out("opt-check", 1)`)
	require.NoError(t, iss.Err())
	prg, err := e.Program(ast)
	require.NoError(t, err)
	_, _, err = prg.Eval(map[string]any{})
	require.NoError(t, err)
	assert.Contains(t, errBuf.String(), "opt-check")

	// Nil writer must not panic and must produce usable (no-op) options.
	require.NotPanics(t, func() {
		nilOpts := env.DebugOutEnvOptions(nil)
		_, err := env.New(context.Background(), nilOpts...)
		require.NoError(t, err)
	})
}
