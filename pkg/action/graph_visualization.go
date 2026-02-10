// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"fmt"
	"io"
	"sort"
)

// GraphVisualization holds visualization data for rendering.
type GraphVisualization struct {
	// Phases contains main action phases for visualization.
	Phases []*VisualizationPhase `json:"phases" yaml:"phases"`

	// FinallyPhases contains finally action phases.
	FinallyPhases []*VisualizationPhase `json:"finallyPhases,omitempty" yaml:"finallyPhases,omitempty"`

	// Edges represents dependencies between actions.
	Edges []*VisualizationEdge `json:"edges" yaml:"edges"`

	// Stats contains graph statistics.
	Stats *VisualizationStats `json:"stats" yaml:"stats"`
}

// VisualizationPhase represents a phase in the execution order.
type VisualizationPhase struct {
	Phase   int      `json:"phase" yaml:"phase"`
	Actions []string `json:"actions" yaml:"actions"`
	Section string   `json:"section" yaml:"section"` // "actions" or "finally"
}

// VisualizationEdge represents a dependency edge.
type VisualizationEdge struct {
	From  string `json:"from" yaml:"from"`
	To    string `json:"to" yaml:"to"`
	Label string `json:"label,omitempty" yaml:"label,omitempty"`
}

// VisualizationStats contains graph statistics.
type VisualizationStats struct {
	TotalActions    int     `json:"totalActions" yaml:"totalActions"`
	TotalPhases     int     `json:"totalPhases" yaml:"totalPhases"`
	MaxParallelism  int     `json:"maxParallelism" yaml:"maxParallelism"`
	AvgDependencies float64 `json:"avgDependencies" yaml:"avgDependencies"`
	HasFinally      bool    `json:"hasFinally" yaml:"hasFinally"`
	ForEachCount    int     `json:"forEachCount" yaml:"forEachCount"`
}

// BuildVisualization creates visualization data from a Graph.
func BuildVisualization(graph *Graph) *GraphVisualization {
	if graph == nil {
		return &GraphVisualization{
			Phases: []*VisualizationPhase{},
			Edges:  []*VisualizationEdge{},
			Stats:  &VisualizationStats{},
		}
	}

	viz := &GraphVisualization{
		Phases:        make([]*VisualizationPhase, 0, len(graph.ExecutionOrder)),
		FinallyPhases: make([]*VisualizationPhase, 0, len(graph.FinallyOrder)),
		Edges:         make([]*VisualizationEdge, 0),
	}

	// Build main action phases
	for i, phase := range graph.ExecutionOrder {
		viz.Phases = append(viz.Phases, &VisualizationPhase{
			Phase:   i,
			Actions: phase,
			Section: "actions",
		})
	}

	// Build finally phases
	for i, phase := range graph.FinallyOrder {
		viz.FinallyPhases = append(viz.FinallyPhases, &VisualizationPhase{
			Phase:   i,
			Actions: phase,
			Section: "finally",
		})
	}

	// Build edges from action dependencies
	for name, action := range graph.Actions {
		for _, dep := range action.Dependencies {
			viz.Edges = append(viz.Edges, &VisualizationEdge{
				From: name,
				To:   dep,
			})
		}
	}

	// Sort edges for deterministic output
	sort.Slice(viz.Edges, func(i, j int) bool {
		if viz.Edges[i].From != viz.Edges[j].From {
			return viz.Edges[i].From < viz.Edges[j].From
		}
		return viz.Edges[i].To < viz.Edges[j].To
	})

	// Calculate stats
	viz.Stats = calculateVisualizationStats(graph, viz)

	return viz
}

// calculateVisualizationStats computes graph statistics.
func calculateVisualizationStats(graph *Graph, _ *GraphVisualization) *VisualizationStats {
	stats := &VisualizationStats{
		TotalActions: len(graph.Actions),
		TotalPhases:  len(graph.ExecutionOrder) + len(graph.FinallyOrder),
		HasFinally:   len(graph.FinallyOrder) > 0,
	}

	// Calculate max parallelism
	maxParallelism := 0
	for _, phase := range graph.ExecutionOrder {
		if len(phase) > maxParallelism {
			maxParallelism = len(phase)
		}
	}
	for _, phase := range graph.FinallyOrder {
		if len(phase) > maxParallelism {
			maxParallelism = len(phase)
		}
	}
	stats.MaxParallelism = maxParallelism

	// Calculate average dependencies
	totalDeps := 0
	forEachCount := 0
	for _, action := range graph.Actions {
		totalDeps += len(action.Dependencies)
		if action.ForEachMetadata != nil {
			forEachCount++
		}
	}
	if len(graph.Actions) > 0 {
		stats.AvgDependencies = float64(totalDeps) / float64(len(graph.Actions))
	}
	stats.ForEachCount = forEachCount

	return stats
}

