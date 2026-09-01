// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package env is a thin adapter over github.com/oakwood-commons/celexp/env. It
// re-exports the library's env constructors and bridges scafctl's original
// *writer.Writer-based API (NewWithWriter / DebugOutEnvOptions) onto the
// library's debug.Sink-based equivalents, normalizing a typed-nil *writer.Writer
// to a nil Sink interface so the library's nil checks behave correctly.
package env

import (
	"context"

	"github.com/google/cel-go/cel"

	upstream "github.com/oakwood-commons/celexp/env"
	"github.com/oakwood-commons/celexp/ext/debug"

	// Blank import guarantees the adapter's seam-wiring init() (pkg/celexp/bridge.go)
	// runs for any consumer that imports this env adapter without also importing the
	// pkg/celexp root. env.New wires debug.out to the debug-sink provider registered
	// by that init(); without it, debug output is silently dropped and host.configDir()
	// loses scafctl's branding.
	_ "github.com/oakwood-commons/scafctl/pkg/celexp"

	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// GlobalCacheSize re-exports the library constant.
const GlobalCacheSize = upstream.GlobalCacheSize

// Function re-exports without scafctl-specific behaviour.
var (
	New         = upstream.New
	GlobalCache = upstream.GlobalCache
)

// NewWithWriter is scafctl's original constructor. It normalizes a possibly
// typed-nil *writer.Writer to a nil debug.Sink interface and delegates to the
// library's NewWithSink. scafctl's *writer.Writer satisfies debug.Sink via its
// DebugOutf method.
func NewWithWriter(ctx context.Context, w *writer.Writer, declarations ...cel.EnvOption) (*cel.Env, error) {
	return upstream.NewWithSink(ctx, sinkFromWriter(w), declarations...)
}

// DebugOutEnvOptions is scafctl's original helper. It normalizes a possibly
// typed-nil *writer.Writer to a nil debug.Sink interface and delegates to the
// library's Sink-based version.
func DebugOutEnvOptions(w *writer.Writer) []cel.EnvOption {
	return upstream.DebugOutEnvOptions(sinkFromWriter(w))
}

// sinkFromWriter converts a *writer.Writer to a debug.Sink, returning a nil
// interface (not a non-nil interface wrapping a nil pointer) when w is nil.
func sinkFromWriter(w *writer.Writer) debug.Sink {
	if w == nil {
		return nil
	}
	return w
}
