// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package lint provides the lint command for validating solutions.
// Rule metadata has been extracted to pkg/lint for reuse across
// CLI, MCP, and future API layers.
package lint

import (
	pkglint "github.com/oakwood-commons/scafctl/pkg/lint"
)

// RuleMeta re-export from pkg/lint for backward compatibility.
type RuleMeta = pkglint.RuleMeta

// KnownRules re-exports the authoritative rule registry from pkg/lint.
var KnownRules = pkglint.KnownRules

// ListRules delegates to pkg/lint.ListRules.
func ListRules() []RuleMeta {
	return pkglint.ListRules()
}

// GetRule delegates to pkg/lint.GetRule.
func GetRule(name string) (RuleMeta, bool) {
	return pkglint.GetRule(name)
}
