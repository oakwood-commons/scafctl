// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureBuiltinKinds(t *testing.T) {
	// Reset the once and registry so we test fresh registration
	resetBuiltinKindsForTesting()
	defer func() {
		// Reset again so other tests in the package start fresh
		resetBuiltinKindsForTesting()
	}()

	err := ensureBuiltinKinds()
	require.NoError(t, err)

	// Verify all 8 built-in kinds are registered
	defs := builtinKindDefinitions()
	for _, def := range defs {
		got, ok := globalKindRegistry.Get(def.Name)
		require.True(t, ok, "kind %q not found in registry", def.Name)
		assert.Equal(t, def.Name, got.Name)
		assert.NotNil(t, got.TypeInfo, "kind %q should have TypeInfo after registration", def.Name)
	}
}

func TestEnsureBuiltinKinds_Idempotent(t *testing.T) {
	// Reset
	resetBuiltinKindsForTesting()
	defer func() {
		resetBuiltinKindsForTesting()
	}()

	// Call twice — should not error or double-register
	err1 := ensureBuiltinKinds()
	require.NoError(t, err1)
	err2 := ensureBuiltinKinds()
	require.NoError(t, err2)
}

func TestGetKind_ReturnsErrorForUnknownKind(t *testing.T) {
	// Reset
	resetBuiltinKindsForTesting()
	defer func() {
		resetBuiltinKindsForTesting()
	}()

	_, err := GetKind("nonexistent-kind-xyz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown kind")
}

func TestGetKind_ByAlias(t *testing.T) {
	// Reset
	resetBuiltinKindsForTesting()
	defer func() {
		resetBuiltinKindsForTesting()
	}()

	// "sol" is an alias for "solution"
	def, err := GetKind("sol")
	require.NoError(t, err)
	assert.Equal(t, "solution", def.Name)
}

func TestListKinds_ReturnsAllBuiltins(t *testing.T) {
	// Reset
	resetBuiltinKindsForTesting()
	defer func() {
		resetBuiltinKindsForTesting()
	}()

	kinds, err := ListKinds()
	require.NoError(t, err)
	assert.Len(t, kinds, len(builtinKindDefinitions()))
}

func TestGetGlobalRegistry_ReturnsRegistryWithKinds(t *testing.T) {
	// Reset
	resetBuiltinKindsForTesting()
	defer func() {
		resetBuiltinKindsForTesting()
	}()

	reg, err := GetGlobalRegistry()
	require.NoError(t, err)
	require.NotNil(t, reg)
	names := reg.Names()
	assert.NotEmpty(t, names)
}

func TestBuiltinKindDefinitions_ReturnsExpectedKinds(t *testing.T) {
	defs := builtinKindDefinitions()

	expectedNames := []string{
		"provider", "solution", "action", "workflow",
		"resolver", "spec", "schema", "retry",
	}
	assert.Len(t, defs, len(expectedNames))

	nameSet := make(map[string]bool)
	for _, def := range defs {
		nameSet[def.Name] = true
		assert.NotEmpty(t, def.Description, "kind %q should have a description", def.Name)
		// TypeInstance is a typed nil pointer like (*Type)(nil) — check it's set (not untyped nil)
		assert.NotEqual(t, nil, def.TypeInstance, "kind %q should have a TypeInstance", def.Name)
	}

	for _, expected := range expectedNames {
		assert.True(t, nameSet[expected], "expected built-in kind %q not found", expected)
	}
}

func BenchmarkEnsureBuiltinKinds(b *testing.B) {
	for i := 0; i < b.N; i++ {
		resetBuiltinKindsForTesting()
		_ = ensureBuiltinKinds()
	}
}

func BenchmarkGetKind(b *testing.B) {
	// Ensure kinds are registered
	resetBuiltinKindsForTesting()
	_ = ensureBuiltinKinds()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = GetKind("provider")
	}
}
