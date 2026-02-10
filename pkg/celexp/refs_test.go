// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetUnderscoreVariables(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		expected   []string
		wantError  bool
	}{
		{
			name:       "single underscore variable",
			expression: "_.user",
			expected:   []string{"user"},
		},
		{
			name:       "multiple underscore variables",
			expression: "_.user + _.config",
			expected:   []string{"user", "config"},
		},
		{
			name:       "underscore variable with property access",
			expression: "_.user.name",
			expected:   []string{"user"},
		},
		{
			name:       "multiple underscore variables with properties",
			expression: "_.user.name + _.config.value",
			expected:   []string{"user", "config"},
		},
		{
			name:       "underscore variable in function call",
			expression: "size(_.items)",
			expected:   []string{"items"},
		},
		{
			name:       "mixed underscore and regular variables",
			expression: "_.user.name + normalVar",
			expected:   []string{"user"},
		},
		{
			name:       "underscore variable in list",
			expression: "[_.item1, _.item2, regularItem]",
			expected:   []string{"item1", "item2"},
		},
		{
			name:       "underscore variable in map",
			expression: `{"key": _.value, "other": normalValue}`,
			expected:   []string{"value"},
		},
		{
			name:       "underscore variable as map key",
			expression: `{_.key: "value"}`,
			expected:   []string{"key"},
		},
		{
			name:       "nested property access",
			expression: "_.user.profile.address.city",
			expected:   []string{"user"},
		},
		{
			name:       "duplicate underscore variables",
			expression: "_.user + _.user.name + _.user.email",
			expected:   []string{"user"},
		},
		{
			name:       "underscore variables in complex expression",
			expression: "_.config.enabled ? _.data.items[0] : _.fallback",
			expected:   []string{"config", "data", "fallback"},
		},
		{
			name:       "underscore variable in comprehension",
			expression: "_.items.map(i, i * 2)",
			expected:   []string{"items"},
		},
		{
			name:       "no underscore variables",
			expression: "user.name + config.value",
			expected:   []string{},
		},
		{
			name:       "empty expression",
			expression: "",
			wantError:  true,
		},
		{
			name:       "only literals",
			expression: `"hello" + 123 + true`,
			expected:   []string{},
		},
		{
			name:       "underscore in string literal (should not match)",
			expression: `"_notAVariable"`,
			expected:   []string{},
		},
		{
			name:       "complex nested structure",
			expression: `_.config.services.map(s, {"name": s.name, "url": _.baseUrl + s.path})`,
			expected:   []string{"config", "baseUrl"},
		},
		{
			name:       "multiple function calls with underscore vars",
			expression: `size(_.items) > 0 && _.items.filter(i, i.active).size() > 0`,
			expected:   []string{"items"},
		},
		{
			name:       "underscore variable in conditional",
			expression: `_.enabled ? _.primary : _.secondary`,
			expected:   []string{"enabled", "primary", "secondary"},
		},
		{
			name:       "underscore variable with method call",
			expression: `_.text.startsWith("prefix")`,
			expected:   []string{"text"},
		},
		{
			name:       "invalid CEL expression",
			expression: "this is not valid CEL +++",
			wantError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := Expression(tt.expression)
			result, err := expr.GetUnderscoreVariables()

			if tt.wantError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Sort both slices for comparison since map iteration order is not guaranteed
			sort.Strings(result)
			sort.Strings(tt.expected)

			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetUnderscoreVariables_ComplexExpressions(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		expected   []string
	}{
		{
			name: "complex nested comprehension",
			expression: `_.items.filter(item, item.active)
						  .map(item, {"id": item.id, "value": _.multiplier * item.value})`,
			expected: []string{"items", "multiplier"},
		},
		{
			name: "multiple levels of nesting",
			expression: `_.config.enabled && 
						 _.data.users.exists(u, u.role == _.requiredRole) &&
						 _.settings.maxCount > 0`,
			expected: []string{"config", "data", "requiredRole", "settings"},
		},
		{
			name:       "all expression with underscore variable",
			expression: `_.items.all(i, i.value > _.threshold)`,
			expected:   []string{"items", "threshold"},
		},
		{
			name:       "exists expression with underscore variable",
			expression: `_.users.exists(u, u.name == _.searchName)`,
			expected:   []string{"users", "searchName"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := Expression(tt.expression)
			result, err := expr.GetUnderscoreVariables()
			require.NoError(t, err)

			// Sort for comparison
			sort.Strings(result)
			sort.Strings(tt.expected)

			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetUnderscoreVariables_EdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		expected   []string
		wantError  bool
	}{
		{
			name:       "single underscore only",
			expression: "_",
			expected:   []string{},
		},
		{
			name:       "underscore with numbers",
			expression: "_.var123 + _.var456",
			expected:   []string{"var123", "var456"},
		},
		{
			name:       "underscore with special naming",
			expression: "_.CamelCase + _.snake_case + _.UPPERCASE",
			expected:   []string{"CamelCase", "snake_case", "UPPERCASE"},
		},
		{
			name:       "deeply nested property access",
			expression: "_.a.b.c.d.e.f.g",
			expected:   []string{"a"},
		},
		{
			name:       "underscore in arithmetic",
			expression: "(_.value1 + _.value2) * _.multiplier / _.divisor",
			expected:   []string{"value1", "value2", "multiplier", "divisor"},
		},
		{
			name:       "underscore in comparison",
			expression: "_.count >= _.minCount && _.count <= _.maxCount",
			expected:   []string{"count", "minCount", "maxCount"},
		},
		{
			name:       "unclosed parenthesis",
			expression: "_.value + (_.other",
			wantError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := Expression(tt.expression)
			result, err := expr.GetUnderscoreVariables()

			if tt.wantError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Sort for comparison
			sort.Strings(result)
			sort.Strings(tt.expected)

			assert.Equal(t, tt.expected, result)
		})
	}
}

func BenchmarkGetUnderscoreVariables_Simple(b *testing.B) {
	expr := Expression("_.user.name + _.config.value")
	b.ResetTimer()
	for b.Loop() {
		_, _ = expr.GetUnderscoreVariables()
	}
}

func BenchmarkGetUnderscoreVariables_Complex(b *testing.B) {
	expr := Expression(`_.config.enabled && _.data.users.exists(u, u.role == _.requiredRole) && _.items.filter(i, i.active).size() > _.threshold`)
	b.ResetTimer()
	for b.Loop() {
		_, _ = expr.GetUnderscoreVariables()
	}
}

func TestGetVariablesWithPrefix(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		prefix     string
		expected   []string
		wantError  bool
	}{
		{
			name:       "default prefix (_.) - single variable",
			expression: "_.user",
			prefix:     "",
			expected:   []string{"user"},
		},
		{
			name:       "default prefix (_.) - multiple variables",
			expression: "_.user + _.config",
			prefix:     "",
			expected:   []string{"user", "config"},
		},
		{
			name:       "explicit _. prefix",
			expression: "_.user.name + _.config.value",
			prefix:     "_.",
			expected:   []string{"user", "config"},
		},
		{
			name:       "custom prefix with dot (ctx.) - in function call",
			expression: "size(ctx.items)",
			prefix:     "ctx.",
			expected:   []string{"items"},
		},
		{
			name:       "custom prefix with dot (env.) - mixed with regular vars",
			expression: "env.config + normalVar + env.settings",
			prefix:     "env.",
			expected:   []string{"config", "settings"},
		},
		{
			name:       "custom prefix with dot (data.) - nested properties",
			expression: "data.user.name + data.config.value",
			prefix:     "data.",
			expected:   []string{"user", "config"},
		},
		{
			name:       "custom prefix (param.) - in comprehension",
			expression: "param.items.all(x, x > param.threshold)",
			prefix:     "param.",
			expected:   []string{"items", "threshold"},
		},
		{
			name:       "custom prefix (app.) - in list",
			expression: "[app.item1, app.item2, regularItem]",
			prefix:     "app.",
			expected:   []string{"item1", "item2"},
		},
		{
			name:       "custom prefix (cfg.) - in map",
			expression: "{'key1': cfg.value1, 'key2': cfg.value2}",
			prefix:     "cfg.",
			expected:   []string{"value1", "value2"},
		},
		{
			name:       "custom prefix (state.) - in arithmetic",
			expression: "state.counter + 5 * state.multiplier",
			prefix:     "state.",
			expected:   []string{"counter", "multiplier"},
		},
		{
			name:       "custom prefix (vars.) - in comparison",
			expression: "vars.age > 18 && vars.active == true",
			prefix:     "vars.",
			expected:   []string{"age", "active"},
		},
		{
			name:       "no matching variables with custom prefix",
			expression: "user + config",
			prefix:     "custom.",
			expected:   []string{},
		},
		{
			name:       "empty expression with custom prefix",
			expression: "",
			prefix:     "test.",
			wantError:  true,
		},
		{
			name:       "invalid expression with custom prefix",
			expression: "(test.user + ",
			prefix:     "test.",
			wantError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := Expression(tt.expression)
			vars, err := expr.GetVariablesWithPrefix(tt.prefix)

			if tt.wantError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Sort both slices for comparison
			sort.Strings(vars)
			expected := make([]string, len(tt.expected))
			copy(expected, tt.expected)
			sort.Strings(expected)

			assert.Equal(t, expected, vars)
		})
	}
}

