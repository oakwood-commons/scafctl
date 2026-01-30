package resolver

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGraph_RenderDOT(t *testing.T) {
	tests := []struct {
		name      string
		graph     *Graph
		wantErr   bool
		checkFunc func(t *testing.T, output string)
	}{
		{
			name: "simple graph",
			graph: &Graph{
				Nodes: []*GraphNode{
					{
						Name:         "resolver1",
						Type:         TypeString,
						Phase:        1,
						Conditional:  false,
						Dependencies: []GraphDependency{},
					},
				},
				Edges: []*GraphEdge{},
				Phases: []*PhaseInfo{
					{
						Phase:       1,
						Resolvers:   []string{"resolver1"},
						Parallelism: 1,
					},
				},
				Stats: &GraphStats{
					TotalResolvers:  1,
					TotalPhases:     1,
					MaxParallelism:  1,
					AvgDependencies: 0,
				},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, output string) {
				assert.Contains(t, output, "digraph Resolvers")
				assert.Contains(t, output, "resolver1")
				assert.Contains(t, output, "Phase 1")
			},
		},
		{
			name: "graph with dependencies",
			graph: &Graph{
				Nodes: []*GraphNode{
					{
						Name:         "base",
						Type:         TypeString,
						Phase:        1,
						Conditional:  false,
						Dependencies: []GraphDependency{},
					},
					{
						Name:  "dependent",
						Type:  TypeString,
						Phase: 2,
						Dependencies: []GraphDependency{
							{Resolver: "base", Field: "base"},
						},
					},
				},
				Edges: []*GraphEdge{
					{From: "dependent", To: "base", Label: "base"},
				},
				Phases: []*PhaseInfo{
					{Phase: 1, Resolvers: []string{"base"}, Parallelism: 1},
					{Phase: 2, Resolvers: []string{"dependent"}, Parallelism: 1},
				},
				Stats: &GraphStats{
					TotalResolvers:  2,
					TotalPhases:     2,
					MaxParallelism:  1,
					AvgDependencies: 0.5,
				},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, output string) {
				assert.Contains(t, output, "base")
				assert.Contains(t, output, "dependent")
				assert.Contains(t, output, "->")
			},
		},
		{
			name: "conditional resolver",
			graph: &Graph{
				Nodes: []*GraphNode{
					{
						Name:         "conditional",
						Type:         TypeString,
						Phase:        1,
						Conditional:  true,
						Dependencies: []GraphDependency{},
					},
				},
				Edges: []*GraphEdge{},
				Phases: []*PhaseInfo{
					{Phase: 1, Resolvers: []string{"conditional"}, Parallelism: 1},
				},
				Stats: &GraphStats{
					TotalResolvers:  1,
					TotalPhases:     1,
					MaxParallelism:  1,
					AvgDependencies: 0,
				},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, output string) {
				assert.Contains(t, output, "conditional")
				assert.Contains(t, output, "dashed")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := tt.graph.RenderDOT(&buf)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			output := buf.String()
			if tt.checkFunc != nil {
				tt.checkFunc(t, output)
			}
		})
	}
}

