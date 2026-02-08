// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"fmt"
	"io"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// DescriptorLookup is a function that retrieves a provider descriptor by name.
// Used during dependency extraction to allow providers to participate in
// extracting dependencies from their inputs.
type DescriptorLookup func(providerName string) *provider.Descriptor

// extractDependencies extracts all resolver references from a resolver definition.
// If lookup is provided, it will use provider-specific ExtractDependencies functions
// when available. If lookup is nil, only generic extraction is performed.
// Explicit dependencies from DependsOn are always included and merged with auto-extracted dependencies.
func extractDependencies(r *Resolver, lookup DescriptorLookup) []string {
	deps := make(map[string]bool) // Use map to deduplicate

	// Include explicit dependencies from DependsOn field
	for _, dep := range r.DependsOn {
		if dep != "" {
			deps[dep] = true
		}
	}

	// Extract from when condition
	if r.When != nil && r.When.Expr != nil {
		extractDepsFromExpression(string(*r.When.Expr), deps)
	}

	// Extract from resolve phase
	if r.Resolve != nil {
		extractDepsFromResolvePhase(r.Resolve, deps, lookup)
	}

	// Extract from transform phase
	if r.Transform != nil {
		extractDepsFromTransformPhase(r.Transform, deps, lookup)
	}

	// Extract from validate phase
	if r.Validate != nil {
		extractDepsFromValidatePhase(r.Validate, deps, lookup)
	}

	// Convert to slice
	result := make([]string, 0, len(deps))
	for dep := range deps {
		result = append(result, dep)
	}

	return result
}

// extractDepsFromExpression extracts resolver references from CEL expressions
// Uses the existing GetUnderscoreVariables() method from pkg/celexp/refs.go
func extractDepsFromExpression(expr string, deps map[string]bool) {
	// Use existing CEL expression parsing functionality
	celExpr := celexp.Expression(expr)

	// Extract all _.resolverName references
	vars, err := celExpr.GetUnderscoreVariables()
	if err != nil {
		// If parsing fails, skip dependency extraction for this expression
		// This is a non-fatal error - the resolver may still be valid
		return
	}

	// Add all found variables to the deps map
	for _, v := range vars {
		deps[v] = true
	}
}

// extractDepsFromValueRef extracts dependencies from a ValueRef
func extractDepsFromValueRef(ref *ValueRef, deps map[string]bool) {
	if ref == nil {
		return
	}

	// Direct resolver reference
	if ref.Resolver != nil {
		deps[*ref.Resolver] = true
		return
	}

	// Expression
	if ref.Expr != nil {
		extractDepsFromExpression(string(*ref.Expr), deps)
		return
	}

	// Template
	if ref.Tmpl != nil {
		extractDepsFromTemplate(string(*ref.Tmpl), deps)
		return
	}

	// Literal string - check if it contains CEL-like expressions (_.resolverName patterns)
	// This handles cases where provider inputs contain expressions as literal strings
	if ref.Literal != nil {
		extractDepsFromLiteral(ref.Literal, deps)
	}
}

// extractDepsFromLiteral recursively extracts dependencies from literal values
// that may contain CEL expression strings or Go template syntax
func extractDepsFromLiteral(literal any, deps map[string]bool) {
	switch v := literal.(type) {
	case string:
		// Check if the string contains CEL-like expressions (_.something patterns)
		if strings.Contains(v, "_.") {
			extractDepsFromExpression(v, deps)
		}
		// Check if the string contains Go template syntax ({{ and }})
		// This handles cases like go-template provider inputs with {{.resolverName}} patterns
		if strings.Contains(v, "{{") && strings.Contains(v, "}}") {
			extractDepsFromTemplate(v, deps)
		}
	case map[string]any:
		// Recursively check map values
		for _, mapVal := range v {
			extractDepsFromLiteral(mapVal, deps)
		}
	case []any:
		// Recursively check array elements
		for _, arrVal := range v {
			extractDepsFromLiteral(arrVal, deps)
		}
	}
}

