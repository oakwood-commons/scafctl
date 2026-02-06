package action

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildVisualization(t *testing.T) {
	tests := []struct {
		name     string
		graph    *Graph
		validate func(t *testing.T, viz *GraphVisualization)
	}{
		{
			name:  "nil graph returns empty visualization",
			graph: nil,
			validate: func(t *testing.T, viz *GraphVisualization) {
				assert.Empty(t, viz.Phases)
				assert.Empty(t, viz.Edges)
				assert.NotNil(t, viz.Stats)
			},
		},
		{
			name: "single action no dependencies",
			graph: &Graph{
				Actions: map[string]*ExpandedAction{
					"deploy": {ExpandedName: "deploy", Dependencies: nil},
				},
				ExecutionOrder: [][]string{{"deploy"}},
				FinallyOrder:   nil,
			},
			validate: func(t *testing.T, viz *GraphVisualization) {
				require.Len(t, viz.Phases, 1)
				assert.Equal(t, 0, viz.Phases[0].Phase)
				assert.Contains(t, viz.Phases[0].Actions, "deploy")
				assert.Empty(t, viz.Edges)
				assert.Equal(t, 1, viz.Stats.TotalActions)
				assert.Equal(t, 1, viz.Stats.TotalPhases)
				assert.Equal(t, 1, viz.Stats.MaxParallelism)
			},
		},
		{
			name: "multiple phases with dependencies",
			graph: &Graph{
				Actions: map[string]*ExpandedAction{
					"build": {ExpandedName: "build", Dependencies: nil},
					"test":  {ExpandedName: "test", Dependencies: []string{"build"}},
					"deploy": {
						ExpandedName: "deploy",
						Dependencies: []string{"test"},
					},
				},
				ExecutionOrder: [][]string{{"build"}, {"test"}, {"deploy"}},
				FinallyOrder:   nil,
			},
			validate: func(t *testing.T, viz *GraphVisualization) {
				require.Len(t, viz.Phases, 3)
				assert.Equal(t, []string{"build"}, viz.Phases[0].Actions)
				assert.Equal(t, []string{"test"}, viz.Phases[1].Actions)
				assert.Equal(t, []string{"deploy"}, viz.Phases[2].Actions)

				assert.Len(t, viz.Edges, 2)
				assert.Equal(t, 3, viz.Stats.TotalActions)
				assert.Equal(t, 3, viz.Stats.TotalPhases)
			},
		},
		{
			name: "parallel actions in single phase",
			graph: &Graph{
				Actions: map[string]*ExpandedAction{
					"setup":      {ExpandedName: "setup", Dependencies: nil},
					"parallel-a": {ExpandedName: "parallel-a", Dependencies: []string{"setup"}},
					"parallel-b": {ExpandedName: "parallel-b", Dependencies: []string{"setup"}},
					"parallel-c": {ExpandedName: "parallel-c", Dependencies: []string{"setup"}},
				},
				ExecutionOrder: [][]string{
					{"setup"},
					{"parallel-a", "parallel-b", "parallel-c"},
				},
				FinallyOrder: nil,
			},
			validate: func(t *testing.T, viz *GraphVisualization) {
				require.Len(t, viz.Phases, 2)
				assert.Len(t, viz.Phases[1].Actions, 3)
				assert.Equal(t, 3, viz.Stats.MaxParallelism)
			},
		},
		{
			name: "with finally actions",
			graph: &Graph{
				Actions: map[string]*ExpandedAction{
					"main":    {ExpandedName: "main", Dependencies: nil},
					"cleanup": {ExpandedName: "cleanup", Dependencies: nil},
				},
				ExecutionOrder: [][]string{{"main"}},
				FinallyOrder:   [][]string{{"cleanup"}},
			},
			validate: func(t *testing.T, viz *GraphVisualization) {
				require.Len(t, viz.Phases, 1)
				require.Len(t, viz.FinallyPhases, 1)
				assert.Equal(t, "cleanup", viz.FinallyPhases[0].Actions[0])
				assert.True(t, viz.Stats.HasFinally)
				assert.Equal(t, 2, viz.Stats.TotalPhases)
			},
		},
		{
			name: "with forEach actions",
			graph: &Graph{
				Actions: map[string]*ExpandedAction{
					"deploy[env1]": {
						ExpandedName:    "deploy[env1]",
						Dependencies:    nil,
						ForEachMetadata: &ForEachExpansionMetadata{ExpandedFrom: "deploy", Index: 0, Item: "env1"},
					},
					"deploy[env2]": {
						ExpandedName:    "deploy[env2]",
						Dependencies:    nil,
						ForEachMetadata: &ForEachExpansionMetadata{ExpandedFrom: "deploy", Index: 1, Item: "env2"},
					},
				},
				ExecutionOrder: [][]string{{"deploy[env1]", "deploy[env2]"}},
				FinallyOrder:   nil,
			},
			validate: func(t *testing.T, viz *GraphVisualization) {
				assert.Equal(t, 2, viz.Stats.ForEachCount)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			viz := BuildVisualization(tc.graph)
			require.NotNil(t, viz)
			tc.validate(t, viz)
		})
	}
}

