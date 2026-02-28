// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package ext

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAll_ContainsSprigAndCustom(t *testing.T) {
	all := All()
	funcMap := all.FuncMap()

	// Should contain sprig functions
	assert.Contains(t, funcMap, "upper")
	assert.Contains(t, funcMap, "lower")
	assert.Contains(t, funcMap, "toJson")
	assert.Contains(t, funcMap, "dict")
	assert.Contains(t, funcMap, "trim")

	// Should contain custom functions
	assert.Contains(t, funcMap, "toHcl")
	assert.Contains(t, funcMap, "toYaml")
	assert.Contains(t, funcMap, "fromYaml")
	assert.Contains(t, funcMap, "mustToYaml")
	assert.Contains(t, funcMap, "mustFromYaml")
}

func TestSprig_ReturnsSprigFunctions(t *testing.T) {
	funcs := Sprig()

	require.NotEmpty(t, funcs)

	// Verify non-custom flag
	for _, fn := range funcs {
		assert.False(t, fn.Custom, "sprig function %q should have Custom=false", fn.Name)
		assert.NotEmpty(t, fn.Name, "function name should not be empty")
		assert.NotNil(t, fn.Func, "function map should not be nil for %q", fn.Name)
		assert.NotEmpty(t, fn.Links, "links should not be empty for %q", fn.Name)
	}

	// Check a known function exists
	found := false
	for _, fn := range funcs {
		if fn.Name == "upper" {
			found = true
			assert.Equal(t, "Converts a string to uppercase", fn.Description)
			break
		}
	}
	assert.True(t, found, "should find 'upper' in sprig functions")
}

func TestCustom_ReturnsCustomFunctions(t *testing.T) {
	funcs := Custom()

	require.NotEmpty(t, funcs)

	// Verify custom flag
	for _, fn := range funcs {
		assert.True(t, fn.Custom, "custom function %q should have Custom=true", fn.Name)
		assert.NotEmpty(t, fn.Name, "function name should not be empty")
		assert.NotNil(t, fn.Func, "function map should not be nil for %q", fn.Name)
	}

	// Check toHcl exists
	found := false
	for _, fn := range funcs {
		if fn.Name == "toHcl" {
			found = true
			assert.NotEmpty(t, fn.Description)
			assert.NotEmpty(t, fn.Examples)
			break
		}
	}
	assert.True(t, found, "should find 'toHcl' in custom functions")

	// Check YAML functions exist
	yamlFuncs := []string{"toYaml", "fromYaml", "mustToYaml", "mustFromYaml"}
	for _, name := range yamlFuncs {
		foundYaml := false
		for _, fn := range funcs {
			if fn.Name == name {
				foundYaml = true
				assert.NotEmpty(t, fn.Description, "YAML function %q should have a description", name)
				break
			}
		}
		assert.True(t, foundYaml, "should find %q in custom functions", name)
	}
}

func TestAll_CustomOverridesSprig(t *testing.T) {
	all := All()

	// Verify that custom functions come after sprig in the list
	sprigCount := len(Sprig())
	customCount := len(Custom())
	assert.Equal(t, sprigCount+customCount, len(all))

	// Last entries should be custom
	for i := sprigCount; i < len(all); i++ {
		assert.True(t, all[i].Custom, "entries after sprig should be custom")
	}
}

func TestAllFuncMap_ReturnsNonNilMap(t *testing.T) {
	funcMap := AllFuncMap()
	require.NotNil(t, funcMap)
	assert.NotEmpty(t, funcMap)
}

func TestSprig_HasDescriptions(t *testing.T) {
	funcs := Sprig()

	// All sprig functions should have some description
	for _, fn := range funcs {
		assert.NotEmpty(t, fn.Description, "function %q should have a description", fn.Name)
	}
}

func TestAll_NoDuplicateNames(t *testing.T) {
	all := All()

	seen := make(map[string]bool)
	for _, fn := range all {
		if seen[fn.Name] {
			t.Errorf("duplicate function name: %q", fn.Name)
		}
		seen[fn.Name] = true
	}
}