// extractDepsFromTemplate extracts resolver references from Go templates
// Uses the gotmpl package's GetReferences function for proper template parsing
func extractDepsFromTemplate(tmplContent string, deps map[string]bool) {
	// Use the gotmpl package to properly parse template references
	refs, err := gotmpl.GetGoTemplateReferences(tmplContent, "", "")
	if err != nil {
		// If parsing fails, this is a non-fatal error - the resolver may still be valid
		return
	}

	// Extract resolver names from paths that reference data
	for _, ref := range refs {
		path := ref.Path
		// Handle different path patterns from template parsing
		// The parser returns paths like:
		// - "._.resolverName" for {{ ._.resolverName }} (ValueRef tmpl pattern)
		// - ".__self" for {{ .__self }} (special variable)
		// - ".resolverName" for {{ .resolverName }} (go-template provider pattern)
		switch {
		case strings.HasPrefix(path, "._."):
			// Extract "resolverName" from "._.resolverName"
			varName := strings.TrimPrefix(path, "._.")
			// Only take the first segment if there are nested accesses
			if idx := strings.Index(varName, "."); idx != -1 {
				varName = varName[:idx]
			}
			deps[varName] = true
		case strings.HasPrefix(path, ".__"):
			// Skip special variables like __self, __item, __index - they are not dependencies
			continue
		case strings.HasPrefix(path, "._"):
			// Handle _.resolverName pattern (without leading dot after _.)
			varName := strings.TrimPrefix(path, "._")
			// Only take the first segment if there are nested accesses
			if idx := strings.Index(varName, "."); idx != -1 {
				varName = varName[:idx]
			}
			deps[varName] = true
		case strings.HasPrefix(path, "."):
			// Handle direct root-level access like ".resolverName" used by go-template provider
			// This pattern is used when resolver data is at the root level of template data
			varName := strings.TrimPrefix(path, ".")
			// Only take the first segment if there are nested accesses (e.g., ".config.host" -> "config")
			if idx := strings.Index(varName, "."); idx != -1 {
				varName = varName[:idx]
			}
			// Skip empty names (from "." alone)
			if varName != "" {
				deps[varName] = true
			}
		}
	}
}

// extractDepsFromProviderInputs attempts to use a provider's ExtractDependencies function
// to extract dependencies from inputs. Returns true if the provider handled the extraction,
// false if generic extraction should be used instead.
func extractDepsFromProviderInputs(providerName string, inputs map[string]*ValueRef, deps map[string]bool, lookup DescriptorLookup) bool {
	if lookup == nil {
		return false
	}

	desc := lookup(providerName)
	if desc == nil || desc.ExtractDependencies == nil {
		return false
	}

	// Convert ValueRef inputs to raw map for the provider's ExtractDependencies function
	rawInputs := make(map[string]any)
	for key, ref := range inputs {
		if ref == nil {
			continue
		}
		// Extract the actual value from the ValueRef
		switch {
		case ref.Literal != nil:
			rawInputs[key] = ref.Literal
		case ref.Resolver != nil:
			rawInputs[key] = map[string]any{"rslvr": *ref.Resolver}
		case ref.Expr != nil:
			rawInputs[key] = map[string]any{"expr": string(*ref.Expr)}
		case ref.Tmpl != nil:
			rawInputs[key] = map[string]any{"tmpl": string(*ref.Tmpl)}
		}
	}

	// Call the provider's ExtractDependencies function
	providerDeps := desc.ExtractDependencies(rawInputs)
	for _, dep := range providerDeps {
		deps[dep] = true
	}

	return true
}

// extractDepsFromResolvePhase extracts dependencies from a resolve phase
func extractDepsFromResolvePhase(phase *ResolvePhase, deps map[string]bool, lookup DescriptorLookup) {
	if phase == nil {
		return
	}

	// Extract from when condition
	if phase.When != nil && phase.When.Expr != nil {
		extractDepsFromExpression(string(*phase.When.Expr), deps)
	}

	// Extract from until condition
	if phase.Until != nil && phase.Until.Expr != nil {
		extractDepsFromExpression(string(*phase.Until.Expr), deps)
	}

	// Extract from each source
	for _, source := range phase.With {
		// Extract from when condition
		if source.When != nil && source.When.Expr != nil {
			extractDepsFromExpression(string(*source.When.Expr), deps)
		}

		// Try provider-specific extraction first
		if extractDepsFromProviderInputs(source.Provider, source.Inputs, deps, lookup) {
			continue
		}

		// Fall back to generic extraction from inputs
		for _, input := range source.Inputs {
			extractDepsFromValueRef(input, deps)
		}
	}
}

