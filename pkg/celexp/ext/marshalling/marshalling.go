package marshalling

import (
	"encoding/json"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/celexp/conversion"
	"gopkg.in/yaml.v3"
)

// JSONMarshalFunc returns a CEL function that marshals a value to a JSON string.
// The function takes any value and returns a compact JSON string representation.
//
// Example usage:
//
//	json.marshal({"name": "John", "age": 30}) // Returns: '{"name":"John","age":30}'
func JSONMarshalFunc() celexp.ExtFunction {
	funcName := "json.marshal"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Marshals a value to a compact JSON string",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Marshal a map to JSON",
				Expression:  `json.marshal({"name": "John", "age": 30})`,
			},
			{
				Description: "Marshal a list to JSON",
				Expression:  `json.marshal(["apple", "banana", "cherry"])`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload("json_marshal",
					[]*cel.Type{cel.DynType},
					cel.StringType,
					cel.UnaryBinding(func(value ref.Val) ref.Val {
						// Convert CEL value to Go value
						goValue := conversion.CelValueToGo(value)

						// Marshal to JSON
						jsonBytes, err := json.Marshal(goValue)
						if err != nil {
							return types.NewErr("json.marshal: %s", err.Error())
						}

						return types.String(jsonBytes)
					}),
				),
			),
		},
	}
}

// JSONMarshalPrettyFunc returns a CEL function that marshals a value to a pretty-printed JSON string.
// The function takes any value and returns an indented JSON string representation.
//
// Example usage:
//
//	json.marshalPretty({"name": "John", "age": 30})
func JSONMarshalPrettyFunc() celexp.ExtFunction {
	funcName := "json.marshalPretty"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Marshals a value to a pretty-printed JSON string with indentation",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Marshal a map to pretty JSON",
				Expression:  `json.marshalPretty({"name": "John", "age": 30})`,
			},
			{
				Description: "Marshal nested structures to pretty JSON",
				Expression:  `json.marshalPretty({"user": {"name": "John", "roles": ["admin", "user"]}})`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload("json_marshalPretty",
					[]*cel.Type{cel.DynType},
					cel.StringType,
					cel.UnaryBinding(func(value ref.Val) ref.Val {
						// Convert CEL value to Go value
						goValue := conversion.CelValueToGo(value)

						// Marshal to pretty JSON
						jsonBytes, err := json.MarshalIndent(goValue, "", "  ")
						if err != nil {
							return types.NewErr("json.marshalPretty: %s", err.Error())
						}

						return types.String(jsonBytes)
					}),
				),
			),
		},
	}
}

// JSONUnmarshalFunc returns a CEL function that unmarshals a JSON string to a value.
// The function takes a JSON string and returns the parsed value (map, list, string, number, bool, or null).
//
// Example usage:
//
//	json.unmarshal('{"name":"John","age":30}') // Returns: {"name": "John", "age": 30}
func JSONUnmarshalFunc() celexp.ExtFunction {
	funcName := "json.unmarshal"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Unmarshals a JSON string to a value",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Unmarshal a JSON object",
				Expression:  `json.unmarshal('{"name":"John","age":30}')`,
			},
			{
				Description: "Unmarshal a JSON array",
				Expression:  `json.unmarshal('["apple","banana","cherry"]')`,
			},
			{
				Description: "Unmarshal and access properties",
				Expression:  `json.unmarshal('{"user":{"name":"John"}}').user.name`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload("json_unmarshal",
					[]*cel.Type{cel.StringType},
					cel.DynType,
					cel.UnaryBinding(func(value ref.Val) ref.Val {
						jsonStr, ok := value.Value().(string)
						if !ok {
							return types.NewErr("json.unmarshal: expected string, got %T", value.Value())
						}

						var result any
						if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
							return types.NewErr("json.unmarshal: %s", err.Error())
						}

						return conversion.GoToCelValue(result)
					}),
				),
			),
		},
	}
}

// YamlMarshalFunc returns a CEL function that marshals a value to a YAML string.
// The function takes any value and returns a YAML string representation.
//
// Example usage:
//
//	yaml.marshal({"name": "John", "age": 30})
func YamlMarshalFunc() celexp.ExtFunction {
	funcName := "yaml.marshal"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Marshals a value to a YAML string",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Marshal a map to YAML",
				Expression:  `yaml.marshal({"name": "John", "age": 30})`,
			},
			{
				Description: "Marshal a list to YAML",
				Expression:  `yaml.marshal(["apple", "banana", "cherry"])`,
			},
			{
				Description: "Marshal nested structures to YAML",
				Expression:  `yaml.marshal({"user": {"name": "John", "roles": ["admin", "user"]}})`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload("yaml_marshal",
					[]*cel.Type{cel.DynType},
					cel.StringType,
					cel.UnaryBinding(func(value ref.Val) ref.Val {
						// Convert CEL value to Go value
						goValue := conversion.CelValueToGo(value)

						// Marshal to YAML
						yamlBytes, err := yaml.Marshal(goValue)
						if err != nil {
							return types.NewErr("yaml.marshal: %s", err.Error())
						}

						return types.String(yamlBytes)
					}),
				),
			),
		},
	}
}

// YamlUnmarshalFunc returns a CEL function that unmarshals a YAML string to a value.
// The function takes a YAML string and returns the parsed value (map, list, string, number, bool, or null).
//
// Example usage:
//
//	yaml.unmarshal('name: John\nage: 30') // Returns: {"name": "John", "age": 30}
func YamlUnmarshalFunc() celexp.ExtFunction {
	funcName := "yaml.unmarshal"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Unmarshals a YAML string to a value",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Unmarshal a YAML object",
				Expression:  `yaml.unmarshal('name: John\nage: 30')`,
			},
			{
				Description: "Unmarshal a YAML array",
				Expression:  `yaml.unmarshal('- apple\n- banana\n- cherry')`,
			},
			{
				Description: "Unmarshal and access properties",
				Expression:  `yaml.unmarshal('user:\n  name: John').user.name`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload("yaml_unmarshal",
					[]*cel.Type{cel.StringType},
					cel.DynType,
					cel.UnaryBinding(func(value ref.Val) ref.Val {
						yamlStr, ok := value.Value().(string)
						if !ok {
							return types.NewErr("yaml.unmarshal: expected string, got %T", value.Value())
						}

						var result any
						if err := yaml.Unmarshal([]byte(yamlStr), &result); err != nil {
							return types.NewErr("yaml.unmarshal: %s", err.Error())
						}

						return conversion.GoToCelValue(result)
					}),
				),
			),
		},
	}
}
