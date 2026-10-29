// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package packagecmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
)

// TestMain points every XDG directory at a temporary home so tests never read
// or write the developer's real local catalog or build cache: package
// solution opens the catalog on every run. The environment variables (not
// just the xdg package variables) are set so they survive the xdg.Reload
// calls individual tests make.
func TestMain(m *testing.M) {
	os.Exit(runIsolated(m))
}

func runIsolated(m *testing.M) int {
	home, err := os.MkdirTemp("", "scafctl-packagecmd-test")
	if err != nil {
		fmt.Fprintf(os.Stderr, "creating test XDG home: %v\n", err)
		return 1
	}
	defer os.RemoveAll(home)

	for _, env := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME"} {
		if err := os.Setenv(env, filepath.Join(home, env)); err != nil {
			fmt.Fprintf(os.Stderr, "setting %s: %v\n", env, err)
			return 1
		}
	}
	xdg.Reload()
	return m.Run()
}
