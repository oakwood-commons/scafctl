package strings

import (
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var (
	cleanReplacer = strings.NewReplacer("-", "", "_", "", " ", "")
	titleCaser    = cases.Title(language.English)
)

// cleanString removes hyphens, underscores, and spaces from a lowercased string.
func cleanString(s string) string {
	return cleanReplacer.Replace(strings.ToLower(s))
}

// titleString converts a string to title case using English language rules.
func titleString(s string) string {
	return titleCaser.String(s)
}

func CleanFunc() celexp.ExtFunction {
	funcName := "strings.clean"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Cleans a string by converting it to lowercase and removing hyphens, underscores, and spaces",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Clean a string with mixed separators",
				Expression:  `strings.clean("My-String_Name Test")`,
			},
			{
				Description: "Clean an uppercase string with hyphens",
				Expression:  `strings.clean("HELLO-WORLD")`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{cel.StringType},
					cel.StringType,
					cel.UnaryBinding(func(dirtyStringRef ref.Val) ref.Val {
						str, ok := dirtyStringRef.Value().(string)
						if !ok {
							return types.NewErr("strings.clean: expected string argument, got %s", dirtyStringRef.Type())
						}
						return types.String(cleanString(str))
					}),
				),
			),
		},
	}
}

func TitleFunc() celexp.ExtFunction {
	funcName := "strings.title"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Converts a string to title case using English language rules",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Convert a lowercase string to title case",
				Expression:  `strings.title("hello world")`,
			},
			{
				Description: "Convert uppercase to proper title case",
				Expression:  `strings.title("HELLO WORLD")`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{cel.StringType},
					cel.StringType,
					cel.UnaryBinding(func(inputStringRef ref.Val) ref.Val {
						str, ok := inputStringRef.Value().(string)
						if !ok {
							return types.NewErr("strings.title: expected string argument, got %s", inputStringRef.Type())
						}
						return types.String(titleString(str))
					}),
				),
			),
		},
	}
}
