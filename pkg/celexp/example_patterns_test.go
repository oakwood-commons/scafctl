package celexp_test

import (
	"fmt"
	"log"

	"github.com/google/cel-go/cel"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
)

// ExampleNewConditional demonstrates simple conditional expressions.
func ExampleNewConditional() {
	// Create a ternary expression: age >= 18 ? "adult" : "minor"
	expr := celexp.NewConditional("age >= 18", `"adult"`, `"minor"`)

	compiled, err := expr.Compile([]cel.EnvOption{
		cel.Variable("age", cel.IntType),
	})
	if err != nil {
		log.Fatal(err)
	}

	// Test with adult age
	result1, _ := compiled.Eval(map[string]any{"age": int64(25)})
	fmt.Printf("Age 25: %v\n", result1)

	// Test with minor age
	result2, _ := compiled.Eval(map[string]any{"age": int64(15)})
	fmt.Printf("Age 15: %v\n", result2)

	// Output:
	// Age 25: adult
	// Age 15: minor
}

// ExampleNewConditional_nested demonstrates nested conditional logic.
func ExampleNewConditional_nested() {
	// Nested: score >= 90 ? "A" : (score >= 80 ? "B" : "C")
	inner := celexp.NewConditional("score >= 80", `"B"`, `"C"`)
	expr := celexp.NewConditional("score >= 90", `"A"`, string(inner))

	compiled, err := expr.Compile([]cel.EnvOption{
		cel.Variable("score", cel.IntType),
	})
	if err != nil {
		log.Fatal(err)
	}

	grades := []int64{95, 85, 75}
	for _, score := range grades {
		result, _ := compiled.Eval(map[string]any{"score": score})
		fmt.Printf("Score %d: Grade %v\n", score, result)
	}

	// Output:
	// Score 95: Grade A
	// Score 85: Grade B
	// Score 75: Grade C
}

// ExampleNewStringInterpolation demonstrates string interpolation.
func ExampleNewStringInterpolation() {
	// Embed variables in a string template
	expr := celexp.NewStringInterpolation("Hello, ${name}! You are ${age} years old.")

	compiled, err := expr.Compile([]cel.EnvOption{
		cel.Variable("name", cel.StringType),
		cel.Variable("age", cel.IntType),
	})
	if err != nil {
		log.Fatal(err)
	}

	result, _ := compiled.Eval(map[string]any{
		"name": "Alice",
		"age":  int64(30),
	})
	fmt.Println(result)

	// Output:
	// Hello, Alice! You are 30 years old.
}

// ExampleNewStringInterpolation_nestedProperties demonstrates interpolation with nested objects.
func ExampleNewStringInterpolation_nestedProperties() {
	expr := celexp.NewStringInterpolation("User: ${user.name} (${user.email})")

	compiled, err := expr.Compile([]cel.EnvOption{
		cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
	})
	if err != nil {
		log.Fatal(err)
	}

	result, _ := compiled.Eval(map[string]any{
		"user": map[string]any{
			"name":  "Bob Smith",
			"email": "bob@example.com",
		},
	})
	fmt.Println(result)

	// Output:
	// User: Bob Smith (bob@example.com)
}

// ExampleNewStringInterpolation_escaping demonstrates literal dollar signs.
func ExampleNewStringInterpolation_escaping() {
	// Use \${ to include literal ${
	expr := celexp.NewStringInterpolation(`The price is \${price}, but the total is ${total}`)

	compiled, err := expr.Compile([]cel.EnvOption{
		cel.Variable("price", cel.IntType),
		cel.Variable("total", cel.IntType),
	})
	if err != nil {
		log.Fatal(err)
	}

	result, _ := compiled.Eval(map[string]any{
		"price": int64(10),
		"total": int64(12),
	})
	fmt.Println(result)

	// Output:
	// The price is ${price}, but the total is 12
}

// ExampleNewCoalesce demonstrates null coalescing for fallback values.
func ExampleNewCoalesce() {
	// Returns first non-null value: nickname, then name, then "Guest"
	expr := celexp.NewCoalesce("user.nickname", "user.name", `"Guest"`)

	compiled, err := expr.Compile([]cel.EnvOption{
		cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
	})
	if err != nil {
		log.Fatal(err)
	}

	// Case 1: Has nickname
	result1, _ := compiled.Eval(map[string]any{
		"user": map[string]any{
			"nickname": "Bobby",
			"name":     "Robert",
		},
	})
	fmt.Printf("With nickname: %v\n", result1)

	// Case 2: Only has name
	result2, _ := compiled.Eval(map[string]any{
		"user": map[string]any{
			"name": "Robert",
		},
	})
	fmt.Printf("With name only: %v\n", result2)

	// Case 3: Neither exists
	result3, _ := compiled.Eval(map[string]any{
		"user": map[string]any{},
	})
	fmt.Printf("Neither exists: %v\n", result3)

	// Output:
	// With nickname: Bobby
	// With name only: Robert
	// Neither exists: Guest
}

// ExampleNewCoalesce_multipleFields demonstrates coalescing across different fields.
func ExampleNewCoalesce_multipleFields() {
	// Try multiple contact methods
	expr := celexp.NewCoalesce("contact.mobile", "contact.phone", "contact.email", `"No contact info"`)

	compiled, err := expr.Compile([]cel.EnvOption{
		cel.Variable("contact", cel.MapType(cel.StringType, cel.DynType)),
	})
	if err != nil {
		log.Fatal(err)
	}

	// Has email but no phone
	result, _ := compiled.Eval(map[string]any{
		"contact": map[string]any{
			"email": "user@example.com",
		},
	})
	fmt.Println(result)

	// Output:
	// user@example.com
}

// Example_combinedPatterns demonstrates combining multiple helper patterns.
func Example_combinedPatterns() {
	// Use coalesce for name and interpolation for greeting
	nameExpr := celexp.NewCoalesce("user.displayName", "user.username", `"Guest"`)
	greetingExpr := celexp.NewStringInterpolation(fmt.Sprintf("Welcome, ${%s}!", nameExpr))

	compiled, err := greetingExpr.Compile([]cel.EnvOption{
		cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
	})
	if err != nil {
		log.Fatal(err)
	}

	// User with display name
	result1, _ := compiled.Eval(map[string]any{
		"user": map[string]any{
			"displayName": "Alice",
			"username":    "alice123",
		},
	})
	fmt.Println(result1)

	// User with only username
	result2, _ := compiled.Eval(map[string]any{
		"user": map[string]any{
			"username": "bob456",
		},
	})
	fmt.Println(result2)

	// No user info
	result3, _ := compiled.Eval(map[string]any{
		"user": map[string]any{},
	})
	fmt.Println(result3)

	// Output:
	// Welcome, Alice!
	// Welcome, bob456!
	// Welcome, Guest!
}