func TestGraph_RenderMermaid(t *testing.T) {
	tests := []struct {
		name      string
		graph     *Graph
		wantErr   bool
		checkFunc func(t *testing.T, output string)
	}{
		{
			name: "simple graph",
			graph: &Graph{
				Nodes: []*GraphNode{
					{
						Name:         "resolver1",
						Type:         TypeString,
						Phase:        1,
						Conditional:  false,
						Dependencies: []GraphDependency{},
					},
				},
				Edges: []*GraphEdge{},
				Phases: []*PhaseInfo{
					{Phase: 1, Resolvers: []string{"resolver1"}, Parallelism: 1},
				},
				Stats: &GraphStats{
					TotalResolvers:  1,
					TotalPhases:     1,
					MaxParallelism:  1,
					AvgDependencies: 0,
				},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, output string) {
				assert.Contains(t, output, "graph LR")
				assert.Contains(t, output, "resolver1")
				assert.Contains(t, output, "Phase_1")
			},
		},
		{
			name: "graph with dependencies",
			graph: &Graph{
				Nodes: []*GraphNode{
					{
						Name:         "base",
						Type:         TypeString,
						Phase:        1,
						Conditional:  false,
						Dependencies: []GraphDependency{},
					},
					{
						Name:  "dependent",
						Type:  TypeString,
						Phase: 2,
						Dependencies: []GraphDependency{
							{Resolver: "base", Field: "base"},
						},
					},
				},
				Edges: []*GraphEdge{
					{From: "dependent", To: "base", Label: "base"},
				},
				Phases: []*PhaseInfo{
					{Phase: 1, Resolvers: []string{"base"}, Parallelism: 1},
					{Phase: 2, Resolvers: []string{"dependent"}, Parallelism: 1},
				},
				Stats: &GraphStats{
					TotalResolvers:  2,
					TotalPhases:     2,
					MaxParallelism:  1,
					AvgDependencies: 0.5,
				},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, output string) {
				assert.Contains(t, output, "base")
				assert.Contains(t, output, "dependent")
				assert.Contains(t, output, "-->")
			},
		},
		{
			name: "conditional resolver",
			graph: &Graph{
				Nodes: []*GraphNode{
					{
						Name:         "enabled",
						Type:         TypeBool,
						Phase:        1,
						Conditional:  false,
						Dependencies: []GraphDependency{},
					},
					{
						Name:        "conditional",
						Type:        TypeString,
						Phase:       2,
						Conditional: true,
						Dependencies: []GraphDependency{
							{Resolver: "enabled", Field: "enabled"},
						},
					},
				},
				Edges: []*GraphEdge{
					{From: "conditional", To: "enabled", Label: "enabled"},
				},
				Phases: []*PhaseInfo{
					{Phase: 1, Resolvers: []string{"enabled"}, Parallelism: 1},
					{Phase: 2, Resolvers: []string{"conditional"}, Parallelism: 1},
				},
				Stats: &GraphStats{
					TotalResolvers:  2,
					TotalPhases:     2,
					MaxParallelism:  1,
					AvgDependencies: 0.5,
				},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, output string) {
				assert.Contains(t, output, "conditional")
				assert.Contains(t, output, "-..")
				assert.Contains(t, output, "classDef conditional")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := tt.graph.RenderMermaid(&buf)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			output := buf.String()
			if tt.checkFunc != nil {
				tt.checkFunc(t, output)
			}
		})
	}
}

func TestGraph_RenderASCII(t *testing.T) {
	tests := []struct {
		name      string
		graph     *Graph
		wantErr   bool
		checkFunc func(t *testing.T, output string)
	}{
		{
			name: "simple graph",
			graph: &Graph{
				Nodes: []*GraphNode{
					{
						Name:         "resolver1",
						Type:         TypeString,
						Phase:        1,
						Conditional:  false,
						Dependencies: []GraphDependency{},
					},
				},
				Edges: []*GraphEdge{},
				Phases: []*PhaseInfo{
					{Phase: 1, Resolvers: []string{"resolver1"}, Parallelism: 1},
				},
				Stats: &GraphStats{
					TotalResolvers:  1,
					TotalPhases:     1,
					MaxParallelism:  1,
					AvgDependencies: 0,
				},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, output string) {
				assert.Contains(t, output, "Resolver Dependency Graph")
				assert.Contains(t, output, "resolver1")
				assert.Contains(t, output, "Phase 1")
				assert.Contains(t, output, "Statistics:")
			},
		},
		{
			name: "graph with dependencies",
			graph: &Graph{
				Nodes: []*GraphNode{
					{
						Name:         "base",
						Type:         TypeString,
						Phase:        1,
						Conditional:  false,
						Dependencies: []GraphDependency{},
					},
					{
						Name:  "dependent",
						Type:  TypeString,
						Phase: 2,
						Dependencies: []GraphDependency{
							{Resolver: "base", Field: "base"},
						},
					},
				},
				Edges: []*GraphEdge{
					{From: "dependent", To: "base", Label: "base"},
				},
				Phases: []*PhaseInfo{
					{Phase: 1, Resolvers: []string{"base"}, Parallelism: 1},
					{Phase: 2, Resolvers: []string{"dependent"}, Parallelism: 1},
				},
				Stats: &GraphStats{
					TotalResolvers:  2,
					TotalPhases:     2,
					MaxParallelism:  1,
					AvgDependencies: 0.5,
				},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, output string) {
				assert.Contains(t, output, "base")
				assert.Contains(t, output, "dependent")
				assert.Contains(t, output, "depends on")
				assert.Contains(t, output, "Total Resolvers: 2")
				assert.Contains(t, output, "Total Phases: 2")
			},
		},
		{
			name: "conditional resolver",
			graph: &Graph{
				Nodes: []*GraphNode{
					{
						Name:         "conditional",
						Type:         TypeString,
						Phase:        1,
						Conditional:  true,
						Dependencies: []GraphDependency{},
					},
				},
				Edges: []*GraphEdge{},
				Phases: []*PhaseInfo{
					{Phase: 1, Resolvers: []string{"conditional"}, Parallelism: 1},
				},
				Stats: &GraphStats{
					TotalResolvers:  1,
					TotalPhases:     1,
					MaxParallelism:  1,
					AvgDependencies: 0,
				},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, output string) {
				assert.Contains(t, output, "conditional")
				assert.Contains(t, output, "[conditional]")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := tt.graph.RenderASCII(&buf)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			output := buf.String()
			if tt.checkFunc != nil {
				tt.checkFunc(t, output)
			}
		})
	}
}

