// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmpl

import "text/template/parse"

// parseTemplatesTolerant parses a template for REFERENCE/DEPENDENCY extraction,
// tolerating unknown functions -- typically solution-author-defined helpers
// (spec.functions) invoked as {{ myFunc .x }}. By default text/template's parser
// rejects a template that invokes a function it does not know, which would
// otherwise make every reference in that template invisible to the extractor
// (breaking dependency inference, lint, state param tracking, and `eval refs`).
//
// It uses the standard-library parser's parse.SkipFuncCheck mode, which disables
// the "is this function defined" check while still reporting genuine syntax
// errors. This is safe for reference extraction because it only affects whether
// unknown identifiers in function position are accepted -- extraction walks the
// resulting parse tree structurally and nothing is executed. It must NOT be used
// for template execution or syntax validation, where an undefined function is a
// real error the caller needs to see.
//
// funcs is still passed through so declared built-in/extension/author names are
// available to the tree (harmless under SkipFuncCheck); the map is not mutated.
func parseTemplatesTolerant(name, content, leftDelim, rightDelim string, funcs map[string]any) (map[string]*parse.Tree, error) {
	t := parse.New(name)
	t.Mode = parse.SkipFuncCheck
	treeSet := make(map[string]*parse.Tree)
	if _, err := t.Parse(content, leftDelim, rightDelim, treeSet, funcs); err != nil {
		return nil, err
	}
	return treeSet, nil
}
