// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"bufio"
	"bytes"
	"strings"
)

// DirectiveType represents the kind of suppression directive.
type DirectiveType int

const (
	// DirectiveIgnore suppresses findings on the next line (or current line if inline).
	DirectiveIgnore DirectiveType = iota

	// DirectiveDisable starts a suppression block.
	DirectiveDisable

	// DirectiveEnable ends a suppression block.
	DirectiveEnable

	// DirectiveDisableFile suppresses findings for the entire file.
	DirectiveDisableFile
)

const (
	prefixIgnore      = "scafctl-lint-ignore"
	prefixDisable     = "scafctl-lint-disable"
	prefixEnable      = "scafctl-lint-enable"
	prefixDisableFile = "scafctl-lint-disable-file"

	// scannerMaxLine is the maximum line length for the scanner (1 MB).
	scannerMaxLine = 1 << 20
)

// Directive represents a single suppression comment in the source YAML.
type Directive struct {
	Type   DirectiveType `json:"type" yaml:"type" doc:"Type of suppression directive" maximum:"3" example:"0"`
	Rules  []string      `json:"rules" yaml:"rules" doc:"Rule names to suppress (empty means all)" maxItems:"50"`
	Line   int           `json:"line" yaml:"line" doc:"Source line number of the directive" maximum:"1000000" example:"10"`
	File   string        `json:"file,omitempty" yaml:"file,omitempty" doc:"Source file path" maxLength:"512"`
	Inline bool          `json:"inline" yaml:"inline" doc:"Whether the directive is an inline comment"`
}

// SuppressionSet holds parsed directives and tracks which ones are consumed.
type SuppressionSet struct {
	directives []Directive
	used       []bool
}

// ParseDirectives scans raw YAML bytes for suppression comments and returns
// a SuppressionSet. The scanning is line-based (not yaml.Node based) to avoid
// comment attachment quirks.
func ParseDirectives(data []byte, file string) *SuppressionSet {
	var directives []Directive
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), scannerMaxLine)

	var inBlockScalar bool
	var blockIndent int

	for lineNum := 1; scanner.Scan(); lineNum++ {
		raw := scanner.Text()
		line := strings.TrimSpace(raw)

		// Track block scalar context (| and > indicators).
		if inBlockScalar {
			if line == "" {
				continue
			}
			indent := len(raw) - len(strings.TrimLeft(raw, " \t"))
			if indent <= blockIndent {
				inBlockScalar = false
				// Fall through to parse this line normally.
			} else {
				continue
			}
		}

		if !strings.HasPrefix(line, "#") && (strings.HasSuffix(line, "|") ||
			strings.HasSuffix(line, ">") ||
			strings.HasSuffix(line, "|-") ||
			strings.HasSuffix(line, ">-") ||
			strings.HasSuffix(line, "|+") ||
			strings.HasSuffix(line, ">+")) {
			inBlockScalar = true
			blockIndent = len(raw) - len(strings.TrimLeft(raw, " \t"))
			continue
		}

		if strings.HasPrefix(line, "#") {
			// Standalone comment line.
			comment := strings.TrimSpace(strings.TrimPrefix(line, "#"))
			if d, ok := parseDirectiveComment(comment, lineNum, file, false); ok {
				directives = append(directives, d)
			}
			continue
		}

		// Check for inline comment, skipping '#' inside quoted strings.
		if idx := findInlineComment(line); idx >= 0 {
			comment := strings.TrimSpace(line[idx+1:])
			if d, ok := parseDirectiveComment(comment, lineNum, file, true); ok {
				directives = append(directives, d)
			}
		}
	}

	// Ignore scanner errors — bytes.Reader cannot produce I/O errors, and
	// lines exceeding scannerMaxLine are silently skipped (no directives lost
	// since directives are short comment lines).

	return &SuppressionSet{
		directives: directives,
		used:       make([]bool, len(directives)),
	}
}

// findInlineComment returns the index of the '#' that starts an inline YAML
// comment, or -1 if none is found. It skips '#' inside single- or
// double-quoted scalars to avoid false positives.
func findInlineComment(line string) int {
	inSingle := false
	inDouble := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				if inDouble && i > 0 && line[i-1] == '\\' {
					continue // escaped quote
				}
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble && i > 0 && line[i-1] == ' ' {
				return i
			}
		}
	}
	return -1
}

// parseDirectiveComment parses a comment string (without the leading '#') into
// a Directive. Returns false if the comment is not a suppression directive.
func parseDirectiveComment(comment string, line int, file string, inline bool) (Directive, bool) {
	// Order matters: disable-file before disable (prefix match).
	var dtype DirectiveType
	var rest string

	switch {
	case strings.HasPrefix(comment, prefixDisableFile):
		dtype = DirectiveDisableFile
		rest = strings.TrimPrefix(comment, prefixDisableFile)
	case strings.HasPrefix(comment, prefixDisable):
		dtype = DirectiveDisable
		rest = strings.TrimPrefix(comment, prefixDisable)
	case strings.HasPrefix(comment, prefixEnable):
		dtype = DirectiveEnable
		rest = strings.TrimPrefix(comment, prefixEnable)
	case strings.HasPrefix(comment, prefixIgnore):
		dtype = DirectiveIgnore
		rest = strings.TrimPrefix(comment, prefixIgnore)
	default:
		return Directive{}, false
	}

	// After the prefix, only ':' or end-of-string is valid.
	rest = strings.TrimSpace(rest)
	if rest != "" && rest[0] != ':' {
		return Directive{}, false
	}

	rules := parseRuleList(rest)

	return Directive{
		Type:   dtype,
		Rules:  rules,
		Line:   line,
		File:   file,
		Inline: inline,
	}, true
}