func TestGraphVisualization_RenderASCII(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"build":  {ExpandedName: "build", Dependencies: nil},
			"test":   {ExpandedName: "test", Dependencies: []string{"build"}},
			"deploy": {ExpandedName: "deploy", Dependencies: []string{"test"}},
		},
		ExecutionOrder: [][]string{{"build"}, {"test"}, {"deploy"}},
	}

	viz := BuildVisualization(graph)
	var buf bytes.Buffer
	err := viz.RenderASCII(&buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Action Dependency Graph:")
	assert.Contains(t, output, "=== Main Actions ===")
	assert.Contains(t, output, "Phase 0:")
	assert.Contains(t, output, "Phase 1:")
	assert.Contains(t, output, "Phase 2:")
	assert.Contains(t, output, "build")
	assert.Contains(t, output, "test")
	assert.Contains(t, output, "deploy")
	assert.Contains(t, output, "depends on:")
	assert.Contains(t, output, "Statistics:")
	assert.Contains(t, output, "Total Actions: 3")
}

func TestGraphVisualization_RenderASCII_WithFinally(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"main":    {ExpandedName: "main", Dependencies: nil},
			"cleanup": {ExpandedName: "cleanup", Dependencies: nil},
		},
		ExecutionOrder: [][]string{{"main"}},
		FinallyOrder:   [][]string{{"cleanup"}},
	}

	viz := BuildVisualization(graph)
	var buf bytes.Buffer
	err := viz.RenderASCII(&buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "=== Main Actions ===")
	assert.Contains(t, output, "=== Finally Actions ===")
	assert.Contains(t, output, "Has Finally: yes")
}

func TestGraphVisualization_RenderDOT(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"build": {ExpandedName: "build", Dependencies: nil},
			"test":  {ExpandedName: "test", Dependencies: []string{"build"}},
		},
		ExecutionOrder: [][]string{{"build"}, {"test"}},
	}

	viz := BuildVisualization(graph)
	var buf bytes.Buffer
	err := viz.RenderDOT(&buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "digraph Actions {")
	assert.Contains(t, output, "rankdir=LR")
	assert.Contains(t, output, "subgraph cluster_phase_0")
	assert.Contains(t, output, "subgraph cluster_phase_1")
	assert.Contains(t, output, `"build"`)
	assert.Contains(t, output, `"test"`)
	assert.Contains(t, output, `"test" -> "build"`)
	assert.Contains(t, output, "}")
}

