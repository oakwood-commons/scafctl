// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"context"
	"fmt"
	"sort"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"k8s.io/apimachinery/pkg/util/sets"
)

// Graph represents the executable action dependency graph.
// It contains expanded actions with materialized inputs and execution order.
type Graph struct {
	// Actions is a map of all expanded actions keyed by their name.
	// ForEach actions are expanded with indexed names like "deploy[0]", "deploy[1]".
	Actions map[string]*ExpandedAction `json:"actions" yaml:"actions" doc:"All expanded actions"`

	// ExecutionOrder contains phases of action names that can run in parallel.
	// Phase 0 contains actions with no dependencies, phase 1 contains actions
	// that depend only on phase 0 actions, and so on.
	ExecutionOrder [][]string `json:"executionOrder" yaml:"executionOrder" doc:"Parallel execution phases for main actions"`

	// FinallyOrder contains phases for the finally section.
	// Finally actions have an implicit dependency on all main actions completing.
	FinallyOrder [][]string `json:"finallyOrder,omitempty" yaml:"finallyOrder,omitempty" doc:"Parallel execution phases for finally actions"`
}

// ExpandedAction is an action with materialized inputs ready for execution.
// For forEach actions, each iteration becomes a separate ExpandedAction.
type ExpandedAction struct {
	// Action is the original action definition.
	*Action `json:",inline" yaml:",inline"`

	// ExpandedName is the name for this expanded action.
	// For forEach actions, this is "baseName[index]" (e.g., "deploy[0]").
	// For regular actions, this matches the action's name.
	ExpandedName string `json:"expandedName" yaml:"expandedName" doc:"Name for this expanded action" example:"deploy[0]"`

	// MaterializedInputs contains inputs that were fully resolved during graph building.
	// These do not reference __actions and can be used directly.
	MaterializedInputs map[string]any `json:"materializedInputs,omitempty" yaml:"materializedInputs,omitempty" doc:"Resolved input values"`

	// DeferredInputs contains inputs that reference __actions and must be resolved at runtime.
	DeferredInputs map[string]*DeferredValue `json:"deferredInputs,omitempty" yaml:"deferredInputs,omitempty" doc:"Inputs requiring runtime resolution"`

	// Section indicates which workflow section this action belongs to.
	Section string `json:"section" yaml:"section" doc:"Workflow section (actions or finally)" example:"actions"`

	// ForEachMetadata contains expansion information if this action was expanded from a forEach.
	ForEachMetadata *ForEachExpansionMetadata `json:"forEachMetadata,omitempty" yaml:"forEachMetadata,omitempty" doc:"ForEach expansion info"`

	// Dependencies contains the effective dependencies for this expanded action.
	// For regular actions, this matches DependsOn plus any implicit dependencies from __actions references.
	// For expanded forEach actions, this includes dependencies on all iterations of referenced forEach actions.
	Dependencies []string `json:"dependencies" yaml:"dependencies" doc:"Effective dependencies for scheduling"`
}

// ForEachExpansionMetadata tracks forEach expansion information.
type ForEachExpansionMetadata struct {
	// ExpandedFrom is the original action name before forEach expansion.
	ExpandedFrom string `json:"expandedFrom" yaml:"expandedFrom" doc:"Original action name" example:"deploy"`

	// Index is the iteration index (0-based) within the forEach expansion.
	Index int `json:"index" yaml:"index" doc:"Iteration index" example:"0"`

	// Item is the current iteration item value.
	Item any `json:"item,omitempty" yaml:"item,omitempty" doc:"Current iteration item"`
}

// BuildGraphOptions configures graph building behavior.
type BuildGraphOptions struct {
	// SkipInputMaterialization skips input materialization (for validation-only use cases).
	SkipInputMaterialization bool
}

// BuildGraph constructs the action dependency graph from a workflow.
// It expands forEach actions, materializes inputs (where possible), extracts dependencies,
// and computes execution phases.
func BuildGraph(ctx context.Context, w *Workflow, resolverData map[string]any, opts *BuildGraphOptions) (*Graph, error) {
	if w == nil {
		return nil, fmt.Errorf("workflow cannot be nil")
	}

	if opts == nil {
		opts = &BuildGraphOptions{}
	}

	graph := &Graph{
		Actions: make(map[string]*ExpandedAction),
	}

	// Process main actions section
	mainExpanded, mainDeps, err := expandSection(ctx, w.Actions, "actions", resolverData, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to expand actions section: %w", err)
	}

	for name, action := range mainExpanded {
		graph.Actions[name] = action
	}

	// Process finally section
	finallyExpanded, finallyDeps, err := expandSection(ctx, w.Finally, "finally", resolverData, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to expand finally section: %w", err)
	}

	for name, action := range finallyExpanded {
		graph.Actions[name] = action
	}

	// Compute execution order for main actions
	mainOrder, err := computeExecutionPhases(mainExpanded, mainDeps)
	if err != nil {
		return nil, fmt.Errorf("failed to compute main execution order: %w", err)
	}
	graph.ExecutionOrder = mainOrder

	// Compute execution order for finally actions
	if len(finallyExpanded) > 0 {
		finallyOrder, err := computeExecutionPhases(finallyExpanded, finallyDeps)
		if err != nil {
			return nil, fmt.Errorf("failed to compute finally execution order: %w", err)
		}
		graph.FinallyOrder = finallyOrder
	}

	return graph, nil
}

