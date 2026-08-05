// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package refactor

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/refindex"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/sourcepos"
	"gopkg.in/yaml.v3"
)

// stepPathRe matches the logical path of a resolver resolve/transform/validate
// step (a with[i] sequence element). Extract Call operates only on such steps.
var stepPathRe = regexp.MustCompile(`\.(resolve|transform|validate)\.with\[\d+\]$`)

// maxColumn is an intentionally over-long column used with LineIndex.Offset to
// resolve the byte offset at the end of a line's visible content (the offset
// clamps to the line end, excluding the trailing newline).
const maxColumn = 1 << 30

// ExtractCall hoists a single resolve/transform/validate step (a provider+inputs
// block identified by blockPath, e.g. "spec.resolvers.app.resolve.with[0]") out
// of its resolver into a reusable spec.calls definition named callName, and
// rewrites the selected step to a "call: <callName>" reference.
//
// It is source-preserving: the change is expressed as byte-exact TextEdits (via
// the shared RenameResult), so comments, key order, and formatting elsewhere in
// the file are untouched. The extracted call reproduces the step's provider and
// inputs verbatim (spliced from the original bytes, re-indented), preserving any
// inline comments on the block.
//
// v1 scope and conservatism:
//   - Only a DIRECT provider step is extractable. A step that already uses
//     "call:" (or that is not a provider step) is rejected.
//   - Only provider/inputs steps are extractable. A step that carries any
//     step-level field a call definition cannot model (when, continueOnError,
//     onError, forEach, or a validation message) is rejected rather than
//     silently dropping that field on extraction.
//   - Argument inference is conservative: a literal block is extracted with NO
//     args (empty Args); the call reproduces the inputs literally. Inferring
//     arguments from near-duplicate steps with varying values is deferred.
//   - The base ExtractCall does NOT scan for or rewrite other identical steps.
//     Use ExtractCallReplacingIdentical for that opt-in behavior.
//
// Like RenameSymbol, it is all-or-nothing: any problem returns an error and NO
// edits. Returned RenameResult has OldName set to blockPath and NewName to
// callName (the type is shared with rename; there is no separate name pair for
// an extraction). Callers apply the change with RenameResult.Apply.
func ExtractCall(sol *solution.Solution, blockPath, callName string) (*RenameResult, error) {
	return extractCall(sol, blockPath, callName, false)
}

// ExtractCallReplacingIdentical behaves exactly like ExtractCall but, in
// addition, rewrites every OTHER resolve/transform/validate step whose
// provider+inputs are structurally identical to the extracted block into a
// "call: <callName>" reference. This is the opt-in, mass-rewrite variant: the
// base ExtractCall never scans for or touches other steps.
//
// "Structurally identical" means the steps decode to deeply-equal YAML values
// (same provider, same inputs, same any other step fields), independent of key
// order and formatting. Only exact structural duplicates are replaced; steps
// with any differing value (a near-match) are left untouched, because inferring
// arguments to unify near-matches is deferred to a later version.
//
// Comments are not part of a step's decoded value, so two steps that differ
// ONLY by an inline/leading comment are treated as identical and both rewritten
// to "call: <callName>". The extracted call body reproduces the SELECTED
// block's comments; a divergent comment on a rewritten duplicate is dropped
// along with the duplicated step it annotated. This is intentional -- the steps
// are semantically identical -- but it is the one case where a comment does not
// survive verbatim.
func ExtractCallReplacingIdentical(sol *solution.Solution, blockPath, callName string) (*RenameResult, error) {
	return extractCall(sol, blockPath, callName, true)
}

