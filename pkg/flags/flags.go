// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package flags

import (
	"fmt"
	"strings"
)

// ParseKeyValue parses a slice of "key=value" strings into a map where
// multiple values for the same key are combined into a slice.
// The first '=' separates key from value - any additional '=' chars
// are part of the value.
//
// This function does NOT parse CSV - each string in pairs is treated as
// a single key=value entry. Use ParseKeyValueCSV for CSV support.
func ParseKeyValue(pairs []string) (map[string][]string, error) {
	result := make(map[string][]string)

	for i, pair := range pairs {
		key, value, err := splitKeyValue(pair)
		if err != nil {
			return nil, fmt.Errorf("invalid key-value pair at index %d: %w", i, err)
		}

		result[key] = append(result[key], value)
	}

	return result, nil
}

// ParseKeyValueCSV parses a slice of "key=value" strings that may contain
// comma-separated pairs. Commas within quoted values are preserved.
// Multiple values for the same key are combined into a slice.
//
// Supports shorthand syntax: values without '=' are treated as additional
// values for the previous key within the same flag.
//
// Examples:
//   - "env=prod,qa" -> env: [prod, qa] (shorthand)
//   - "region=us-east1,region=us-west1" -> region: [us-east1, us-west1]
//   - "env=prod,qa,staging" -> env: [prod, qa, staging] (shorthand)
//   - "region=us-east,env=prod,debug" -> region: [us-east], env: [prod, debug]
//   - "msg=\"Hello, world\"" -> msg: ["Hello, world"]
//   - "key=\"escaped \\\"quotes\\\"\"" -> key: ["escaped \"quotes\""]
//
// Whitespace around commas is trimmed.
func ParseKeyValueCSV(pairs []string) (map[string][]string, error) {
	expanded, err := splitCSVEntries(pairs)
	if err != nil {
		return nil, err
	}
	return ParseKeyValue(expanded)
}

// splitCSVEntries splits comma-separated key=value pairs while respecting quotes.
// Quoted values can contain commas and escaped quotes (\").
// Whitespace around commas is trimmed.
//
// Supports shorthand: values without '=' are expanded using the previous key
// within the same flag. Example: "env=prod,qa" -> ["env=prod", "env=qa"]
func splitCSVEntries(pairs []string) ([]string, error) {
	var result []string

	for _, pair := range pairs {
		entries, err := splitCSV(pair)
		if err != nil {
			return nil, fmt.Errorf("failed to parse CSV in %q: %w", pair, err)
		}

		// Track last key within this flag for shorthand expansion
		var lastKey string
		for _, entry := range entries {
			// Check if entry contains '='
			if strings.Contains(entry, "=") {
				// Extract and remember the key
				key, _, _ := strings.Cut(entry, "=")
				lastKey = strings.TrimSpace(key)
				result = append(result, entry)
			} else {
				// No '=', use last key from this flag
				if lastKey == "" {
					return nil, fmt.Errorf("value %q has no key (no previous key=value pair in this flag)", entry)
				}
				// Expand to full key=value
				result = append(result, lastKey+"="+entry)
			}
		}
	}

	return result, nil
}

// Supported URI schemes for literal value parsing.
// When these schemes are detected in values, commas are treated as literal.
var supportedSchemes = []string{
	"json://",
	"yaml://",
	"base64://",
	"http://",
	"https://",
	"file://",
}