// expandSection expands all actions in a section, handling forEach expansion.
// Returns the expanded actions and their dependencies.
func expandSection(
	ctx context.Context,
	actions map[string]*Action,
	section string,
	resolverData map[string]any,
	opts *BuildGraphOptions,
) (map[string]*ExpandedAction, map[string][]string, error) {
	expanded := make(map[string]*ExpandedAction)
	deps := make(map[string][]string)

	// Track which actions have forEach for dependency rewriting
	forEachActions := make(map[string]bool)
	forEachExpansions := make(map[string][]string) // original name -> expanded names

	// First pass: identify forEach actions and expand them
	for name, action := range actions {
		if action == nil {
			continue
		}
		// Set name from map key
		action.Name = name

		if action.ForEach != nil {
			forEachActions[name] = true

			// Evaluate the forEach.In array
			items, err := evaluateForEachArray(ctx, action.ForEach, resolverData)
			if err != nil {
				return nil, nil, fmt.Errorf("action %q: failed to evaluate forEach: %w", name, err)
			}

			expandedNames := make([]string, 0, len(items))

			// Store original name before creating iterations
			originalName := name

			// Create an expanded action for each iteration
			for i, item := range items {
				expandedName := fmt.Sprintf("%s[%d]", originalName, i)
				expandedNames = append(expandedNames, expandedName)

				expandedAction, err := createExpandedAction(ctx, action, section, i, item, originalName, resolverData, opts)
				if err != nil {
					return nil, nil, fmt.Errorf("action %q iteration %d: %w", originalName, i, err)
				}
				// Set the expanded name (this is a separate field to avoid overwriting the base action's name)
				expandedAction.ExpandedName = expandedName

				expanded[expandedName] = expandedAction
			}

			forEachExpansions[originalName] = expandedNames
		} else {
			// Non-forEach action - create single expanded action
			expandedAction, err := createExpandedAction(ctx, action, section, -1, nil, name, resolverData, opts)
			if err != nil {
				return nil, nil, fmt.Errorf("action %q: %w", name, err)
			}

			expanded[name] = expandedAction
		}
	}

	// Second pass: compute dependencies with rewriting for forEach expansions
	for name, expandedAction := range expanded {
		effectiveDeps := computeEffectiveDependencies(expandedAction, forEachExpansions)
		expandedAction.Dependencies = effectiveDeps
		deps[name] = effectiveDeps
	}

	return expanded, deps, nil
}

// evaluateForEachArray evaluates the forEach.In expression to get the array to iterate over.
func evaluateForEachArray(ctx context.Context, forEach *spec.ForEachClause, resolverData map[string]any) ([]any, error) {
	if forEach == nil {
		return nil, fmt.Errorf("forEach clause is nil")
	}

	if forEach.In == nil {
		return nil, fmt.Errorf("forEach.in is required for actions")
	}

	// Evaluate the In expression
	result, err := forEach.In.Resolve(ctx, resolverData, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate forEach.in: %w", err)
	}

	// Convert to []any
	switch v := result.(type) {
	case []any:
		return v, nil
	case []string:
		items := make([]any, len(v))
		for i, s := range v {
			items[i] = s
		}
		return items, nil
	case []int:
		items := make([]any, len(v))
		for i, n := range v {
			items[i] = n
		}
		return items, nil
	case []float64:
		items := make([]any, len(v))
		for i, f := range v {
			items[i] = f
		}
		return items, nil
	case nil:
		return []any{}, nil
	default:
		return nil, fmt.Errorf("forEach.in must evaluate to an array, got %T", result)
	}
}

