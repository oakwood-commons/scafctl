// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"fmt"
	"os"
	"strings"
	"unicode"
)

// CI output mode constants.
const (
	// CIModeAuto detects supported CI environments from standard environment variables.
	CIModeAuto = "auto"

	// CIModeNone disables CI workflow commands and annotations.
	CIModeNone = "none"

	// CIModeGitHubActions emits GitHub Actions workflow commands (::group::, ::endgroup::, ::error).
	CIModeGitHubActions = "github-actions"
)

// ValidCIModes returns the list of valid CI output mode values.
func ValidCIModes() []string {
	return []string{CIModeAuto, CIModeNone, CIModeGitHubActions}
}

// ParseCIMode validates and normalizes a CI output mode string.
// An empty string resolves to CIModeAuto.
func ParseCIMode(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", CIModeAuto:
		return CIModeAuto, nil
	case CIModeNone:
		return CIModeNone, nil
	case CIModeGitHubActions:
		return CIModeGitHubActions, nil
	default:
		return "", fmt.Errorf("invalid CI output mode %q (valid: auto, none, github-actions)", raw)
	}
}

// CIDialect formats CI-specific log groups and error annotations for action execution.
type CIDialect interface {
	// Name returns the canonical dialect mode name ("none" or "github-actions").
	Name() string

	// IsCI reports whether this dialect emits CI workflow commands.
	IsCI() bool

	// GroupStart returns the line opening a collapsible log group, or "" when unsupported.
	GroupStart(actionName, description string) string

	// GroupEnd returns the line closing an open collapsible log group, or "" when unsupported.
	GroupEnd() string

	// ErrorAnnotation returns a CI error annotation line for a failed action, or "" when unsupported.
	ErrorAnnotation(actionName, message string) string

	// SanitizeInline neutralizes control characters, embedded whitespace/newlines,
	// "::" workflow command markers, and "##[" legacy command prefixes in a
	// single-line human banner segment. No-op when IsCI() is false.
	SanitizeInline(text string) string

	// SanitizeMultiline normalizes line endings (including CR, NEL, LS, and PS),
	// neutralizes any line whose leading Unicode-trimmed characters begin a "::"
	// workflow command, and neutralizes "##[" legacy command prefixes anywhere on
	// a line while preserving multi-line diagnostic structure. No-op when IsCI()
	// is false.
	SanitizeMultiline(text string) string
}

// ResolveCIDialect resolves the active CIDialect from a mode string and environment lookup.
// When getenv is nil, os.Getenv is used.
func ResolveCIDialect(mode string, getenv func(string) string) (CIDialect, error) {
	parsed, err := ParseCIMode(mode)
	if err != nil {
		return nil, err
	}
	if getenv == nil {
		getenv = os.Getenv
	}
	switch parsed {
	case CIModeNone:
		return PlainCIDialect(), nil
	case CIModeGitHubActions:
		return GitHubActionsDialect(), nil
	case CIModeAuto:
		if getenv("GITHUB_ACTIONS") == "true" {
			return GitHubActionsDialect(), nil
		}
		return PlainCIDialect(), nil
	default:
		return PlainCIDialect(), nil
	}
}

type plainCIDialect struct{}

// PlainCIDialect returns a CIDialect that emits no CI workflow commands.
func PlainCIDialect() CIDialect {
	return plainCIDialect{}
}

func (plainCIDialect) Name() string                       { return CIModeNone }
func (plainCIDialect) IsCI() bool                         { return false }
func (plainCIDialect) GroupStart(_, _ string) string      { return "" }
func (plainCIDialect) GroupEnd() string                   { return "" }
func (plainCIDialect) ErrorAnnotation(_, _ string) string { return "" }
func (plainCIDialect) SanitizeInline(text string) string  { return text }
func (plainCIDialect) SanitizeMultiline(text string) string {
	return text
}

type githubActionsDialect struct{}

// GitHubActionsDialect returns a CIDialect that emits GitHub Actions workflow commands.
func GitHubActionsDialect() CIDialect {
	return githubActionsDialect{}
}

func (githubActionsDialect) Name() string { return CIModeGitHubActions }
func (githubActionsDialect) IsCI() bool   { return true }

func (githubActionsDialect) GroupStart(actionName, description string) string {
	return "::group::" + sanitizeGitHubGroupTitle(actionName, description)
}

func (githubActionsDialect) GroupEnd() string {
	return "::endgroup::"
}

