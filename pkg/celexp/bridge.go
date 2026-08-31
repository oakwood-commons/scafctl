// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp

import (
	"context"

	"github.com/go-logr/logr"

	upstream "github.com/oakwood-commons/celexp"
	celexpenv "github.com/oakwood-commons/celexp/env"
	"github.com/oakwood-commons/celexp/ext/debug"
	celexphost "github.com/oakwood-commons/celexp/ext/host"

	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// init runs the adapter's seam-wiring once, before any consumer of pkg/celexp
// evaluates an expression. An init function is the correct mechanism here:
// there is no single explicit entry point in the library-consumer relationship
// to hook, and the wiring is the only scafctl-specific behaviour in the adapter
// (everything else is a pure re-export).
//
//nolint:gochecknoinits // adapter seam-wiring must run before first use; no single entry point exists
func init() {
	WireBridge()
}

// WireBridge wires scafctl's concrete providers into the celexp library's
// injectable seams and aligns the library's mutable defaults with scafctl's
// settings. It is called once from init(); it is exported so tests can re-run
// the wiring after poking library state. Calling it more than once is safe --
// each call simply re-registers the same providers and re-seeds the defaults.
func WireBridge() {
	// SEAM 1 -- LOGGER: keep scafctl's context logger flowing into the library.
	// logger.FromContext returns a *logr.Logger; deref for the library's
	// value-typed provider signature. It is documented never to return nil, but
	// this provider runs on every CEL evaluation, so guard the deref to keep a
	// future pkg/logger change from panicking the hot path.
	upstream.SetLoggerProvider(func(ctx context.Context) logr.Logger {
		if l := logger.FromContext(ctx); l != nil {
			return *l
		}
		return logr.Discard()
	})

	// SEAM 2 -- DEBUG SINK: route debug.out CEL output to scafctl's writer.
	// A nil *writer.Writer boxed into debug.Sink would be a non-nil interface
	// and defeat the library's nil check, so normalize a typed-nil pointer to
	// an explicit nil interface.
	celexpenv.SetSinkProvider(func(ctx context.Context) debug.Sink {
		w := writer.FromContext(ctx)
		if w == nil {
			return nil
		}
		return w
	})

	// SEAM 3 -- CONFIG DIR: honor scafctl's paths.SetAppName branding for the
	// host.configDir() CEL function.
	celexphost.SetConfigDirProvider(paths.ConfigDir)

	// SETTINGS PARITY: keep scafctl's settings as the source of truth for the
	// library's mutable defaults.
	upstream.DefaultCacheSize = settings.DefaultCELCacheSize
	upstream.SetDefaultCostLimit(settings.DefaultCELCostLimit)
}
