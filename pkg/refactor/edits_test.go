// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package refactor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// editFixture is a solution exercising every removal/replace shape the edit
// builders must handle: a resolver with a dependsOn list (some redundant), a
// deprecated onError scalar, a nested block resolver to remove wholesale, and
// sibling entries that must survive untouched. Comments are present so
// source-preservation can be asserted.
const editFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: edits-test # keep this comment
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    region:
      resolve:
        with:
          - provider: parameter
            onError: continue # trailing comment
            inputs:
              value: us
    appName:
      dependsOn:
        - environment
        - region
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                expr: _.environment
`

// reparseYAML asserts the rewritten bytes still parse as generic YAML, so a
// removal/replacement never produces a structurally broken document.
func reparseYAML(t *testing.T, out []byte) {
	t.Helper()
	var m map[string]any
	require.NoError(t, yaml.Unmarshal(out, &m), "rewritten output must re-parse as YAML")
}

func TestRemoveMappingEntry_UnusedResolver(t *testing.T) {
	raw := []byte(editFixture)

	edit, err := RemoveMappingEntry(raw, "spec.resolvers.region")
	require.NoError(t, err)

	out, err := Apply(raw, []TextEdit{edit})
	require.NoError(t, err)
	s := string(out)

	assert.NotContains(t, s, "region:", "the region resolver block is gone")
	assert.NotContains(t, s, "value: us", "its nested content is gone")
	// Siblings survive, including their comments.
	assert.Contains(t, s, "environment:")
	assert.Contains(t, s, "appName:")
	assert.Contains(t, s, "# keep this comment")
	// No blank line left where the block was removed.
	assert.NotContains(t, s, "\n\n    appName:")
	reparseYAML(t, out)
}

func TestRemoveMappingEntry_DependsOnWholeEntry(t *testing.T) {
	raw := []byte(editFixture)

	edit, err := RemoveMappingEntry(raw, "spec.resolvers.appName.dependsOn")
	require.NoError(t, err)

	out, err := Apply(raw, []TextEdit{edit})
	require.NoError(t, err)
	s := string(out)

	assert.NotContains(t, s, "dependsOn:")
	assert.NotContains(t, s, "- environment\n")
	// The resolve block that followed dependsOn is intact.
	assert.Contains(t, s, "expr: _.environment")
	reparseYAML(t, out)
}

func TestRemoveMappingEntry_Errors(t *testing.T) {
	raw := []byte(editFixture)
	_, err := RemoveMappingEntry(raw, "spec.resolvers.doesNotExist")
	require.Error(t, err)
}

func TestRemoveSequenceElement_MiddleAndFirst(t *testing.T) {
	raw := []byte(editFixture)

	// Remove the first dependsOn element (environment).
	edit, err := RemoveSequenceElement(raw, "spec.resolvers.appName.dependsOn[0]")
	require.NoError(t, err)
	out, err := Apply(raw, []TextEdit{edit})
	require.NoError(t, err)
	s := string(out)

	assert.NotContains(t, s, "- environment")
	assert.Contains(t, s, "- region", "the sibling element survives")
	assert.Contains(t, s, "dependsOn:", "the list key survives")
	reparseYAML(t, out)
}

func TestRemoveSequenceElement_Errors(t *testing.T) {
	raw := []byte(editFixture)
	// A path that is not a sequence element scalar line.
	_, err := RemoveSequenceElement(raw, "spec.resolvers.appName.dependsOn[9]")
	require.Error(t, err)
}

func TestReplaceMappingKeyAndValue_OnErrorContinue(t *testing.T) {
	raw := []byte(editFixture)

	edit, err := ReplaceMappingKeyAndValue(raw,
		"spec.resolvers.region.resolve.with[0].onError", "continueOnError", "true")
	require.NoError(t, err)

	out, err := Apply(raw, []TextEdit{edit})
	require.NoError(t, err)
	s := string(out)

	assert.Contains(t, s, "continueOnError: true")
	assert.NotContains(t, s, "onError: continue")
	// The trailing inline comment on the value line is preserved.
	assert.Contains(t, s, "# trailing comment")
	// Indentation preserved (12 spaces before the key in this fixture).
	assert.Contains(t, s, "            continueOnError: true")
	reparseYAML(t, out)
}

func TestReplaceMappingKeyAndValue_Errors(t *testing.T) {
	raw := []byte(editFixture)
	// Value node that is a mapping, not a scalar.
	_, err := ReplaceMappingKeyAndValue(raw, "spec.resolvers.environment", "x", "y")
	require.Error(t, err)
	// Nonexistent path.
	_, err = ReplaceMappingKeyAndValue(raw, "spec.resolvers.nope.onError", "x", "y")
	require.Error(t, err)
}

func TestReplaceMappingKeyAndValue_QuotedValue(t *testing.T) {
	const quoted = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: q
spec:
  resolvers:
    r:
      resolve:
        with:
          - provider: parameter
            onError: "fail"
            inputs:
              value: x
`
	raw := []byte(quoted)
	edit, err := ReplaceMappingKeyAndValue(raw,
		"spec.resolvers.r.resolve.with[0].onError", "continueOnError", "false")
	require.NoError(t, err)
	out, err := Apply(raw, []TextEdit{edit})
	require.NoError(t, err)
	s := string(out)
	assert.Contains(t, s, "continueOnError: false")
	assert.NotContains(t, s, `onError: "fail"`)
	assert.NotContains(t, s, `"fail"`)
	reparseYAML(t, out)
}

// TestReplaceMappingKeyAndValue_EscapedQuotedValue exercises the span
// computation for quoted scalars whose raw token length differs from the decoded
// value length because of escapes. Computing the span from the decoded length
// would stop short and leave an orphaned quote; the source-scanning end must
// consume the whole token.
func TestReplaceMappingKeyAndValue_EscapedQuotedValue(t *testing.T) {
	cases := map[string]string{
		// double-quoted with an escaped quote: decoded a"b (3) != raw "a\"b" (6).
		"double-quoted escape": `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: q
  description: "a\"b"
spec:
  resolvers: {}
`,
		// single-quoted with a doubled '' escape: decoded a'b (3) != raw 'a''b' (6).
		"single-quoted escape": `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: q
  description: 'a''b'
spec:
  resolvers: {}
`,
	}
	for name, y := range cases {
		t.Run(name, func(t *testing.T) {
			raw := []byte(y)
			edit, err := ReplaceMappingKeyAndValue(raw, "metadata.description", "displayName", "renamed")
			require.NoError(t, err)
			out, err := Apply(raw, []TextEdit{edit})
			require.NoError(t, err)
			s := string(out)
			assert.Contains(t, s, "displayName: renamed")
			// No fragment of the original quoted token survives.
			assert.NotContains(t, s, "description:")
			assert.NotContains(t, s, `b"`)
			assert.NotContains(t, s, `b'`)
			reparseYAML(t, out)
		})
	}
}

func TestIsSequenceMarker(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		indent int
		want   bool
	}{
		{"dash space value", "  - value", 2, true},
		{"dash tab value", "  -\tvalue", 2, true},
		{"bare dash at eol", "  -", 2, true},
		{"dash CR (crlf)", "  -\r", 2, true},
		{"double dash is scalar", "  --foo", 2, false},
		{"dash fused to letter", "  -foo", 2, false},
		{"not a dash", "  foo", 2, false},
		{"indent past end", "  ", 2, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isSequenceMarker([]byte(tt.line), tt.indent))
		})
	}
}
