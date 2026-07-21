// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package cmdutil

import (
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// RenderVerifyResult formats a bundle verification result for terminal output.
//
// It groups results into sections (static paths, glob coverage, plugins) and
// renders bare (uncategorized) warnings at the end. It is shared by the
// producer (package solution) and consumer (catalog pull) verification hooks so
// both render a VerifyResult identically.
//
// When errorsAsWarnings is true, missing/failed items (VerifyResult.Errors) are
// rendered as warnings rather than errors. The consumer (catalog pull) uses this
// in non-strict mode, where incompleteness is a warning, not a failure -- so the
// output does not contradict the "pull succeeded" result with red error lines.
// The producer (package) and any --strict path pass false so failures render as
// errors.
func RenderVerifyResult(w *writer.Writer, result *bundler.VerifyResult, errorsAsWarnings bool) {
	if w == nil || result == nil {
		return
	}

	// renderFail prints a failed/missing item as either an error or a warning
	// depending on the caller's policy.
	renderFail := func(format string, args ...any) {
		if errorsAsWarnings {
			w.Warningf(format, args...)
		} else {
			w.Errorf(format, args...)
		}
	}

	// Static paths
	hasStatic := false
	for _, s := range result.Successes {
		if !strings.HasPrefix(s, "glob:") && !strings.HasPrefix(s, "plugin:") {
			if !hasStatic {
				w.Plain("")
				w.Plain("  Static paths:")
				hasStatic = true
			}
			w.Successf("    ✓ %s", s)
		}
	}
	for _, e := range result.Errors {
		if !strings.HasPrefix(e.Path, "glob:") && !strings.HasPrefix(e.Path, "plugin:") {
			if !hasStatic {
				w.Plain("")
				w.Plain("  Static paths:")
				hasStatic = true
			}
			renderFail("    ✗ %s -- %s", e.Path, e.Reason)
		}
	}

	// Glob coverage
	hasGlob := false
	for _, s := range result.Successes {
		if strings.HasPrefix(s, "glob:") {
			if !hasGlob {
				w.Plain("")
				w.Plain("  Bundle includes (glob coverage):")
				hasGlob = true
			}
			w.Successf("    ✓ %s", s[len("glob:"):])
		}
	}
	for _, warning := range result.Warnings {
		if strings.HasPrefix(warning, "pattern ") {
			if !hasGlob {
				w.Plain("")
				w.Plain("  Bundle includes (glob coverage):")
				hasGlob = true
			}
			w.Warningf("    ⚠ %s", warning)
		}
	}

	// Plugins
	hasPlugin := false
	for _, s := range result.Successes {
		if strings.HasPrefix(s, "plugin:") {
			if !hasPlugin {
				w.Plain("")
				w.Plain("  Plugins:")
				hasPlugin = true
			}
			w.Successf("    ✓ %s", s[len("plugin:"):])
		}
	}

	// Show non-categorized warnings (e.g., no-bundle warnings)
	for _, warning := range result.Warnings {
		if !strings.HasPrefix(warning, "pattern ") {
			w.Warningf("  ⚠ %s", warning)
		}
	}
}
