// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"fmt"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/lint"
	"github.com/oakwood-commons/scafctl/pkg/refactor"
	glsp "github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// codeActionFeature wires textDocument/codeAction so editors offer one-click
// quick fixes for lint diagnostics (deprecated onError, redundant dependsOn,
// unused resolver). It advertises the QuickFix kind explicitly: glsp derives
// CodeActionProvider=true from the wired handler, but the issue asks the server
// to announce the specific kind it provides so clients can filter menus.
func codeActionFeature() feature {
	return feature{
		name: "codeAction",
		wire: func(h *protocol.Handler, s *Server) {
			h.TextDocumentCodeAction = s.codeAction
		},
		advertise: func(c *protocol.ServerCapabilities) {
			c.CodeActionProvider = &protocol.CodeActionOptions{
				CodeActionKinds: []protocol.CodeActionKind{protocol.CodeActionKindQuickFix},
			}
		},
	}
}

// codeAction answers textDocument/codeAction from the per-document cache. It
// re-lints the cached solution (the same path diagnostics take) to obtain
// findings with positions, keeps only those that are both fixable and relevant
// to the request range/diagnostics, and returns each as a QuickFix code action
// carrying the source edits from lint.QuickFixFor.
//
// It returns (nil, nil) -- never an error -- when the document is unknown, not
// parsed, or has no applicable fixes, so an editor that requests actions on
// every cursor move gets an empty result rather than a failure.
func (s *Server) codeAction(_ *glsp.Context, params *protocol.CodeActionParams) (any, error) {
	entry, ok := s.getDoc(params.TextDocument.URI)
	if !ok || entry.Sol == nil {
		return nil, nil
	}

	// Respect the client's kind filter: if it asked for kinds and none of them
	// is quickfix, there is nothing for this server to contribute.
	if !onlyAllowsQuickFix(params.Context.Only) {
		return nil, nil
	}

	result := lint.Solution(entry.Sol, uriToPath(params.TextDocument.URI), s.registry)

	actions := make([]protocol.CodeAction, 0)
	for _, f := range result.Findings {
		edits, fixable := lint.QuickFixFor(entry.Sol, f)
		if !fixable || len(edits) == 0 {
			continue
		}
		if !findingMatchesRequest(f, params) {
			continue
		}
		actions = append(actions, buildCodeAction(f, edits, params))
	}

	if len(actions) == 0 {
		return nil, nil
	}
	return actions, nil
}

// onlyAllowsQuickFix reports whether the request's Context.Only permits a
// quickfix action. An empty/absent Only list means "no filter" (allow). A
// non-empty list allows only when it contains the quickfix kind.
func onlyAllowsQuickFix(only []protocol.CodeActionKind) bool {
	if len(only) == 0 {
		return true
	}
	for _, k := range only {
		if k == protocol.CodeActionKindQuickFix {
			return true
		}
	}
	return false
}

// findingMatchesRequest reports whether finding f is relevant to the code-action
// request: either its source line overlaps the requested range, or its rule name
// matches the Code of an incoming diagnostic whose range overlaps the request.
// Line-level overlap is intentionally coarse -- it is robust to the column
// differences between a finding's squiggle and the client's cursor selection.
func findingMatchesRequest(f *lint.Finding, params *protocol.CodeActionParams) bool {
	// A finding with no resolved source position (Line 0) has no location to
	// match; rely solely on an incoming diagnostic's rule-name match below so it
	// is not spuriously offered on line 0 of every request.
	if f.Line > 0 {
		findingLine := lspLine(f.Line)
		if lineInRange(findingLine, params.Range) {
			return true
		}
		for _, d := range params.Context.Diagnostics {
			if diagnosticCode(d) != f.RuleName {
				continue
			}
			if rangesOverlapLines(d.Range, params.Range) || lineInRange(findingLine, d.Range) {
				return true
			}
		}
		return false
	}
	for _, d := range params.Context.Diagnostics {
		if diagnosticCode(d) == f.RuleName && rangesOverlapLines(d.Range, params.Range) {
			return true
		}
	}
	return false
}

// diagnosticCode returns the string form of a diagnostic's Code (the rule name
// scafctl publishes), or "" when it is absent or not a string.
func diagnosticCode(d protocol.Diagnostic) string {
	if d.Code == nil {
		return ""
	}
	if s, ok := d.Code.Value.(string); ok {
		return s
	}
	return ""
}

// lineInRange reports whether a 0-based line falls within an LSP range's line
// span (inclusive).
func lineInRange(line uint32, r protocol.Range) bool {
	return line >= r.Start.Line && line <= r.End.Line
}

// rangesOverlapLines reports whether two LSP ranges share any line.
func rangesOverlapLines(a, b protocol.Range) bool {
	return a.Start.Line <= b.End.Line && b.Start.Line <= a.End.Line
}

// buildCodeAction assembles the QuickFix code action for a fixable finding: a
// human-readable title, the quickfix kind, the incoming diagnostics it resolves,
// the workspace edit built from lint.QuickFixFor's edits, and IsPreferred so the
// editor's auto-fix targets it.
func buildCodeAction(f *lint.Finding, edits []refactor.TextEdit, params *protocol.CodeActionParams) protocol.CodeAction {
	lspEdits := make([]protocol.TextEdit, 0, len(edits))
	for _, e := range edits {
		lspEdits = append(lspEdits, protocol.TextEdit{Range: toLSPRange(e.Range), NewText: e.NewText})
	}

	kind := protocol.CodeActionKindQuickFix
	preferred := true
	return protocol.CodeAction{
		Title:       codeActionTitle(f),
		Kind:        &kind,
		Diagnostics: matchingDiagnostics(f, params),
		IsPreferred: &preferred,
		Edit: &protocol.WorkspaceEdit{
			Changes: map[protocol.DocumentUri][]protocol.TextEdit{
				params.TextDocument.URI: lspEdits,
			},
		},
	}
}

// matchingDiagnostics returns the subset of the request's incoming diagnostics
// whose Code equals the finding's rule name, so the action is attached to the
// diagnostic(s) it resolves.
func matchingDiagnostics(f *lint.Finding, params *protocol.CodeActionParams) []protocol.Diagnostic {
	var out []protocol.Diagnostic
	for _, d := range params.Context.Diagnostics {
		if diagnosticCode(d) == f.RuleName {
			out = append(out, d)
		}
	}
	return out
}

// codeActionTitle returns the menu title for a fixable finding, keyed by rule.
func codeActionTitle(f *lint.Finding) string {
	switch f.RuleName {
	case "deprecated-field":
		return "Replace deprecated 'onError' with 'continueOnError'"
	case "redundant-depends-on":
		return "Remove redundant dependsOn"
	case "unused-resolver":
		return fmt.Sprintf("Remove unused resolver '%s'", resolverNameFromLocation(f.Location))
	default:
		return "Fix lint issue"
	}
}

// resolverNameFromLocation extracts the resolver name from a "resolvers.<name>"
// finding location (with or without a spec. prefix) for display in the action
// title. It returns the raw location when the shape is unexpected.
func resolverNameFromLocation(loc string) string {
	const prefix = "resolvers."
	if i := strings.Index(loc, prefix); i >= 0 {
		return loc[i+len(prefix):]
	}
	return loc
}