func TestGetPhaseColor(t *testing.T) {
	tests := []struct {
		name  string
		phase int
		want  string
	}{
		{
			name:  "phase 0",
			phase: 0,
			want:  "lightblue",
		},
		{
			name:  "phase 1",
			phase: 1,
			want:  "lightgreen",
		},
		{
			name:  "phase 5 (wraps around)",
			phase: 5,
			want:  "lightblue",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getPhaseColor(tt.phase)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractDepsFromTemplate_InvalidTemplate(t *testing.T) {
	deps := make(map[string]bool)
	// Test with invalid template syntax
	extractDepsFromTemplate("{{ ._.invalid syntax", deps)
	// Should not panic and should result in empty deps
	assert.Empty(t, deps)
}

func TestExtractDepsFromTemplate_NestedAccess(t *testing.T) {
	deps := make(map[string]bool)
	// Test with nested field access - should only extract first segment
	extractDepsFromTemplate("{{ ._.config.nested.field }}", deps)
	assert.Contains(t, deps, "config")
	assert.NotContains(t, deps, "nested")
	assert.NotContains(t, deps, "field")
}

func TestExtractDepsFromResolvePhase_NilPhase(t *testing.T) {
	deps := make(map[string]bool)
	extractDepsFromResolvePhase(nil, deps, nil)
	assert.Empty(t, deps)
}

func TestExtractDepsFromTransformPhase_NilPhase(t *testing.T) {
	deps := make(map[string]bool)
	extractDepsFromTransformPhase(nil, deps, nil)
	assert.Empty(t, deps)
}

func TestExtractDepsFromTransformPhase_WithWhen(t *testing.T) {
	deps := make(map[string]bool)
	phase := &TransformPhase{
		When: &Condition{
			Expr: celExpPtr("_.enabled == true"),
		},
		With: []ProviderTransform{
			{
				Provider: "cel",
				When: &Condition{
					Expr: celExpPtr("_.transform_enabled == true"),
				},
				Inputs: map[string]*ValueRef{
					"value": {Resolver: stringPtr("base")},
				},
			},
		},
	}
	extractDepsFromTransformPhase(phase, deps, nil)
	assert.Contains(t, deps, "enabled")
	assert.Contains(t, deps, "transform_enabled")
	assert.Contains(t, deps, "base")
}

func TestExtractDepsFromValidatePhase_NilPhase(t *testing.T) {
	deps := make(map[string]bool)
	extractDepsFromValidatePhase(nil, deps, nil)
	assert.Empty(t, deps)
}

func TestExtractDepsFromValidatePhase_WithWhen(t *testing.T) {
	deps := make(map[string]bool)
	phase := &ValidatePhase{
		When: &Condition{
			Expr: celExpPtr("_.validate_enabled == true"),
		},
		With: []ProviderValidation{
			{
				Provider: "validation",
				Inputs: map[string]*ValueRef{
					"rule": {Expr: celExpPtr("__self != _.invalid")},
				},
			},
		},
	}
	extractDepsFromValidatePhase(phase, deps, nil)
	assert.Contains(t, deps, "validate_enabled")
	assert.Contains(t, deps, "invalid")
}

func TestGraph_FindNode_NotFound(t *testing.T) {
	graph := &Graph{
		Nodes: []*GraphNode{
			{Name: "resolver1"},
		},
	}

	node := graph.findNode("nonexistent")
	assert.Nil(t, node)
}

func TestBuildGraph_ErrorInBuildPhases(t *testing.T) {
	// Create resolvers with circular dependency
	resolvers := []*Resolver{
		{
			Name: "a",
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "cel",
						Inputs: map[string]*ValueRef{
							"value": {Resolver: stringPtr("b")},
						},
					},
				},
			},
		},
		{
			Name: "b",
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "cel",
						Inputs: map[string]*ValueRef{
							"value": {Resolver: stringPtr("a")},
						},
					},
				},
			},
		},
	}

	graph, err := BuildGraph(resolvers, nil)
	assert.Error(t, err)
	assert.Nil(t, graph)
	assert.Contains(t, err.Error(), "build phases")
}

