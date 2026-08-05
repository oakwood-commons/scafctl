// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"bytes"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/refindex"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"gopkg.in/yaml.v3"
)

// CursorKind classifies what an editor cursor sits on in a solution document.
// It is the shared vocabulary for hover, completion, and signature help so each
// feature dispatches on one classification instead of growing its own fragile
// "am I inside {{ }} / on a YAML key?" scanner.
type CursorKind int

const (
	// CursorNone is whitespace or an unclassifiable position.
	CursorNone CursorKind = iota
	// CursorSymbolRef is a located reference to a resolver/action/call/function
	// (a refindex.Reference range).
	CursorSymbolRef
	// CursorYAMLKey is a mapping key (schema context).
	CursorYAMLKey
	// CursorEnumValue is a scalar value whose schema field is a fixed enum.
	CursorEnumValue
	// CursorCEL is inside a CEL expression or condition value.
	CursorCEL
	// CursorTemplate is inside a Go-template (tmpl) value.
	CursorTemplate
	// CursorProviderName is a provider: value.
	CursorProviderName
)

// String returns a short label for the cursor kind (for tests/debugging).
func (k CursorKind) String() string {
	switch k {
	case CursorNone:
		return "none"
	case CursorSymbolRef:
		return "symbolRef"
	case CursorYAMLKey:
		return "yamlKey"
	case CursorEnumValue:
		return "enumValue"
	case CursorCEL:
		return "cel"
	case CursorTemplate:
		return "template"
	case CursorProviderName:
		return "providerName"
	default:
		return "none"
	}
}

// CursorContext is the resolved classification of one cursor position.
type CursorContext struct {
	// Kind is the classification.
	Kind CursorKind
	// Path is the logical YAML path at the cursor (dotted keys + [i]), when known.
	Path string
	// Node is the enclosing value node, when known.
	Node *yaml.Node
	// Ref is set when Kind == CursorSymbolRef.
	Ref *refindex.Reference
	// PartialToken is the text typed so far at the cursor (for completion
	// filtering) -- the identifier fragment under/just before the cursor.
	PartialToken string
	// ExprPrefix is the reference prefix in a CEL/template token: "_." or
	// "__actions." in CEL; "._.", ".__actions.", or "." in a template. Empty
	// when the token is a bare function name or there is no prefix.
	ExprPrefix string
}

// Reference-prefix markers captured in ExprPrefix. CEL uses the underscore data
// root directly; Go templates dot-scope it.
const (
	celResolverPrefix  = "_."
	celActionPrefix    = "__actions."
	tmplResolverPrefix = "._."
	tmplActionPrefix   = ".__actions."
	tmplFieldPrefix    = "."
)

// celValueKeys are the leaf mapping keys whose scalar value holds a CEL
// expression or condition (see pkg/spec ValueRef.Expr, pkg/spec Condition, and
// author-function cel bodies).
var celValueKeys = map[string]struct{}{
	"expr":       {},
	"expression": {},
	"when":       {},
	"until":      {},
	"retryIf":    {},
	"cel":        {},
}

// templateValueKeys are the leaf mapping keys whose scalar value holds a Go
// template (ValueRef.Tmpl, author-function template bodies).
var templateValueKeys = map[string]struct{}{
	"tmpl":     {},
	"template": {},
}

// enumValueKeys are the leaf mapping keys whose scalar value is a fixed enum in
// the current solution schema. The classifier only needs to know a field is an
// enum; downstream completion sources the concrete values. Keep in sync with the
// typed string enums in pkg/spec and pkg/action (OnErrorBehavior, BackoffType,
// ResultSchemaMode, FingerprintScope). Note onError is deprecated in favor of
// the boolean continueOnError, but remains a functional enum field.
var enumValueKeys = map[string]struct{}{
	"onError":          {},
	"backoff":          {},
	"resultSchemaMode": {},
	"scope":            {},
}

