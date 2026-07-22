// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package contextvars provides a registry of the special "context variables"
// injected into scafctl CEL and Go-template evaluation (e.g. _, __self, __item,
// __plan, __execution, __actions, __cwd, __params, __error, and the Go-template
// .__file* path parts).
//
// No other tool in scafctl enumerates these injected variables or the phase in
// which each is available: list_cel_functions returns functions, and the
// solution schema describes shape, not the runtime evaluation environment. This
// package is the single source of truth consumed by both the MCP
// list_context_variables tool and the "context-variables" concept prose.
//
// The canonical variable names are pinned to the celexp.Var* constants where
// they exist, so the registry cannot silently drift from the names the engine
// actually injects. The phase/availability metadata is authored here because the
// engine constants do not carry it.
package contextvars

import (
	"sort"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
)

// Phase identifiers describe where in evaluation a context variable is
// available. They are stable strings suitable for filtering (e.g. via the MCP
// list_context_variables tool's "phase" argument).
const (
	// PhaseResolve is a resolver's resolve phase (provider inputs and when).
	PhaseResolve = "resolve"
	// PhaseTransform is a resolver's transform phase.
	PhaseTransform = "transform"
	// PhaseValidate is a resolver's validate phase.
	PhaseValidate = "validate"
	// PhaseForEach is a forEach iteration (resolve or transform).
	PhaseForEach = "forEach"
	// PhaseAction is an action's when conditions and inputs.
	PhaseAction = "action"
	// PhaseStateBackend is a state backend's input expressions.
	PhaseStateBackend = "state-backend"
	// PhaseError is a failure context (continueOnError conditions, error messages).
	PhaseError = "error"
	// PhaseTemplateFile is Go-template file generation (directory/render-tree/
	// write-tree outputPath rendering).
	PhaseTemplateFile = "template-file"
)

// Language identifies the expression language a variable applies to.
const (
	// LangCEL means the variable is available in CEL expressions.
	LangCEL = "cel"
	// LangTemplate means the variable is available in Go templates.
	LangTemplate = "go-template"
)

// ContextVariable describes a single injected evaluation variable.
type ContextVariable struct {
	// Name is the variable identifier as used in an expression (e.g. "__self",
	// or ".__filePath" for Go-template path parts).
	Name string `json:"name" yaml:"name"`

	// Language is the expression language the variable applies to (cel or
	// go-template).
	Language string `json:"language" yaml:"language"`

	// Phases lists the evaluation phases in which the variable is available.
	Phases []string `json:"phases" yaml:"phases"`

	// Description explains what the variable contains and how to use it.
	Description string `json:"description" yaml:"description"`

	// Example is a short expression demonstrating the variable.
	Example string `json:"example,omitempty" yaml:"example,omitempty"`
}

