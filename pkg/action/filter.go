// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"fmt"
	"sort"
)

// FilterWorkflowActions returns a new Workflow containing only the specified
// actions and their transitive dependsOn dependencies. Finally actions are
// always included regardless of the filter. When targetNames is empty the
// original workflow is returned unchanged.
//
// An error is returned if any target name does not match an action in the
// workflow's actions section.
func FilterWorkflowActions(w *Workflow, targetNames []string) (*Workflow, error) {
	if len(targetNames) == 0 || w == nil {
		return w, nil
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