// RenderASCII generates ASCII art representation of the action graph.
func (v *GraphVisualization) RenderASCII(w io.Writer) error {
	fmt.Fprintln(w, "Action Dependency Graph:")
	fmt.Fprintln(w)

	// Render main action phases
	if len(v.Phases) > 0 {
		fmt.Fprintln(w, "=== Main Actions ===")
		for _, phase := range v.Phases {
			fmt.Fprintf(w, "Phase %d:\n", phase.Phase)
			for _, actionName := range phase.Actions {
				deps := v.getDependencies(actionName)
				if len(deps) > 0 {
					fmt.Fprintf(w, "  - %s\n", actionName)
					fmt.Fprintln(w, "    depends on:")
					for _, dep := range deps {
						fmt.Fprintf(w, "      * %s\n", dep)
					}
				} else {
					fmt.Fprintf(w, "  - %s\n", actionName)
				}
			}
			fmt.Fprintln(w)
		}
	}

	// Render finally phases
	if len(v.FinallyPhases) > 0 {
		fmt.Fprintln(w, "=== Finally Actions ===")
		for _, phase := range v.FinallyPhases {
			fmt.Fprintf(w, "Phase %d:\n", phase.Phase)
			for _, actionName := range phase.Actions {
				deps := v.getDependencies(actionName)
				if len(deps) > 0 {
					fmt.Fprintf(w, "  - %s\n", actionName)
					fmt.Fprintln(w, "    depends on:")
					for _, dep := range deps {
						fmt.Fprintf(w, "      * %s\n", dep)
					}
				} else {
					fmt.Fprintf(w, "  - %s\n", actionName)
				}
			}
			fmt.Fprintln(w)
		}
	}

	// Render stats
	fmt.Fprintln(w, "Statistics:")
	fmt.Fprintf(w, "  Total Actions: %d\n", v.Stats.TotalActions)
	fmt.Fprintf(w, "  Total Phases: %d\n", v.Stats.TotalPhases)
	fmt.Fprintf(w, "  Max Parallelism: %d\n", v.Stats.MaxParallelism)
	fmt.Fprintf(w, "  Avg Dependencies: %.2f\n", v.Stats.AvgDependencies)
	if v.Stats.ForEachCount > 0 {
		fmt.Fprintf(w, "  ForEach Expansions: %d\n", v.Stats.ForEachCount)
	}
	if v.Stats.HasFinally {
		fmt.Fprintln(w, "  Has Finally: yes")
	}

	return nil
}

// getDependencies returns dependencies for an action from edges.
func (v *GraphVisualization) getDependencies(actionName string) []string {
	deps := make([]string, 0)
	for _, edge := range v.Edges {
		if edge.From == actionName {
			deps = append(deps, edge.To)
		}
	}
	sort.Strings(deps)
	return deps
}

