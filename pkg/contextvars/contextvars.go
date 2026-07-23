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
	// Name is the canonical injected variable name, without any accessor
	// syntax (e.g. "__self", "__filePath"). In Go templates it is accessed
	// with a leading dot ({{ .__filePath }}); in CEL it is used bare (__self).
	Name string `json:"name" yaml:"name"`

	// Languages lists the expression languages the variable is available in
	// (cel, go-template, or both). Most scafctl context variables are injected
	// into both CEL evaluation and Go-template data; a few (the __file* path
	// parts) are Go-template only.
	Languages []string `json:"languages" yaml:"languages"`

	// Phases lists the evaluation phases in which the variable is available.
	Phases []string `json:"phases" yaml:"phases"`

	// Description explains what the variable contains and how to use it.
	Description string `json:"description" yaml:"description"`

	// Example is a short expression demonstrating the variable.
	Example string `json:"example,omitempty" yaml:"example,omitempty"`
}

// clonePhasesAndLangs returns a copy of v with its Languages and Phases slices
// deep-copied, so callers cannot mutate the package-level registry through a
// returned value.
func clonePhasesAndLangs(v ContextVariable) ContextVariable {
	langs := make([]string, len(v.Languages))
	copy(langs, v.Languages)
	phases := make([]string, len(v.Phases))
	copy(phases, v.Phases)
	v.Languages = langs
	v.Phases = phases
	return v
}

// langBoth and langTemplateOnly are the common language sets, defined once to
// keep the registry entries terse.
var (
	langBoth         = []string{LangCEL, LangTemplate}
	langCELOnly      = []string{LangCEL}
	langTemplateOnly = []string{LangTemplate}
)

