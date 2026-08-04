// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package authorfuncs

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompile_InvalidName(t *testing.T) {
	_, err := Compile(map[string]*spec.Function{
		"1bad": {Cel: "1"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a valid identifier")
}

func TestCompile_ReservedPrefix(t *testing.T) {
	_, err := Compile(map[string]*spec.Function{
		"__hidden": {Cel: "1"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reserved prefix")
}

func TestCompile_BuiltinCollision(t *testing.T) {
	_, err := Compile(map[string]*spec.Function{
		"upper": {Params: []*spec.ParamDef{{Name: "x"}}, Cel: "_.args.x"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "collides with a built-in")
}

func TestCompile_NoBody(t *testing.T) {
	_, err := Compile(map[string]*spec.Function{
		"f": {Params: []*spec.ParamDef{{Name: "x"}}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must set exactly one of cel or template")
}

func TestCompile_BothBodies(t *testing.T) {
	_, err := Compile(map[string]*spec.Function{
		"f": {Cel: "1", Template: "x"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestCompile_InvalidCelBody(t *testing.T) {
	_, err := Compile(map[string]*spec.Function{
		"f": {Cel: "_.args.x +"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid cel body")
}

func TestCompile_InvalidTemplateBody(t *testing.T) {
	_, err := Compile(map[string]*spec.Function{
		"f": {Template: "{{ notAFunc }}"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid template body")
}

func TestCompile_DuplicateParam(t *testing.T) {
	_, err := Compile(map[string]*spec.Function{
		"f": {
			Params: []*spec.ParamDef{{Name: "x"}, {Name: "x"}},
			Cel:    "_.args.x",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "declared more than once")
}

func TestCompile_RequiredWithDefault(t *testing.T) {
	_, err := Compile(map[string]*spec.Function{
		"f": {
			Params: []*spec.ParamDef{{Name: "x", Required: true, Default: "v"}},
			Cel:    "_.args.x",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "may not declare a default")
}

func TestCompile_RequiredAfterOptional(t *testing.T) {
	_, err := Compile(map[string]*spec.Function{
		"f": {
			Params: []*spec.ParamDef{
				{Name: "a", Default: "v"},
				{Name: "b", Required: true},
			},
			Cel: "_.args.a",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not follow an optional parameter")
}

func TestCompile_UnknownParamType(t *testing.T) {
	_, err := Compile(map[string]*spec.Function{
		"f": {
			Params: []*spec.ParamDef{{Name: "x", Type: spec.Type("nope")}},
			Cel:    "_.args.x",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown type")
}

func TestCompile_InvalidParamName(t *testing.T) {
	_, err := Compile(map[string]*spec.Function{
		"f": {
			Params: []*spec.ParamDef{{Name: "1x"}},
			Cel:    "1",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a valid identifier")
}

func TestCompile_NilParam(t *testing.T) {
	_, err := Compile(map[string]*spec.Function{
		"f": {
			Params: []*spec.ParamDef{nil},
			Cel:    "1",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is empty")
}

func TestCompile_DirectCycle(t *testing.T) {
	_, err := Compile(map[string]*spec.Function{
		"self": {
			Params:   []*spec.ParamDef{{Name: "x"}},
			Template: "{{ self .args.x }}",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestCompile_IndirectCycle(t *testing.T) {
	_, err := Compile(map[string]*spec.Function{
		"a": {Params: []*spec.ParamDef{{Name: "x"}}, Template: "{{ b .args.x }}"},
		"b": {Params: []*spec.ParamDef{{Name: "x"}}, Template: "{{ a .args.x }}"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "a -> b -> a")
}

func TestCompile_AcyclicChainOK(t *testing.T) {
	lib, err := Compile(map[string]*spec.Function{
		"a": {Params: []*spec.ParamDef{{Name: "x"}}, Template: "{{ b .args.x }}"},
		"b": {Params: []*spec.ParamDef{{Name: "x"}}, Template: "B:{{ .args.x }}"},
	})
	require.NoError(t, err)
	require.NotNil(t, lib)
}

func TestCompile_AggregatesProblems(t *testing.T) {
	_, err := Compile(map[string]*spec.Function{
		"1bad":     {Cel: "1"},
		"__hidden": {Cel: "1"},
	})
	require.Error(t, err)
	// Both problems are surfaced together.
	assert.Contains(t, err.Error(), "not a valid identifier")
	assert.Contains(t, err.Error(), "reserved prefix")
}

func TestValidateFunctionName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"valid simple", "greet", ""},
		{"valid underscore", "greet_user", ""},
		{"valid leading underscore", "_greet", ""},
		{"empty", "", "not a valid function name"},
		{"leading digit", "1greet", "not a valid function name"},
		{"hyphen not allowed", "greet-user", "not a valid function name"},
		{"reserved prefix", "__greet", "reserved prefix"},
		{"builtin collision", "printf", "built-in template function"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFunctionName(tt.input)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
