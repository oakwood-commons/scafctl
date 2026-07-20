// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package spec

// ArgDef declares a single named, typed argument accepted by a Call definition.
// Arguments are supplied at each call site and bound (defaulted, required-checked,
// and type-coerced) before the call's provider inputs are resolved.
type ArgDef struct {
	// Type is the argument's declared type. Supplied values are coerced to this
	// type via CoerceType. When omitted, the value passes through unchanged.
	// Supported types: string, int, float (alias: number), bool, array, object,
	// time, duration, and any.
	Type Type `json:"type,omitempty" yaml:"type,omitempty" doc:"Argument type (string, int, float, number, bool, array, object, time, duration, any)" example:"string"`

	// Required marks the argument as mandatory at every call site.
	// A required argument cannot declare a Default.
	Required bool `json:"required,omitempty" yaml:"required,omitempty" doc:"Whether the argument must be supplied at every call site"`

	// Default is applied when the argument is omitted at a call site.
	// Only valid when Required is false.
	Default any `json:"default,omitempty" yaml:"default,omitempty" doc:"Default value applied when the argument is omitted (only valid when required is false)"`

	// Description documents the argument for readers and tooling.
	Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"Human-readable description of the argument" maxLength:"500"`
}

// Call is a reusable, provider-agnostic request definition. It declares typed
// arguments and a provider plus inputs that reference those arguments via the
// args namespace (_.args.x in CEL, {{ .args.x }} in Go templates). A single
// definition can be invoked from multiple call sites, each supplying its own
// argument values and producing an independent result.
type Call struct {
	// Description documents the call definition for readers and tooling.
	Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"Human-readable description of the call definition" maxLength:"500"`

	// Args declares the named, typed arguments the definition accepts, keyed by
	// argument name.
	Args map[string]*ArgDef `json:"args,omitempty" yaml:"args,omitempty" doc:"Named, typed argument declarations keyed by argument name"`

	// Provider is the provider name executed when the call is invoked.
	Provider string `json:"provider" yaml:"provider" doc:"Provider name or reference to execute" maxLength:"100" example:"http" pattern:"^[a-zA-Z0-9][a-zA-Z0-9._:/@^~><=+*-]*$" patternDescription:"A provider name or reference: bare name, name@version, or registry/name@version"`

	// Inputs are the provider inputs. They reference arguments via the args
	// namespace and are resolved after arguments are bound.
	Inputs map[string]*ValueRef `json:"inputs,omitempty" yaml:"inputs,omitempty" doc:"Provider inputs; reference arguments via the args namespace"`

	// Dedup enables opt-in, in-memory de-duplication of identical invocations
	// within a single run. Results are keyed by the bound argument values and
	// are never persisted to state or shared across runs.
	Dedup bool `json:"dedup,omitempty" yaml:"dedup,omitempty" doc:"Opt-in in-memory de-duplication for identical bound args within a single run (never persisted)"`
}

// CallRef is embedded into step and action types to allow invoking a Call
// definition in place of a direct provider. Exactly one of {Call, Provider} may
// be set on a host step; this invariant is enforced by validation, not by the
// type system.
type CallRef struct {
	// Call is the name of a spec.calls definition to invoke.
	Call string `json:"call,omitempty" yaml:"call,omitempty" doc:"Name of a spec.calls definition to invoke" maxLength:"100" pattern:"^[a-zA-Z_][a-zA-Z0-9_-]*$" patternDescription:"Must reference an existing calls definition"`

	// Args supplies argument values for the call, keyed by argument name.
	// Values are standard ValueRefs (literals, rslvr, expr, or tmpl) and are
	// resolved in the host step's context.
	Args map[string]*ValueRef `json:"args,omitempty" yaml:"args,omitempty" doc:"Argument values for the call, keyed by argument name (literals, rslvr, expr, or tmpl)"`
}

// HasCall reports whether this reference invokes a call definition.
func (c *CallRef) HasCall() bool {
	return c != nil && c.Call != ""
}
