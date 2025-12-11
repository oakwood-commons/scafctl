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
			result, err := GetUnderscoreVariables(tt.expression)

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
			result, err := GetUnderscoreVariables(tt.expression)
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
			result, err := GetUnderscoreVariables(tt.expression)

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
	expr := "_.user.name + _.config.value"
	b.ResetTimer()
	for b.Loop() {
		_, _ = GetUnderscoreVariables(expr)
	}
}

func BenchmarkGetUnderscoreVariables_Complex(b *testing.B) {
	expr := `_.config.enabled && _.data.users.exists(u, u.role == _.requiredRole) && _.items.filter(i, i.active).size() > _.threshold`
	b.ResetTimer()
	for b.Loop() {
		_, _ = GetUnderscoreVariables(expr)
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
			vars, err := GetVariablesWithPrefix(tt.expression, tt.prefix)

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
	expr := "_.user.name + _.config.value"

	for b.Loop() {
		_, _ = GetVariablesWithPrefix(expr, "")
	}
}

func BenchmarkGetVariablesWithPrefix_CustomPrefix(b *testing.B) {
	expr := "ctx.user.name + ctx.config.value"
	b.ResetTimer()
	for b.Loop() {
		_, _ = GetVariablesWithPrefix(expr, "ctx.")
	}
}

func BenchmarkGetVariablesWithPrefix_ComplexCustomPrefix(b *testing.B) {
	expr := "env.config.enabled && env.data.users.exists(u, u.role == env.requiredRole) && env.items.filter(i, i.active).size() > env.threshold"
	b.ResetTimer()
	for b.Loop() {
		_, _ = GetVariablesWithPrefix(expr, "env.")
	}
}