func sanitizeAnnotationText(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\u0085' || r == '\u2028' || r == '\u2029':
			b.WriteByte('\n')
		case r == '\r' || r == '\n' || r == '\t' || !unicode.IsControl(r):
			b.WriteRune(r)
		}
	}
	return neutralizeLegacyHashCommands(b.String())
}

func (githubActionsDialect) ErrorAnnotation(actionName, message string) string {
	title := strings.TrimSpace(sanitizeAnnotationText(actionName))
	if title == "" {
		title = "action"
	}
	return fmt.Sprintf("::error title=%s::%s", escapeGitHubProperty(title), escapeGitHubData(sanitizeAnnotationText(message)))
}

func neutralizeLegacyHashCommands(s string) string {
	for strings.Contains(s, "##[") {
		s = strings.ReplaceAll(s, "##[", "# #[")
	}
	return s
}

func (githubActionsDialect) SanitizeInline(text string) string {
	if text == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		switch {
		case unicode.IsSpace(r) || r == '\u2028' || r == '\u2029':
			b.WriteByte(' ')
		case unicode.IsControl(r):
			// Strip non-printable control characters.
		default:
			b.WriteRune(r)
		}
	}
	collapsed := strings.Join(strings.Fields(b.String()), " ")
	for strings.Contains(collapsed, "::") {
		collapsed = strings.ReplaceAll(collapsed, "::", ": :")
	}
	return neutralizeLegacyHashCommands(collapsed)
}

func (githubActionsDialect) SanitizeMultiline(text string) string {
	if text == "" {
		return ""
	}
	// Normalize CRLF, bare CR, and Unicode line separators (NEL/LS/PS) to LF,
	// and neutralize both "::" workflow commands and "##[" legacy hash prefixes.
	normalized := strings.ReplaceAll(text, "\r\n", "\n")

	var clean strings.Builder
	clean.Grow(len(normalized))
	for _, r := range normalized {
		switch {
		case r == '\r' || r == '\u0085' || r == '\u2028' || r == '\u2029':
			clean.WriteByte('\n')
		case r == '\n' || r == '\t' || !unicode.IsControl(r):
			clean.WriteRune(r)
		}
	}

	lines := strings.Split(clean.String(), "\n")
	for i, line := range lines {
		// GitHub Actions runner trims all Unicode leading whitespace (e.g. \u00a0 NBSP)
		// before checking whether a line starts with "::".
		trimmed := strings.TrimLeftFunc(line, unicode.IsSpace)
		for strings.HasPrefix(trimmed, "::") {
			indent := line[:len(line)-len(trimmed)]
			line = indent + ": :" + trimmed[2:]
			trimmed = strings.TrimLeftFunc(line, unicode.IsSpace)
		}
		lines[i] = neutralizeLegacyHashCommands(line)
	}
	return strings.Join(lines, "\n")
}

var (
	githubDataReplacer = strings.NewReplacer(
		"%", "%25",
		"\r", "%0D",
		"\n", "%0A",
	)
	githubPropertyReplacer = strings.NewReplacer(
		"%", "%25",
		"\r", "%0D",
		"\n", "%0A",
		":", "%3A",
		",", "%2C",
	)
)

// escapeGitHubData escapes workflow command message data per GitHub Actions runner rules.
func escapeGitHubData(s string) string {
	return githubDataReplacer.Replace(s)
}

// escapeGitHubProperty escapes workflow command property values (such as title=).
func escapeGitHubProperty(s string) string {
	return githubPropertyReplacer.Replace(s)
}

// sanitizeGitHubGroupTitle formats "<actionName>: <description>" as a single safe line
// so embedded newlines, control characters, or "::" sequences cannot inject workflow commands.
func sanitizeGitHubGroupTitle(actionName, description string) string {
	name := sanitizeGitHubGroupSegment(actionName)
	if name == "" {
		name = "action"
	}
	desc := sanitizeGitHubGroupSegment(description)
	if desc == "" {
		return name
	}
	return name + ": " + desc
}

func sanitizeGitHubGroupSegment(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case unicode.IsSpace(r) || r == '\u2028' || r == '\u2029':
			b.WriteByte(' ')
		case unicode.IsControl(r):
			// Drop non-printable control characters.
		case r == '%':
			b.WriteString("%25")
		default:
			b.WriteRune(r)
		}
	}
	collapsed := strings.Join(strings.Fields(b.String()), " ")
	for strings.Contains(collapsed, "::") {
		collapsed = strings.ReplaceAll(collapsed, "::", ":")
	}
	collapsed = neutralizeLegacyHashCommands(collapsed)
	return strings.Trim(collapsed, ": ")
}
