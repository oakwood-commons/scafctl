// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package spec

// ParamDef declares a single positional, typed parameter accepted by a Function
// definition. Unlike a Call's ArgDef (which is keyed by name and supplied by
// name at the call site), a Function parameter is positional: it is bound to the
// argument at the same index when the helper is invoked from a Go template
// (e.g. {{ myHelper .a .b }} binds .a to the first parameter and .b to the
// second). The parameter's Name is how the value is referenced inside the
// function body under the args namespace (_.args.name in CEL, {{ .args.name }}
// in a Go template body).
type ParamDef struct {
	// Name is the parameter's identifier. It is how the bound value is
	// referenced inside the function body under the args namespace. Names must
	// be unique within a function and be valid identifiers.
	Name string `json:"name" yaml:"name" doc:"Parameter name; referenced inside the body as _.args.name (CEL) or {{ .args.name }} (template)" maxLength:"100" example:"value" pattern:"^[a-zA-Z_][a-zA-Z0-9_]*$" patternDescription:"Must start with a letter or underscore, followed by letters, numbers, or underscores"`

	// Type is the parameter's declared type. Supplied values are coerced to this
	// type via CoerceType. When omitted, the value passes through unchanged.
	// Supported types: string, int, float (alias: number), bool, array, object,
	// time, duration, and any.
	Type Type `json:"type,omitempty" yaml:"type,omitempty" doc:"Parameter type (string, int, float, number, bool, array, object, time, duration, any)" example:"string"`

	// Required marks the parameter as mandatory at every call site.
	// A required parameter cannot declare a Default.
	Required bool `json:"required,omitempty" yaml:"required,omitempty" doc:"Whether the parameter must be supplied at every call site"`

	// Default is applied when the argument is omitted at a call site.
	// Only valid when Required is false.
	Default any `json:"default,omitempty" yaml:"default,omitempty" doc:"Default value applied when the argument is omitted (only valid when required is false)"`

	// Description documents the parameter for readers and tooling.
	Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"Human-readable description of the parameter" maxLength:"500"`
}

// Function is a solution-author-defined, named Go template helper. It is
// declared once under spec.functions and becomes callable from the Go templates
// the solution renders through the go-template provider (in resolvers and
// actions) as {{ name arg... }}. A function declares ordered, typed parameters
// and exactly one body:
//
//   - Cel: a CEL expression evaluated with the bound arguments available under
//     the args namespace (_.args.name). The expression's result is returned to
//     the template verbatim (any type), fitting scafctl's CEL-first design.
//   - Template: a Go template body rendered with the bound arguments available
//     as {{ .args.name }}. It returns the rendered string, in the style of
//     Helm named templates. A template body may call sibling functions and all
//     built-in (sprig + custom) helpers.
//
// Cel and Template are mutually exclusive; exactly one must be set. This
// invariant is enforced by validation, not by the type system.
type Function struct {
	// Description documents the function for readers and tooling.
	Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"Human-readable description of the function" maxLength:"500"`

	// Params declares the ordered, positional parameters the function accepts.
	// Arguments supplied at a call site are bound to parameters by position.
	Params []*ParamDef `json:"params,omitempty" yaml:"params,omitempty" doc:"Ordered, positional parameter declarations bound by position at the call site"`

	// Cel is a CEL expression body. The bound arguments are available under the
	// args namespace (_.args.name). Mutually exclusive with Template.
	Cel string `json:"cel,omitempty" yaml:"cel,omitempty" doc:"CEL expression body; bound arguments available as _.args.name. Mutually exclusive with template." maxLength:"65536" example:"_.args.value.upperAscii()"`

	// Template is a Go template body. The bound arguments are available as
	// {{ .args.name }}. Mutually exclusive with Cel.
	Template string `json:"template,omitempty" yaml:"template,omitempty" doc:"Go template body; bound arguments available as {{ .args.name }}. Mutually exclusive with cel." maxLength:"65536" example:"{{ .args.value | upper }}"`
}

// HasCel reports whether the function declares a CEL expression body.
func (f *Function) HasCel() bool {
	return f != nil && f.Cel != ""
}

// HasTemplate reports whether the function declares a Go template body.
func (f *Function) HasTemplate() bool {
	return f != nil && f.Template != ""
}
