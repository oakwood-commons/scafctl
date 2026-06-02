// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

// MergeParameters combines saved parameters from state with CLI parameters.
// CLI parameters take precedence (overwrite existing keys). New CLI keys are added.
// Returns the effective parameter set for this execution.
func MergeParameters(saved, cli map[string]any) map[string]any {
	merged := make(map[string]any)
	for k, v := range saved {
		merged[k] = v
	}
	for k, v := range cli {
		merged[k] = v // CLI overrides saved
	}
	return merged
}