// builtinVariables is the canonical list of context variables. Names are pinned
// to celexp.Var* constants where available so this registry tracks the engine.
var builtinVariables = []ContextVariable{
	{
		Name:        "_",
		Language:    LangCEL,
		Phases:      []string{PhaseResolve, PhaseTransform, PhaseValidate, PhaseAction},
		Description: "Map of resolved resolver values, keyed by resolver name. The primary way to reference other resolvers' outputs. Referencing _.other also creates an implicit dependency edge.",
		Example:     "_.region",
	},
	{
		Name:        celexp.VarSelf, // "__self"
		Language:    LangCEL,
		Phases:      []string{PhaseTransform, PhaseValidate},
		Description: "The current resolver's in-progress value: the resolved value in transform steps, and the final value in validate steps. Not available during resolve.",
		Example:     "__self.trimSpace()",
	},
	{
		Name:        celexp.VarItem, // "__item"
		Language:    LangCEL,
		Phases:      []string{PhaseForEach},
		Description: "The current element in a forEach iteration (resolve or transform).",
		Example:     "__item.name",
	},
	{
		Name:        celexp.VarIndex, // "__index"
		Language:    LangCEL,
		Phases:      []string{PhaseForEach},
		Description: "The current zero-based index in a forEach iteration (resolve or transform).",
		Example:     "__index == 0",
	},
	{
		Name:        celexp.VarPlan, // "__plan"
		Language:    LangCEL,
		Phases:      []string{PhaseResolve},
		Description: "Pre-execution resolver topology, injected before any resolver runs, so resolver when conditions and provider inputs can reason about the plan. Each entry exposes .phase, .dependsOn, and .dependencyCount.",
		Example:     "__plan[\"myResolver\"].phase",
	},
	{
		Name:        celexp.VarExecution, // "__execution"
		Language:    LangCEL,
		Phases:      []string{PhaseAction},
		Description: "Resolver execution metadata available to actions: __execution.resolvers.<name>.status/phase/duration and __execution.summary.{phaseCount,resolverCount,totalDuration}.",
		Example:     "__execution.resolvers.build.status",
	},
	{
		Name:        celexp.VarActions, // "__actions"
		Language:    LangCEL,
		Phases:      []string{PhaseAction},
		Description: "Results of completed actions, keyed by action name: __actions.<name>.results and __actions.<name>.status. Available in downstream action when conditions and inputs.",
		Example:     "__actions.build.results.exitCode",
	},
	{
		Name:        celexp.VarCwd, // "__cwd"
		Language:    LangCEL,
		Phases:      []string{PhaseAction},
		Description: "The original working directory, available in ACTIONS ONLY. Useful when --output-dir redirects action output but an action still needs to reference the invocation directory. Not injected into resolvers.",
		Example:     "__cwd + \"/dist\"",
	},
	{
		Name:        celexp.VarParams, // "__params"
		Language:    LangCEL,
		Phases:      []string{PhaseStateBackend},
		Description: "Raw CLI parameters (-r key=value), available in STATE BACKEND input expressions only. Unlike _, which holds resolver outputs, __params always holds the raw parameters regardless of resolver execution state.",
		Example:     "__params.gcp_project",
	},
	{
		Name:        celexp.VarError, // "__error"
		Language:    LangCEL,
		Phases:      []string{PhaseError},
		Description: "The error text bound in failure contexts: continueOnError conditions and messages.error, for both resolvers and actions.",
		Example:     "__error.contains(\"timeout\")",
	},
	{
		Name:        ".__filePath",
		Language:    LangTemplate,
		Phases:      []string{PhaseTemplateFile},
		Description: "Full source path of the current file during directory -> render-tree -> write-tree generation. Available in the file provider's outputPath template for renaming files.",
		Example:     "{{ .__fileDir }}/{{ .__fileStem }}",
	},
	{
		Name:        ".__fileName",
		Language:    LangTemplate,
		Phases:      []string{PhaseTemplateFile},
		Description: "Base file name (including extension) of the current file during tree generation.",
		Example:     "{{ .__fileName }}",
	},
	{
		Name:        ".__fileStem",
		Language:    LangTemplate,
		Phases:      []string{PhaseTemplateFile},
		Description: "File name without its extension during tree generation. Commonly used to strip a .tpl suffix.",
		Example:     "{{ .__fileStem }}",
	},
	{
		Name:        ".__fileExtension",
		Language:    LangTemplate,
		Phases:      []string{PhaseTemplateFile},
		Description: "File extension (including the leading dot) of the current file during tree generation.",
		Example:     "{{ .__fileExtension }}",
	},
	{
		Name:        ".__fileDir",
		Language:    LangTemplate,
		Phases:      []string{PhaseTemplateFile},
		Description: "Directory portion of the current file's path during tree generation, used to preserve the source tree structure in output.",
		Example:     "{{ if .__fileDir }}{{ .__fileDir }}/{{ end }}{{ .__fileStem }}",
	},
}

// registry indexes builtinVariables by name for O(1) lookup.
var registry = func() map[string]ContextVariable {
	m := make(map[string]ContextVariable, len(builtinVariables))
	for _, v := range builtinVariables {
		m[v.Name] = v
	}
	return m
}()

// lessByLangName orders context variables by language then name. It is the
// shared comparator used by List and ByPhase so their ordering cannot drift.
func lessByLangName(a, b ContextVariable) bool {
	if a.Language != b.Language {
		return a.Language < b.Language
	}
	return a.Name < b.Name
}

// List returns all context variables sorted by language then name.
func List() []ContextVariable {
	out := make([]ContextVariable, len(builtinVariables))
	copy(out, builtinVariables)
	sort.Slice(out, func(i, j int) bool {
		return lessByLangName(out[i], out[j])
	})
	return out
}

// Get returns a context variable by exact name. The second return value is
// false if no variable with that name exists.
func Get(name string) (ContextVariable, bool) {
	v, ok := registry[name]
	return v, ok
}

// ByPhase returns all context variables available in the given phase, sorted by
// language then name. An unknown phase yields an empty slice.
func ByPhase(phase string) []ContextVariable {
	var out []ContextVariable
	for _, v := range builtinVariables {
		for _, p := range v.Phases {
			if p == phase {
				out = append(out, v)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return lessByLangName(out[i], out[j])
	})
	return out
}

// Phases returns the sorted set of distinct phase identifiers across all
// registered variables.
func Phases() []string {
	seen := map[string]bool{}
	for _, v := range builtinVariables {
		for _, p := range v.Phases {
			seen[p] = true
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
