// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package dns provides Go template extension functions for converting
// arbitrary strings into DNS-safe label format (RFC 1123).
package dns

import (
	"regexp"
	"strings"
	"text/template"

	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
)

const (
	// maxDNSLabelLength is the maximum length of a DNS label per RFC 1123.
	maxDNSLabelLength = 63
)

var (
	// nonDNSChars matches any character that is not a lowercase letter, digit, or hyphen.
	nonDNSChars = regexp.MustCompile(`[^a-z0-9-]`)
	// multipleHyphens matches consecutive hyphens.
	multipleHyphens = regexp.MustCompile(`-{2,}`)
)

// SlugifyFunc returns an ExtFunction that converts a string into a
// DNS-safe label (RFC 1123). The output is lowercase, contains only
// [a-z0-9-], has no leading/trailing/consecutive hyphens, and is
// truncated to 63 characters.
//
// Example usage in a Go template:
//
//	{{ .name | slugify }}
//	{{ slugify .orgName }}
func SlugifyFunc() gotmpl.ExtFunction {
	return gotmpl.ExtFunction{
		Name: "slugify",
		Description: "Converts a string into a DNS-safe label (RFC 1123). " +
			"Lowercases the input, replaces non-alphanumeric characters with hyphens, " +
			"collapses consecutive hyphens, strips leading/trailing hyphens, and " +
			"truncates to 63 characters.",
		Custom: true,
		Links:  []string{"https://tools.ietf.org/html/rfc1123"},
		Examples: []gotmpl.Example{
			{
				Description: "Convert a name to a DNS-safe label",
				Template:    `{{ "My Application Name" | slugify }}`,
			},
			{
				Description: "Use with pipeline",
				Template:    `{{ .githubOrg | slugify }}`,
			},
			{
				Description: "Handle special characters",
				Template:    `{{ "hello_world@2024!" | slugify }}`,
			},
		},
		Func: template.FuncMap{
			"slugify": Slugify,
		},
	}
}

// ToDNSStringFunc returns an ExtFunction that is an alias for slugify,
// providing backward compatibility for templates using the toDnsString name.
//
// Example usage in a Go template:
//
//	{{ .name | toDnsString }}
func ToDNSStringFunc() gotmpl.ExtFunction {
	return gotmpl.ExtFunction{
		Name: "toDnsString",
		Description: "Alias for slugify. Converts a string into a DNS-safe label (RFC 1123). " +
			"Lowercases the input, replaces non-alphanumeric characters with hyphens, " +
			"collapses consecutive hyphens, strips leading/trailing hyphens, and " +
			"truncates to 63 characters.",
		Custom: true,
		Links:  []string{"https://tools.ietf.org/html/rfc1123"},
		Examples: []gotmpl.Example{
			{
				Description: "Convert a name to a DNS label",
				Template:    `{{ .kubeNamespace | toDnsString }}`,
			},
		},
		Func: template.FuncMap{
			"toDnsString": Slugify,
		},
	}
}

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
	if len(result) > maxDNSLabelLength {
		result = result[:maxDNSLabelLength]
	}

	// 6. Strip trailing hyphen from truncation
	result = strings.TrimRight(result, "-")

	return result
}