func TestGraphVisualization_RenderDOT_WithFinally(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"main":    {ExpandedName: "main", Dependencies: nil},
			"cleanup": {ExpandedName: "cleanup", Dependencies: nil},
		},
		ExecutionOrder: [][]string{{"main"}},
		FinallyOrder:   [][]string{{"cleanup"}},
	}

	viz := BuildVisualization(graph)
	var buf bytes.Buffer
	err := viz.RenderDOT(&buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "subgraph cluster_finally_0")
	assert.Contains(t, output, "lightyellow")
	assert.Contains(t, output, "lightsalmon")
}

func TestGraphVisualization_RenderMermaid(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"build": {ExpandedName: "build", Dependencies: nil},
			"test":  {ExpandedName: "test", Dependencies: []string{"build"}},
		},
		ExecutionOrder: [][]string{{"build"}, {"test"}},
	}

	viz := BuildVisualization(graph)
	var buf bytes.Buffer
	err := viz.RenderMermaid(&buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "graph LR")
	assert.Contains(t, output, `subgraph Phase_0["Phase 0"]`)
	assert.Contains(t, output, `subgraph Phase_1["Phase 1"]`)
	assert.Contains(t, output, "build")
	assert.Contains(t, output, "test")
	assert.Contains(t, output, "test --> build")
}

func TestGraphVisualization_RenderMermaid_WithFinally(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"main":    {ExpandedName: "main", Dependencies: nil},
			"cleanup": {ExpandedName: "cleanup", Dependencies: nil},
		},
		ExecutionOrder: [][]string{{"main"}},
		FinallyOrder:   [][]string{{"cleanup"}},
	}

	viz := BuildVisualization(graph)
	var buf bytes.Buffer
	err := viz.RenderMermaid(&buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, `subgraph Finally_0["Finally Phase 0"]`)
	assert.Contains(t, output, ":::finally")
	assert.Contains(t, output, "classDef finally")
}

func TestGraphVisualization_RenderMermaid_ForEachActions(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"deploy[prod]": {
				ExpandedName:    "deploy[prod]",
				Dependencies:    nil,
				ForEachMetadata: &ForEachExpansionMetadata{ExpandedFrom: "deploy", Index: 0, Item: "prod"},
			},
		},
		ExecutionOrder: [][]string{{"deploy[prod]"}},
	}

	viz := BuildVisualization(graph)
	var buf bytes.Buffer
	err := viz.RenderMermaid(&buf)
	require.NoError(t, err)

	output := buf.String()
	// ForEach actions get double brackets in Mermaid
	assert.Contains(t, output, `[["deploy[prod]"]]`)
}