// ResolveCursor classifies pos against a cached document snapshot. It never
// panics: a nil/parse-error doc or an out-of-range position yields the best
// available classification (falling back to CursorNone), so features degrade
// gracefully while the user is mid-edit.
func ResolveCursor(doc *DocEntry, pos protocol.Position) CursorContext {
	if doc == nil {
		return CursorContext{Kind: CursorNone}
	}

	// 1. Located symbol references win: the index has exact ranges for every
	// resolver/action/call/function occurrence. Unavailable when the solution
	// failed to build (Index == nil), in which case we fall through to
	// structural/text classification.
	if doc.Index != nil {
		if ref, ok := symbolAt(doc.Index, pos); ok {
			r := ref
			return CursorContext{Kind: CursorSymbolRef, Ref: &r}
		}
	}

	line, ok := lineAt(doc.Raw, int(pos.Line))
	if !ok {
		return CursorContext{Kind: CursorNone}
	}
	runes := []rune(line)
	cursor := int(pos.Character)
	if cursor > len(runes) {
		cursor = len(runes)
	}

	// Parse the "key:" structure of the current line (text-based, so it works
	// even when the whole document failed to parse).
	key, keyStart, keyEnd, valueStart, hasValue := parseKeyLine(runes)

	// 2. Cursor within the key token -> schema key context.
	if key != "" && cursor >= keyStart && cursor <= keyEnd {
		path, node := scalarLeafOnLine(doc, int(pos.Line)+1, cursor)
		if path == "" {
			path = blockKeyPath(doc, int(pos.Line)+1, key)
		}
		return CursorContext{Kind: CursorYAMLKey, Path: path, Node: node, PartialToken: string(runes[keyStart:cursor])}
	}

	// 3. Value context: classify by the leaf key. Determine the key/path/value
	// boundary from the parsed node map when available (single-line scalar or an
	// enclosing multi-line block scalar), else fall back to the text value on the
	// line so a mid-edit/unparsed document still classifies. Partial tokens are
	// scanned from the RAW line, which is uniform across plain, quoted, and
	// block-scalar values (no quote/indent offset math).
	//
	// Classification keys off the leaf mapping key alone (e.g. "expr" -> CEL,
	// "provider" -> provider). This is a deliberate text heuristic per the issue:
	// a free-form user input whose key happens to be a spec keyword (e.g. an
	// input literally named "expr") can be misclassified. Spec-owned keys make
	// this rare in practice.
	leafPath, leafNode := scalarLeafOnLine(doc, int(pos.Line)+1, cursor)
	var keyName string
	valueStart2 := 0
	switch {
	case leafPath != "" && leafNode != nil:
		keyName = lastSegment(leafPath)
		valueStart2 = leafNode.Column - 1
	default:
		if bp, bn := blockScalarCovering(doc, int(pos.Line)+1); bp != "" {
			// Cursor is inside a multi-line block-scalar body (expr:|, tmpl:|).
			leafPath, leafNode = bp, bn
			keyName = lastSegment(bp)
			valueStart2 = firstNonSpace(runes)
		} else if key != "" && hasValue {
			keyName = key
			valueStart2 = valueStart
		} else {
			return CursorContext{Kind: CursorNone}
		}
	}

	// Cursor must be at or past where the value begins to be "in the value".
	if cursor < valueStart2 {
		return CursorContext{Kind: CursorNone, Path: leafPath, Node: leafNode}
	}

	switch {
	case keyName == "provider":
		return CursorContext{Kind: CursorProviderName, Path: leafPath, Node: leafNode, PartialToken: backwardIdent(line, cursor)}
	case isKey(enumValueKeys, keyName):
		return CursorContext{Kind: CursorEnumValue, Path: leafPath, Node: leafNode, PartialToken: backwardIdent(line, cursor)}
	case isKey(celValueKeys, keyName):
		ctx := CursorContext{Kind: CursorCEL, Path: leafPath, Node: leafNode}
		ctx.PartialToken, ctx.ExprPrefix = celTokenAt(line, cursor)
		return ctx
	case isKey(templateValueKeys, keyName):
		ctx := CursorContext{Kind: CursorTemplate, Path: leafPath, Node: leafNode}
		ctx.PartialToken, ctx.ExprPrefix = templateTokenAt(line, cursor)
		return ctx
	default:
		return CursorContext{Kind: CursorNone, Path: leafPath, Node: leafNode}
	}
}

// lineAt returns the raw text of the 0-based line index, without its trailing
// carriage return/newline. ok is false when the line is out of range. It scans
// for the target line's byte span rather than splitting the whole document, so
// it stays cheap on the per-keystroke hot path.
func lineAt(raw []byte, line int) (string, bool) {
	if line < 0 {
		return "", false
	}
	start := 0
	for l := 0; l < line; l++ {
		nl := bytes.IndexByte(raw[start:], '\n')
		if nl < 0 {
			return "", false // fewer than line+1 lines
		}
		start += nl + 1
	}
	end := bytes.IndexByte(raw[start:], '\n')
	if end < 0 {
		end = len(raw)
	} else {
		end += start
	}
	return string(bytes.TrimRight(raw[start:end], "\r")), true
}

