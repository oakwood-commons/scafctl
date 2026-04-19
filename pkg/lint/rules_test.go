// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKnownRulesHaveRequiredFields(t *testing.T) {
	for name, rule := range KnownRules {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, name, rule.Rule, "map key must match Rule field")
			assert.NotEmpty(t, rule.Severity, "Severity required")
			assert.NotEmpty(t, rule.Category, "Category required")
			assert.NotEmpty(t, rule.Description, "Description required")
			assert.NotEmpty(t, rule.Why, "Why required")
			assert.NotEmpty(t, rule.Fix, "Fix required")

			// Severity must be valid
			validSeverity := map[string]bool{
				string(SeverityError):   true,
				string(SeverityWarning): true,
				string(SeverityInfo):    true,
			}
			assert.True(t, validSeverity[rule.Severity], "invalid severity %q for rule %q", rule.Severity, name)
		})
	}
}

func TestKnownRulesCount(t *testing.T) {
	// We expect exactly 31 rules — update this when new rules are added
	assert.Equal(t, 31, len(KnownRules), "expected 31 known lint rules")
}

func TestListRules(t *testing.T) {
	rules := ListRules()
	require.Equal(t, len(KnownRules), len(rules))

	// Verify sorted by severity then name
	severityOrder := map[string]int{
		string(SeverityError):   0,
		string(SeverityWarning): 1,
		string(SeverityInfo):    2,
	}

	for i := 1; i < len(rules); i++ {
		prev := rules[i-1]
		curr := rules[i]
		prevOrd := severityOrder[prev.Severity]
		currOrd := severityOrder[curr.Severity]
		if prevOrd == currOrd {
			assert.LessOrEqual(t, prev.Rule, curr.Rule, "rules within same severity should be alphabetical")
		} else {
			assert.Less(t, prevOrd, currOrd, "errors before warnings before info")
		}
	}
}

func TestGetRule(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		rule, ok := GetRule("empty-solution")
		assert.True(t, ok)
		assert.Equal(t, "empty-solution", rule.Rule)
		assert.Equal(t, string(SeverityError), rule.Severity)
	})

	t.Run("not found", func(t *testing.T) {
		_, ok := GetRule("nonexistent-rule")
		assert.False(t, ok)
	})
}