// extractDepsFromTransformPhase extracts dependencies from a transform phase
func extractDepsFromTransformPhase(phase *TransformPhase, deps map[string]bool, lookup DescriptorLookup) {
	if phase == nil {
		return
	}

	// Extract from when condition
	if phase.When != nil && phase.When.Expr != nil {
		extractDepsFromExpression(string(*phase.When.Expr), deps)
	}

	// Extract from each transform step
	for _, transform := range phase.With {
		// Extract from when condition
		if transform.When != nil && transform.When.Expr != nil {
			extractDepsFromExpression(string(*transform.When.Expr), deps)
		}

		// Extract from forEach.In (if using forEach with custom source)
		if transform.ForEach != nil && transform.ForEach.In != nil {
			extractDepsFromValueRef(transform.ForEach.In, deps)
		}

		// Try provider-specific extraction first
		if extractDepsFromProviderInputs(transform.Provider, transform.Inputs, deps, lookup) {
			continue
		}

		// Fall back to generic extraction from inputs
		for _, input := range transform.Inputs {
			extractDepsFromValueRef(input, deps)
		}
	}
}

// extractDepsFromValidatePhase extracts dependencies from a validate phase
func extractDepsFromValidatePhase(phase *ValidatePhase, deps map[string]bool, lookup DescriptorLookup) {
	if phase == nil {
		return
	}

	// Extract from when condition
	if phase.When != nil && phase.When.Expr != nil {
		extractDepsFromExpression(string(*phase.When.Expr), deps)
	}

	// Extract from each validation rule
	for _, validation := range phase.With {
		// Try provider-specific extraction first
		if extractDepsFromProviderInputs(validation.Provider, validation.Inputs, deps, lookup) {
			// Still extract from message even if provider handled inputs
			extractDepsFromValueRef(validation.Message, deps)
			continue
		}

		// Fall back to generic extraction from inputs
		for _, input := range validation.Inputs {
			extractDepsFromValueRef(input, deps)
		}

		// Extract from message
		extractDepsFromValueRef(validation.Message, deps)
	}
}

// GraphNode represents a resolver node in the dependency graph
type GraphNode struct {
	Name         string            `json:"id" yaml:"id" doc:"Resolver name"`
	Type         Type              `json:"type" yaml:"type" doc:"Resolver type"`
	Phase        int               `json:"phase" yaml:"phase" doc:"Execution phase (1-based)"`
	Conditional  bool              `json:"conditional" yaml:"conditional" doc:"Whether resolver has conditional execution"`
	Dependencies []GraphDependency `json:"dependencies" yaml:"dependencies" doc:"List of dependencies"`
}

// GraphDependency represents a dependency edge
type GraphDependency struct {
	Resolver string `json:"resolver" yaml:"resolver" doc:"Target resolver name"`
	Field    string `json:"field" yaml:"field" doc:"Field name in reference"`
}

// GraphEdge represents a directed edge
type GraphEdge struct {
	From  string `json:"from" yaml:"from" doc:"Source resolver name"`
	To    string `json:"to" yaml:"to" doc:"Target resolver name"`
	Label string `json:"label" yaml:"label" doc:"Edge label"`
}

// Graph represents the complete resolver dependency graph
type Graph struct {
	Nodes  []*GraphNode `json:"nodes" yaml:"nodes" doc:"Graph nodes"`
	Edges  []*GraphEdge `json:"edges" yaml:"edges" doc:"Graph edges"`
	Phases []*PhaseInfo `json:"phases" yaml:"phases" doc:"Phase information"`
	Stats  *GraphStats  `json:"stats" yaml:"stats" doc:"Graph statistics"`
}

// PhaseInfo contains information about a phase
type PhaseInfo struct {
	Phase       int      `json:"phase" yaml:"phase" doc:"Phase number (1-based)"`
	Resolvers   []string `json:"resolvers" yaml:"resolvers" doc:"Resolver names in this phase"`
	Parallelism int      `json:"parallelism" yaml:"parallelism" doc:"Number of resolvers that can execute in parallel"`
}