// parseKeyLine finds the "key:" separator on a YAML line, tolerating a leading
// indent and block-sequence dashes ("- "). It returns the key text, the 0-based
// rune columns spanning the key, the 0-based rune column where the value begins
// (first non-space after the colon), and whether a value is present on the line.
// All results are zero/empty when the line has no mapping key.
func parseKeyLine(runes []rune) (key string, keyStart, keyEnd, valueStart int, hasValue bool) {
	i := 0
	// Skip indentation and any block-sequence "- " markers.
	for i < len(runes) {
		if runes[i] == ' ' || runes[i] == '\t' {
			i++
			continue
		}
		if runes[i] == '-' && i+1 < len(runes) && (runes[i+1] == ' ' || runes[i+1] == '\t') {
			i += 2
			continue
		}
		break
	}
	if i >= len(runes) || runes[i] == '#' {
		return "", 0, 0, 0, false
	}
	keyStart = i
	// The key ends at the first ": " or a trailing ":" (YAML's key/value
	// separator). A colon inside the value (e.g. a CEL string) is not it.
	sep := -1
	for j := i; j < len(runes); j++ {
		if runes[j] == ':' && (j+1 >= len(runes) || runes[j+1] == ' ' || runes[j+1] == '\t') {
			sep = j
			break
		}
	}
	if sep < 0 {
		return "", 0, 0, 0, false
	}
	keyEnd = sep
	key = strings.TrimSpace(string(runes[keyStart:sep]))
	if key == "" {
		return "", 0, 0, 0, false
	}
	// Value begins at the first non-space after the colon.
	v := sep + 1
	for v < len(runes) && (runes[v] == ' ' || runes[v] == '\t') {
		v++
	}
	valueStart = v
	hasValue = v < len(runes) && runes[v] != '#'
	return key, keyStart, keyEnd, valueStart, hasValue
}

// scalarLeafOnLine returns the scalar value node whose start is on the 1-based
// line and, among several on the same line (flow mappings), the one whose value
// span contains the cursor column; it falls back to the deepest path. Returns
// "", nil when the document has no node map (broken YAML) or no scalar value on
// that line.
func scalarLeafOnLine(doc *DocEntry, line, cursor int) (string, *yaml.Node) {
	if doc.Nodes == nil {
		return "", nil
	}
	bestPath := ""
	var bestNode *yaml.Node
	containingPath := ""
	var containingNode *yaml.Node
	for path, n := range doc.Nodes {
		if n == nil || n.Kind != yaml.ScalarNode || n.Line != line {
			continue
		}
		// Prefer a node whose value span contains the cursor column.
		start := n.Column - 1
		end := start + len([]rune(n.Value))
		if cursor >= start && cursor <= end && len(path) > len(containingPath) {
			containingPath = path
			containingNode = n
		}
		if len(path) > len(bestPath) {
			bestPath = path
			bestNode = n
		}
	}
	if containingNode != nil {
		return containingPath, containingNode
	}
	return bestPath, bestNode
}

// blockScalarCovering returns the block scalar (literal | or folded >) whose
// multi-line body covers the 1-based line, and its path. yaml.v3 positions a
// block scalar at its key line, with the body on the following lines, so a
// cursor inside the body has no node on its own line -- this finds the enclosing
// block instead. Returns "", nil when no block scalar covers the line.
func blockScalarCovering(doc *DocEntry, line int) (string, *yaml.Node) {
	if doc.Nodes == nil {
		return "", nil
	}
	best := ""
	var bestNode *yaml.Node
	for path, n := range doc.Nodes {
		if n == nil || n.Kind != yaml.ScalarNode {
			continue
		}
		if n.Style != yaml.LiteralStyle && n.Style != yaml.FoldedStyle {
			continue
		}
		bodyEnd := n.Line + strings.Count(n.Value, "\n")
		if line > n.Line && line <= bodyEnd && len(path) > len(best) {
			best = path
			bestNode = n
		}
	}
	return best, bestNode
}

