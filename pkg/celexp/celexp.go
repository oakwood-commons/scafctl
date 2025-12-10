package celexp

import "github.com/google/cel-go/cel"

type ExtFunction struct {
	Name          string          `json:"name,omitempty" yaml:"name,omitempty"`
	Links         []string        `json:"links,omitempty" yaml:"links,omitempty"`
	Description   string          `json:"description,omitempty" yaml:"description,omitempty"`
	Custom        bool            `json:"custom,omitempty" yaml:"custom,omitempty"`
	EnvOptions    []cel.EnvOption `json:"-" yaml:"-"`
	FunctionNames []string        `json:"functionNames,omitempty" yaml:"functionNames,omitempty"`
	Macros        []string        `json:"macros,omitempty" yaml:"macros,omitempty"`
}
