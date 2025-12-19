package gotmpl_test

import (
	"context"
	"fmt"
	"log"
	"strings"
	"text/template"

	"github.com/kcloutie/scafctl/pkg/gotmpl"
	"github.com/kcloutie/scafctl/pkg/logger"
)

// ExampleExecute demonstrates basic template execution
func ExampleExecute() {
	ctx := logger.WithLogger(context.Background(), logger.Get(0))

	result, err := gotmpl.Execute(ctx, gotmpl.TemplateOptions{
		Name:    "greeting.tmpl",
		Content: "Hello, {{.Name}}! You are {{.Age}} years old.",
		Data: map[string]any{
			"Name": "Alice",
			"Age":  30,
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.Output)
	// Output: Hello, Alice! You are 30 years old.
}

// ExampleService_Execute_customDelimiters shows using custom delimiters
func ExampleService_Execute_customDelimiters() {
	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	svc := gotmpl.NewService(nil)

	result, err := svc.Execute(ctx, gotmpl.TemplateOptions{
		Name:       "jinja-style.tmpl",
		Content:    "Welcome, [[.User]]!",
		LeftDelim:  "[[",
		RightDelim: "]]",
		Data: map[string]any{
			"User": "Bob",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.Output)
	// Output: Welcome, Bob!
}

// ExampleService_Execute_customFunctions demonstrates using custom template functions
func ExampleService_Execute_customFunctions() {
	ctx := logger.WithLogger(context.Background(), logger.Get(0))

	// Create service with default functions
	svc := gotmpl.NewService(template.FuncMap{
		"upper": strings.ToUpper,
		"lower": strings.ToLower,
	})

	result, err := svc.Execute(ctx, gotmpl.TemplateOptions{
		Name:    "formatted.tmpl",
		Content: "{{upper .FirstName}} {{lower .LastName}}",
		Data: map[string]any{
			"FirstName": "john",
			"LastName":  "DOE",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.Output)
	// Output: JOHN doe
}

// ExampleService_Execute_replacements shows protecting literal strings from template processing
func ExampleService_Execute_replacements() {
	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	svc := gotmpl.NewService(nil)

	// Template contains both template syntax and literal template syntax
	content := `Name: {{.Name}}
Literal template syntax: {{KEEP_THIS_LITERAL}}`

	result, err := svc.Execute(ctx, gotmpl.TemplateOptions{
		Name:    "mixed.tmpl",
		Content: content,
		Data: map[string]any{
			"Name": "Charlie",
		},
		Replacements: []gotmpl.Replacement{
			{Find: "{{KEEP_THIS_LITERAL}}"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.Output)
	// Output: Name: Charlie
	// Literal template syntax: {{KEEP_THIS_LITERAL}}
}

// ExampleService_Execute_complex demonstrates combining multiple features
func ExampleService_Execute_complex() {
	ctx := logger.WithLogger(context.Background(), logger.Get(0))

	// Create service with helper functions
	svc := gotmpl.NewService(template.FuncMap{
		"default": func(defaultVal, val string) string {
			if val == "" {
				return defaultVal
			}
			return val
		},
	})

	result, err := svc.Execute(ctx, gotmpl.TemplateOptions{
		Name:       "report.tmpl",
		Content:    "User: [[.Name]], Status: [[default \"inactive\" .Status]], Code: LITERAL_CODE",
		LeftDelim:  "[[",
		RightDelim: "]]",
		Data: map[string]any{
			"Name":   "David",
			"Status": "",
		},
		Replacements: []gotmpl.Replacement{
			{Find: "LITERAL_CODE", Replace: "TEMP_12345"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.Output)
	// Output: User: David, Status: inactive, Code: LITERAL_CODE
}

// ExampleService_Execute_loopAndConditionals shows template control structures
func ExampleService_Execute_loopAndConditionals() {
	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	svc := gotmpl.NewService(nil)

	content := `{{if .Title}}Title: {{.Title}}
{{end}}Items:{{range .Items}}
- {{.}}{{end}}`

	result, err := svc.Execute(ctx, gotmpl.TemplateOptions{
		Name:    "list.tmpl",
		Content: content,
		Data: map[string]any{
			"Title": "Shopping List",
			"Items": []string{"Apples", "Bananas", "Oranges"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.Output)
	// Output: Title: Shopping List
	// Items:
	// - Apples
	// - Bananas
	// - Oranges
}

// ExampleService_Execute_missingKeyHandling demonstrates handling missing keys
func ExampleService_Execute_missingKeyHandling() {
	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	svc := gotmpl.NewService(nil)

	// Default behavior: prints "<no value>"
	result1, _ := svc.Execute(ctx, gotmpl.TemplateOptions{
		Name:       "default.tmpl",
		Content:    "Status: {{.Status}}",
		Data:       map[string]any{}, // Status key missing
		MissingKey: gotmpl.MissingKeyDefault,
	})
	fmt.Println(result1.Output)

	// Zero behavior: returns zero value (empty string for string type)
	result2, _ := svc.Execute(ctx, gotmpl.TemplateOptions{
		Name:       "zero.tmpl",
		Content:    "Count: {{.Count}}",
		Data:       map[string]any{}, // Count key missing
		MissingKey: gotmpl.MissingKeyZero,
	})
	fmt.Println(result2.Output)

	// Output: Status: <no value>
	// Count: <no value>
}

// ExampleGetGoTemplateReferences demonstrates extracting data references from templates
// using the convenience function
func ExampleGetGoTemplateReferences() {
	template := `
{{if .User.IsAdmin}}
	Welcome, {{.User.Name}}!
	{{range .User.Permissions}}
		- {{.}}
	{{end}}
{{end}}
`

	refs, err := gotmpl.GetGoTemplateReferences(template, "", "")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Found references:")
	for _, ref := range refs {
		fmt.Printf("  %s\n", ref.Path)
	}
	// Output: Found references:
	//   .User.IsAdmin
	//   .User.Name
	//   .User.Permissions
}

// ExampleService_GetReferences demonstrates extracting references using the Service
func ExampleService_GetReferences() {
	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	svc := gotmpl.NewService(nil)

	refs, err := svc.GetReferences(ctx, gotmpl.TemplateOptions{
		Name:       "config.tmpl",
		Content:    "[[.App.Name]] version [[.App.Version]]",
		LeftDelim:  "[[",
		RightDelim: "]]",
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Template references:")
	for _, ref := range refs {
		fmt.Printf("  %s\n", ref.Path)
	}
	// Output: Template references:
	//   .App.Name
	//   .App.Version
}
