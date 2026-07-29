// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solution

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTemplateFuncBinder_None(t *testing.T) {
	s := &Solution{Spec: Spec{}}
	binder, err := s.TemplateFuncBinder()
	require.NoError(t, err)
	assert.Nil(t, binder)

	var nilSol *Solution
	binder, err = nilSol.TemplateFuncBinder()
	require.NoError(t, err)
	assert.Nil(t, binder)
}

func TestTemplateFuncBinder_Valid(t *testing.T) {
	s := &Solution{Spec: Spec{
		Functions: map[string]*spec.Function{
			"doubled": {
				Params: []*spec.ParamDef{{Name: "n", Type: spec.TypeInt, Required: true}},
				Cel:    "_.args.n * 2",
			},
		},
	}}
	binder, err := s.TemplateFuncBinder()
	require.NoError(t, err)
	require.NotNil(t, binder)
	assert.Equal(t, []string{"doubled"}, binder.Names())
}

func TestTemplateFuncBinder_Invalid(t *testing.T) {
	s := &Solution{Spec: Spec{
		Functions: map[string]*spec.Function{
			"printf": {Cel: "1"}, // collides with built-in
		},
	}}
	binder, err := s.TemplateFuncBinder()
	require.Error(t, err)
	assert.Nil(t, binder)
}

func TestHasFunctions(t *testing.T) {
	assert.False(t, (&Spec{}).HasFunctions())
	assert.False(t, (&Spec{Functions: map[string]*spec.Function{}}).HasFunctions())
	assert.True(t, (&Spec{Functions: map[string]*spec.Function{"f": {Cel: "1"}}}).HasFunctions())

	var nilSpec *Spec
	assert.False(t, nilSpec.HasFunctions())
}

func TestValidateFunctions(t *testing.T) {
	tests := []struct {
		name    string
		funcs   map[string]*spec.Function
		wantSub string
	}{
		{
			name:    "no functions",
			funcs:   nil,
			wantSub: "",
		},
		{
			name: "valid",
			funcs: map[string]*spec.Function{
				"f": {Params: []*spec.ParamDef{{Name: "x"}}, Cel: "_.args.x"},
			},
			wantSub: "",
		},
		{
			name: "builtin collision",
			funcs: map[string]*spec.Function{
				"printf": {Cel: "1"},
			},
			wantSub: "collides with a built-in",
		},
		{
			name: "no body",
			funcs: map[string]*spec.Function{
				"f": {Params: []*spec.ParamDef{{Name: "x"}}},
			},
			wantSub: "must set exactly one",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Solution{Spec: Spec{Functions: tt.funcs}}
			problems := s.validateFunctions()
			if tt.wantSub == "" {
				assert.Empty(t, problems)
				return
			}
			require.NotEmpty(t, problems)
			assert.Contains(t, joinProblems(problems), tt.wantSub)
		})
	}
}