// createExpandedAction creates an ExpandedAction from an action definition.
// For forEach iterations, index >= 0, item is set, and originalName is the base action name.
func createExpandedAction(
	ctx context.Context,
	action *Action,
	section string,
	index int,
	item any,
	originalName string,
	resolverData map[string]any,
	opts *BuildGraphOptions,
) (*ExpandedAction, error) {
	expanded := &ExpandedAction{
		Action:       action,
		Section:      section,
		ExpandedName: originalName, // Default to original name, may be overwritten for forEach
	}

	// Set forEach metadata if this is an iteration
	if index >= 0 {
		expanded.ForEachMetadata = &ForEachExpansionMetadata{
			ExpandedFrom: originalName,
			Index:        index,
			Item:         item,
		}
	}

	// Materialize inputs unless skipped
	if !opts.SkipInputMaterialization && action.Inputs != nil {
		materialized := make(map[string]any)
		deferred := make(map[string]*DeferredValue)

		// Build iteration context for forEach actions
		var iterCtx *spec.IterationContext
		if index >= 0 && action.ForEach != nil {
			iterCtx = &spec.IterationContext{
				Item:       item,
				Index:      index,
				ItemAlias:  action.ForEach.Item,
				IndexAlias: action.ForEach.Index,
			}
		}

		for inputName, valueRef := range action.Inputs {
			if valueRef == nil {
				materialized[inputName] = nil
				continue
			}

			// Check if the value references __actions (needs deferral)
			if valueRef.ReferencesVariable(celexp.VarActions) {
				dv, err := materializeDeferred(valueRef)
				if err != nil {
					return nil, fmt.Errorf("failed to defer input %q: %w", inputName, err)
				}
				deferred[inputName] = dv
				continue
			}

			// Evaluate immediately with iteration context if available
			var val any
			var err error
			if iterCtx != nil {
				val, err = valueRef.ResolveWithIterationContext(ctx, resolverData, nil, iterCtx)
			} else {
				val, err = valueRef.Resolve(ctx, resolverData, nil)
			}
			if err != nil {
				return nil, fmt.Errorf("failed to materialize input %q: %w", inputName, err)
			}
			materialized[inputName] = val
		}

		if len(materialized) > 0 {
			expanded.MaterializedInputs = materialized
		}
		if len(deferred) > 0 {
			expanded.DeferredInputs = deferred
		}
	}

	return expanded, nil
}

// computeEffectiveDependencies computes the effective dependencies for an expanded action.
// This includes explicit dependsOn entries and implicit dependencies from __actions references.
// Dependencies on forEach actions are rewritten to depend on all iterations.
func computeEffectiveDependencies(
	expanded *ExpandedAction,
	forEachExpansions map[string][]string,
) []string {
	depSet := sets.Set[string]{}

	// Add explicit dependsOn entries
	if expanded.Action != nil {
		for _, dep := range expanded.DependsOn {
			// Check if this dependency was expanded by forEach
			if expandedNames, ok := forEachExpansions[dep]; ok {
				// Depend on all iterations
				depSet.Insert(expandedNames...)
			} else {
				depSet.Insert(dep)
			}
		}
	}

	// Add implicit dependencies from __actions references in deferred inputs
	for _, dv := range expanded.DeferredInputs {
		refs := extractActionsRefsFromDeferred(dv)
		for _, ref := range refs {
			// Check if this dependency was expanded by forEach
			if expandedNames, ok := forEachExpansions[ref]; ok {
				// Depend on all iterations
				depSet.Insert(expandedNames...)
			} else {
				depSet.Insert(ref)
			}
		}
	}

	// Convert to sorted slice for deterministic order
	deps := sets.List(depSet)
	sort.Strings(deps)
	return deps
}

// extractActionsRefsFromDeferred extracts __actions references from a deferred value.
func extractActionsRefsFromDeferred(dv *DeferredValue) []string {
	if dv == nil {
		return nil
	}

	refs := sets.Set[string]{}

	if dv.OriginalExpr != "" {
		// Parse as CEL expression
		exprRefs := parseActionsRefsForGraph(dv.OriginalExpr)
		refs.Insert(exprRefs...)
	}

	if dv.OriginalTmpl != "" {
		// Parse as Go template
		tmplRefs := parseActionsRefsForGraph(dv.OriginalTmpl)
		refs.Insert(tmplRefs...)
	}

	return sets.List(refs)
}