// GraphStats contains graph statistics
type GraphStats struct {
	TotalResolvers  int     `json:"totalResolvers" yaml:"totalResolvers" doc:"Total number of resolvers"`
	TotalPhases     int     `json:"totalPhases" yaml:"totalPhases" doc:"Total number of execution phases"`
	MaxParallelism  int     `json:"maxParallelism" yaml:"maxParallelism" doc:"Maximum parallelism across all phases"`
	AvgDependencies float64 `json:"avgDependencies" yaml:"avgDependencies" doc:"Average number of dependencies per resolver"`
}

// BuildGraph creates a Graph from resolvers.
// If lookup is provided, provider-specific ExtractDependencies functions will be used
// when available for more accurate dependency detection.
func BuildGraph(resolvers []*Resolver, lookup DescriptorLookup) (*Graph, error) {
	// Build phases first
	phases, err := BuildPhases(resolvers, lookup)
	if err != nil {
		return nil, fmt.Errorf("build phases: %w", err)
	}

	graph := &Graph{
		Nodes:  make([]*GraphNode, 0, len(resolvers)),
		Edges:  make([]*GraphEdge, 0),
		Phases: make([]*PhaseInfo, 0, len(phases)),
	}

	// Create nodes
	for _, phase := range phases {
		phaseInfo := &PhaseInfo{
			Phase:       phase.Phase,
			Resolvers:   make([]string, 0, len(phase.Resolvers)),
			Parallelism: len(phase.Resolvers),
		}

		for _, r := range phase.Resolvers {
			// Extract dependencies
			deps := extractDependencies(r, lookup)
			graphDeps := make([]GraphDependency, 0, len(deps))

			for _, dep := range deps {
				graphDeps = append(graphDeps, GraphDependency{
					Resolver: dep,
					Field:    dep,
				})

				// Create edge (from dependent to dependency)
				graph.Edges = append(graph.Edges, &GraphEdge{
					From:  r.Name,
					To:    dep,
					Label: dep,
				})
			}

			node := &GraphNode{
				Name:         r.Name,
				Type:         r.Type,
				Phase:        phase.Phase,
				Conditional:  r.When != nil,
				Dependencies: graphDeps,
			}

			graph.Nodes = append(graph.Nodes, node)
			phaseInfo.Resolvers = append(phaseInfo.Resolvers, r.Name)
		}

		graph.Phases = append(graph.Phases, phaseInfo)
	}

	// Calculate stats
	graph.Stats = calculateGraphStats(graph)

	return graph, nil
}

// calculateGraphStats computes graph statistics
func calculateGraphStats(graph *Graph) *GraphStats {
	totalDeps := 0
	maxParallelism := 0

	for _, node := range graph.Nodes {
		totalDeps += len(node.Dependencies)
	}

	for _, phase := range graph.Phases {
		if phase.Parallelism > maxParallelism {
			maxParallelism = phase.Parallelism
		}
	}

	avgDeps := 0.0
	if len(graph.Nodes) > 0 {
		avgDeps = float64(totalDeps) / float64(len(graph.Nodes))
	}

	return &GraphStats{
		TotalResolvers:  len(graph.Nodes),
		TotalPhases:     len(graph.Phases),
		MaxParallelism:  maxParallelism,
		AvgDependencies: avgDeps,
	}
}

// findNode finds a node by name
func (g *Graph) findNode(name string) *GraphNode {
	for _, node := range g.Nodes {
		if node.Name == name {
			return node
		}
	}
	return nil
}