// RenderDOT generates GraphViz DOT format.
func (v *GraphVisualization) RenderDOT(w io.Writer) error {
	fmt.Fprintln(w, "digraph Actions {")
	fmt.Fprintln(w, "  rankdir=LR;")
	fmt.Fprintln(w, "  node [shape=box, style=rounded];")
	fmt.Fprintln(w)

	// Main action phase subgraphs
	for _, phase := range v.Phases {
		fmt.Fprintf(w, "  subgraph cluster_phase_%d {\n", phase.Phase)
		fmt.Fprintf(w, "    label=\"Phase %d\";\n", phase.Phase)
		fmt.Fprintln(w, "    style=filled;")
		fmt.Fprintln(w, "    color=lightgrey;")
		fmt.Fprintln(w)

		for _, actionName := range phase.Actions {
			color := getActionPhaseColor(phase.Phase)
			style := "rounded,filled"
			if isForEachAction(actionName) {
				style = "rounded,filled,bold"
			}
			fmt.Fprintf(w, "    \"%s\" [fillcolor=%s, style=\"%s\"];\n",
				actionName, color, style)
		}

		fmt.Fprintln(w, "  }")
		fmt.Fprintln(w)
	}

	// Finally action phase subgraphs
	for i, phase := range v.FinallyPhases {
		phaseNum := len(v.Phases) + i
		fmt.Fprintf(w, "  subgraph cluster_finally_%d {\n", phase.Phase)
		fmt.Fprintf(w, "    label=\"Finally Phase %d\";\n", phase.Phase)
		fmt.Fprintln(w, "    style=filled;")
		fmt.Fprintln(w, "    color=lightyellow;")
		fmt.Fprintln(w)

		for _, actionName := range phase.Actions {
			color := "lightsalmon"
			fmt.Fprintf(w, "    \"%s\" [fillcolor=%s, style=\"rounded,filled\"];\n",
				actionName, color)
		}

		fmt.Fprintln(w, "  }")
		_ = phaseNum
		fmt.Fprintln(w)
	}

	// Edges
	fmt.Fprintln(w, "  // Dependencies")
	for _, edge := range v.Edges {
		label := ""
		if edge.Label != "" {
			label = fmt.Sprintf(" [label=\"%s\"]", edge.Label)
		}
		fmt.Fprintf(w, "  \"%s\" -> \"%s\"%s;\n", edge.From, edge.To, label)
	}

	fmt.Fprintln(w, "}")
	return nil
}

// getActionPhaseColor returns a color for a phase number.
func getActionPhaseColor(phase int) string {
	colors := []string{"lightblue", "lightgreen", "lightcyan", "lightpink", "lavender"}
	return colors[phase%len(colors)]
}

// isForEachAction checks if an action name indicates a forEach expansion.
func isForEachAction(name string) bool {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == ']' {
			for j := i - 1; j >= 0; j-- {
				if name[j] == '[' {
					return true
				}
			}
		}
	}
	return false
}

// RenderMermaid generates Mermaid diagram format.
func (v *GraphVisualization) RenderMermaid(w io.Writer) error {
	fmt.Fprintln(w, "graph LR")

	// Main action phase subgraphs
	for _, phase := range v.Phases {
		fmt.Fprintf(w, "  subgraph Phase_%d[\"Phase %d\"]\n", phase.Phase, phase.Phase)
		for _, actionName := range phase.Actions {
			nodeID := sanitizeMermaidID(actionName)
			if isForEachAction(actionName) {
				fmt.Fprintf(w, "    %s[[\"%s\"]]\n", nodeID, actionName)
			} else {
				fmt.Fprintf(w, "    %s[\"%s\"]\n", nodeID, actionName)
			}
		}
		fmt.Fprintln(w, "  end")
	}

	// Finally phase subgraphs
	for _, phase := range v.FinallyPhases {
		fmt.Fprintf(w, "  subgraph Finally_%d[\"Finally Phase %d\"]\n", phase.Phase, phase.Phase)
		for _, actionName := range phase.Actions {
			nodeID := sanitizeMermaidID(actionName)
			fmt.Fprintf(w, "    %s[\"%s\"]:::finally\n", nodeID, actionName)
		}
		fmt.Fprintln(w, "  end")
	}

	// Edges
	for _, edge := range v.Edges {
		fromID := sanitizeMermaidID(edge.From)
		toID := sanitizeMermaidID(edge.To)
		if edge.Label != "" {
			fmt.Fprintf(w, "  %s -->|%s| %s\n", fromID, edge.Label, toID)
		} else {
			fmt.Fprintf(w, "  %s --> %s\n", fromID, toID)
		}
	}

	// Styles
	fmt.Fprintln(w, "  classDef finally fill:#ffc,stroke:#aa0")

	return nil
}

// sanitizeMermaidID converts an action name to a valid Mermaid node ID.
func sanitizeMermaidID(name string) string {
	result := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
			result = append(result, c)
		case c >= 'A' && c <= 'Z':
			result = append(result, c)
		case c >= '0' && c <= '9':
			result = append(result, c)
		case c == '_' || c == '-':
			result = append(result, c)
		case c == '[':
			result = append(result, '_')
		case c == ']':
			// skip
		default:
			result = append(result, '_')
		}
	}
	return string(result)
}