// computeExecutionPhases computes topologically sorted execution phases.
// Each phase contains actions that can run in parallel (all dependencies satisfied).
func computeExecutionPhases(
	actions map[string]*ExpandedAction,
	deps map[string][]string,
) ([][]string, error) {
	if len(actions) == 0 {
		return nil, nil
	}

	// Build in-degree map and adjacency list
	inDegree := make(map[string]int)
	dependents := make(map[string][]string) // action -> actions that depend on it

	// Initialize in-degree for all actions
	for name := range actions {
		inDegree[name] = 0
	}

	// Calculate in-degrees
	for name, actionDeps := range deps {
		for _, dep := range actionDeps {
			// Only count dependencies that exist in this section
			if _, exists := actions[dep]; exists {
				inDegree[name]++
				dependents[dep] = append(dependents[dep], name)
			}
		}
	}

	// Kahn's algorithm with phase tracking
	var phases [][]string
	remaining := len(actions)

	for remaining > 0 {
		// Find all actions with in-degree 0
		var phase []string
		for name, degree := range inDegree {
			if degree == 0 {
				phase = append(phase, name)
			}
		}

		if len(phase) == 0 {
			// Cycle detected - should have been caught by validation
			return nil, fmt.Errorf("cycle detected in action dependencies")
		}

		// Sort phase for deterministic ordering
		sort.Strings(phase)
		phases = append(phases, phase)

		// Remove processed actions and update in-degrees
		for _, name := range phase {
			delete(inDegree, name)
			remaining--

			// Decrease in-degree of dependents
			for _, dependent := range dependents[name] {
				if _, exists := inDegree[dependent]; exists {
					inDegree[dependent]--
				}
			}
		}
	}

	return phases, nil
}

// GetAllActionNames returns all action names in the graph including expanded forEach names.
func (g *Graph) GetAllActionNames() []string {
	names := make([]string, 0, len(g.Actions))
	for name := range g.Actions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// GetMainActionNames returns only the main section action names.
func (g *Graph) GetMainActionNames() []string {
	var names []string
	for name, action := range g.Actions {
		if action.Section == "actions" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// GetFinallyActionNames returns only the finally section action names.
func (g *Graph) GetFinallyActionNames() []string {
	var names []string
	for name, action := range g.Actions {
		if action.Section == "finally" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// GetForEachIterations returns all expanded iteration names for a base action name.
// Returns nil if the action is not a forEach action or doesn't exist.
func (g *Graph) GetForEachIterations(baseName string) []string {
	var iterations []string
	for name, action := range g.Actions {
		if action.ForEachMetadata != nil && action.ForEachMetadata.ExpandedFrom == baseName {
			iterations = append(iterations, name)
		}
	}
	sort.Strings(iterations)
	return iterations
}

// HasDeferredInputs returns true if the action has any deferred inputs.
func (e *ExpandedAction) HasDeferredInputs() bool {
	return len(e.DeferredInputs) > 0
}

// GetOriginalName returns the original action name (before forEach expansion).
func (e *ExpandedAction) GetOriginalName() string {
	if e.ForEachMetadata != nil {
		return e.ForEachMetadata.ExpandedFrom
	}
	return e.ExpandedName
}

// GetExpandedName returns the expanded action name.
// For forEach actions this is "baseName[index]", for regular actions it matches the original name.
func (e *ExpandedAction) GetExpandedName() string {
	return e.ExpandedName
}

// IsForEachIteration returns true if this action is an expanded forEach iteration.
func (e *ExpandedAction) IsForEachIteration() bool {
	return e.ForEachMetadata != nil
}

// TotalPhases returns the total number of execution phases for main actions.
func (g *Graph) TotalPhases() int {
	return len(g.ExecutionOrder)
}

// TotalFinallyPhases returns the total number of execution phases for finally actions.
func (g *Graph) TotalFinallyPhases() int {
	return len(g.FinallyOrder)
}

// GetActionsByPhase returns all actions in a specific execution phase.
func (g *Graph) GetActionsByPhase(phase int) []*ExpandedAction {
	if phase < 0 || phase >= len(g.ExecutionOrder) {
		return nil
	}

	var actions []*ExpandedAction
	for _, name := range g.ExecutionOrder[phase] {
		if action, ok := g.Actions[name]; ok {
			actions = append(actions, action)
		}
	}
	return actions
}

// GetFinallyActionsByPhase returns all finally actions in a specific execution phase.
func (g *Graph) GetFinallyActionsByPhase(phase int) []*ExpandedAction {
	if phase < 0 || phase >= len(g.FinallyOrder) {
		return nil
	}

	var actions []*ExpandedAction
	for _, name := range g.FinallyOrder[phase] {
		if action, ok := g.Actions[name]; ok {
			actions = append(actions, action)
		}
	}
	return actions
}

// parseActionsRefsForGraph extracts action names from __actions.NAME.results patterns.
// This wraps the parseActionsRefsFromString function for use in graph building.
func parseActionsRefsForGraph(s string) []string {
	refs := make(map[string]struct{})
	parseActionsRefsFromString(s, refs)

	result := make([]string, 0, len(refs))
	for name := range refs {
		result = append(result, name)
	}
	return result
}
