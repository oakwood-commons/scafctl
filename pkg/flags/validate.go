// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package flags

import (
	"fmt"
	"sort"
	"strings"

	"github.com/agext/levenshtein"
)

// maxSuggestionDistance is the maximum Levenshtein distance for a key to be
// considered a plausible typo of a known key.
const maxSuggestionDistance = 3

// ValidateInputKeys checks that every key in inputs exists in validKeys.
// Unknown keys produce an error with a "did you mean?" suggestion when a
// close match (Levenshtein distance ≤ maxSuggestionDistance) is found.
//
// contextName is used in error messages (e.g. "provider \"http\"" or "solution").
func ValidateInputKeys(inputs map[string]any, validKeys []string, contextName string) error {
	if len(inputs) == 0 || len(validKeys) == 0 {
		return nil
	}

	validSet := make(map[string]bool, len(validKeys))
	for _, k := range validKeys {
		validSet[k] = true
	}

	// Collect unknown keys in deterministic order for stable error messages.
	unknown := make([]string, 0)
	for k := range inputs {
		if !validSet[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)

	var errs []string
	for _, key := range unknown {
		if suggestion := closestKey(key, validKeys); suggestion != "" {
			errs = append(errs, fmt.Sprintf("%s does not accept input %q — did you mean %q?", contextName, key, suggestion))
		} else {
			errs = append(errs, fmt.Sprintf("%s does not accept input %q", contextName, key))
		}
	}

	sortedValid := make([]string, len(validKeys))
	copy(sortedValid, validKeys)
	sort.Strings(sortedValid)
	if len(errs) == 1 {
		return fmt.Errorf("%s (valid inputs: %s)", errs[0], strings.Join(sortedValid, ", "))
	}
	return fmt.Errorf("unknown inputs for %s:\n  - %s\nvalid inputs: %s",
		contextName, strings.Join(errs, "\n  - "), strings.Join(sortedValid, ", "))
}

// closestKey returns the validKey with the smallest Levenshtein distance to
// key, provided it is within maxSuggestionDistance. Returns "" if no match
// is close enough.
func closestKey(key string, validKeys []string) string {
	best := ""
	bestDist := maxSuggestionDistance + 1
	for _, candidate := range validKeys {
		d := levenshtein.Distance(key, candidate, nil)
		if d < bestDist {
			bestDist = d
			best = candidate
		}
	}
	return best
}
