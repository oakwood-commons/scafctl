// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"os"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/celexp/env"
)

// TestMain wires the standard CEL environment and cache factories that
// production installs via RegisterDefaults(). Without this, custom CEL
// functions (e.g. json.unmarshal) are undeclared, so any test exercising the
// evaluate_cel path with extension functions would fail.
func TestMain(m *testing.M) {
	celexp.SetEnvFactory(env.New)
	celexp.SetCacheFactory(env.GlobalCache)
	os.Exit(m.Run())
}
