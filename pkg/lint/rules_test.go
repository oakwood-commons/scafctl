// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"fmt"
	"strings"
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
	// We expect exactly 69 rules — update this when new rules are added
	assert.Equal(t, 69, len(KnownRules), "expected 69 known lint rules")
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

// parseSummaryTiers parses RuleSummaryText output into a map of severity tier
// heading ("Errors"/"Warnings"/"Info") to the set of rule names listed under
// it. It mirrors the exact format produced by RuleSummaryText so the drift
// guard tests can assert every rule lands under its true severity tier.
func parseSummaryTiers(t *testing.T, summary string) map[string]map[string]bool {
	t.Helper()
	tiers := map[string]map[string]bool{}
	current := ""
	for _, line := range strings.Split(summary, "\n") {
		switch {
		case strings.HasPrefix(line, "    "): // category line: "    cat:  rule1, rule2"
			require.NotEmpty(t, current, "category line before any tier heading: %q", line)
			_, rest, ok := strings.Cut(strings.TrimSpace(line), ":")
			require.True(t, ok, "category line missing colon: %q", line)
			for _, name := range strings.Split(rest, ",") {
				name = strings.TrimSpace(name)
				if name != "" {
					tiers[current][name] = true
				}
			}
		case strings.HasPrefix(line, "  "): // tier heading: "  Errors (38):"
			heading := strings.TrimSpace(line)
			heading, _, _ = strings.Cut(heading, " ") // drop the " (N):" suffix
			current = heading
			if tiers[current] == nil {
				tiers[current] = map[string]bool{}
			}
		}
	}
	return tiers
}

func TestRuleSummaryText(t *testing.T) {
	summary := RuleSummaryText()

	// Header reports the exact total from the registry.
	assert.Contains(t, summary, fmt.Sprintf("LINT RULES (%d total):", len(KnownRules)))

	tiers := parseSummaryTiers(t, summary)

	headingForSeverity := map[string]string{
		string(SeverityError):   "Errors",
		string(SeverityWarning): "Warnings",
		string(SeverityInfo):    "Info",
	}

	// Drift guard: every rule appears exactly once, under its true severity
	// tier. This is the structural protection the hand-maintained catalog
	// lacked -- it cannot diverge from KnownRules because it is generated from
	// it and verified against it here.
	seen := map[string]int{}
	for _, tier := range tiers {
		for name := range tier {
			seen[name]++
		}
	}
	for name, rule := range KnownRules {
		wantHeading := headingForSeverity[rule.Severity]
		require.NotEmpty(t, wantHeading, "rule %q has unknown severity %q", name, rule.Severity)
		assert.Truef(t, tiers[wantHeading][name],
			"rule %q (severity %q) must be listed under %q", name, rule.Severity, wantHeading)
		assert.Equalf(t, 1, seen[name], "rule %q must appear exactly once in summary", name)
	}

	// Per-tier counts match ListRules() grouping.
	wantCounts := map[string]int{}
	for _, r := range ListRules() {
		wantCounts[headingForSeverity[r.Severity]]++
	}
	for heading, names := range tiers {
		assert.Equalf(t, wantCounts[heading], len(names),
			"tier %q count mismatch", heading)
	}
}

func TestRuleSummaryTextRegressionPins(t *testing.T) {
	// Regression pins for issue #748: the hand-maintained catalog listed these
	// two rules under the wrong tier. Assert they now render under their real
	// severity, so the drift can never silently return.
	tiers := parseSummaryTiers(t, RuleSummaryText())

	assert.True(t, tiers["Warnings"]["unused-resolver"],
		"unused-resolver is a warning, not an error")
	assert.False(t, tiers["Errors"]["unused-resolver"],
		"unused-resolver must not be listed under Errors")

	assert.True(t, tiers["Errors"]["finally-with-foreach"],
		"finally-with-foreach is an error, not a warning")
	assert.False(t, tiers["Warnings"]["finally-with-foreach"],
		"finally-with-foreach must not be listed under Warnings")
}

func BenchmarkRuleSummaryText(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = RuleSummaryText()
	}
}
