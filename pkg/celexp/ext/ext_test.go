package ext

import (
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/kcloutie/scafctl/pkg/celexp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetFunctionNames(t *testing.T) {
	funcs := BuiltIn()

	// Call SetFunctionNames to populate the FunctionNames field
	err := SetFunctionNames(funcs)
	require.NoError(t, err)

	tests := []struct {
		name             string
		expectedFuncs    []string
		shouldHaveFuncs  bool
		minFunctionCount int
	}{
		{
			name:             "strings",
			shouldHaveFuncs:  true,
			minFunctionCount: 5, // strings extension should have multiple functions
		},
		{
			name:             "lists",
			shouldHaveFuncs:  true,
			minFunctionCount: 5, // lists extension should have multiple functions
		},
		{
			name:             "bindings",
			shouldHaveFuncs:  true,
			minFunctionCount: 1, // cel.bind
		},
		{
			name:             "encoders",
			shouldHaveFuncs:  true,
			minFunctionCount: 1, // base64.encode, base64.decode
		},
		{
			name:             "math",
			shouldHaveFuncs:  true,
			minFunctionCount: 5, // math extension should have multiple functions
		},
		{
			name:            "protos",
			shouldHaveFuncs: false, // protos extension works with proto2 extension fields, doesn't add new functions
		},
		{
			name:             "sets",
			shouldHaveFuncs:  true,
			minFunctionCount: 1, // sets.contains, sets.equivalent, sets.intersects
		},
		{
			name:             "optionalTypes",
			shouldHaveFuncs:  true,
			minFunctionCount: 1, // optional.of, optional.none, hasValue, value, or, orValue, etc.
		},
		{
			name:            "homogeneousAggregateLiterals",
			shouldHaveFuncs: false, // This is a validation option, not a function library
		},
		{
			name:            "eagerlyValidateDeclarations",
			shouldHaveFuncs: false, // This is an env option, not a function library
		},
		{
			name:            "defaultUTCTimeZone",
			shouldHaveFuncs: false, // This is an env option, not a function library
		},
		{
			name:            "crossTypeNumericComparisons",
			shouldHaveFuncs: false, // This is an env option, not a function library
		},
		{
			name:            "astValidators",
			shouldHaveFuncs: false, // This is a validation option, not a function library
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Find the function in the list
			var found bool
			var funcNames []string
			for _, f := range funcs {
				if f.Name == tt.name {
					found = true
					funcNames = f.FunctionNames
					break
				}
			}

			require.True(t, found, "Function %s should exist in the list", tt.name)

			if tt.shouldHaveFuncs {
				assert.NotEmpty(t, funcNames, "Function %s should have function names populated", tt.name)
				assert.GreaterOrEqual(t, len(funcNames), tt.minFunctionCount,
					"Function %s should have at least %d functions, got %d: %v",
					tt.name, tt.minFunctionCount, len(funcNames), funcNames)

				// Verify function names are sorted
				for i := 0; i < len(funcNames)-1; i++ {
					assert.LessOrEqual(t, funcNames[i], funcNames[i+1],
						"Function names should be sorted: %v", funcNames)
				}
			} else {
				// Options that don't add functions should have empty or no function names
				assert.Empty(t, funcNames, "Function %s should not have function names (it's an option, not a function library)", tt.name)
			}
		})
	}
}

func TestSetFunctionNames_SpecificFunctions(t *testing.T) {
	funcs := BuiltIn()
	err := SetFunctionNames(funcs)
	require.NoError(t, err)

	tests := []struct {
		extensionName    string
		expectedContains []string
	}{
		{
			extensionName: "strings",
			expectedContains: []string{
				"charAt",
				"indexOf",
				"lastIndexOf",
				"lowerAscii",
				"upperAscii",
				"replace",
				"split",
				"substring",
				"trim",
			},
		},
		{
			extensionName: "math",
			expectedContains: []string{
				"math.@max", // greatest
				"math.@min", // least
				"math.abs",
				"math.ceil",
				"math.floor",
			},
		},
		{
			extensionName: "sets",
			expectedContains: []string{
				"sets.contains",
				"sets.equivalent",
				"sets.intersects",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.extensionName, func(t *testing.T) {
			var funcNames []string
			for _, f := range funcs {
				if f.Name == tt.extensionName {
					funcNames = f.FunctionNames
					break
				}
			}

			for _, expected := range tt.expectedContains {
				assert.Contains(t, funcNames, expected,
					"Extension %s should contain function %s, got: %v",
					tt.extensionName, expected, funcNames)
			}
		})
	}
}

func TestSetFunctionNames_EmptyList(t *testing.T) {
	emptyList := celexp.ExtFunctionList{}
	err := SetFunctionNames(emptyList)
	require.NoError(t, err)
}

func TestSetFunctionNames_NoEnvOptions(t *testing.T) {
	funcs := celexp.ExtFunctionList{
		{
			Name:       "test",
			EnvOptions: []cel.EnvOption{},
		},
	}
	err := SetFunctionNames(funcs)
	require.NoError(t, err)
	assert.Empty(t, funcs[0].FunctionNames)
}

// TestSetFunctionNames_PrintExample demonstrates the function output
// This test is mainly for documentation purposes
func TestSetFunctionNames_PrintExample(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping example test in short mode")
	}

	funcs := BuiltIn()
	err := SetFunctionNames(funcs)
	require.NoError(t, err)

	// Print out some examples
	t.Log("Function names extracted from CEL extensions:")
	for _, f := range funcs {
		if len(f.FunctionNames) > 0 && len(f.FunctionNames) <= 10 {
			t.Logf("%s: %v", f.Name, f.FunctionNames)
		} else if len(f.FunctionNames) > 10 {
			t.Logf("%s: %v... (%d total)", f.Name, f.FunctionNames[:5], len(f.FunctionNames))
		}
	}
}

