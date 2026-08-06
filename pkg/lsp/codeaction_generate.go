// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/lint"
	"github.com/oakwood-commons/scafctl/pkg/refactor"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"gopkg.in/yaml.v3"
)

// Client-side prompt command identifiers. These are registered by the editor
// extension (editors/vscode), NOT dispatched by the server: a code action
// referencing one asks the client to collect input (a call name, a resolver
// name + provider) and THEN invoke the matching server command
// (cmdApplyExtractCall / cmdApplyAddResolver) via workspace/executeCommand.
const (
	// cmdPromptExtractToCall prompts for a new call name, then invokes
	// cmdApplyExtractCall. Code-action arguments: [uri, blockPath].
	cmdPromptExtractToCall = "scafctl.extractToCall"
	// cmdPromptAddResolver prompts for a resolver name and provider, then
	// invokes cmdApplyAddResolver. Code-action arguments: [uri].
	cmdPromptAddResolver = "scafctl.addResolver"
)

// stepWithPathRe matches the logical path of a resolver resolve/transform/
// validate step (a with[i] sequence element), mirroring refactor's stepPathRe.
// Extract-to-call is offered only for such steps.
var stepWithPathRe = regexp.MustCompile(`\.(resolve|transform|validate)\.with\[\d+\]$`)

// resolverRefNameRe pulls the quoted resolver name out of an
// unknown-resolver-reference finding message (format: reference to undefined
// resolver "NAME").
var resolverRefNameRe = regexp.MustCompile(`reference to undefined resolver "([^"]+)"`)

// resolverNameRe is the canonical resolver-name pattern (shared with
// pkg/refactor). Generated resolver names -- whether extracted from a diagnostic
// message or supplied to applyAddResolver over the public executeCommand surface
// -- are validated against it before being spliced into the document as a YAML
// key, so a malformed name (e.g. a bracket-access reference like _["q\"x"] whose
// name contains a quote, or a client-supplied name with whitespace) can never
// corrupt the file.
var resolverNameRe = regexp.MustCompile(refactor.ResolverNamePattern)

// generativeCodeActions returns the generative/refactor code actions available
// for a request beyond the quick fixes handled by s.codeAction:
//
//   - "Create missing resolver" (QuickFix): a DIRECT insertion edit that adds a
//     stub resolver for each unknown-resolver-reference diagnostic in range.
//   - "Extract to call..." (RefactorExtract): a COMMAND action that asks the
//     client to prompt for a call name, then run cmdApplyExtractCall.
//   - "Add resolver..." (Source): a COMMAND action that asks the client to prompt
//     for a resolver name + provider, then run cmdApplyAddResolver.
//
// Command actions carry no Edit: the extension collects input and invokes the
// server command, which computes and applies the edit. The caller (s.codeAction)
// is responsible for the Context.Only kind filter; this function assumes each
// action kind has already been permitted.
func (s *Server) generativeCodeActions(entry *DocEntry, params *protocol.CodeActionParams) []protocol.CodeAction {
	var actions []protocol.CodeAction
	actions = append(actions, s.createMissingResolverActions(entry, params)...)
	if a, ok := s.extractToCallAction(entry, params); ok {
		actions = append(actions, a)
	}
	if a, ok := s.addResolverAction(entry, params); ok {
		actions = append(actions, a)
	}
	return actions
}

// createMissingResolverActions builds a QuickFix per unknown-resolver-reference
// finding relevant to the request. Each action inserts a minimal stub resolver of
// the referenced name directly (no command round-trip).
func (s *Server) createMissingResolverActions(entry *DocEntry, params *protocol.CodeActionParams) []protocol.CodeAction {
	if entry.Sol == nil {
		return nil
	}
	result := lint.Solution(entry.Sol, uriToPath(params.TextDocument.URI), s.registry)

	var actions []protocol.CodeAction
	seen := make(map[string]bool)
	for _, f := range result.Findings {
		if f.RuleName != "unknown-resolver-reference" {
			continue
		}
		if !findingMatchesRequest(f, params) {
			continue
		}
		name := missingResolverName(f.Message)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true

		stub := resolverStub(name, "static")
		insert, err := refactor.InsertMappingEntry(entry.Raw, "spec.resolvers", stub)
		if err != nil {
			continue
		}
		kind := protocol.CodeActionKindQuickFix
		actions = append(actions, protocol.CodeAction{
			Title:       fmt.Sprintf("Create missing resolver %q", name),
			Kind:        &kind,
			Diagnostics: matchingDiagnostics(f, params),
			Edit:        workspaceEditFor(params.TextDocument.URI, []refactor.TextEdit{insert}),
		})
	}
	return actions
}

// extractToCallAction offers a RefactorExtract command action when the request
// range sits inside an extractable provider step (a resolve/transform/validate
// with[i] step that is not already a call). It returns ok=false otherwise.
func (s *Server) extractToCallAction(entry *DocEntry, params *protocol.CodeActionParams) (protocol.CodeAction, bool) {
	blockPath, ok := extractableStepPath(entry, params.Range)
	if !ok {
		return protocol.CodeAction{}, false
	}
	kind := protocol.CodeActionKindRefactorExtract
	uri := params.TextDocument.URI
	return protocol.CodeAction{
		Title: "Extract to call...",
		Kind:  &kind,
		Command: &protocol.Command{
			Title:     "Extract to call...",
			Command:   cmdPromptExtractToCall,
			Arguments: []any{string(uri), blockPath},
		},
	}, true
}

