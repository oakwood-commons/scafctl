package celexp

import (
	"fmt"
	"regexp"
	"strings"
)

// NewConditional creates a CEL expression for simple if/then/else logic.
// This is a convenience wrapper for the ternary operator.
//
// Example:
//
//	expr := celexp.NewConditional("age >= 18", `"adult"`, `"minor"`)
//	// Equivalent to: age >= 18 ? "adult" : "minor"
func NewConditional(condition, trueExpr, falseExpr string) Expression {
	return Expression(fmt.Sprintf("(%s) ? (%s) : (%s)", condition, trueExpr, falseExpr))
}

// NewCoalesce creates a CEL expression that returns the first non-null value.
// Similar to SQL COALESCE or JavaScript ?? operator.
//
// Note: For map property access (e.g., "user.name"), this uses the has() macro
// to check for existence. For simple variables, it checks against null.
//
// Example:
//
//	expr := celexp.NewCoalesce("user.nickname", "user.name", `"Guest"`)
//	// Returns: has(user.nickname) ? user.nickname : has(user.name) ? user.name : "Guest"
func NewCoalesce(values ...string) Expression {
	if len(values) == 0 {
		return Expression("null")
	}
	if len(values) == 1 {
		return Expression(values[0])
	}

	// Build nested ternary using has() for property access or null check for variables
	result := values[len(values)-1]
	for i := len(values) - 2; i >= 0; i-- {
		expr := values[i]
		// Use has() for property access (contains a dot), otherwise check != null
		if strings.Contains(expr, ".") {
			result = fmt.Sprintf("has(%s) ? %s : (%s)", expr, expr, result)
		} else {
			result = fmt.Sprintf("(%s) != null ? (%s) : (%s)", expr, expr, result)
		}
	}

	return Expression(result)
}

// NewStringInterpolation creates a CEL expression for string interpolation.
// Replaces ${var} placeholders with CEL variable references and automatically
// converts non-string expressions to strings.
//
// Supported patterns:
//   - Simple variables: ${name} → name
//   - Nested expressions: ${user.name} → user.name
//   - Auto string conversion: ${age} → string(age)
//   - Escaping: \${literal} → literal "${literal}"
//
// Example:
//
//	expr := celexp.NewStringInterpolation("Hello, ${name}! You are ${age} years old.")
//	// Converts to: "Hello, " + string(name) + "! You are " + string(age) + " years old."
//
//	expr := celexp.NewStringInterpolation("User: ${user.name} (${user.email})")
//	// Converts to: "User: " + string(user.name) + " (" + string(user.email) + ")"
func NewStringInterpolation(template string) Expression {
	return Expression(parseInterpolation(template))
}

// parseInterpolation converts ${var} syntax to CEL concatenation with string conversion.
// Handles escaping of \${ as literal ${.
func parseInterpolation(template string) string {
	// Replace escaped \${ with a placeholder
	const escapedPlaceholder = "\x00ESCAPED_DOLLAR\x00"
	template = strings.ReplaceAll(template, `\${`, escapedPlaceholder)

	// Regex to match ${expression}
	re := regexp.MustCompile(`\$\{([^}]+)\}`)

	matches := re.FindAllStringSubmatchIndex(template, -1)
	if len(matches) == 0 {
		// No interpolation needed, just return as quoted string
		// But restore escaped placeholders
		result := strings.ReplaceAll(template, escapedPlaceholder, "${")
		return fmt.Sprintf(`"%s"`, escapeString(result))
	}

	parts := make([]string, 0, len(matches)*2+1)
	lastEnd := 0

	for _, match := range matches {
		start := match[0]
		end := match[1]
		exprStart := match[2]
		exprEnd := match[3]

		// Add the literal text before this match
		if start > lastEnd {
			literal := template[lastEnd:start]
			// Restore escaped placeholders in literal text
			literal = strings.ReplaceAll(literal, escapedPlaceholder, "${")
			if literal != "" {
				parts = append(parts, fmt.Sprintf(`"%s"`, escapeString(literal)))
			}
		}

		// Add the expression with string conversion
		expr := template[exprStart:exprEnd]
		parts = append(parts, fmt.Sprintf("string(%s)", expr))

		lastEnd = end
	}

	// Add any remaining literal text
	if lastEnd < len(template) {
		literal := template[lastEnd:]
		// Restore escaped placeholders in literal text
		literal = strings.ReplaceAll(literal, escapedPlaceholder, "${")
		if literal != "" {
			parts = append(parts, fmt.Sprintf(`"%s"`, escapeString(literal)))
		}
	}

	// Join all parts with +
	return strings.Join(parts, " + ")
}

// escapeString escapes special characters for CEL string literals.
func escapeString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}
