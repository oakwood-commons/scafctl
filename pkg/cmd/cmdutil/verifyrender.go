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
func RenderVerifyResult(w *writer.Writer, result *bundler.VerifyResult) {
	if w == nil || result == nil {
		return
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
			w.Errorf("    ✗ %s -- %s", e.Path, e.Reason)
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