// firstNonSpace returns the 0-based rune index of the first non-space/tab rune,
// or len(runes) when the line is all whitespace.
func firstNonSpace(runes []rune) int {
	for i, r := range runes {
		if r != ' ' && r != '\t' {
			return i
		}
	}
	return len(runes)
}

// blockKeyPath finds a best-effort logical path for a mapping key that has no
// scalar value on its own line (a nested block). YAML positions a mapping node
// at its first child, so the node whose leaf segment matches key and whose start
// is nearest at/after the key line is the closest match.
func blockKeyPath(doc *DocEntry, line int, key string) string {
	if doc.Nodes == nil {
		return ""
	}
	best := ""
	bestLine := -1
	for path, n := range doc.Nodes {
		if n == nil || lastSegment(path) != key || n.Line < line {
			continue
		}
		if bestLine == -1 || n.Line < bestLine {
			best = path
			bestLine = n.Line
		}
	}
	return best
}

// lastSegment returns the final dotted/indexed path segment's key name, e.g.
// "spec.resolvers.a.resolve.with[0].provider" -> "provider".
func lastSegment(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		path = path[i+1:]
	}
	if i := strings.IndexByte(path, '['); i >= 0 {
		path = path[:i]
	}
	return path
}

func isKey(set map[string]struct{}, k string) bool {
	_, ok := set[k]
	return ok
}

// backwardIdent returns the identifier fragment ending at rune index i in s,
// scanning back over identifier characters. Used to capture the partial token a
// user has typed for a provider/enum value.
func backwardIdent(s string, i int) string {
	runes := []rune(s)
	if i > len(runes) {
		i = len(runes)
	}
	if i < 0 {
		i = 0
	}
	start := i
	for start > 0 && isIdentRune(runes[start-1]) {
		start--
	}
	return string(runes[start:i])
}

// celTokenAt extracts the partial token and reference prefix at rune index i in
// a CEL expression value.
func celTokenAt(expr string, i int) (partial, prefix string) {
	tok := backwardDotIdent(expr, i)
	switch {
	case strings.HasPrefix(tok, celResolverPrefix):
		return tok[len(celResolverPrefix):], celResolverPrefix
	case strings.HasPrefix(tok, celActionPrefix):
		return tok[len(celActionPrefix):], celActionPrefix
	default:
		return tok, ""
	}
}

// templateTokenAt extracts the partial token and reference prefix at rune index
// i in a Go-template value. The token is only meaningful inside a {{ ... }}
// action; outside one, prefix and partial are empty.
func templateTokenAt(tmpl string, i int) (partial, prefix string) {
	action, ok := enclosingAction(tmpl, i)
	if !ok {
		return "", ""
	}
	tok := backwardDotIdent(action, len([]rune(action)))
	switch {
	case strings.HasPrefix(tok, tmplResolverPrefix):
		return tok[len(tmplResolverPrefix):], tmplResolverPrefix
	case strings.HasPrefix(tok, tmplActionPrefix):
		return tok[len(tmplActionPrefix):], tmplActionPrefix
	case strings.HasPrefix(tok, tmplFieldPrefix):
		return tok[len(tmplFieldPrefix):], tmplFieldPrefix
	default:
		return tok, ""
	}
}

// enclosingAction returns the text of the {{ ... }} action from just after the
// opening "{{" up to rune index i, when i is inside an open (unclosed-before-i)
// action. ok is false when i is not inside an action.
func enclosingAction(tmpl string, i int) (string, bool) {
	runes := []rune(tmpl)
	if i > len(runes) {
		i = len(runes)
	}
	open := -1
	for j := 0; j+1 < i; j++ {
		if runes[j] == '{' && runes[j+1] == '{' {
			open = j + 2
		}
		if runes[j] == '}' && runes[j+1] == '}' {
			open = -1
		}
	}
	if open < 0 || open > i {
		return "", false
	}
	return string(runes[open:i]), true
}

// backwardDotIdent scans back from rune index i over identifier characters plus
// '.', returning the dotted token (e.g. "_.env", "._.env", ".__actions.foo",
// "toUpper").
func backwardDotIdent(s string, i int) string {
	runes := []rune(s)
	if i > len(runes) {
		i = len(runes)
	}
	if i < 0 {
		i = 0
	}
	start := i
	for start > 0 && (isIdentRune(runes[start-1]) || runes[start-1] == '.') {
		start--
	}
	return string(runes[start:i])
}

func isIdentRune(r rune) bool {
	return r == '_' ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9')
}
