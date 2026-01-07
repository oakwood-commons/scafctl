package celexp

import (
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConditional(t *testing.T) {
	tests := []struct {
		name       string
		condition  string
		trueExpr   string
		falseExpr  string
		want       string
		evalVars   map[string]any
		evalResult any
	}{
		{
			name:       "simple age check",
			condition:  "age >= 18",
			trueExpr:   `"adult"`,
			falseExpr:  `"minor"`,
			want:       `(age >= 18) ? ("adult") : ("minor")`,
			evalVars:   map[string]any{"age": int64(25)},
			evalResult: "adult",
		},
		{
			name:       "numeric result",
			condition:  "x > 10",
			trueExpr:   "100",
			falseExpr:  "0",
			want:       "(x > 10) ? (100) : (0)",
			evalVars:   map[string]any{"x": int64(5)},
			evalResult: int64(0),
		},
		{
			name:       "nested expressions",
			condition:  "user.verified",
			trueExpr:   "user.name",
			falseExpr:  `"Guest"`,
			want:       `(user.verified) ? (user.name) : ("Guest")`,
			evalVars:   map[string]any{"user": map[string]any{"verified": false, "name": "Alice"}},
			evalResult: "Guest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := NewConditional(tt.condition, tt.trueExpr, tt.falseExpr)
			assert.Equal(t, tt.want, string(expr))

			// Test evaluation
			compiled, err := expr.Compile([]cel.EnvOption{
				cel.Variable("age", cel.IntType),
				cel.Variable("x", cel.IntType),
				cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
			})
			require.NoError(t, err)

			result, err := compiled.Eval(tt.evalVars)
			require.NoError(t, err)
			assert.Equal(t, tt.evalResult, result)
		})
	}
}

func TestNewCoalesce(t *testing.T) {
	tests := []struct {
		name       string
		values     []string
		want       string
		envOpts    []cel.EnvOption
		evalVars   map[string]any
		evalResult any
		skipEval   bool // Skip evaluation test for cases that won't compile
	}{
		{
			name:       "empty values returns null",
			values:     []string{},
			want:       "null",
			envOpts:    []cel.EnvOption{},
			evalVars:   map[string]any{},
			evalResult: nil,
			skipEval:   true, // CEL null is structpb.NullValue, not nil
		},
		{
			name:       "single value",
			values:     []string{"x"},
			want:       "x",
			envOpts:    []cel.EnvOption{cel.Variable("x", cel.IntType)},
			evalVars:   map[string]any{"x": int64(42)},
			evalResult: int64(42),
		},
		{
			name:       "nested property access with has()",
			values:     []string{"user.nickname", "user.name", `"Guest"`},
			want:       `has(user.nickname) ? user.nickname : (has(user.name) ? user.name : ("Guest"))`,
			envOpts:    []cel.EnvOption{cel.Variable("user", cel.MapType(cel.StringType, cel.DynType))},
			evalVars:   map[string]any{"user": map[string]any{"name": "Alice"}},
			evalResult: "Alice",
		},
		{
			name:       "all properties missing, use default",
			values:     []string{"user.nickname", "user.displayName", `"Guest"`},
			want:       `has(user.nickname) ? user.nickname : (has(user.displayName) ? user.displayName : ("Guest"))`,
			envOpts:    []cel.EnvOption{cel.Variable("user", cel.MapType(cel.StringType, cel.DynType))},
			evalVars:   map[string]any{"user": map[string]any{}},
			evalResult: "Guest",
		},
		{
			name:       "first property exists",
			values:     []string{"user.nickname", "user.name", `"Guest"`},
			want:       `has(user.nickname) ? user.nickname : (has(user.name) ? user.name : ("Guest"))`,
			envOpts:    []cel.EnvOption{cel.Variable("user", cel.MapType(cel.StringType, cel.DynType))},
			evalVars:   map[string]any{"user": map[string]any{"nickname": "Bobby", "name": "Bob"}},
			evalResult: "Bobby",
		},
		{
			name:       "mixed - property access and literal",
			values:     []string{"config.value", `42`},
			want:       `has(config.value) ? config.value : (42)`,
			envOpts:    []cel.EnvOption{cel.Variable("config", cel.MapType(cel.StringType, cel.IntType))},
			evalVars:   map[string]any{"config": map[string]any{}},
			evalResult: int64(42),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := NewCoalesce(tt.values...)
			assert.Equal(t, tt.want, string(expr))

			if tt.skipEval {
				return
			}

			// Test evaluation
			compiled, err := expr.Compile(tt.envOpts)
			require.NoError(t, err)

			result, err := compiled.Eval(tt.evalVars)
			require.NoError(t, err)
			assert.Equal(t, tt.evalResult, result)
		})
	}
}