// builtinVariables is the canonical list of context variables. Names are pinned
// to celexp.Var* constants where available so this registry tracks the engine.
var builtinVariables = []ContextVariable{
	{
		Name:        "_",
		Languages:   langBoth,
		Phases:      []string{PhaseResolve, PhaseTransform, PhaseValidate, PhaseForEach, PhaseAction},
		Description: "Map of resolved resolver values, keyed by resolver name. The primary way to reference other resolvers' outputs. Available in both CEL (_.region) and Go templates ({{ ._.region }}). Referencing _.other also creates an implicit dependency edge. Resolver data is in scope during forEach iterations too.",
		Example:     "_.region",
	},
	{
		Name:        celexp.VarSelf, // "__self"
		Languages:   langBoth,
		Phases:      []string{PhaseResolve, PhaseTransform, PhaseValidate, PhaseForEach},
		Description: "The current resolver's in-progress value. In transform it is the resolved value; in validate it is the final value; in a resolve-phase 'until' condition it is the current candidate value; and it is also bound during forEach iterations. Available in CEL (__self) and Go templates ({{ .__self }}). Not available in a plain resolve provider input (only in that step's 'until' condition).",
		Example:     "__self.trim()",
	},
	{
		Name:        celexp.VarItem, // "__item"
		Languages:   langBoth,
		Phases:      []string{PhaseForEach},
		Description: "The current element in a forEach iteration (resolve or transform). Available in CEL (__item) and Go templates ({{ .__item }}).",
		Example:     "__item.name",
	},
	{
		Name:        celexp.VarIndex, // "__index"
		Languages:   langBoth,
		Phases:      []string{PhaseForEach},
		Description: "The current zero-based index in a forEach iteration (resolve or transform). Available in CEL (__index) and Go templates ({{ .__index }}).",
		Example:     "__index == 0",
	},
	{
		Name:        celexp.VarPlan, // "__plan"
		Languages:   langCELOnly,
		Phases:      []string{PhaseResolve},
		Description: "Pre-execution resolver topology, injected before any resolver runs, so resolver when conditions and provider inputs can reason about the plan. Each entry exposes .phase, .dependsOn, and .dependencyCount.",
		Example:     "__plan[\"myResolver\"].phase",
	},
	{
		Name:        celexp.VarExecution, // "__execution"
		Languages:   langBoth,
		Phases:      []string{PhaseAction},
		Description: "Resolver execution metadata available to actions: __execution.resolvers.<name>.status/phase/duration and __execution.summary.{phaseCount,resolverCount,totalDuration}. Available in CEL and Go templates.",
		Example:     "__execution.resolvers.build.status",
	},
	{
		Name:        celexp.VarActions, // "__actions"
		Languages:   langBoth,
		Phases:      []string{PhaseAction},
		Description: "Results of completed actions, keyed by action name: __actions.<name>.results and __actions.<name>.status. Available in downstream action when conditions and inputs, in both CEL (__actions.build.results) and Go templates ({{ .__actions.build.results }}).",
		Example:     "__actions.build.results.exitCode",
	},
	{
		Name:        celexp.VarCwd, // "__cwd"
		Languages:   langBoth,
		Phases:      []string{PhaseAction},
		Description: "The original working directory, available in ACTIONS ONLY (in both CEL and Go templates). Useful when --output-dir redirects action output but an action still needs to reference the invocation directory. Not injected into resolvers.",
		Example:     "__cwd + \"/dist\"",
	},
	{
		Name:        celexp.VarParams, // "__params"
		Languages:   langCELOnly,
		Phases:      []string{PhaseStateBackend},
		Description: "Raw CLI parameters (-r key=value), available in STATE BACKEND input expressions only. Unlike _, which holds resolver outputs, __params always holds the raw parameters regardless of resolver execution state.",
		Example:     "__params.gcp_project",
	},
	{
		Name:        celexp.VarError, // "__error"
		Languages:   langCELOnly,
		Phases:      []string{PhaseError},
		Description: "The error bound in failure contexts. Its SHAPE is context-dependent: in RESOLVER continueOnError conditions and messages.error it is a STRING (the error text), so __error.contains(\"...\") works. In ACTION continueOnError and retryIf conditions it is a structured MAP with fields message, type, statusCode, exitCode, attempt, and maxAttempts -- use __error.message.contains(\"...\") and __error.statusCode there, not __error.contains(...).",
		Example:     "__error.contains(\"timeout\") // resolver; use __error.message.contains(\"timeout\") in actions",
	},
	{
		Name:        "__filePath",
		Languages:   langTemplateOnly,
		Phases:      []string{PhaseTemplateFile},
		Description: "Full source path of the current file during directory -> render-tree -> write-tree generation. Available in the file provider's outputPath template for renaming files.",
		Example:     "{{ .__fileDir }}/{{ .__fileStem }}",
	},
	{
		Name:        "__fileName",
		Languages:   langTemplateOnly,
		Phases:      []string{PhaseTemplateFile},
		Description: "Base file name (including extension) of the current file during tree generation.",
		Example:     "{{ .__fileName }}",
	},
	{
		Name:        "__fileStem",
		Languages:   langTemplateOnly,
		Phases:      []string{PhaseTemplateFile},
		Description: "File name without its extension during tree generation. Commonly used to strip a .tpl suffix.",
		Example:     "{{ .__fileStem }}",
	},
	{
		Name:        "__fileExtension",
		Languages:   langTemplateOnly,
		Phases:      []string{PhaseTemplateFile},
		Description: "File extension (including the leading dot) of the current file during tree generation.",
		Example:     "{{ .__fileExtension }}",
	},
	{
		Name:        "__fileDir",
		Languages:   langTemplateOnly,
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

// primaryLang returns the variable's first declared language, or "" if none.
// Used only for stable ordering.
func primaryLang(v ContextVariable) string {
	if len(v.Languages) == 0 {
		return ""
	}
	return v.Languages[0]
}

// lessByLangName orders context variables by primary language then name. It is
// the shared comparator used by List and ByPhase so their ordering cannot drift.
func lessByLangName(a, b ContextVariable) bool {
	if la, lb := primaryLang(a), primaryLang(b); la != lb {
		return la < lb
	}
	return a.Name < b.Name
}

// List returns all context variables sorted by language then name. Returned
// values are deep-copied, so callers cannot mutate the package registry.
func List() []ContextVariable {
	out := make([]ContextVariable, len(builtinVariables))
	for i, v := range builtinVariables {
		out[i] = clonePhasesAndLangs(v)
	}
	sort.Slice(out, func(i, j int) bool {
		return lessByLangName(out[i], out[j])
	})
	return out
}

// Get returns a context variable by exact name. The second return value is
// false if no variable with that name exists. The returned value is deep-copied,
// so mutating its slices cannot corrupt the package registry.
func Get(name string) (ContextVariable, bool) {
	v, ok := registry[name]
	if !ok {
		return ContextVariable{}, false
	}
	return clonePhasesAndLangs(v), true
}

// ByPhase returns all context variables available in the given phase, sorted by
// language then name. An unknown phase yields an empty slice. Returned values
// are deep-copied, so callers cannot mutate the package registry.
func ByPhase(phase string) []ContextVariable {
	var out []ContextVariable
	for _, v := range builtinVariables {
		for _, p := range v.Phases {
			if p == phase {
				out = append(out, clonePhasesAndLangs(v))
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
