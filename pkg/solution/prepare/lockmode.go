// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package prepare

// LockMode controls how buildProviderDependency resolves external plugin
// versions. The mode determines whether a lock file is consulted and, if so,
// whether the resolved version (exact pin) or the original constraint (range)
// is surfaced as the dep's Version.
type LockMode uint8

const (
	// LockModeStrict pins dep.Version to the lock entry's resolved version
	// (lp.Version). The pool routes through ensureOneByVersion with no
	// network resolution, producing a fully deterministic run. Requires a
	// non-nil lock file.
	LockModeStrict LockMode = iota + 1

	// LockModeConstrained keeps the lock entry's original constraint
	// (lp.Constraint, falling back to dep.Version) as dep.Version. The pool
	// routes through ensureOneByConstraint and may fetch to resolve a
	// concrete version satisfying the constraint. Requires a non-nil lock
	// file.
	LockModeConstrained

	// LockModeBestEffort does not require a lock file. When a lock is
	// present it is consulted opportunistically: if a matching entry with a
	// non-empty ResolvedCanonical is found and maps to a configured catalog,
	// the lock entry's constraint and catalog are used (constrained-style).
	// When the lock is absent or the entry is missing/incomplete, resolution
	// falls back to the inline registry (sourced plugins) or leaves the
	// catalog empty for later binding by resolveProviderRefs (unsourced).
	LockModeBestEffort
)

// OrDefault returns LockModeStrict when m is the zero value or invalid.
func (m LockMode) OrDefault() LockMode {
	if m.IsValid() {
		return m
	}
	return LockModeStrict
}

// IsValid reports whether m is a defined LockMode value.
func (m LockMode) IsValid() bool {
	return m == LockModeStrict || m == LockModeConstrained || m == LockModeBestEffort
}

// String returns the lowercase name of the mode.
func (m LockMode) String() string {
	switch m {
	case LockModeStrict:
		return "strict"
	case LockModeConstrained:
		return "constrained"
	case LockModeBestEffort:
		return "bestEffort"
	default:
		return "unknown"
	}
}