// RenderDOT generates GraphViz DOT format
func (g *Graph) RenderDOT(w io.Writer) error {
	fmt.Fprintln(w, "digraph Resolvers {")
	fmt.Fprintln(w, "  rankdir=LR;")
	fmt.Fprintln(w, "  node [shape=box, style=rounded];")
	fmt.Fprintln(w)

	// Phase subgraphs
	for _, phase := range g.Phases {
		fmt.Fprintf(w, "  subgraph cluster_phase_%d {\n", phase.Phase)
		fmt.Fprintf(w, "    label=\"Phase %d\";\n", phase.Phase)
		fmt.Fprintln(w, "    style=filled;")
		fmt.Fprintln(w, "    color=lightgrey;")
		fmt.Fprintln(w)

		// Nodes in this phase
		for _, resolverName := range phase.Resolvers {
			node := g.findNode(resolverName)
			if node == nil {
				continue
			}

			color := getPhaseColor(phase.Phase)
			style := "rounded,filled"
			if node.Conditional {
				style = "rounded,dashed"
				color = "lightpink"
			}

			fmt.Fprintf(w, "    \"%s\" [fillcolor=%s, style=\"%s\"];\n",
				node.Name, color, style)
		}

		fmt.Fprintln(w, "  }")
		fmt.Fprintln(w)
	}

	// Edges
	fmt.Fprintln(w, "  // Dependencies")
	for _, edge := range g.Edges {
		style := ""
		fromNode := g.findNode(edge.From)
		if fromNode != nil && fromNode.Conditional {
			style = " [style=dashed]"
		}

		fmt.Fprintf(w, "  \"%s\" -> \"%s\" [label=\"%s\"]%s;\n",
			edge.From, edge.To, edge.Label, style)
	}

	fmt.Fprintln(w, "}")
	return nil
}

// getPhaseColor returns a color for a phase number
func getPhaseColor(phase int) string {
	colors := []string{"lightblue", "lightgreen", "lightyellow", "lightcoral", "lightcyan"}
	return colors[phase%len(colors)]
}

// RenderMermaid generates Mermaid diagram format
func (g *Graph) RenderMermaid(w io.Writer) error {
	fmt.Fprintln(w, "graph LR")

	// Phase subgraphs
	for _, phase := range g.Phases {
		fmt.Fprintf(w, "  subgraph Phase_%d[\"Phase %d\"]\n", phase.Phase, phase.Phase)
		for _, resolverName := range phase.Resolvers {
			node := g.findNode(resolverName)
			if node == nil {
				continue
			}

			nodeStyle := resolverName
			if node.Conditional {
				nodeStyle = resolverName + ":::conditional"
			}
			fmt.Fprintf(w, "    %s[%s]\n", nodeStyle, resolverName)
		}
		fmt.Fprintln(w, "  end")
	}

	// Edges
	for _, edge := range g.Edges {
		fromNode := g.findNode(edge.From)
		arrow := "-->"
		if fromNode != nil && fromNode.Conditional {
			arrow = "-.."
		}
		fmt.Fprintf(w, "  %s %s|%s| %s\n", edge.From, arrow, edge.Label, edge.To)
	}

	// Styles
	fmt.Fprintln(w, "  classDef conditional stroke-dasharray: 5 5")
	return nil
}

// RenderASCII generates ASCII art representation
func (g *Graph) RenderASCII(w io.Writer) error {
	fmt.Fprintln(w, "Resolver Dependency Graph:")
	fmt.Fprintln(w)

	for _, phase := range g.Phases {
		fmt.Fprintf(w, "Phase %d:\n", phase.Phase)
		for _, resolverName := range phase.Resolvers {
			node := g.findNode(resolverName)
			if node == nil {
				continue
			}

			conditional := ""
			if node.Conditional {
				conditional = " [conditional]"
			}

			fmt.Fprintf(w, "  - %s%s\n", node.Name, conditional)
			if len(node.Dependencies) > 0 {
				fmt.Fprintln(w, "    depends on:")
				for _, dep := range node.Dependencies {
					fmt.Fprintf(w, "      * %s\n", dep.Resolver)
				}
			}
		}
		fmt.Fprintln(w)
	}

	// Stats
	fmt.Fprintln(w, "Statistics:")
	fmt.Fprintf(w, "  Total Resolvers: %d\n", g.Stats.TotalResolvers)
	fmt.Fprintf(w, "  Total Phases: %d\n", g.Stats.TotalPhases)
	fmt.Fprintf(w, "  Max Parallelism: %d\n", g.Stats.MaxParallelism)
	fmt.Fprintf(w, "  Avg Dependencies: %.2f\n", g.Stats.AvgDependencies)

	return nil
}
