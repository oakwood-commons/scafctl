// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package identity defines plugin identity value types and trusted signing
// identity template validation and expansion.
package identity

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// PlaceholderName is the trusted-identity template token expanded to the
	// verified artifact's short name (e.g., "github", "entra", "my-solution").
	PlaceholderName = "{name}"

	// PlaceholderKind is the trusted-identity template token expanded to the
	// verified artifact's catalog kind ("solution", "provider", "auth-handler").
	PlaceholderKind = "{kind}"
)

// ErrInvalidIdentityPattern indicates that a trusted identity pattern is empty,
// contains whitespace or control characters, has unbalanced or nested braces,
// or references an unknown placeholder token.
var ErrInvalidIdentityPattern = errors.New("invalid trusted identity pattern")

// HasPlaceholders reports whether pattern contains any supported artifact
// identity placeholder ({name} or {kind}).
func HasPlaceholders(pattern string) bool {
	return strings.Contains(pattern, PlaceholderName) || strings.Contains(pattern, PlaceholderKind)
}

// ValidateTrustedIdentityPattern checks that pattern is a non-empty, valid UTF-8
// glob/template string free of whitespace, control characters, unbalanced or
// nested braces, empty "{}" tokens, and unsupported "{...}" placeholders.
// Only {name} and {kind} are accepted placeholder tokens.
func ValidateTrustedIdentityPattern(pattern string) error {
	_, err := parseTrustedIdentityPattern(pattern, "", "", false)
	return err
}

// ExpandTrustedIdentityPattern validates pattern and substitutes {name} and
// {kind} in a single pass so substituted values are never re-scanned for
// placeholder tokens. Callers remain responsible for validating that name and
// kind satisfy artifact naming rules before trusting the expanded pattern.
func ExpandTrustedIdentityPattern(pattern, name, kind string) (string, error) {
	return parseTrustedIdentityPattern(pattern, name, kind, true)
}

func parseTrustedIdentityPattern(pattern, name, kind string, expand bool) (string, error) {
	if pattern == "" {
		return "", fmt.Errorf("%w: pattern must not be empty", ErrInvalidIdentityPattern)
	}
	if !utf8.ValidString(pattern) {
		return "", fmt.Errorf("%w: pattern contains invalid UTF-8", ErrInvalidIdentityPattern)
	}
	for _, r := range pattern {
		if r < 0x20 || r == 0x7f || unicode.IsControl(r) {
			return "", fmt.Errorf("%w: pattern %q must not contain control characters", ErrInvalidIdentityPattern, pattern)
		}
		if unicode.IsSpace(r) {
			return "", fmt.Errorf("%w: pattern %q must not contain whitespace", ErrInvalidIdentityPattern, pattern)
		}
	}

	var b strings.Builder
	copied := 0
	expanded := false

	for i := 0; i < len(pattern); {
		switch pattern[i] {
		case '}':
			return "", fmt.Errorf("%w: unmatched '}' in %q", ErrInvalidIdentityPattern, pattern)
		case '{':
			j := i + 1
			for j < len(pattern) {
				if pattern[j] == '{' {
					return "", fmt.Errorf("%w: nested or unbalanced '{' in %q", ErrInvalidIdentityPattern, pattern)
				}
				if pattern[j] == '}' {
					break
				}
				j++
			}
			if j == len(pattern) {
				return "", fmt.Errorf("%w: unclosed '{' in %q", ErrInvalidIdentityPattern, pattern)
			}
			token := pattern[i+1 : j]
			if token == "" {
				return "", fmt.Errorf("%w: empty placeholder \"{}\" in %q", ErrInvalidIdentityPattern, pattern)
			}

			var repl string
			switch token {
			case "name":
				if expand {
					if strings.TrimSpace(name) == "" {
						return "", fmt.Errorf("%w: placeholder %s requires a non-empty artifact name", ErrInvalidIdentityPattern, PlaceholderName)
					}
					repl = name
				}
			case "kind":
				if expand {
					if strings.TrimSpace(kind) == "" {
						return "", fmt.Errorf("%w: placeholder %s requires a non-empty artifact kind", ErrInvalidIdentityPattern, PlaceholderKind)
					}
					repl = kind
				}
			default:
				return "", fmt.Errorf("%w: unknown placeholder {%s} in %q (supported: %s, %s)",
					ErrInvalidIdentityPattern, token, pattern, PlaceholderName, PlaceholderKind)
			}

			if expand {
				if !expanded {
					b.Grow(len(pattern) + len(repl))
					expanded = true
				}
				b.WriteString(pattern[copied:i])
				b.WriteString(repl)
				copied = j + 1
			}
			i = j + 1
		default:
			i++
		}
	}

	if !expand || !expanded {
		return pattern, nil
	}
	b.WriteString(pattern[copied:])
	return b.String(), nil
}