func TestNewStringInterpolation(t *testing.T) {
	tests := []struct {
		name       string
		template   string
		want       string
		evalVars   map[string]any
		evalResult string
	}{
		{
			name:       "simple variable",
			template:   "Hello, ${name}!",
			want:       `"Hello, " + string(name) + "!"`,
			evalVars:   map[string]any{"name": "Alice"},
			evalResult: "Hello, Alice!",
		},
		{
			name:       "multiple variables",
			template:   "Hello, ${name}! You are ${age} years old.",
			want:       `"Hello, " + string(name) + "! You are " + string(age) + " years old."`,
			evalVars:   map[string]any{"name": "Bob", "age": int64(25)},
			evalResult: "Hello, Bob! You are 25 years old.",
		},
		{
			name:       "nested property access",
			template:   "User: ${user.name} (${user.email})",
			want:       `"User: " + string(user.name) + " (" + string(user.email) + ")"`,
			evalVars:   map[string]any{"user": map[string]any{"name": "Charlie", "email": "charlie@example.com"}},
			evalResult: "User: Charlie (charlie@example.com)",
		},
		{
			name:       "no interpolation",
			template:   "Plain text",
			want:       `"Plain text"`,
			evalVars:   map[string]any{},
			evalResult: "Plain text",
		},
		{
			name:       "escaped placeholder",
			template:   `Price: \${amount}`,
			want:       `"Price: ${amount}"`,
			evalVars:   map[string]any{},
			evalResult: "Price: ${amount}",
		},
		{
			name:       "mixed escaped and interpolated",
			template:   `Total: \${cost} is ${amount}`,
			want:       `"Total: ${cost} is " + string(amount)`,
			evalVars:   map[string]any{"amount": int64(100)},
			evalResult: "Total: ${cost} is 100",
		},
		{
			name:       "starts with variable",
			template:   "${greeting}, world!",
			want:       `string(greeting) + ", world!"`,
			evalVars:   map[string]any{"greeting": "Hello"},
			evalResult: "Hello, world!",
		},
		{
			name:       "ends with variable",
			template:   "Hello, ${name}",
			want:       `"Hello, " + string(name)`,
			evalVars:   map[string]any{"name": "David"},
			evalResult: "Hello, David",
		},
		{
			name:       "only variable",
			template:   "${value}",
			want:       "string(value)",
			evalVars:   map[string]any{"value": "test"},
			evalResult: "test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := NewStringInterpolation(tt.template)
			assert.Equal(t, tt.want, string(expr))

			// Test evaluation
			compiled, err := expr.Compile([]cel.EnvOption{
				cel.Variable("name", cel.StringType),
				cel.Variable("age", cel.IntType),
				cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
				cel.Variable("greeting", cel.StringType),
				cel.Variable("amount", cel.IntType),
				cel.Variable("value", cel.StringType),
			})
			require.NoError(t, err)

			result, err := compiled.Eval(tt.evalVars)
			require.NoError(t, err)
			assert.Equal(t, tt.evalResult, result)
		})
	}
}

