// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kvx

import "sync"

// TODO(https://github.com/oakwood-commons/kvx/issues -- file upstream):
// remove renderMu (and WithRenderLock) once kvx makes tui.CurrentTheme()
// race-safe (e.g. sync.Once/RWMutex around the lazy theme init).

// renderMu serializes all calls into the upstream kvx tui package, across
// every caller in the process -- not just calls made through the same
// OutputOptions/ViewerOptions instance, and not just within this package.
//
// tui.CurrentTheme() (github.com/oakwood-commons/kvx/internal/ui/theme.go)
// lazily initializes a package-level theme global with no synchronization:
// it reads currentTheme, and if unset, writes DefaultTheme() into it. Every
// tui.Render*/tui.Run entry point reaches this on first use, so concurrent
// renders from this package (or from tests that call them in parallel) race
// on that global under `go test -race`, and could theoretically race on real
// concurrent renders in an embedding application (e.g. a server rendering
// kvx output for multiple requests at once).
//
// Until the race is fixed upstream, serialize every render call at this
// wrapper boundary so scafctl (and its embedders) never trigger it.
// Non-interactive renders are fast, so contention there is a non-issue.
// Interactive sessions (tui.Run) are the exception: they hold this lock for
// the full duration of the user's session, so a second render (interactive
// or not) will block until that session ends. This is an accepted tradeoff
// for typical single-user CLI usage; a server-style embedder running many
// concurrent interactive sessions would serialize on this lock and should
// consider a finer-grained guard (e.g. one that only protects the lazy theme
// initialization, not the whole render) if that becomes a real workload.
var renderMu sync.Mutex

// WithRenderLock runs fn while holding renderMu, for callers outside this
// package that invoke the upstream kvx tui package directly (e.g.
// pkg/auth/loginui, which builds its own tui.Run invocations for interactive
// login flows). renderMu is unexported, so this is the only way external
// callers can participate in the same serialization as pkg/terminal/kvx's own
// render call sites; without it, those callers would be invisible to the
// guard and could still trigger the upstream race.
func WithRenderLock(fn func() error) error {
	renderMu.Lock()
	defer renderMu.Unlock()
	return fn()
}