// extractCall is the shared implementation behind ExtractCall (replaceIdentical
// false) and ExtractCallReplacingIdentical (replaceIdentical true).
func extractCall(sol *solution.Solution, blockPath, callName string, replaceIdentical bool) (*RenameResult, error) {
	if sol == nil {
		return nil, fmt.Errorf("extract call: nil solution")
	}
	if !resolverNameRe.MatchString(callName) {
		return nil, fmt.Errorf("extract call: %q is not a valid call name (must match %s)", callName, ResolverNamePattern)
	}
	if _, exists := sol.Spec.Calls[callName]; exists {
		return nil, fmt.Errorf("extract call: call %q already exists", callName)
	}
	if !stepPathRe.MatchString(blockPath) {
		return nil, fmt.Errorf("extract call: %q is not a resolve/transform/validate step path (expected a ...with[i] step)", blockPath)
	}

	raw := sol.RawContent()
	nodes, err := refindex.NodeMap(raw)
	if err != nil {
		return nil, fmt.Errorf("extract call: %w", err)
	}
	node, ok := nodes[blockPath]
	if !ok {
		return nil, fmt.Errorf("extract call: block path %q does not resolve to a node", blockPath)
	}
	if err := requireProviderStep(node); err != nil {
		return nil, fmt.Errorf("extract call: %w", err)
	}

	li := sourcepos.NewLineIndex(raw, "")
	sm := sol.SourceMap()

	// A spec.calls key present in source but with no decodable entries (an inline
	// "calls: {}", or a bare/comment-only "calls:") is ambiguous: HasCalls() is
	// false, so the insertion logic below would create a SECOND "calls:" key and
	// corrupt the file. Refuse rather than emit a broken edit set (all-or-nothing).
	if _, present := sm.Get("spec.calls"); present && !sol.Spec.HasCalls() {
		return nil, fmt.Errorf("extract call: spec.calls is present but has no entries (empty or inline); populate or remove it before extracting")
	}

	// The byte-insertion below assumes the insertion target is block-style YAML:
	// when spec.calls already exists it APPENDS a block entry after the last one,
	// and when it does not it inserts a new block "calls:" child of spec. A
	// flow-style target -- an existing "calls: {a: {...}}", or a flow-style
	// "spec: {...}" -- cannot receive that block text without producing invalid
	// YAML. Refuse rather than emit a corrupt edit set (all-or-nothing).
	if err := requireBlockStyleInsertion(nodes, sol.Spec.HasCalls()); err != nil {
		return nil, fmt.Errorf("extract call: %w", err)
	}

	// The extracted block bytes (spliced, re-indented) become the call body.
	startByte, endByte, markerIndent, err := stepBlockRange(raw, li, node)
	if err != nil {
		return nil, fmt.Errorf("extract call: %w", err)
	}

	callsKeyIndent, callNameIndent, targetIndent, err := callIndents(sol, sm)
	if err != nil {
		return nil, fmt.Errorf("extract call: %w", err)
	}

	blockText := string(raw[startByte:endByte])
	body := reindentStepAsCall(blockText, markerIndent, targetIndent)
	entry := strings.Repeat(" ", callNameIndent) + callName + ":\n" + body

	// Which step(s) become a "call:" reference. Always the selected block; with
	// the opt-in variant, also every structural duplicate.
	targetPaths := []string{blockPath}
	if replaceIdentical {
		targetPaths = identicalStepPaths(nodes, node, blockPath)
	}

	edits := make([]TextEdit, 0, len(targetPaths)+1)
	for _, p := range targetPaths {
		n := nodes[p]
		s, e, _, rerr := stepBlockRange(raw, li, n)
		if rerr != nil {
			return nil, fmt.Errorf("extract call: %s: %w", p, rerr)
		}
		edits = append(edits, TextEdit{
			Range:   li.Range(s, e),
			NewText: "- call: " + callName,
		})
	}

	insert, err := callsInsertion(sol, sm, li, raw, entry, callsKeyIndent)
	if err != nil {
		return nil, fmt.Errorf("extract call: %w", err)
	}
	edits = append(edits, insert)

	return &RenameResult{OldName: blockPath, NewName: callName, Edits: edits}, nil
}

// requireBlockStyleInsertion rejects solutions whose spec.calls insertion target
// is flow-style YAML, which the block-style byte insertion in callsInsertion
// would corrupt. When spec.calls already exists, the target is spec.calls
// itself (appending a block entry to a flow "calls: {a: ...}" yields invalid
// YAML); when it does not, the target is spec (a new block "calls:" child cannot
// be spliced into a flow-style "spec: {...}"). Either way, refuse rather than
// emit a document that no longer parses (all-or-nothing).
func requireBlockStyleInsertion(nodes map[string]*yaml.Node, hasCalls bool) error {
	target := "spec"
	if hasCalls {
		target = "spec.calls"
	}
	if n, ok := nodes[target]; ok && n != nil && n.Style&yaml.FlowStyle != 0 {
		return fmt.Errorf("%s is written in flow style; rewrite it in block style before extracting", target)
	}
	return nil
}

// requireProviderStep verifies node is a mapping that represents a direct
// provider step that is safe to hoist verbatim: it must declare a "provider"
// key, must NOT declare a "call" key, and must NOT declare any step-level field
// other than provider/inputs. Steps that already use a call, that are not
// provider steps, or that carry conditional/iteration/error-handling fields
// (when, continueOnError, onError, forEach, message) are not extractable in v1.
//
// The restriction to provider/inputs exists because a step is hoisted verbatim
// into a spec.calls definition, whose type (spec.Call) only models provider/
// inputs (plus description/args/dedup, which a step never has). Splicing any
// other step field into the call body would silently drop it (spec.Call ignores
// unknown keys) AND strip it from the call site, changing behavior with no error.
func requireProviderStep(node *yaml.Node) error {
	if node == nil || node.Kind != yaml.MappingNode {
		return fmt.Errorf("step is not a mapping block")
	}
	hasProvider, hasCall := false, false
	var unsupported []string
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		switch key {
		case "provider":
			hasProvider = true
		case "call":
			hasCall = true
		case "inputs":
			// allowed
		default:
			unsupported = append(unsupported, key)
		}
	}
	if hasCall {
		return fmt.Errorf("step already uses a call reference; only direct provider steps are extractable")
	}
	if !hasProvider {
		return fmt.Errorf("step is not a provider step (no provider key); only direct provider steps are extractable")
	}
	if len(unsupported) > 0 {
		sort.Strings(unsupported)
		return fmt.Errorf("step declares unsupported field(s) %v; only provider/inputs steps are extractable in v1 "+
			"(step-level when/continueOnError/onError/forEach/message would be silently lost)", unsupported)
	}
	return nil
}

