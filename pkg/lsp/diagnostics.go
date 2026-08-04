// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package lsp implements the language-server features for solution files. The
// logic here is transport-agnostic and testable in isolation; the glsp wiring
// and the `lsp` command are thin layers on top.
package lsp

import (
	"bytes"
	"fmt"
	"unicode/utf8"

	"github.com/oakwood-commons/scafctl/pkg/lint"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// Diagnostics parses solution content, runs lint, and maps the findings to LSP
// diagnostics. source labels each diagnostic (typically the binary name). A
// whole-document parse failure yields a single diagnostic at the top of the
// file. The returned slice is always non-nil so callers can publish it to clear
// stale diagnostics when there are none.
//
// Character offsets are counted per rune, which is correct for ASCII/BMP text
// (the overwhelming case for solution files); astral-plane characters before a
// finding could shift the column under the LSP's UTF-16 convention.
func Diagnostics(content []byte, filePath, source string, registry *provider.Registry) []protocol.Diagnostic {
	sol := &solution.Solution{}
	if err := sol.UnmarshalFromBytes(content); err != nil {
		return []protocol.Diagnostic{parseErrorDiagnostic(err, source)}
	}

	result := lint.Solution(sol, filePath, registry)
	lines := bytes.Split(content, []byte("\n"))

	diags := make([]protocol.Diagnostic, 0, len(result.Findings))
	for _, f := range result.Findings {
		diags = append(diags, findingToDiagnostic(f, lines, source))
	}
	return diags
}

// parseErrorDiagnostic reports a whole-document parse failure at the top of the
// file.
func parseErrorDiagnostic(err error, source string) protocol.Diagnostic {
	sev := protocol.DiagnosticSeverityError
	src := source
	code := protocol.IntegerOrString{Value: "parse-error"}
	return protocol.Diagnostic{
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 0},
			End:   protocol.Position{Line: 0, Character: 1},
		},
		Severity: &sev,
		Source:   &src,
		Code:     &code,
		Message:  fmt.Sprintf("failed to parse solution: %v", err),
	}
}

// findingToDiagnostic maps a single lint finding to an LSP diagnostic, spanning
// from the finding's column to the end of its line so the squiggle is visible.
func findingToDiagnostic(f *lint.Finding, lines [][]byte, source string) protocol.Diagnostic {
	line := f.Line - 1 // lint lines are 1-based; LSP is 0-based
	if line < 0 {
		line = 0
	}
	startChar := f.Column - 1
	if startChar < 0 {
		startChar = 0
	}

	endChar := startChar + 1
	if line < len(lines) {
		lineRunes := utf8.RuneCount(bytes.TrimRight(lines[line], "\r"))
		if startChar > lineRunes {
			startChar = lineRunes
		}
		endChar = lineRunes
		if endChar <= startChar {
			endChar = startChar + 1
		}
	}

	sev := severityFor(f.Severity)
	src := source
	code := protocol.IntegerOrString{Value: f.RuleName}

	msg := f.Message
	if f.Suggestion != "" {
		msg = fmt.Sprintf("%s (%s)", f.Message, f.Suggestion)
	}

	return protocol.Diagnostic{
		Range: protocol.Range{
			Start: protocol.Position{Line: uint32(line), Character: uint32(startChar)},
			End:   protocol.Position{Line: uint32(line), Character: uint32(endChar)},
		},
		Severity: &sev,
		Source:   &src,
		Code:     &code,
		Message:  msg,
	}
}

// severityFor maps a lint severity to an LSP diagnostic severity.
func severityFor(s lint.SeverityLevel) protocol.DiagnosticSeverity {
	switch s {
	case lint.SeverityError:
		return protocol.DiagnosticSeverityError
	case lint.SeverityWarning:
		return protocol.DiagnosticSeverityWarning
	case lint.SeverityInfo:
		return protocol.DiagnosticSeverityInformation
	default:
		return protocol.DiagnosticSeverityInformation
	}
}
