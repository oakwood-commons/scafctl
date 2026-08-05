// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/schema"
	"github.com/stretchr/testify/require"
)

// vscodeSnippet mirrors the VS Code snippet JSON format
// (https://code.visualstudio.com/docs/editor/userdefinedsnippets): a name ->
// {prefix, body, description} map. body is an array of lines joined with
// "\n" when expanded in the editor.
type vscodeSnippet struct {
	Prefix      string   `json:"prefix"`
	Body        []string `json:"body"`
	Description string   `json:"description"`
}

// snippetKeyLineRe matches a YAML "key:" at the start of a line (after
// optional indentation and an optional leading "- " list marker), capturing
// the indentation and the key name. It intentionally only matches a
// *literal* leading identifier -- tab-stop placeholders like
// "${1:myResolver}:" never match, since they don't start with a letter or
// underscore.
var snippetKeyLineRe = regexp.MustCompile(`^(\s*)(?:-\s*)?([A-Za-z_][A-Za-z0-9_]*):(\s|$)`)

func lineIndent(s string) int {
	n := 0
	for _, c := range s {
		if c != ' ' {
			break
		}
		n++
	}
	return n
}

// extractNotableKeys returns the set of literal, schema-relevant YAML keys
// referenced in a snippet body, in first-seen order.
//
// It deliberately excludes:
//   - tab-stop placeholder "keys" (e.g. "${1:myResolver}:"), which stand in
//     for author-chosen identifiers (resolver/action/call/function/arg
//     names), not schema field names.
//   - keys nested under an "inputs:" block, which are provider-specific
//     input field names (e.g. "url", "method", "key") validated by each
//     provider's own descriptor, not the generic solution schema.
func extractNotableKeys(body []string) []string {
	seen := map[string]bool{}
	var keys []string
	opaqueIndent := -1 // indent level of an active "inputs:" block; -1 = none
	for _, raw := range body {
		indent := lineIndent(raw)
		if opaqueIndent >= 0 && indent <= opaqueIndent {
			opaqueIndent = -1 // dedented out of the inputs: block
		}
		m := snippetKeyLineRe.FindStringSubmatch(raw)
		if m == nil {
			continue
		}
		key := m[2]
		if opaqueIndent < 0 {
			if !seen[key] {
				seen[key] = true
				keys = append(keys, key)
			}
		}
		if key == "inputs" {
			opaqueIndent = indent
		}
	}
	return keys
}

// schemaKeySet walks the generated solution JSON schema (all $defs plus the
// top-level document) and returns the set of every property name that
// appears anywhere in it. This is intentionally permissive (a flat set
// rather than a per-object check) since snippet bodies compose fragments
// from several different schema objects (e.g. a resolver's "type" and a
// call's "args") and re-deriving the exact nesting path for every literal
// line is unnecessary to catch the drift this guard cares about: a snippet
// referencing a field name that no longer exists anywhere in the schema.
func schemaKeySet(t *testing.T) map[string]bool {
	t.Helper()

	schemaBytes, err := schema.GenerateSolutionSchema()
	require.NoError(t, err, "generating solution schema")

	var doc map[string]any
	require.NoError(t, json.Unmarshal(schemaBytes, &doc), "unmarshalling solution schema")

	keys := map[string]bool{}
	var walk func(node any)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			if props, ok := v["properties"].(map[string]any); ok {
				for k := range props {
					keys[k] = true
				}
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(doc)
	return keys
}

// TestSnippetsMatchSchema is a drift guard: every literal YAML key that a VS
// Code snippet body hard-codes (resolver/action/call/function/spec field
// names -- not author-chosen identifiers or provider-specific input names)
// must still exist somewhere in the generated solution JSON schema. If the
// schema changes a field name and the snippets are not updated to match,
// this test fails loudly instead of the drift going unnoticed.
func TestSnippetsMatchSchema(t *testing.T) {
	projectRoot := findProjectRoot()
	snippetsPath := filepath.Join(projectRoot, "editors", "vscode", "snippets", "scafctl.json")

	data, err := os.ReadFile(snippetsPath)
	require.NoError(t, err, "reading %s", snippetsPath)

	var snippets map[string]vscodeSnippet
	require.NoError(t, json.Unmarshal(data, &snippets), "parsing %s", snippetsPath)
	require.NotEmpty(t, snippets, "expected at least one snippet in %s", snippetsPath)

	validKeys := schemaKeySet(t)

	for name, snip := range snippets {
		t.Run(name, func(t *testing.T) {
			require.NotEmpty(t, snip.Prefix, "snippet %q is missing a prefix", name)
			require.NotEmpty(t, snip.Body, "snippet %q has an empty body", name)

			for _, key := range extractNotableKeys(snip.Body) {
				require.True(t, validKeys[key],
					"snippet %q (prefix %q) references key %q, which no longer exists "+
						"in the generated solution schema -- update the snippet body or, "+
						"if the key is a provider-specific input name, ensure it is nested "+
						"under an \"inputs:\" block so this guard skips it",
					name, snip.Prefix, key)
			}
		})
	}
}
