// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package detail is a thin adapter that re-exports
// github.com/oakwood-commons/celexp/detail under scafctl's original import path
// so consumers need no import changes. The detail functions accept
// *celexp.ExtFunction / celexp.ExtFunctionList; because the adapter's celexp
// types are aliases of the library types, these signatures line up directly.
package detail

import (
	upstream "github.com/oakwood-commons/celexp/detail"
)

// Function re-exports.
var (
	BuildFunctionDetail = upstream.BuildFunctionDetail
	BuildFunctionList   = upstream.BuildFunctionList
)
