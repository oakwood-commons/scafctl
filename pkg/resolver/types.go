// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package resolver provides type coercion utilities.
//
// Type coercion functions have been moved to pkg/spec/types.go.
// This file is kept for backward compatibility - CoerceType is re-exported
// in resolver.go from the spec package.
package resolver

// Note: CoerceType is now defined in resolver.go as:
//   var CoerceType = spec.CoerceType
//
// For new code, import directly from pkg/spec:
//   import "github.com/oakwood-commons/scafctl/pkg/spec"
//   result, err := spec.CoerceType(value, spec.TypeString)