func TestGraph_RenderDOT_WithEdges(t *testing.T) {
	// More comprehensive test with edges and dependencies
	graph := &Graph{
		Nodes: []*GraphNode{
			{
				Name:         "base",
				Type:         TypeString,
				Phase:        1,
				Conditional:  false,
				Dependencies: []GraphDependency{},
			},
			{
				Name:  "transform",
				Type:  TypeString,
				Phase: 2,
				Dependencies: []GraphDependency{
					{Resolver: "base", Field: "base"},
				},
			},
			{
				Name:  "output",
				Type:  TypeString,
				Phase: 3,
				Dependencies: []GraphDependency{
					{Resolver: "transform", Field: "transform"},
				},
			},
		},
		Edges: []*GraphEdge{
			{From: "transform", To: "base", Label: "base"},
			{From: "output", To: "transform", Label: "transform"},
		},
		Phases: []*PhaseInfo{
			{Phase: 1, Resolvers: []string{"base"}, Parallelism: 1},
			{Phase: 2, Resolvers: []string{"transform"}, Parallelism: 1},
			{Phase: 3, Resolvers: []string{"output"}, Parallelism: 1},
		},
		Stats: &GraphStats{
			TotalResolvers:  3,
			TotalPhases:     3,
			MaxParallelism:  1,
			AvgDependencies: 0.67,
		},
	}

	var buf bytes.Buffer
	err := graph.RenderDOT(&buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "base")
	assert.Contains(t, output, "transform")
	assert.Contains(t, output, "output")
	assert.Contains(t, output, "->")
}

func TestGraph_RenderMermaid_WithConditionals(t *testing.T) {
	// Test with multiple conditional resolvers
	graph := &Graph{
		Nodes: []*GraphNode{
			{
				Name:         "config",
				Type:         TypeAny,
				Phase:        1,
				Conditional:  false,
				Dependencies: []GraphDependency{},
			},
			{
				Name:        "feature1",
				Type:        TypeString,
				Phase:       2,
				Conditional: true,
				Dependencies: []GraphDependency{
					{Resolver: "config", Field: "config"},
				},
			},
			{
				Name:        "feature2",
				Type:        TypeString,
				Phase:       2,
				Conditional: true,
				Dependencies: []GraphDependency{
					{Resolver: "config", Field: "config"},
				},
			},
		},
		Edges: []*GraphEdge{
			{From: "feature1", To: "config", Label: "config"},
			{From: "feature2", To: "config", Label: "config"},
		},
		Phases: []*PhaseInfo{
			{Phase: 1, Resolvers: []string{"config"}, Parallelism: 1},
			{Phase: 2, Resolvers: []string{"feature1", "feature2"}, Parallelism: 2},
		},
		Stats: &GraphStats{
			TotalResolvers:  3,
			TotalPhases:     2,
			MaxParallelism:  2,
			AvgDependencies: 0.67,
		},
	}

	var buf bytes.Buffer
	err := graph.RenderMermaid(&buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "feature1")
	assert.Contains(t, output, "feature2")
	assert.Contains(t, output, "-..")
	assert.Contains(t, output, "classDef conditional")
}

func TestGraph_RenderASCII_ComplexGraph(t *testing.T) {
	// Test ASCII rendering with multiple phases and dependencies
	graph := &Graph{
		Nodes: []*GraphNode{
			{
				Name:         "input1",
				Type:         TypeString,
				Phase:        1,
				Conditional:  false,
				Dependencies: []GraphDependency{},
			},
			{
				Name:         "input2",
				Type:         TypeString,
				Phase:        1,
				Conditional:  false,
				Dependencies: []GraphDependency{},
			},
			{
				Name:  "processor",
				Type:  TypeString,
				Phase: 2,
				Dependencies: []GraphDependency{
					{Resolver: "input1", Field: "input1"},
					{Resolver: "input2", Field: "input2"},
				},
			},
		},
		Edges: []*GraphEdge{
			{From: "processor", To: "input1", Label: "input1"},
			{From: "processor", To: "input2", Label: "input2"},
		},
		Phases: []*PhaseInfo{
			{Phase: 1, Resolvers: []string{"input1", "input2"}, Parallelism: 2},
			{Phase: 2, Resolvers: []string{"processor"}, Parallelism: 1},
		},
		Stats: &GraphStats{
			TotalResolvers:  3,
			TotalPhases:     2,
			MaxParallelism:  2,
			AvgDependencies: 0.67,
		},
	}

	var buf bytes.Buffer
	err := graph.RenderASCII(&buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "input1")
	assert.Contains(t, output, "input2")
	assert.Contains(t, output, "processor")
	assert.Contains(t, output, "depends on")
	assert.Contains(t, output, "Max Parallelism: 2")
}