// addResolverAction offers a Source command action to add a new resolver, when
// the document parsed (so spec.resolvers insertion has a valid target).
func (s *Server) addResolverAction(entry *DocEntry, params *protocol.CodeActionParams) (protocol.CodeAction, bool) {
	if entry.Sol == nil {
		return protocol.CodeAction{}, false
	}
	kind := protocol.CodeActionKindSource
	uri := params.TextDocument.URI
	return protocol.CodeAction{
		Title: "Add resolver...",
		Kind:  &kind,
		Command: &protocol.Command{
			Title:     "Add resolver...",
			Command:   cmdPromptAddResolver,
			Arguments: []any{string(uri)},
		},
	}, true
}

// extractableStepPath finds the logical path of the resolve/transform/validate
// step whose block covers a line in the request range and that is a DIRECT
// provider step (extractable by refactor.ExtractCall -- not already a call:).
// When several steps qualify it returns the deepest (longest-path) match, and it
// reports ok=false when no extractable step overlaps the range.
func extractableStepPath(entry *DocEntry, rng protocol.Range) (string, bool) {
	if entry.Nodes == nil {
		return "", false
	}
	var candidates []string
	for path, node := range entry.Nodes {
		if node == nil || !stepWithPathRe.MatchString(path) {
			continue
		}
		if !nodeCoversRange(entry, path, rng) {
			continue
		}
		if !isProviderStepNode(node) {
			continue
		}
		candidates = append(candidates, path)
	}
	if len(candidates) == 0 {
		return "", false
	}
	// Deterministic, most-specific choice: longest path, then lexical.
	sort.Slice(candidates, func(i, j int) bool {
		if len(candidates[i]) != len(candidates[j]) {
			return len(candidates[i]) > len(candidates[j])
		}
		return candidates[i] < candidates[j]
	})
	return candidates[0], true
}

// nodeCoversRange reports whether the step block at path spans any line in the
// request range. The block starts at the node's line and extends through its
// indentation-scoped extent; the coarse line overlap mirrors codeaction.go's
// finding matching and is robust to cursor-vs-block column differences.
func nodeCoversRange(entry *DocEntry, path string, rng protocol.Range) bool {
	node := entry.Nodes[path]
	if node == nil || entry.Raw == nil {
		return false
	}
	startLine := lspLine(node.Line)
	last := scanStepEndLine(entry.Raw, node.Line)
	endLine := lspLine(last)
	// Overlap between [startLine, endLine] and the request's line span.
	return startLine <= rng.End.Line && rng.Start.Line <= endLine
}

// scanStepEndLine returns the 1-based last line of the step block that begins at
// markerLine, by indentation scanning from the marker's own indent. It mirrors
// the extent logic refactor uses so the offered range matches what would be
// extracted.
func scanStepEndLine(raw []byte, markerLine int) int {
	lines := strings.Split(string(raw), "\n")
	if markerLine < 1 || markerLine > len(lines) {
		return markerLine
	}
	markerIndent := leadingSpaceCount(lines[markerLine-1])
	last := markerLine
	for l := markerLine + 1; l <= len(lines); l++ {
		line := lines[l-1]
		if strings.TrimSpace(line) == "" {
			continue
		}
		if leadingSpaceCount(line) <= markerIndent {
			break
		}
		last = l
	}
	return last
}

// leadingSpaceCount counts the leading ASCII spaces of a line.
func leadingSpaceCount(line string) int {
	n := 0
	for n < len(line) && line[n] == ' ' {
		n++
	}
	return n
}

// isProviderStepNode reports whether a with[i] step node is a DIRECT provider
// step: a mapping that declares "provider" and does NOT declare "call". This is
// the LSP-side gate for offering extract-to-call; refactor.ExtractCall applies
// the full v1 conservatism (rejecting unsupported step fields) when actually run.
func isProviderStepNode(node *yaml.Node) bool {
	if node == nil || node.Kind != yaml.MappingNode {
		return false
	}
	hasProvider, hasCall := false, false
	for i := 0; i+1 < len(node.Content); i += 2 {
		switch node.Content[i].Value {
		case "provider":
			hasProvider = true
		case "call":
			hasCall = true
		}
	}
	return hasProvider && !hasCall
}

// missingResolverName extracts the resolver name from an unknown-resolver-
// reference finding message, or "" when the message does not match the expected
// format or the extracted name is not a valid resolver identifier. The validity
// check matters because a bracket-access reference (_["q\"x"]) yields a name
// containing a quote, which the message regex would truncate; splicing such a
// fragment as a YAML key would corrupt the file instead of fixing anything.
func missingResolverName(message string) string {
	m := resolverRefNameRe.FindStringSubmatch(message)
	if len(m) != 2 {
		return ""
	}
	if !resolverNameRe.MatchString(m[1]) {
		return ""
	}
	return m[1]
}

// resolverStub returns a minimal, valid resolver definition as a zero-indented
// keyed block suitable for refactor.InsertMappingEntry. The stub uses a single
// provider step with a generic empty-string input placeholder; richer,
// schema-driven per-provider input pre-fill is a documented future enhancement.
// An empty provider defaults to "static".
func resolverStub(name, provider string) string {
	if provider == "" {
		provider = "static"
	}
	return name + ":\n" +
		"  resolve:\n" +
		"    with:\n" +
		"      - provider: " + provider + "\n" +
		"        inputs:\n" +
		"          value: \"\"\n"
}