// parseRuleList parses the optional ": rule-a, rule-b" suffix after a directive prefix.
// Returns nil (all rules) if no colon is present.
func parseRuleList(rest string) []string {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return nil // no colon → all rules
	}
	if !strings.HasPrefix(rest, ":") {
		return nil
	}
	rest = strings.TrimPrefix(rest, ":")
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return nil
	}

	parts := strings.Split(rest, ",")
	rules := make([]string, 0, len(parts))
	for _, p := range parts {
		r := strings.TrimSpace(p)
		if r != "" {
			rules = append(rules, r)
		}
	}
	if len(rules) == 0 {
		return nil
	}
	return rules
}

// Filter removes suppressed findings from the input slice and returns the
// remaining findings. Directives that match at least one finding are marked
// as used.
func (ss *SuppressionSet) Filter(findings []*Finding) []*Finding {
	if len(ss.directives) == 0 {
		return findings
	}

	kept := make([]*Finding, 0, len(findings))
	for _, f := range findings {
		if ss.isSuppressed(f) {
			continue
		}
		kept = append(kept, f)
	}
	return kept
}

// isSuppressed checks whether a finding is suppressed by any directive.
func (ss *SuppressionSet) isSuppressed(f *Finding) bool {
	for i := range ss.directives {
		d := &ss.directives[i]

		if !directiveMatchesRule(d, f.RuleName) {
			continue
		}

		if !directiveMatchesFile(d, f) {
			continue
		}

		switch d.Type {
		case DirectiveDisableFile:
			ss.used[i] = true
			return true

		case DirectiveIgnore:
			if d.Inline {
				// Inline directive suppresses only the current line.
				if f.Line == d.Line {
					ss.used[i] = true
					return true
				}
			} else {
				// Standalone comment suppresses its own line and the next line.
				if f.Line == d.Line || f.Line == d.Line+1 {
					ss.used[i] = true
					return true
				}
			}

		case DirectiveDisable:
			// Find the matching enable directive (or EOF).
			endLine := ss.findMatchingEnable(i)
			if f.Line >= d.Line && f.Line <= endLine {
				ss.used[i] = true
				return true
			}

		case DirectiveEnable:
			// Enable directives don't suppress — they end a disable block.
		}
	}
	return false
}

// findMatchingEnable returns the line number of the matching enable directive
// for the disable directive at index i. Returns a large number (EOF sentinel)
// if no matching enable is found.
func (ss *SuppressionSet) findMatchingEnable(disableIdx int) int {
	d := &ss.directives[disableIdx]
	for j := disableIdx + 1; j < len(ss.directives); j++ {
		e := &ss.directives[j]
		if e.Type != DirectiveEnable {
			continue
		}
		if e.File != d.File {
			continue
		}
		// Match when enable covers exactly the same rules as disable.
		if rulesMatch(d.Rules, e.Rules) {
			ss.used[j] = true
			return e.Line
		}
	}
	// No matching enable → suppress to EOF.
	return 1<<31 - 1
}

// directiveMatchesRule checks if a directive applies to the given rule name.
// A directive with no rules matches all rules.
func directiveMatchesRule(d *Directive, ruleName string) bool {
	if len(d.Rules) == 0 {
		return true // all rules
	}
	for _, r := range d.Rules {
		if r == ruleName {
			return true
		}
	}
	return false
}

// directiveMatchesFile checks if a directive applies to the same file as the finding.
func directiveMatchesFile(d *Directive, f *Finding) bool {
	// If the finding has no source file info, match on any directive.
	if f.SourceFile == "" {
		return true
	}
	// If the directive has no file info, match on any finding.
	if d.File == "" {
		return true
	}
	return d.File == f.SourceFile
}

// rulesMatch checks if two rule lists match exactly (same set of rules).
// Empty list means "all rules", which only matches another empty list.
func rulesMatch(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	// Build a set from a and check b against it.
	set := make(map[string]struct{}, len(a))
	for _, r := range a {
		set[r] = struct{}{}
	}
	for _, r := range b {
		if _, ok := set[r]; !ok {
			return false
		}
	}
	return true
}

// rulesOverlap checks if two rule lists have overlap. Empty list means "all",
// which overlaps with everything.
func rulesOverlap(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	for _, ar := range a {
		for _, br := range b {
			if ar == br {
				return true
			}
		}
	}
	return false
}

// UnusedFindings returns info-level findings for suppression directives that
// did not match any lint finding. This helps detect stale or misspelled
// suppression comments.
func (ss *SuppressionSet) UnusedFindings() []*Finding {
	var findings []*Finding
	for i, d := range ss.directives {
		if ss.used[i] {
			continue
		}

		// Paired enable directives (used by a disable block) are already
		// marked as used. Unpaired enables are reported below.
		if d.Type == DirectiveEnable {
			findings = append(findings, &Finding{
				Severity:   SeverityInfo,
				Category:   "suppression",
				Location:   "directive",
				Message:    "enable directive has no matching disable",
				Suggestion: "Remove the stray enable comment or add a corresponding disable",
				RuleName:   "unused-suppression",
				Line:       d.Line,
				SourceFile: d.File,
			})
			continue
		}

		ruleDesc := "all rules"
		if len(d.Rules) > 0 {
			ruleDesc = strings.Join(d.Rules, ", ")
		}

		findings = append(findings, &Finding{
			Severity:   SeverityInfo,
			Category:   "suppression",
			Location:   "directive",
			Message:    "suppression directive did not match any finding: " + ruleDesc,
			Suggestion: "Remove the suppression comment or fix the rule name",
			RuleName:   "unused-suppression",
			Line:       d.Line,
			SourceFile: d.File,
		})
	}
	return findings
}