// stepBlockRange computes the full source byte span [startByte, endByte) of a
// with[i] step block, plus the indentation (leading spaces) of its "- " marker.
//
// yaml.v3 reports only the start of a node; the mapping value node's Line points
// at the "- provider:" line. The block's end is found by INDENTATION scanning:
// beginning at the marker line, the block spans every following line that is
// blank or indented MORE than the marker, stopping at the first non-blank line
// whose indentation is <= the marker's (the next "- " sibling, or a dedent out
// of the with: list), or at end of file. The span starts at the "- " marker
// column and ends at the last block content line's end (excluding its trailing
// newline, so the replacement leaves surrounding structure intact).
func stepBlockRange(raw []byte, li *sourcepos.LineIndex, node *yaml.Node) (startByte, endByte, markerIndent int, err error) {
	if node == nil {
		return 0, 0, 0, fmt.Errorf("nil step node")
	}
	markerLine := node.Line
	lineStart := li.Offset(sourcepos.Position{Line: markerLine, Column: 1})
	lineEnd := li.Offset(sourcepos.Position{Line: markerLine, Column: maxColumn})
	lineBytes := raw[lineStart:lineEnd]
	markerIndent = leadingSpaces(lineBytes)
	if markerIndent >= len(lineBytes) || lineBytes[markerIndent] != '-' {
		return 0, 0, 0, fmt.Errorf("step at line %d does not begin with a %q sequence marker (only block-style steps are supported)", markerLine, "- ")
	}
	startByte = lineStart + markerIndent
	lastLine := scanBlockEndLine(raw, li, markerLine, markerIndent)
	endByte = li.Offset(sourcepos.Position{Line: lastLine, Column: maxColumn})
	return startByte, endByte, markerIndent, nil
}

// scanBlockEndLine returns the 1-based line number of the last line belonging to
// a block that begins at startLine and whose owning marker/key is indented
// ownIndent spaces. A line belongs to the block if it is blank or indented more
// than ownIndent; scanning stops at the first non-blank line indented <=
// ownIndent, or at end of file. Trailing blank lines are excluded from the
// returned end.
func scanBlockEndLine(raw []byte, li *sourcepos.LineIndex, startLine, ownIndent int) int {
	last := startLine
	total := li.Len()
	for l := startLine + 1; l <= total; l++ {
		s := li.Offset(sourcepos.Position{Line: l, Column: 1})
		e := li.Offset(sourcepos.Position{Line: l, Column: maxColumn})
		lb := raw[s:e]
		if isBlank(lb) {
			continue
		}
		if leadingSpaces(lb) <= ownIndent {
			break
		}
		last = l
	}
	return last
}

// reindentStepAsCall converts a step block's spliced bytes (starting at the
// "- " marker) into the body of a spec.calls entry: the provider/inputs mapping
// re-indented to targetIndent. The "- " marker is dropped (a call body is a
// plain mapping, not a sequence element) and every line is shifted so the
// mapping's top-level keys land at targetIndent, preserving relative
// indentation, blank lines, and inline comments.
func reindentStepAsCall(blockText string, markerIndent, targetIndent int) string {
	// The mapping keys of a "- key:" sequence element sit two columns past the
	// marker (the width of "- ").
	srcIndent := markerIndent + 2
	delta := targetIndent - srcIndent

	lines := strings.Split(blockText, "\n")
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		if i == 0 {
			// First line is "- provider: ..."; drop the marker, keep content.
			content := strings.TrimLeft(strings.TrimPrefix(line, "-"), " ")
			out = append(out, strings.Repeat(" ", targetIndent)+content)
			continue
		}
		if isBlank([]byte(line)) {
			out = append(out, "")
			continue
		}
		ind := leadingSpaces([]byte(line))
		newInd := ind + delta
		if newInd < 0 {
			newInd = 0
		}
		out = append(out, strings.Repeat(" ", newInd)+line[ind:])
	}
	return strings.Join(out, "\n")
}

