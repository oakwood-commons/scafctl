// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package aliassave provides the shared post-login "--save-alias" persistence
// flow used by both `auth login` and `kube login`. It centralizes the config
// load/mutate/save sequence and the consistent warning behavior: because the
// login has already succeeded by the time an alias is persisted, a load or save
// failure is surfaced as a warning (never an error) so it cannot fail the login.
package aliassave

import (
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// Mutator applies an alias change to the loaded config and reports whether it
// overwrote an existing alias of the same name.
type Mutator func(cfg *config.Config) (overwrote bool)

// Persist loads the config at configPath, applies mutate, and saves. name is the
// alias name, used only for user-facing messages and must be non-empty. A load
// or save failure is reported via WarnStderrf and returns without error: the
// caller has already completed the login, so persistence is best-effort. All
// status messages go to stderr so they never corrupt structured stdout (e.g.
// `-o json`). Overwriting an existing alias warns (and still updates); a fresh
// alias reports success.
func Persist(w *writer.Writer, configPath, name string, mutate Mutator) {
	if w == nil || mutate == nil {
		return
	}
	if strings.TrimSpace(name) == "" {
		w.WarnStderrf("logged in, but could not save alias: no alias name was provided")
		return
	}
	mgr := config.NewManager(configPath)
	cfg, err := mgr.Load()
	if err != nil {
		w.WarnStderrf("logged in, but could not load config to save alias %q: %v", name, err)
		return
	}
	overwrote := mutate(cfg)
	if err := mgr.Save(); err != nil {
		w.WarnStderrf("logged in, but could not save alias %q: %v", name, err)
		return
	}
	if overwrote {
		w.WarnStderrf("alias %q already existed and was updated", name)
		return
	}
	w.SuccessStderrf("Saved alias %q", name)
}
