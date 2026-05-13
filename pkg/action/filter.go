// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"fmt"
	"sort"
)

// FilterWorkflowActions returns a new Workflow containing only the specified
// actions and their transitive dependsOn dependencies. Finally actions are
// always included regardless of the filter. When targetNames is empty,
// explicit actions (and their exclusive dependents) are excluded; any
// non-explicit action whose transitive deps include an explicit action will
// still pull that explicit action into the result.
//
// An error is returned if any target name does not match an action in the
// workflow's actions section.
func FilterWorkflowActions(w *Workflow, targetNames []string) (*Workflow, error) {
	if w == nil {
		return w, nil
	}

	if len(targetNames) == 0 {
		// Exclude explicit actions from bare invocations, but preserve
		// dependency closure: start from all non-explicit actions, then
		// collect their transitive deps (which may include explicit ones).
		hasExplicit := false
		for _, act := range w.Actions {
			if act.Explicit {
				hasExplicit = true
				break
			}
		}
		if !hasExplicit {
			return w, nil
		}

		needed := make(map[string]bool)
		var collectDeps func(name string)
		collectDeps = func(name string) {
			if needed[name] {
				return
			}
			a, exists := w.Actions[name]
			if !exists || a == nil {
				return
			}
			needed[name] = true
			for _, dep := range a.DependsOn {
				collectDeps(dep)
			}
		}
		for name, act := range w.Actions {
			if !act.Explicit {
				collectDeps(name)
			}
		}

		filtered := &Workflow{
			Actions:          make(map[string]*Action, len(needed)),
			Finally:          w.Finally,
			ResultSchemaMode: w.ResultSchemaMode,
		}
		for name := range needed {
			filtered.Actions[name] = w.Actions[name]
		}
		return filtered, nil
	}

	// Build alias → name map for resolving alias references
	aliasMap := make(map[string]string)
	for name, a := range w.Actions {
		if a != nil && a.Alias != "" {
			aliasMap[a.Alias] = name
		}
	}

	// Resolve target names (may be aliases) and validate existence
	resolved := make([]string, 0, len(targetNames))
	for _, t := range targetNames {
		name := t
		if canonical, ok := aliasMap[t]; ok {
			name = canonical
		}
		if _, exists := w.Actions[name]; !exists {
			available := make([]string, 0, len(w.Actions))
			for n := range w.Actions {
				available = append(available, n)
			}
			sort.Strings(available)
			return nil, fmt.Errorf("action %q not found in workflow (available: %v)", t, available)
		}
		resolved = append(resolved, name)
	}

	// Collect transitive dependsOn dependencies
	needed := make(map[string]bool)
	var collectDeps func(name string)
	collectDeps = func(name string) {
		if needed[name] {
			return
		}
		a, exists := w.Actions[name]
		if !exists || a == nil {
			return
		}
		needed[name] = true
		for _, dep := range a.DependsOn {
			collectDeps(dep)
		}
	}
	for _, name := range resolved {
		collectDeps(name)
	}

	// Build filtered actions map preserving only needed actions
	filtered := make(map[string]*Action, len(needed))
	for name := range needed {
		filtered[name] = w.Actions[name]
	}

	return &Workflow{
		Actions:          filtered,
		Finally:          w.Finally,
		ResultSchemaMode: w.ResultSchemaMode,
	}, nil
}
