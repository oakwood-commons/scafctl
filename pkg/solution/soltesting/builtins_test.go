package soltesting_test

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuiltinTests_AllReturned(t *testing.T) {
	builtins := soltesting.BuiltinTests(nil)
	require.Len(t, builtins, 4)

	names := make([]string, len(builtins))
	for i, b := range builtins {
		names[i] = b.Name
	}
	assert.Contains(t, names, "builtin:parse")
	assert.Contains(t, names, "builtin:lint")
	assert.Contains(t, names, "builtin:resolve-defaults")
	assert.Contains(t, names, "builtin:render-defaults")
}

func TestBuiltinTests_NamesHavePrefix(t *testing.T) {
	builtins := soltesting.BuiltinTests(nil)
	for _, b := range builtins {
		assert.True(t, soltesting.IsBuiltin(b.Name),
			"builtin %q should have builtin: prefix", b.Name)
	}
}

func TestBuiltinTests_SkipAll(t *testing.T) {
	tc := &soltesting.TestConfig{
		SkipBuiltins: soltesting.SkipBuiltinsValue{All: true},
	}
	builtins := soltesting.BuiltinTests(tc)
	assert.Empty(t, builtins)
}

func TestBuiltinTests_SkipSpecific(t *testing.T) {
	tc := &soltesting.TestConfig{
		SkipBuiltins: soltesting.SkipBuiltinsValue{
			Names: []string{"lint", "parse"},
		},
	}
	builtins := soltesting.BuiltinTests(tc)
	require.Len(t, builtins, 2)

	names := make([]string, len(builtins))
	for i, b := range builtins {
		names[i] = b.Name
	}
	assert.Contains(t, names, "builtin:resolve-defaults")
	assert.Contains(t, names, "builtin:render-defaults")
	assert.NotContains(t, names, "builtin:lint")
	assert.NotContains(t, names, "builtin:parse")
}

func TestBuiltinTests_DefaultConfig(t *testing.T) {
	tc := &soltesting.TestConfig{}
	builtins := soltesting.BuiltinTests(tc)
	assert.Len(t, builtins, 4)
}

func TestBuiltinTests_AlphabeticalOrder(t *testing.T) {
	builtins := soltesting.BuiltinTests(nil)
	require.Len(t, builtins, 4)
	// Builtins should be in alphabetical order.
	for i := 1; i < len(builtins); i++ {
		assert.Less(t, builtins[i-1].Name, builtins[i].Name,
			"builtins should be alphabetically ordered")
	}
}

func TestIsBuiltin(t *testing.T) {
	assert.True(t, soltesting.IsBuiltin("builtin:parse"))
	assert.True(t, soltesting.IsBuiltin("builtin:lint"))
	assert.False(t, soltesting.IsBuiltin("my-test"))
	assert.False(t, soltesting.IsBuiltin("_template"))
}

func TestBuiltinName(t *testing.T) {
	assert.Equal(t, "builtin:parse", soltesting.BuiltinName("parse"))
	assert.Equal(t, "builtin:lint", soltesting.BuiltinName("lint"))
}

func TestBuiltinTests_LintHasCommand(t *testing.T) {
	builtins := soltesting.BuiltinTests(nil)
	for _, b := range builtins {
		if b.Name == "builtin:lint" {
			assert.Equal(t, []string{"lint"}, b.Command)
			return
		}
	}
	t.Fatal("builtin:lint not found")
}

func TestBuiltinTests_RenderDefaultsHasCommand(t *testing.T) {
	builtins := soltesting.BuiltinTests(nil)
	for _, b := range builtins {
		if b.Name == "builtin:render-defaults" {
			assert.Equal(t, []string{"render", "solution"}, b.Command)
			return
		}
	}
	t.Fatal("builtin:render-defaults not found")
}

func TestBuiltinTests_ResolveDefaultsHasCommand(t *testing.T) {
	builtins := soltesting.BuiltinTests(nil)
	for _, b := range builtins {
		if b.Name == "builtin:resolve-defaults" {
			assert.Equal(t, []string{"run", "resolver"}, b.Command)
			return
		}
	}
	t.Fatal("builtin:resolve-defaults not found")
}
