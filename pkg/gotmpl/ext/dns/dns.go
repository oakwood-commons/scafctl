// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package dns provides Go template extension functions for converting
// arbitrary strings into DNS-safe label format (RFC 1123).
package dns

import (
	"text/template"

	"github.com/oakwood-commons/scafctl/pkg/dnslabel"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
)

// Slugify converts an arbitrary string into a DNS-safe label (RFC 1123).
// It delegates to dnslabel.Slugify, the shared implementation used by both
// the Go template and CEL engines so their output stays identical.
func Slugify(s string) string {
	return dnslabel.Slugify(s)
}

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