func TestBuiltIn(t *testing.T) {
	funcs := BuiltIn()

	// Should have exactly 13 built-in extensions
	assert.Len(t, funcs, 13, "Should have 13 built-in extensions")

	// All should have Custom=false
	for _, f := range funcs {
		assert.False(t, f.Custom, "Built-in function %s should have Custom=false", f.Name)
		assert.NotEmpty(t, f.Name, "Built-in function should have a name")
		assert.NotEmpty(t, f.Description, "Built-in function %s should have a description", f.Name)
	}

	// Check for expected built-in extensions
	expectedNames := []string{
		"strings", "lists", "bindings", "encoders", "math", "protos", "sets",
		"homogeneousAggregateLiterals", "eagerlyValidateDeclarations",
		"defaultUTCTimeZone", "crossTypeNumericComparisons", "optionalTypes",
		"astValidators",
	}

	for _, expectedName := range expectedNames {
		found := false
		for _, f := range funcs {
			if f.Name == expectedName {
				found = true
				break
			}
		}
		assert.True(t, found, "Should contain built-in extension %s", expectedName)
	}
}

func TestCustom(t *testing.T) {
	funcs := Custom()

	// Should have custom functions (at least 20+)
	assert.GreaterOrEqual(t, len(funcs), 20, "Should have at least 20 custom functions")

	// All should have Custom=true
	for _, f := range funcs {
		assert.True(t, f.Custom, "Custom function %s should have Custom=true", f.Name)
		assert.NotEmpty(t, f.Name, "Custom function should have a name")
		assert.NotEmpty(t, f.Description, "Custom function %s should have a description", f.Name)
		assert.NotEmpty(t, f.EnvOptions, "Custom function %s should have EnvOptions", f.Name)
	}

	// Check for expected custom function categories
	expectedCategories := map[string][]string{
		"arrays":      {"arrays.strings.add", "arrays.strings.unique"},
		"debug":       {"debug.throw", "debug.sleep"},
		"filepath":    {"filepath.dir", "filepath.normalize", "filepath.exists", "filepath.join"},
		"guid":        {"guid.new"},
		"map":         {"map.add", "map.select", "map.omit", "map.merge"},
		"marshalling": {"json.marshal", "json.unmarshal", "yaml.marshal", "yaml.unmarshal"},
		"out":         {"out.nil"},
		"sort":        {"sort.objects"},
		"strings":     {"strings.clean", "strings.title"},
		"time":        {"time.now"},
	}

	for category, expectedFuncs := range expectedCategories {
		for _, expectedFunc := range expectedFuncs {
			found := false
			for _, f := range funcs {
				if f.Name == expectedFunc {
					found = true
					break
				}
			}
			assert.True(t, found, "Should contain custom function %s from category %s", expectedFunc, category)
		}
	}
}

func TestAll(t *testing.T) {
	allFuncs := All()
	builtInFuncs := BuiltIn()
	customFuncs := Custom()

	// Should have both built-in and custom functions
	expectedTotal := len(builtInFuncs) + len(customFuncs)
	assert.Len(t, allFuncs, expectedTotal, "Should have all built-in and custom functions")

	// Count Custom=true and Custom=false
	var builtInCount, customCount int
	for _, f := range allFuncs {
		if f.Custom {
			customCount++
		} else {
			builtInCount++
		}
	}

	assert.Equal(t, len(builtInFuncs), builtInCount, "Should have correct number of built-in functions")
	assert.Equal(t, len(customFuncs), customCount, "Should have correct number of custom functions")

	// Verify all functions have required fields
	for _, f := range allFuncs {
		assert.NotEmpty(t, f.Name, "Function should have a name")
		assert.NotEmpty(t, f.Description, "Function %s should have a description", f.Name)
	}
}

func TestAll_NoDuplicates(t *testing.T) {
	allFuncs := All()

	// Check for duplicate names
	nameSet := make(map[string]bool)
	for _, f := range allFuncs {
		assert.False(t, nameSet[f.Name], "Duplicate function name found: %s", f.Name)
		nameSet[f.Name] = true
	}
}
