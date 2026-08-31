// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package conversion is a thin adapter that re-exports
// github.com/oakwood-commons/celexp/conversion under scafctl's original import
// path so consumers need no import changes.
package conversion

import (
	upstream "github.com/oakwood-commons/celexp/conversion"
)

// Function re-exports.
var (
	CelValueToGo      = upstream.CelValueToGo
	GoToCelValue      = upstream.GoToCelValue
	ListToObjectSlice = upstream.ListToObjectSlice
	ListToStringSlice = upstream.ListToStringSlice
	NullSafeValue     = upstream.NullSafeValue
	ToObject          = upstream.ToObject
)
