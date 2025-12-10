package guid

import (
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/uuid"
	"github.com/kcloutie/scafctl/pkg/celexp"
)

// NewFunc generates a new UUID (GUID).
func NewFunc() celexp.ExtFunction {
	funcName := "guid.new"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Generates a new random UUID (GUID) in standard format. Use guid.new() to create a universally unique identifier",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Generate a new UUID",
				Expression:  `guid.new()`,
			},
			{
				Description: "Use in string concatenation",
				Expression:  `"id-" + guid.new()`,
			},
			{
				Description: "Generate multiple UUIDs",
				Expression:  `[guid.new(), guid.new(), guid.new()]`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{},
					cel.StringType,
					cel.FunctionBinding(func(_ ...ref.Val) ref.Val {
						newUUID := uuid.New()
						return types.String(newUUID.String())
					}),
				),
			),
		},
	}
}
