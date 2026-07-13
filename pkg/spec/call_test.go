// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestCallRef_HasCall(t *testing.T) {
	tests := []struct {
		name string
		ref  *CallRef
		want bool
	}{
		{name: "nil ref", ref: nil, want: false},
		{name: "empty call", ref: &CallRef{}, want: false},
		{name: "with call", ref: &CallRef{Call: "groupCheck"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.ref.HasCall())
		})
	}
}

func TestCall_UnmarshalYAML(t *testing.T) {
	const src = `
args:
  user:
    type: string
    required: true
  groups:
    type: array
    required: false
    default: []
provider: http
inputs:
  method:
    tmpl: "POST"
  body:
    tmpl: |
      {"user": "{{ .args.user }}"}
dedup: true
`
	var c Call
	require.NoError(t, yaml.Unmarshal([]byte(src), &c))

	assert.Equal(t, "http", c.Provider)
	assert.True(t, c.Dedup)

	require.Contains(t, c.Args, "user")
	assert.Equal(t, TypeString, c.Args["user"].Type)
	assert.True(t, c.Args["user"].Required)

	require.Contains(t, c.Args, "groups")
	assert.Equal(t, TypeArray, c.Args["groups"].Type)
	assert.False(t, c.Args["groups"].Required)
	assert.Equal(t, []any{}, c.Args["groups"].Default)

	require.Contains(t, c.Inputs, "method")
	require.Contains(t, c.Inputs, "body")
}

func TestArgDef_JSONRoundTrip(t *testing.T) {
	orig := ArgDef{
		Type:        TypeInt,
		Required:    false,
		Default:     42,
		Description: "the count",
	}

	data, err := json.Marshal(orig)
	require.NoError(t, err)

	var got ArgDef
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, orig.Type, got.Type)
	assert.Equal(t, orig.Required, got.Required)
	assert.Equal(t, orig.Description, got.Description)
	// JSON numbers decode as float64.
	assert.EqualValues(t, 42, got.Default)
}

// hostStep mirrors the way resolver/action step structs embed CallRef inline so
// the inline promotion behavior is exercised at the spec package boundary.
type hostStep struct {
	CallRef  `yaml:",inline"`
	Provider string `json:"provider,omitempty" yaml:"provider,omitempty"`
}

func TestCallRef_InlineEmbedding_YAML(t *testing.T) {
	const src = `
call: groupCheck
args:
  user:
    expr: "_.requestor"
`
	var step hostStep
	require.NoError(t, yaml.Unmarshal([]byte(src), &step))

	assert.Empty(t, step.Provider)
	assert.Equal(t, "groupCheck", step.Call)
	require.Contains(t, step.Args, "user")
	require.NotNil(t, step.Args["user"].Expr)
	assert.Equal(t, "_.requestor", string(*step.Args["user"].Expr))
}

func TestCallRef_InlineEmbedding_JSON(t *testing.T) {
	step := hostStep{
		CallRef: CallRef{Call: "groupCheck"},
	}
	data, err := json.Marshal(step)
	require.NoError(t, err)

	// The embedded Call field must promote to the top level, not nest.
	assert.JSONEq(t, `{"call":"groupCheck"}`, string(data))

	var got hostStep
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "groupCheck", got.Call)
}