func TestNewStringInterpolation_SpecialCharacters(t *testing.T) {
	tests := []struct {
		name       string
		template   string
		evalVars   map[string]any
		evalResult string
	}{
		{
			name:       "newline in literal",
			template:   "Line1\n${text}",
			evalVars:   map[string]any{"text": "Line2"},
			evalResult: "Line1\nLine2",
		},
		{
			name:       "tab in literal",
			template:   "Name:\t${name}",
			evalVars:   map[string]any{"name": "Test"},
			evalResult: "Name:\tTest",
		},
		{
			name:       "quotes in literal",
			template:   `Say "${message}"`,
			evalVars:   map[string]any{"message": "hello"},
			evalResult: `Say "hello"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := NewStringInterpolation(tt.template)

			// Test evaluation
			compiled, err := expr.Compile([]cel.EnvOption{
				cel.Variable("text", cel.StringType),
				cel.Variable("name", cel.StringType),
				cel.Variable("message", cel.StringType),
			})
			require.NoError(t, err)

			result, err := compiled.Eval(tt.evalVars)
			require.NoError(t, err)
			assert.Equal(t, tt.evalResult, result)
		})
	}
}

// Integration tests showing end-to-end usage of helpers

func TestHelpers_Integration_UserGreeting(t *testing.T) {
	// Use NewCoalesce for name fallback and NewStringInterpolation for greeting
	nameExpr := NewCoalesce("user.displayName", "user.username", `"Guest"`)
	compiled, err := nameExpr.Compile([]cel.EnvOption{
		cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
	})
	require.NoError(t, err)

	// User with display name
	name, err := compiled.Eval(map[string]any{
		"user": map[string]any{"displayName": "Alice Smith", "username": "alice123"},
	})
	require.NoError(t, err)
	assert.Equal(t, "Alice Smith", name)

	// User without display name
	name, err = compiled.Eval(map[string]any{
		"user": map[string]any{"username": "bob456"},
	})
	require.NoError(t, err)
	assert.Equal(t, "bob456", name)

	// No user data (empty map)
	name, err = compiled.Eval(map[string]any{
		"user": map[string]any{},
	})
	require.NoError(t, err)
	assert.Equal(t, "Guest", name)
}

func TestHelpers_Integration_ConditionalMessage(t *testing.T) {
	// Combine conditional and string interpolation
	statusExpr := NewConditional(
		"score >= 90",
		`"Excellent"`,
		string(NewConditional("score >= 70", `"Good"`, `"Needs Improvement"`)),
	)

	compiled, err := statusExpr.Compile([]cel.EnvOption{
		cel.Variable("score", cel.IntType),
	})
	require.NoError(t, err)

	tests := []struct {
		score  int64
		status string
	}{
		{95, "Excellent"},
		{85, "Good"},
		{60, "Needs Improvement"},
	}

	for _, tt := range tests {
		result, err := compiled.Eval(map[string]any{"score": tt.score})
		require.NoError(t, err)
		assert.Equal(t, tt.status, result)
	}
}

func TestHelpers_Integration_ComplexTemplate(t *testing.T) {
	// Build a complex email template using helpers
	template := NewStringInterpolation(
		"Dear ${recipient.name},\n\n" +
			"Your order #${order.id} has been ${order.status}.\n" +
			"Total: ${order.total}",
	)

	compiled, err := template.Compile([]cel.EnvOption{
		cel.Variable("recipient", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("order", cel.MapType(cel.StringType, cel.DynType)),
	})
	require.NoError(t, err)

	result, err := compiled.Eval(map[string]any{
		"recipient": map[string]any{"name": "John Doe"},
		"order": map[string]any{
			"id":     "ORD-12345",
			"status": "shipped",
			"total":  int64(99),
		},
	})
	require.NoError(t, err)

	expected := "Dear John Doe,\n\n" +
		"Your order #ORD-12345 has been shipped.\n" +
		"Total: 99"
	assert.Equal(t, expected, result)
}
