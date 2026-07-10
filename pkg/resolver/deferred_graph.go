// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import "sort"

// DeferredReportingPlan describes the order in which deferred (cross-resolver)
// validation results should be evaluated and reported.
//
// The reporting graph is derived from DeferredValidationUnit.DependsOn edges
// (which resolver a rule references). Unlike the resolution DAG, this graph is
// used only for ordering and reporting -- it MAY contain cycles (two resolvers
// can validate against each other). Cycles are tolerated: they are condensed
// into strongly connected components so a deterministic, root-cause-first order
// can still be produced.
type DeferredReportingPlan struct {
	// Order lists the resolver names owning deferred units in root-cause-first
	// order: a resolver whose deferred rules reference other resolvers is reported
	// after those referenced resolvers, so upstream failures surface first and can
	// suppress cascaded failures.
	Order []string
	// Cycles lists strongly connected components with more than one member. These
	// are informational: a cycle in the reporting graph is not an error (deferred
	// rules only read final values), but surfacing it helps authors understand why
	// ordering within the component is name-sorted rather than dependency-driven.
	Cycles [][]string
}

// buildDeferredReportingPlan computes a deterministic, cycle-tolerant reporting
// order for the given deferred validation units.
//
// It builds a graph whose edges are the depends-on references between unit
// owners (references to resolvers without deferred units are irrelevant to
// ordering and are ignored), then runs Tarjan's strongly-connected-components
// algorithm. Tarjan emits SCCs in reverse topological order of the condensation,
// which for depends-on edges is dependencies-first (root-cause-first). Nodes are
// visited in sorted order and each component is name-sorted so the result is
// stable across runs.
func buildDeferredReportingPlan(units []DeferredValidationUnit) DeferredReportingPlan {
	owners := make(map[string]bool, len(units))
	for _, u := range units {
		owners[u.ResolverName] = true
	}

	adj := make(map[string][]string, len(units))
	nodes := make([]string, 0, len(units))
	for _, u := range units {
		nodes = append(nodes, u.ResolverName)
		var edges []string
		for _, dep := range u.DependsOn {
			if dep != u.ResolverName && owners[dep] {
				edges = append(edges, dep)
			}
		}
		sort.Strings(edges)
		adj[u.ResolverName] = edges
	}
	sort.Strings(nodes)

	tj := &tarjan{
		index:   make(map[string]int, len(nodes)),
		lowlink: make(map[string]int, len(nodes)),
		onStack: make(map[string]bool, len(nodes)),
		adj:     adj,
	}
	for _, n := range nodes {
		if _, seen := tj.index[n]; !seen {
			tj.strongConnect(n)
		}
	}

	plan := DeferredReportingPlan{Order: make([]string, 0, len(units))}
	for _, scc := range tj.sccs {
		sort.Strings(scc)
		plan.Order = append(plan.Order, scc...)
		if len(scc) > 1 {
			plan.Cycles = append(plan.Cycles, scc)
		}
	}
	return plan
}

// tarjan holds the mutable state for Tarjan's SCC algorithm.
type tarjan struct {
	index   map[string]int
	lowlink map[string]int
	onStack map[string]bool
	stack   []string
	adj     map[string][]string
	counter int
	sccs    [][]string
}

// strongConnect performs the recursive depth-first traversal of Tarjan's
// algorithm. Recursion depth is bounded by the number of resolvers with deferred
// validation rules, which is small in practice.
func (t *tarjan) strongConnect(v string) {
	t.index[v] = t.counter
	t.lowlink[v] = t.counter
	t.counter++
	t.stack = append(t.stack, v)
	t.onStack[v] = true

	for _, w := range t.adj[v] {
		if _, seen := t.index[w]; !seen {
			t.strongConnect(w)
			if t.lowlink[w] < t.lowlink[v] {
				t.lowlink[v] = t.lowlink[w]
			}
		} else if t.onStack[w] {
			if t.index[w] < t.lowlink[v] {
				t.lowlink[v] = t.index[w]
			}
		}
	}

	// v is a root node of an SCC: pop the stack down to v.
	if t.lowlink[v] == t.index[v] {
		var scc []string
		for {
			n := len(t.stack) - 1
			w := t.stack[n]
			t.stack = t.stack[:n]
			t.onStack[w] = false
			scc = append(scc, w)
			if w == v {
				break
			}
		}
		t.sccs = append(t.sccs, scc)
	}
}
