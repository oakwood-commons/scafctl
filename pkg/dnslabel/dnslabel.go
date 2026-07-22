// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package dnslabel converts arbitrary strings into DNS-safe label format
// (RFC 1123). It has no dependencies on the CEL or Go template engines so it
// can be shared by both without creating an import cycle.
package dnslabel

import (
	"regexp"
	"strings"
)

// MaxLabelLength is the maximum length of a DNS label per RFC 1123.
const MaxLabelLength = 63

var (
	// nonDNSChars matches any character that is not a lowercase letter, digit, or hyphen.
	nonDNSChars = regexp.MustCompile(`[^a-z0-9-]`)
	// multipleHyphens matches consecutive hyphens.
	multipleHyphens = regexp.MustCompile(`-{2,}`)
)

// Slugify converts an arbitrary string into a DNS-safe label (RFC 1123).
//
// The transformation:
//  1. Converts to lowercase
//  2. Replaces any character not in [a-z0-9-] with a hyphen
//  3. Collapses consecutive hyphens into a single hyphen
//  4. Strips leading and trailing hyphens
//  5. Truncates to 63 characters (the DNS label limit)
//  6. Strips any trailing hyphen introduced by truncation
//
// Returns an empty string if the input is empty or contains no valid characters.
func Slugify(s string) string {
	// 1. Lowercase
	result := strings.ToLower(s)

	// 2. Replace non-DNS characters with hyphens
	result = nonDNSChars.ReplaceAllString(result, "-")

	// 3. Collapse consecutive hyphens
	result = multipleHyphens.ReplaceAllString(result, "-")

	// 4. Strip leading/trailing hyphens
	result = strings.Trim(result, "-")

	// 5. Truncate to max DNS label length
	if len(result) > MaxLabelLength {
		result = result[:MaxLabelLength]
	}

	// 6. Strip trailing hyphen from truncation
	result = strings.TrimRight(result, "-")

	return result
}
