// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package ext is a thin adapter that re-exports
// github.com/oakwood-commons/celexp/ext under scafctl's original import path so
// consumers need no import changes.
package ext

import (
	upstream "github.com/oakwood-commons/celexp/ext"

	// Blank import guarantees the adapter's seam-wiring init() (pkg/celexp/bridge.go)
	// runs for any consumer that imports this ext adapter without also importing the
	// pkg/celexp root. ext.Custom() builds host.configDir()/debug.out functions whose
	// behavior depends on that wiring (config-dir branding, debug-sink routing), so the
	// wiring must be present whenever this package is used.
	_ "github.com/oakwood-commons/scafctl/pkg/celexp"
)

// Function re-exports.
var (
	All                                 = upstream.All
	BuiltIn                             = upstream.BuiltIn
	Custom                              = upstream.Custom
	SetFunctionNames                    = upstream.SetFunctionNames
	SetHomogeneousAggregateLiterals     = upstream.SetHomogeneousAggregateLiterals
	HomogeneousAggregateLiteralsEnabled = upstream.HomogeneousAggregateLiteralsEnabled
)
