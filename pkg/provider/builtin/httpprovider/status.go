// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpprovider

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// statusAcceptance decides whether an HTTP response status code is considered
// successful. It is built from the optional acceptableStatusCodes input.
//
// When the input is not configured, a status is successful only if it is a 2xx
// code (the conventional default). When configured, a status is successful only
// if it matches one of the supplied exact codes, class shorthands (e.g. "2xx"),
// or inclusive ranges (e.g. "200-204").
type statusAcceptance struct {
	configured bool
	exact      map[int]struct{}
	classes    map[int]struct{} // first digit of the code, e.g. 2 for any 2xx
	ranges     [][2]int         // inclusive [lo, hi] ranges
}

// isSuccess reports whether the given status code should be treated as a
// successful response.
func (s statusAcceptance) isSuccess(code int) bool {
	if !s.configured {
		return code >= 200 && code < 300
	}
	return s.matches(code)
}

// matches reports whether the code satisfies any configured exact code, class,
// or range. It is only meaningful when configured is true.
func (s statusAcceptance) matches(code int) bool {
	if _, ok := s.exact[code]; ok {
		return true
	}
	if _, ok := s.classes[code/100]; ok {
		return true
	}
	for _, r := range s.ranges {
		if code >= r[0] && code <= r[1] {
			return true
		}
	}
	return false
}

// describe returns a human-readable summary of the acceptable codes for use in
// error messages. The output is sorted so the message is deterministic.
func (s statusAcceptance) describe() string {
	exactCodes := make([]int, 0, len(s.exact))
	for code := range s.exact {
		exactCodes = append(exactCodes, code)
	}
	sort.Ints(exactCodes)

	classes := make([]int, 0, len(s.classes))
	for class := range s.classes {
		classes = append(classes, class)
	}
	sort.Ints(classes)

	ranges := make([][2]int, len(s.ranges))
	copy(ranges, s.ranges)
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i][0] != ranges[j][0] {
			return ranges[i][0] < ranges[j][0]
		}
		return ranges[i][1] < ranges[j][1]
	})

	parts := make([]string, 0, len(exactCodes)+len(classes)+len(ranges))
	for _, code := range exactCodes {
		parts = append(parts, strconv.Itoa(code))
	}
	for _, class := range classes {
		parts = append(parts, fmt.Sprintf("%dxx", class))
	}
	for _, r := range ranges {
		parts = append(parts, fmt.Sprintf("%d-%d", r[0], r[1]))
	}
	return strings.Join(parts, ", ")
}

// parseAcceptableStatusCodes builds a statusAcceptance from the provider inputs.
// It returns an unconfigured statusAcceptance (configured == false) when the
// acceptableStatusCodes input is absent or empty, preserving the default
// behaviour where only 2xx responses are considered successful and non-2xx
// responses are returned without error.
//
// Each entry may be an integer (200), a class shorthand string ("2xx"), an
// inclusive range string ("200-204"), or a status code as a string ("404").
func parseAcceptableStatusCodes(inputs map[string]any) (statusAcceptance, error) {
	raw, ok := inputs[fieldAcceptableStatusCodes]
	if !ok || raw == nil {
		return statusAcceptance{}, nil
	}

	list, ok := raw.([]any)
	if !ok {
		return statusAcceptance{}, fmt.Errorf("%s must be an array, got %T", fieldAcceptableStatusCodes, raw)
	}

	acc := statusAcceptance{
		configured: true,
		exact:      make(map[int]struct{}),
		classes:    make(map[int]struct{}),
	}

	for _, item := range list {
		switch v := item.(type) {
		case int:
			acc.addExact(v)
		case int64:
			acc.addExact(int(v))
		case float64:
			if v != math.Trunc(v) {
				return statusAcceptance{}, fmt.Errorf("%s entry %v must be a whole number", fieldAcceptableStatusCodes, v)
			}
			acc.addExact(int(v))
		case string:
			if err := acc.addStringEntry(v); err != nil {
				return statusAcceptance{}, err
			}
		default:
			return statusAcceptance{}, fmt.Errorf("%s entries must be integers or strings, got %T", fieldAcceptableStatusCodes, item)
		}
	}

	// An explicitly empty list is treated as unconfigured so it does not reject
	// every response.
	if len(acc.exact) == 0 && len(acc.classes) == 0 && len(acc.ranges) == 0 {
		return statusAcceptance{}, nil
	}

	return acc, nil
}

func (s *statusAcceptance) addExact(code int) {
	s.exact[code] = struct{}{}
}

// addStringEntry parses a single string entry: a class shorthand ("2xx"), an
// inclusive range ("200-204"), or a plain status code ("404").
func (s *statusAcceptance) addStringEntry(entry string) error {
	e := strings.TrimSpace(strings.ToLower(entry))
	if e == "" {
		return fmt.Errorf("%s entry must not be empty", fieldAcceptableStatusCodes)
	}

	// Class shorthand, e.g. "2xx" (first digit 1-5 followed by "xx").
	if len(e) == 3 && e[1] == 'x' && e[2] == 'x' && e[0] >= '1' && e[0] <= '5' {
		s.classes[int(e[0]-'0')] = struct{}{}
		return nil
	}

	// Inclusive range, e.g. "200-204".
	if strings.Contains(e, "-") {
		parts := strings.SplitN(e, "-", 2)
		lo, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		hi, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err1 != nil || err2 != nil {
			return fmt.Errorf("invalid %s range %q", fieldAcceptableStatusCodes, entry)
		}
		if lo > hi {
			return fmt.Errorf("invalid %s range %q: start is greater than end", fieldAcceptableStatusCodes, entry)
		}
		s.ranges = append(s.ranges, [2]int{lo, hi})
		return nil
	}

	// Plain status code as a string.
	code, err := strconv.Atoi(e)
	if err != nil {
		return fmt.Errorf("invalid %s entry %q: expected an integer, class shorthand (e.g. \"2xx\"), or range (e.g. \"200-204\")", fieldAcceptableStatusCodes, entry)
	}
	s.addExact(code)
	return nil
}
