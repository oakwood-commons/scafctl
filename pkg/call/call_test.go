// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package call

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func strArg() *spec.ArgDef {
	return &spec.ArgDef{Type: spec.TypeString, Required: true}
}

func TestBindArgs_SuppliedAndCoerced(t *testing.T) {
	def := &spec.Call{
		Args: map[string]*spec.ArgDef{
			"user":  strArg(),
			"count": {Type: spec.TypeInt},
		},
		Provider: "http",
	}

	bound, err := BindArgs("groupCheck", def, map[string]any{
		"user":  "alice",
		"count": "42", // string coerced to int
	})
	require.NoError(t, err)
	assert.Equal(t, "alice", bound["user"])
	assert.EqualValues(t, 42, bound["count"])
}

func TestBindArgs_MissingRequired(t *testing.T) {
	def := &spec.Call{
		Args:     map[string]*spec.ArgDef{"user": strArg()},
		Provider: "http",
	}
	_, err := BindArgs("groupCheck", def, map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `call "groupCheck"`)
	assert.Contains(t, err.Error(), `missing required argument "user"`)
}

// TestBindArgs_DeterministicFirstError verifies that when several required
// arguments are missing, the error names the first in sorted declared-arg
// order, independent of Go's randomized map iteration order.
func TestBindArgs_DeterministicFirstError(t *testing.T) {
	def := &spec.Call{
		Args: map[string]*spec.ArgDef{
			"zeta":  strArg(),
			"alpha": strArg(),
			"mid":   strArg(),
		},
		Provider: "http",
	}
	for i := 0; i < 50; i++ {
		_, err := BindArgs("c", def, map[string]any{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `missing required argument "alpha"`)
	}
}

func TestBindArgs_UnknownArg(t *testing.T) {
	def := &spec.Call{
		Args:     map[string]*spec.ArgDef{"user": strArg()},
		Provider: "http",
	}
	_, err := BindArgs("groupCheck", def, map[string]any{
		"user":  "alice",
		"extra": "nope",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown argument(s) "extra"`)
	assert.Contains(t, err.Error(), "declared args: [user]")
}

func TestBindArgs_DefaultsApplied(t *testing.T) {
	def := &spec.Call{
		Args: map[string]*spec.ArgDef{
			"env":    {Type: spec.TypeString, Default: "dev"},
			"groups": {Type: spec.TypeArray},
		},
		Provider: "http",
	}
	bound, err := BindArgs("c", def, map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, "dev", bound["env"])
	// Omitted optional array with no default becomes an empty (non-nil) slice.
	assert.Equal(t, []any{}, bound["groups"])
}

func TestBindArgs_TypeCoercionError(t *testing.T) {
	def := &spec.Call{
		Args:     map[string]*spec.ArgDef{"count": {Type: spec.TypeInt}},
		Provider: "http",
	}
	_, err := BindArgs("c", def, map[string]any{"count": "not-a-number"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `argument "count"`)
}

func TestBindArgs_ListAndMapPreserved(t *testing.T) {
	def := &spec.Call{
		Args: map[string]*spec.ArgDef{
			"groups": {Type: spec.TypeArray, Required: true},
			"labels": {Type: spec.TypeObject, Required: true},
		},
		Provider: "http",
	}
	bound, err := BindArgs("c", def, map[string]any{
		"groups": []any{"a", "b"},
		"labels": map[string]any{"team": "core"},
	})
	require.NoError(t, err)
	assert.Equal(t, []any{"a", "b"}, bound["groups"])
	assert.Equal(t, map[string]any{"team": "core"}, bound["labels"])
}

func TestBindArgs_NilDefinition(t *testing.T) {
	_, err := BindArgs("c", nil, map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil definition")
}

func TestExpandData_InjectsArgsWithoutMutating(t *testing.T) {
	resolverData := map[string]any{"requestor": "alice"}
	boundArgs := map[string]any{"user": "alice"}

	enriched := ExpandData(resolverData, boundArgs)

	assert.Equal(t, "alice", enriched["requestor"])
	assert.Equal(t, boundArgs, enriched[ArgsNamespace])

	// Original resolver data must not gain an args key.
	_, mutated := resolverData[ArgsNamespace]
	assert.False(t, mutated)
}

func TestDedupKey_StableAcrossArgOrder(t *testing.T) {
	a := map[string]any{"user": "alice", "count": 1}
	b := map[string]any{"count": 1, "user": "alice"}

	ka, err := DedupKey("c", a)
	require.NoError(t, err)
	kb, err := DedupKey("c", b)
	require.NoError(t, err)

	assert.Equal(t, ka, kb)
}

func TestDedupKey_DiffersByCallAndArgs(t *testing.T) {
	base := map[string]any{"user": "alice"}
	other := map[string]any{"user": "bob"}

	k1, _ := DedupKey("c", base)
	k2, _ := DedupKey("c", other)
	k3, _ := DedupKey("d", base)

	assert.NotEqual(t, k1, k2)
	assert.NotEqual(t, k1, k3)
}
