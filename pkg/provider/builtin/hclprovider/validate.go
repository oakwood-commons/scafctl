// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hclprovider

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// ValidateHCL checks HCL content for syntax errors and returns structured
// diagnostic information. It reports validity, error count, and detailed
// diagnostics with severity, summary, detail, and source position.
func ValidateHCL(src []byte, filename string) map[string]any {
	if filename == "" {
		filename = "input.tf"
	}

	_, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})

	result := map[string]any{
		"valid":       !diags.HasErrors(),
		"error_count": countDiagsBySeverity(diags, hcl.DiagError),
		"diagnostics": diagnosticsToSlice(diags),
	}

	return result
}

// countDiagsBySeverity counts diagnostics matching a given severity.
func countDiagsBySeverity(diags hcl.Diagnostics, sev hcl.DiagnosticSeverity) int {
	count := 0
	for _, d := range diags {
		if d.Severity == sev {
			count++
		}
	}
	return count
}

// diagnosticsToSlice converts HCL diagnostics to a JSON-friendly slice.
func diagnosticsToSlice(diags hcl.Diagnostics) []any {
	result := make([]any, 0, len(diags))
	for _, d := range diags {
		entry := map[string]any{
			"severity": severityString(d.Severity),
			"summary":  d.Summary,
		}
		if d.Detail != "" {
			entry["detail"] = d.Detail
		}
		if d.Subject != nil {
			entry["range"] = rangeToMap(*d.Subject)
		}
		result = append(result, entry)
	}
	return result
}

// severityString converts a diagnostic severity to a human-readable string.
func severityString(s hcl.DiagnosticSeverity) string {
	switch s {
	case hcl.DiagError:
		return "error"
	case hcl.DiagWarning:
		return "warning"
	case hcl.DiagInvalid:
		return "invalid"
	default:
		return "unknown"
	}
}

// rangeToMap converts an HCL source range to a map for JSON output.
func rangeToMap(r hcl.Range) map[string]any {
	return map[string]any{
		"filename": r.Filename,
		"start": map[string]any{
			"line":   r.Start.Line,
			"column": r.Start.Column,
			"byte":   r.Start.Byte,
		},
		"end": map[string]any{
			"line":   r.End.Line,
			"column": r.End.Column,
			"byte":   r.End.Byte,
		},
	}
}