func TestSanitizeMermaidID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"with-dash", "with-dash"},
		{"with_underscore", "with_underscore"},
		{"deploy[prod]", "deploy_prod"},
		{"action.name", "action_name"},
		{"Mixed123", "Mixed123"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := sanitizeMermaidID(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestIsForEachAction(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"deploy", false},
		{"deploy[prod]", true},
		{"deploy[env1]", true},
		{"action-name", false},
		{"action[a][b]", true},
		{"action[]", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isForEachAction(tc.name)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestGetActionPhaseColor(t *testing.T) {
	// Test color cycling
	colors := []string{"lightblue", "lightgreen", "lightcyan", "lightpink", "lavender"}
	for i := 0; i < 10; i++ {
		color := getActionPhaseColor(i)
		assert.Equal(t, colors[i%5], color)
	}
}

func TestGraphVisualization_GetDependencies(t *testing.T) {
	viz := &GraphVisualization{
		Edges: []*VisualizationEdge{
			{From: "test", To: "build"},
			{From: "deploy", To: "test"},
			{From: "deploy", To: "build"},
		},
	}

	deps := viz.getDependencies("deploy")
	assert.Len(t, deps, 2)
	assert.Contains(t, deps, "test")
	assert.Contains(t, deps, "build")

	// Should be sorted
	assert.Equal(t, "build", deps[0])
	assert.Equal(t, "test", deps[1])
}

func TestGraphVisualization_Stats(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"a": {ExpandedName: "a", Dependencies: nil},
			"b": {ExpandedName: "b", Dependencies: []string{"a"}},
			"c": {ExpandedName: "c", Dependencies: []string{"a", "b"}},
		},
		ExecutionOrder: [][]string{{"a"}, {"b"}, {"c"}},
	}

	viz := BuildVisualization(graph)

	assert.Equal(t, 3, viz.Stats.TotalActions)
	assert.Equal(t, 3, viz.Stats.TotalPhases)
	assert.Equal(t, 1, viz.Stats.MaxParallelism)
	assert.InDelta(t, 1.0, viz.Stats.AvgDependencies, 0.01) // 3 deps / 3 actions
	assert.False(t, viz.Stats.HasFinally)
	assert.Equal(t, 0, viz.Stats.ForEachCount)
}

func TestGraphVisualization_EdgesSorted(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"z": {ExpandedName: "z", Dependencies: []string{"a", "m", "b"}},
			"a": {ExpandedName: "a", Dependencies: nil},
			"m": {ExpandedName: "m", Dependencies: nil},
			"b": {ExpandedName: "b", Dependencies: nil},
		},
		ExecutionOrder: [][]string{{"a", "m", "b"}, {"z"}},
	}

	viz := BuildVisualization(graph)

	// Edges should be sorted by From, then To
	require.Len(t, viz.Edges, 3)

	// All edges should have From="z" and be sorted by To
	for _, edge := range viz.Edges {
		assert.Equal(t, "z", edge.From)
	}

	// The To values should be sorted
	tos := make([]string, len(viz.Edges))
	for i, edge := range viz.Edges {
		tos[i] = edge.To
	}
	assert.Equal(t, []string{"a", "b", "m"}, tos)
}

func TestGraphVisualization_ComplexGraph(t *testing.T) {
	// Test a more complex graph scenario
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"init":           {ExpandedName: "init", Dependencies: nil},
			"build-frontend": {ExpandedName: "build-frontend", Dependencies: []string{"init"}},
			"build-backend":  {ExpandedName: "build-backend", Dependencies: []string{"init"}},
			"test-frontend":  {ExpandedName: "test-frontend", Dependencies: []string{"build-frontend"}},
			"test-backend":   {ExpandedName: "test-backend", Dependencies: []string{"build-backend"}},
			"deploy":         {ExpandedName: "deploy", Dependencies: []string{"test-frontend", "test-backend"}},
			"notify":         {ExpandedName: "notify", Dependencies: nil},
		},
		ExecutionOrder: [][]string{
			{"init"},
			{"build-frontend", "build-backend"},
			{"test-frontend", "test-backend"},
			{"deploy"},
		},
		FinallyOrder: [][]string{{"notify"}},
	}

	viz := BuildVisualization(graph)

	// Verify phases
	assert.Len(t, viz.Phases, 4)
	assert.Len(t, viz.FinallyPhases, 1)

	// Verify max parallelism
	assert.Equal(t, 2, viz.Stats.MaxParallelism)

	// Verify stats
	assert.Equal(t, 7, viz.Stats.TotalActions)
	assert.Equal(t, 5, viz.Stats.TotalPhases) // 4 main + 1 finally
	assert.True(t, viz.Stats.HasFinally)

	// Verify we can render all formats without error
	var buf bytes.Buffer

	err := viz.RenderASCII(&buf)
	assert.NoError(t, err)
	assert.NotEmpty(t, buf.String())

	buf.Reset()
	err = viz.RenderDOT(&buf)
	assert.NoError(t, err)
	assert.True(t, strings.HasPrefix(buf.String(), "digraph"))

	buf.Reset()
	err = viz.RenderMermaid(&buf)
	assert.NoError(t, err)
	assert.True(t, strings.HasPrefix(buf.String(), "graph LR"))
}
