// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"encoding/json"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnumOptOmitsKeyWhenEmpty is the targeted guard for issue #819: an empty
// computed enum must omit the "enum" key entirely rather than emit "enum": null
// (invalid JSON Schema draft 2020-12) or "enum": [] (semantically "no value
// allowed"). This exercises the property builder directly, independent of any
// ambient example state, so it catches a regression even in a repo checkout
// where the broad meta-validation below happens to pass.
func TestEnumOptOmitsKeyWhenEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		values     []string
		wantKey    bool
		wantValues []string
	}{
		{name: "nil slice omits key", values: nil, wantKey: false},
		{name: "empty slice omits key", values: []string{}, wantKey: false},
		{name: "non-empty slice writes array", values: []string{"a", "b"}, wantKey: true, wantValues: []string{"a", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			schema := map[string]any{}
			enumOpt(tt.values...)(schema)

			v, ok := schema["enum"]
			assert.Equal(t, tt.wantKey, ok, "presence of enum key")
			if tt.wantKey {
				assert.Equal(t, tt.wantValues, v)
			} else {
				// Explicitly assert the key is absent -- a nil value under the
				// key would still marshal to "enum": null, which is the bug.
				_, present := schema["enum"]
				assert.False(t, present, "enum key must be absent, not present-with-nil")
			}
		})
	}
}

// TestAllToolSchemasAreValidDraft2020 compiles every registered tool's
// inputSchema and outputSchema against JSON Schema draft 2020-12. A single
// invalid schema (e.g. "enum": null) causes strict MCP clients to reject the
// ENTIRE tool list, so this broad gate fails the build if any advertised schema
// is malformed. The meta-schemas are embedded in the jsonschema library, so
// this runs fully offline.
//
// NOTE: this broad test alone would NOT have caught the original #819 bug in a
// repo checkout (where the embedded examples populate the enum to a valid,
// non-empty array); TestEnumOptOmitsKeyWhenEmpty is the targeted companion that
// does. Both are kept deliberately.
func TestAllToolSchemasAreValidDraft2020(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	tools := srv.mcpServer.ListTools()
	require.NotEmpty(t, tools, "server should have registered tools")

	for name, st := range tools {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Marshal the tool exactly as an MCP client would receive it, then
			// validate each schema block it carries.
			raw, err := json.Marshal(st.Tool)
			require.NoError(t, err)

			var toolJSON map[string]any
			require.NoError(t, json.Unmarshal(raw, &toolJSON))

			for _, key := range []string{"inputSchema", "outputSchema"} {
				schema, ok := toolJSON[key].(map[string]any)
				if !ok {
					continue
				}
				assertValidDraft2020Schema(t, name, key, schema)
			}
		})
	}
}

// assertValidDraft2020Schema fails the test if schemaDoc is not a well-formed
// JSON Schema draft 2020-12 document. Compilation performs meta-schema
// validation, so an invalid construct (e.g. a non-array enum) is reported.
func assertValidDraft2020Schema(t *testing.T, toolName, field string, schemaDoc map[string]any) {
	t.Helper()

	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)

	const loc = "mem://tool-schema.json"
	require.NoError(t, c.AddResource(loc, schemaDoc),
		"tool %q %s: adding schema resource", toolName, field)

	_, err := c.Compile(loc)
	assert.NoErrorf(t, err,
		"tool %q %s is not a valid JSON Schema draft 2020-12 document", toolName, field)
}