func BenchmarkGetVariablesWithPrefix_DefaultPrefix(b *testing.B) {
	expr := Expression("_.user.name + _.config.value")

	for b.Loop() {
		_, _ = expr.GetVariablesWithPrefix("")
	}
}

func BenchmarkGetVariablesWithPrefix_CustomPrefix(b *testing.B) {
	expr := Expression("ctx.user.name + ctx.config.value")
	b.ResetTimer()
	for b.Loop() {
		_, _ = expr.GetVariablesWithPrefix("ctx.")
	}
}

func BenchmarkGetVariablesWithPrefix_ComplexCustomPrefix(b *testing.B) {
	expr := Expression("env.config.enabled && env.data.users.exists(u, u.role == env.requiredRole) && env.items.filter(i, i.active).size() > env.threshold")
	b.ResetTimer()
	for b.Loop() {
		_, _ = expr.GetVariablesWithPrefix("env.")
	}
}

func TestRequiredVariables(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		expected   []string
		wantError  bool
	}{
		{
			name:       "simple arithmetic expression",
			expression: "x + y",
			expected:   []string{"x", "y"},
		},
		{
			name:       "single variable",
			expression: "count",
			expected:   []string{"count"},
		},
		{
			name:       "property access",
			expression: "user.name",
			expected:   []string{"user"},
		},
		{
			name:       "multiple variables with properties",
			expression: "user.name + config.value",
			expected:   []string{"config", "user"},
		},
		{
			name:       "nested property access",
			expression: "user.profile.address.city",
			expected:   []string{"user"},
		},
		{
			name:       "variable in function call",
			expression: "size(items)",
			expected:   []string{"items"},
		},
		{
			name:       "multiple variables in function",
			expression: "max(x, y, z)",
			expected:   []string{"x", "y", "z"},
		},
		{
			name:       "variables in conditional",
			expression: "enabled ? data : fallback",
			expected:   []string{"data", "enabled", "fallback"},
		},
		{
			name:       "variables in list",
			expression: "[item1, item2, item3]",
			expected:   []string{"item1", "item2", "item3"},
		},
		{
			name:       "variables in map",
			expression: `{"key": value, "other": anotherValue}`,
			expected:   []string{"anotherValue", "value"},
		},
		{
			name:       "variable as map key",
			expression: `{key: "value"}`,
			expected:   []string{"key"},
		},
		{
			name:       "duplicate variables",
			expression: "user + user.name + user.email",
			expected:   []string{"user"},
		},
		{
			name:       "complex expression",
			expression: "config.enabled && data.items[0].name == expected && count > threshold",
			expected:   []string{"config", "count", "data", "expected", "threshold"},
		},
		{
			name:       "comprehension - filter excludes iteration variable",
			expression: "[1, 2, 3].filter(x, x > 1)",
			expected:   []string{},
		},
		{
			name:       "comprehension with external variable",
			expression: "items.filter(x, x > threshold)",
			expected:   []string{"items", "threshold"},
		},
		{
			name:       "comprehension - map",
			expression: "items.map(item, item * multiplier)",
			expected:   []string{"items", "multiplier"},
		},
		{
			name:       "comprehension - exists",
			expression: "users.exists(u, u.role == requiredRole)",
			expected:   []string{"requiredRole", "users"},
		},
		{
			name:       "comprehension - all",
			expression: "scores.all(s, s >= minScore)",
			expected:   []string{"minScore", "scores"},
		},
		{
			name:       "nested comprehensions",
			expression: "matrix.map(row, row.filter(val, val > threshold))",
			expected:   []string{"matrix", "threshold"},
		},
		{
			name:       "method chaining",
			expression: "text.toLowerCase().contains(searchTerm)",
			expected:   []string{"searchTerm", "text"},
		},
		{
			name:       "logical operators",
			expression: "a && b || c && !d",
			expected:   []string{"a", "b", "c", "d"},
		},
		{
			name:       "comparison operators",
			expression: "x == y && a != b && c < d && e > f",
			expected:   []string{"a", "b", "c", "d", "e", "f", "x", "y"},
		},
		{
			name:       "mixed with underscore prefix (should capture all)",
			expression: "_.user.name + normalVar",
			expected:   []string{"_", "normalVar"},
		},
		{
			name:       "no variables - only literals",
			expression: `"hello" + 123 + true`,
			expected:   []string{},
		},
		{
			name:       "no variables - empty list",
			expression: "[]",
			expected:   []string{},
		},
		{
			name:       "no variables - empty map",
			expression: "{}",
			expected:   []string{},
		},
		{
			name:       "variable in index expression",
			expression: "arr[index]",
			expected:   []string{"arr", "index"},
		},
		{
			name:       "optional chaining with has()",
			expression: "has(user.profile) ? user.profile.name : defaultName",
			expected:   []string{"defaultName", "user"},
		},
		{
			name:       "type casting",
			expression: "int(stringValue) + 10",
			expected:   []string{"stringValue"},
		},
		{
			name:       "string interpolation",
			expression: `"Hello, " + name + "!"`,
			expected:   []string{"name"},
		},
		{
			name:       "empty expression",
			expression: "",
			wantError:  true,
		},
		{
			name:       "invalid syntax",
			expression: "x +",
			wantError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := Expression(tt.expression)
			vars, err := expr.RequiredVariables()

			if tt.wantError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Results are already sorted by the implementation
			assert.Equal(t, tt.expected, vars)
		})
	}
}

func BenchmarkRequiredVariables_Simple(b *testing.B) {
	expr := Expression("x + y + z")
	b.ResetTimer()
	for b.Loop() {
		_, _ = expr.RequiredVariables()
	}
}

func BenchmarkRequiredVariables_Complex(b *testing.B) {
	expr := Expression("config.enabled && data.users.exists(u, u.role == requiredRole) && items.filter(i, i.active).size() > threshold")
	b.ResetTimer()
	for b.Loop() {
		_, _ = expr.RequiredVariables()
	}
}

func BenchmarkRequiredVariables_WithComprehensions(b *testing.B) {
	expr := Expression("matrix.map(row, row.filter(val, val > threshold).map(v, v * multiplier))")
	b.ResetTimer()
	for b.Loop() {
		_, _ = expr.RequiredVariables()
	}
}