// splitCSV splits a string on commas, respecting quoted sections and URI schemes.
// Supports both single and double quotes. Quotes can be escaped with backslash.
// URI scheme values (json://, yaml://, etc.) preserve all content including commas and quotes.
func splitCSV(s string) ([]string, error) {
	var result []string
	var current strings.Builder
	var inQuote bool
	var quoteChar rune
	var escaped bool
	var inSchemeValue bool

	s = strings.TrimSpace(s)

	for i, ch := range s {
		// Handle escaping only inside quotes (not in scheme values)
		if inQuote && !inSchemeValue {
			if escaped {
				// Previous char was backslash, add this char literally
				current.WriteRune(ch)
				escaped = false
				continue
			}

			if ch == '\\' {
				// Mark as escaped for next iteration
				escaped = true
				continue
			}
		}

		// Don't process quotes if we're in a scheme value
		if !inSchemeValue {
			if (ch == '"' || ch == '\'') && !inQuote && !escaped {
				// Starting a quoted section
				inQuote = true
				quoteChar = ch
				continue
			}

			if ch == quoteChar && inQuote && !escaped {
				// Ending a quoted section
				inQuote = false
				continue
			}
		}

		// Detect scheme in unquoted values after '='
		if !inQuote && !inSchemeValue && ch == '=' {
			// Check if what follows is a scheme
			remaining := s[i+1:]
			for _, scheme := range supportedSchemes {
				if strings.HasPrefix(remaining, scheme) {
					inSchemeValue = true
					break
				}
			}
		}

		if ch == ',' && !inQuote {
			if inSchemeValue {
				// In scheme value - check if next part looks like key=value
				remaining := s[i+1:]
				remaining = strings.TrimSpace(remaining)
				if looksLikeKeyValue(remaining) {
					// This comma separates entries
					inSchemeValue = false
					entry := strings.TrimSpace(current.String())
					if entry != "" {
						result = append(result, entry)
					}
					current.Reset()
					continue
				}
				// Comma is part of scheme value
				current.WriteRune(ch)
				continue
			}

			// Found separator outside quotes and schemes
			entry := strings.TrimSpace(current.String())
			if entry != "" {
				result = append(result, entry)
			}
			current.Reset()
			continue
		}

		current.WriteRune(ch)

		// Check if we're at the end
		if i == len(s)-1 {
			entry := strings.TrimSpace(current.String())
			if entry != "" {
				result = append(result, entry)
			}
		}
	}

	if inQuote {
		return nil, fmt.Errorf("unterminated quote in %q", s)
	}

	// Handle trailing content if any
	if current.Len() > 0 {
		entry := strings.TrimSpace(current.String())
		if entry != "" {
			// Check if we haven't already added this
			if len(result) == 0 || result[len(result)-1] != entry {
				result = append(result, entry)
			}
		}
	}

	return result, nil
}

// looksLikeKeyValue checks if a string starts with a valid key=value pattern.
// Used to detect CSV separators after scheme values.
// A valid key contains only alphanumeric, dash, underscore characters.
func looksLikeKeyValue(s string) bool {
	// Must contain an equals sign
	idx := strings.Index(s, "=")
	if idx == -1 || idx == 0 {
		return false
	}

	// Everything before = should be a valid key
	key := s[:idx]

	// Key shouldn't contain whitespace or special chars (except - and _)
	for _, ch := range key {
		if !isValidKeyChar(ch) {
			return false
		}
	}

	return true
}

// isValidKeyChar checks if a character is valid in a key name.
// Valid: a-z, A-Z, 0-9, -, _
func isValidKeyChar(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') ||
		ch == '-' ||
		ch == '_'
}

// splitKeyValue splits a "key=value" string on the first '=' character.
// Returns an error if the format is invalid.
func splitKeyValue(pair string) (key, value string, err error) {
	var found bool
	key, value, found = strings.Cut(pair, "=")

	if !found {
		return "", "", fmt.Errorf("missing '=' separator in %q", pair)
	}

	// Validate key is not empty
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", fmt.Errorf("empty key in %q", pair)
	}

	// Validate key doesn't contain spaces or newlines
	if strings.ContainsAny(key, " \t\n\r") {
		return "", "", fmt.Errorf("key cannot contain whitespace or newlines: %q", key)
	}

	return key, value, nil
}

// GetFirst returns the first value for a key, or empty string if not present.
func GetFirst(m map[string][]string, key string) string {
	if values, ok := m[key]; ok && len(values) > 0 {
		return values[0]
	}
	return ""
}

// GetAll returns all values for a key, or nil if not present.
func GetAll(m map[string][]string, key string) []string {
	return m[key]
}

// Has checks if a key exists in the map.
func Has(m map[string][]string, key string) bool {
	_, ok := m[key]
	return ok
}