// callIndents determines the three indentation levels used to render the calls
// entry, matching the file's existing indentation style: the indent of the
// "calls:" key, of each call name key, and of the call body's top-level keys.
// When spec.calls already exists, the name/body indents are taken from a real
// existing entry; otherwise they are derived from the spec/resolvers indent
// unit.
func callIndents(sol *solution.Solution, sm *sourcepos.SourceMap) (callsKeyIndent, callNameIndent, targetIndent int, err error) {
	specPos, ok := sm.Get("spec")
	if !ok {
		return 0, 0, 0, fmt.Errorf("cannot locate spec block")
	}
	specIndent := specPos.Column - 1

	unit := 2
	if resolversPos, ok := sm.Get("spec.resolvers"); ok {
		if u := (resolversPos.Column - 1) - specIndent; u > 0 {
			unit = u
		}
	}

	if sol.Spec.HasCalls() {
		callsPos, ok := sm.Get("spec.calls")
		if !ok {
			return 0, 0, 0, fmt.Errorf("cannot locate existing spec.calls block")
		}
		callsKeyIndent = callsPos.Column - 1
		callNameIndent = callsKeyIndent + unit
		// Prefer a real existing entry's key indent when available.
		for name := range sol.Spec.Calls {
			if p, ok := sm.Get("spec.calls." + name); ok {
				callNameIndent = p.Column - 1
				break
			}
		}
		targetIndent = callNameIndent + unit
		return callsKeyIndent, callNameIndent, targetIndent, nil
	}

	callsKeyIndent = specIndent + unit
	callNameIndent = callsKeyIndent + unit
	targetIndent = callNameIndent + unit
	return callsKeyIndent, callNameIndent, targetIndent, nil
}

// callsInsertion builds the zero-width TextEdit that inserts the rendered call
// definition. When spec.calls already exists, the entry is appended after the
// last existing call entry. When it does not, a new "calls:" key (holding the
// entry) is inserted as the first child of spec, immediately after the "spec:"
// line -- a deterministic, always-valid insertion point.
func callsInsertion(sol *solution.Solution, sm *sourcepos.SourceMap, li *sourcepos.LineIndex, raw []byte, entry string, callsKeyIndent int) (TextEdit, error) {
	if sol.Spec.HasCalls() {
		callsPos, ok := sm.Get("spec.calls")
		if !ok {
			return TextEdit{}, fmt.Errorf("cannot locate existing spec.calls block")
		}
		endLine := scanBlockEndLine(raw, li, callsPos.Line, callsPos.Column-1)
		insertByte := li.Offset(sourcepos.Position{Line: endLine, Column: maxColumn})
		pos := li.Position(insertByte)
		return TextEdit{
			Range:   sourcepos.Range{Start: pos, End: pos},
			NewText: "\n" + entry,
		}, nil
	}

	specPos, ok := sm.Get("spec")
	if !ok {
		return TextEdit{}, fmt.Errorf("cannot locate spec block")
	}
	insertByte := li.Offset(sourcepos.Position{Line: specPos.Line + 1, Column: 1})
	pos := li.Position(insertByte)
	block := strings.Repeat(" ", callsKeyIndent) + "calls:\n" + entry + "\n"
	return TextEdit{
		Range:   sourcepos.Range{Start: pos, End: pos},
		NewText: block,
	}, nil
}

// identicalStepPaths returns the sorted set of with[i] step paths (including
// blockPath) whose decoded YAML value is deeply equal to the selected block.
// Comparison is structural (key order and formatting are irrelevant); only exact
// duplicates match.
func identicalStepPaths(nodes map[string]*yaml.Node, node *yaml.Node, blockPath string) []string {
	var want any
	if err := node.Decode(&want); err != nil {
		// If the selected block cannot be decoded, fall back to it alone.
		return []string{blockPath}
	}
	var paths []string
	for p, n := range nodes {
		if !stepPathRe.MatchString(p) || n.Kind != yaml.MappingNode {
			continue
		}
		var got any
		if err := n.Decode(&got); err != nil {
			continue
		}
		if reflect.DeepEqual(want, got) {
			paths = append(paths, p)
		}
	}
	// Guarantee the selected block is present even if decoding oddities arise.
	if !containsString(paths, blockPath) {
		paths = append(paths, blockPath)
	}
	sort.Strings(paths)
	return paths
}

func containsString(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// leadingSpaces counts the leading ASCII space characters of a line.
func leadingSpaces(line []byte) int {
	n := 0
	for n < len(line) && line[n] == ' ' {
		n++
	}
	return n
}

// isBlank reports whether a line contains only whitespace.
func isBlank(line []byte) bool {
	for _, b := range line {
		if b != ' ' && b != '\t' && b != '\r' {
			return false
		}
	}
	return true
}
