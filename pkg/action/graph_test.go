// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helper to create a CEL expression for graph tests
func graphCelExpr(s string) *celexp.Expression {
	e := celexp.Expression(s)
	return &e
}

// helper to create a resolver reference
func resolverRef(name string) *string {
	return &name
}

// helper to create a ValueRef with literal
func literalRef(v any) *spec.ValueRef {
	return &spec.ValueRef{Literal: v}
}

// helper to create a ValueRef with expression for graph tests
func graphExprRef(e string) *spec.ValueRef {
	return &spec.ValueRef{Expr: graphCelExpr(e)}
}

// helper to create a ValueRef with resolver reference
func rslvrRef(name string) *spec.ValueRef {
	return &spec.ValueRef{Resolver: resolverRef(name)}
}

func TestBuildGraph_NilWorkflow(t *testing.T) {
	ctx := context.Background()
	graph, err := BuildGraph(ctx, nil, nil, nil)
	require.Error(t, err)
	assert.Nil(t, graph)
	assert.Contains(t, err.Error(), "workflow cannot be nil")
}

func TestBuildGraph_EmptyWorkflow(t *testing.T) {
	ctx := context.Background()
	w := &Workflow{}

	graph, err := BuildGraph(ctx, w, nil, nil)
	require.NoError(t, err)
	assert.NotNil(t, graph)
	assert.Empty(t, graph.Actions)
	assert.Nil(t, graph.ExecutionOrder)
	assert.Nil(t, graph.FinallyOrder)
}

func TestBuildGraph_SingleAction(t *testing.T) {
	ctx := context.Background()
	w := &Workflow{
		Actions: map[string]*Action{
			"deploy": {
				Provider: "shell",
				Inputs: map[string]*spec.ValueRef{
					"command": literalRef("echo hello"),
				},
			},
		},
	}

	graph, err := BuildGraph(ctx, w, nil, nil)
	require.NoError(t, err)
	assert.NotNil(t, graph)

	// Should have one action
	assert.Len(t, graph.Actions, 1)
	assert.Contains(t, graph.Actions, "deploy")

	// Should have one phase with one action
	require.Len(t, graph.ExecutionOrder, 1)
	assert.Equal(t, []string{"deploy"}, graph.ExecutionOrder[0])

	// Action should have materialized input
	action := graph.Actions["deploy"]
	assert.Equal(t, "actions", action.Section)
	assert.Nil(t, action.ForEachMetadata)
	assert.Equal(t, "echo hello", action.MaterializedInputs["command"])
	assert.Empty(t, action.DeferredInputs)
	assert.Empty(t, action.Dependencies)
}

func TestBuildGraph_ActionDependencies(t *testing.T) {
	ctx := context.Background()
	w := &Workflow{
		Actions: map[string]*Action{
			"build": {
				Provider: "shell",
				Inputs: map[string]*spec.ValueRef{
					"command": literalRef("go build"),
				},
			},
			"test": {
				Provider:  "shell",
				DependsOn: []string{"build"},
				Inputs: map[string]*spec.ValueRef{
					"command": literalRef("go test"),
				},
			},
			"deploy": {
				Provider:  "shell",
				DependsOn: []string{"test"},
				Inputs: map[string]*spec.ValueRef{
					"command": literalRef("kubectl apply"),
				},
			},
		},
	}

	graph, err := BuildGraph(ctx, w, nil, nil)
	require.NoError(t, err)

	// Should have three phases
	require.Len(t, graph.ExecutionOrder, 3)
	assert.Equal(t, []string{"build"}, graph.ExecutionOrder[0])
	assert.Equal(t, []string{"test"}, graph.ExecutionOrder[1])
	assert.Equal(t, []string{"deploy"}, graph.ExecutionOrder[2])

	// Check dependencies
	assert.Empty(t, graph.Actions["build"].Dependencies)
	assert.Equal(t, []string{"build"}, graph.Actions["test"].Dependencies)
	assert.Equal(t, []string{"test"}, graph.Actions["deploy"].Dependencies)
}

func TestBuildGraph_ParallelActions(t *testing.T) {
	ctx := context.Background()
	w := &Workflow{
		Actions: map[string]*Action{
			"task_a": {Provider: "shell"},
			"task_b": {Provider: "shell"},
			"task_c": {Provider: "shell"},
		},
	}

	graph, err := BuildGraph(ctx, w, nil, nil)
	require.NoError(t, err)

	// All actions should be in one phase (no dependencies)
	require.Len(t, graph.ExecutionOrder, 1)
	assert.Len(t, graph.ExecutionOrder[0], 3)

	// Should be sorted alphabetically
	assert.Equal(t, []string{"task_a", "task_b", "task_c"}, graph.ExecutionOrder[0])
}

func TestBuildGraph_DiamondDependencies(t *testing.T) {
	ctx := context.Background()
	w := &Workflow{
		Actions: map[string]*Action{
			"start": {Provider: "shell"},
			"path_a": {
				Provider:  "shell",
				DependsOn: []string{"start"},
			},
			"path_b": {
				Provider:  "shell",
				DependsOn: []string{"start"},
			},
			"end": {
				Provider:  "shell",
				DependsOn: []string{"path_a", "path_b"},
			},
		},
	}

	graph, err := BuildGraph(ctx, w, nil, nil)
	require.NoError(t, err)

	// Should have three phases
	require.Len(t, graph.ExecutionOrder, 3)
	assert.Equal(t, []string{"start"}, graph.ExecutionOrder[0])
	assert.ElementsMatch(t, []string{"path_a", "path_b"}, graph.ExecutionOrder[1])
	assert.Equal(t, []string{"end"}, graph.ExecutionOrder[2])
}

func TestBuildGraph_WithResolverData(t *testing.T) {
	ctx := context.Background()
	resolverData := map[string]any{
		"environment": "production",
		"regions":     []any{"us-east", "eu-west"},
	}

	w := &Workflow{
		Actions: map[string]*Action{
			"deploy": {
				Provider: "shell",
				Inputs: map[string]*spec.ValueRef{
					"env":     rslvrRef("environment"),
					"message": graphExprRef("'Deploying to ' + _.environment"),
				},
			},
		},
	}

	graph, err := BuildGraph(ctx, w, resolverData, nil)
	require.NoError(t, err)

	action := graph.Actions["deploy"]
	assert.Equal(t, "production", action.MaterializedInputs["env"])
	assert.Equal(t, "Deploying to production", action.MaterializedInputs["message"])
}

func TestBuildGraph_DeferredInputs(t *testing.T) {
	ctx := context.Background()
	w := &Workflow{
		Actions: map[string]*Action{
			"build": {
				Provider: "shell",
				Inputs: map[string]*spec.ValueRef{
					"command": literalRef("go build -o app"),
				},
			},
			"deploy": {
				Provider:  "shell",
				DependsOn: []string{"build"},
				Inputs: map[string]*spec.ValueRef{
					"artifact": graphExprRef("__actions.build.results.artifact"),
					"env":      literalRef("production"),
				},
			},
		},
	}

	graph, err := BuildGraph(ctx, w, nil, nil)
	require.NoError(t, err)

	// build should have only materialized inputs
	buildAction := graph.Actions["build"]
	assert.NotEmpty(t, buildAction.MaterializedInputs)
	assert.Empty(t, buildAction.DeferredInputs)

	// deploy should have one deferred and one materialized input
	deployAction := graph.Actions["deploy"]
	assert.Equal(t, "production", deployAction.MaterializedInputs["env"])

	require.Contains(t, deployAction.DeferredInputs, "artifact")
	deferredArtifact := deployAction.DeferredInputs["artifact"]
	assert.True(t, deferredArtifact.IsDeferred())
	assert.Equal(t, "__actions.build.results.artifact", deferredArtifact.OriginalExpr)

	// Implicit dependency on build should be added
	assert.Contains(t, deployAction.Dependencies, "build")
}

func TestBuildGraph_ImplicitDependencies(t *testing.T) {
	ctx := context.Background()
	w := &Workflow{
		Actions: map[string]*Action{
			"task_a": {
				Provider: "shell",
			},
			"task_b": {
				Provider: "shell",
				Inputs: map[string]*spec.ValueRef{
					// References task_a but no explicit dependsOn
					"result": graphExprRef("__actions.task_a.results.output"),
				},
			},
		},
	}

	graph, err := BuildGraph(ctx, w, nil, nil)
	require.NoError(t, err)

	// task_b should have implicit dependency on task_a
	taskB := graph.Actions["task_b"]
	assert.Contains(t, taskB.Dependencies, "task_a")

	// Should have two phases
	require.Len(t, graph.ExecutionOrder, 2)
	assert.Equal(t, []string{"task_a"}, graph.ExecutionOrder[0])
	assert.Equal(t, []string{"task_b"}, graph.ExecutionOrder[1])
}

func TestBuildGraph_ForEachExpansion(t *testing.T) {
	ctx := context.Background()
	resolverData := map[string]any{
		"regions": []any{"us-east", "eu-west", "ap-south"},
	}

	w := &Workflow{
		Actions: map[string]*Action{
			"deploy": {
				Provider: "shell",
				ForEach: &spec.ForEachClause{
					Item: "region",
					In:   rslvrRef("regions"),
				},
				Inputs: map[string]*spec.ValueRef{
					"region": graphExprRef("region"),
				},
			},
		},
	}

	graph, err := BuildGraph(ctx, w, resolverData, nil)
	require.NoError(t, err)

	// Should have 3 expanded actions
	assert.Len(t, graph.Actions, 3)
	assert.Contains(t, graph.Actions, "deploy[0]")
	assert.Contains(t, graph.Actions, "deploy[1]")
	assert.Contains(t, graph.Actions, "deploy[2]")

	// All should be in the same phase (no dependencies)
	require.Len(t, graph.ExecutionOrder, 1)
	assert.ElementsMatch(t, []string{"deploy[0]", "deploy[1]", "deploy[2]"}, graph.ExecutionOrder[0])

	// Check metadata
	for i, regionName := range []string{"us-east", "eu-west", "ap-south"} {
		name := "deploy[" + string(rune('0'+i)) + "]"
		action := graph.Actions[name]
		require.NotNil(t, action.ForEachMetadata)
		assert.Equal(t, "deploy", action.ForEachMetadata.ExpandedFrom)
		assert.Equal(t, i, action.ForEachMetadata.Index)
		assert.Equal(t, regionName, action.ForEachMetadata.Item)
		assert.Equal(t, regionName, action.MaterializedInputs["region"])
	}
}

func TestBuildGraph_ForEachWithDependency(t *testing.T) {
	ctx := context.Background()
	resolverData := map[string]any{
		"regions": []any{"us-east", "eu-west"},
	}

	w := &Workflow{
		Actions: map[string]*Action{
			"build": {
				Provider: "shell",
				Inputs: map[string]*spec.ValueRef{
					"command": literalRef("go build"),
				},
			},
			"deploy": {
				Provider:  "shell",
				DependsOn: []string{"build"},
				ForEach: &spec.ForEachClause{
					Item: "region",
					In:   rslvrRef("regions"),
				},
				Inputs: map[string]*spec.ValueRef{
					"region": graphExprRef("region"),
				},
			},
		},
	}

	graph, err := BuildGraph(ctx, w, resolverData, nil)
	require.NoError(t, err)

	// Should have 3 actions: build + 2 deploy iterations
	assert.Len(t, graph.Actions, 3)

	// build should be phase 0, both deploy iterations should be phase 1
	require.Len(t, graph.ExecutionOrder, 2)
	assert.Equal(t, []string{"build"}, graph.ExecutionOrder[0])
	assert.ElementsMatch(t, []string{"deploy[0]", "deploy[1]"}, graph.ExecutionOrder[1])

	// Both deploy iterations should depend on build
	assert.Equal(t, []string{"build"}, graph.Actions["deploy[0]"].Dependencies)
	assert.Equal(t, []string{"build"}, graph.Actions["deploy[1]"].Dependencies)
}

func TestBuildGraph_DependOnForEach(t *testing.T) {
	ctx := context.Background()
	resolverData := map[string]any{
		"regions": []any{"us-east", "eu-west"},
	}

	w := &Workflow{
		Actions: map[string]*Action{
			"deploy": {
				Provider: "shell",
				ForEach: &spec.ForEachClause{
					Item: "region",
					In:   rslvrRef("regions"),
				},
			},
			"notify": {
				Provider:  "shell",
				DependsOn: []string{"deploy"},
				Inputs: map[string]*spec.ValueRef{
					"message": literalRef("All deployments complete"),
				},
			},
		},
	}

	graph, err := BuildGraph(ctx, w, resolverData, nil)
	require.NoError(t, err)

	// notify should depend on all deploy iterations
	assert.ElementsMatch(t, []string{"deploy[0]", "deploy[1]"}, graph.Actions["notify"].Dependencies)

	// deploy iterations should be phase 0, notify should be phase 1
	require.Len(t, graph.ExecutionOrder, 2)
	assert.ElementsMatch(t, []string{"deploy[0]", "deploy[1]"}, graph.ExecutionOrder[0])
	assert.Equal(t, []string{"notify"}, graph.ExecutionOrder[1])
}

func TestBuildGraph_FinallySection(t *testing.T) {
	ctx := context.Background()
	w := &Workflow{
		Actions: map[string]*Action{
			"deploy": {Provider: "shell"},
		},
		Finally: map[string]*Action{
			"cleanup": {Provider: "shell"},
		},
	}

	graph, err := BuildGraph(ctx, w, nil, nil)
	require.NoError(t, err)

	// Should have 2 actions total
	assert.Len(t, graph.Actions, 2)
	assert.Equal(t, "actions", graph.Actions["deploy"].Section)
	assert.Equal(t, "finally", graph.Actions["cleanup"].Section)

	// Execution orders should be separate
	require.Len(t, graph.ExecutionOrder, 1)
	assert.Equal(t, []string{"deploy"}, graph.ExecutionOrder[0])

	require.Len(t, graph.FinallyOrder, 1)
	assert.Equal(t, []string{"cleanup"}, graph.FinallyOrder[0])
}

func TestBuildGraph_FinallyWithDependencies(t *testing.T) {
	ctx := context.Background()
	w := &Workflow{
		Finally: map[string]*Action{
			"cleanup_a": {Provider: "shell"},
			"cleanup_b": {
				Provider:  "shell",
				DependsOn: []string{"cleanup_a"},
			},
		},
	}

	graph, err := BuildGraph(ctx, w, nil, nil)
	require.NoError(t, err)

	require.Len(t, graph.FinallyOrder, 2)
	assert.Equal(t, []string{"cleanup_a"}, graph.FinallyOrder[0])
	assert.Equal(t, []string{"cleanup_b"}, graph.FinallyOrder[1])
}

func TestBuildGraph_SkipInputMaterialization(t *testing.T) {
	ctx := context.Background()
	w := &Workflow{
		Actions: map[string]*Action{
			"deploy": {
				Provider: "shell",
				Inputs: map[string]*spec.ValueRef{
					"command": literalRef("echo hello"),
				},
			},
		},
	}

	opts := &BuildGraphOptions{SkipInputMaterialization: true}
	graph, err := BuildGraph(ctx, w, nil, opts)
	require.NoError(t, err)

	// Inputs should not be materialized
	action := graph.Actions["deploy"]
	assert.Nil(t, action.MaterializedInputs)
	assert.Nil(t, action.DeferredInputs)
}

func TestBuildGraph_ComplexForEach(t *testing.T) {
	ctx := context.Background()
	resolverData := map[string]any{
		"configs": []any{
			map[string]any{"name": "web", "port": 8080},
			map[string]any{"name": "api", "port": 9090},
		},
	}

	w := &Workflow{
		Actions: map[string]*Action{
			"deploy": {
				Provider: "shell",
				ForEach: &spec.ForEachClause{
					Item:  "config",
					Index: "i",
					In:    rslvrRef("configs"),
				},
				Inputs: map[string]*spec.ValueRef{
					"name":  graphExprRef("config.name"),
					"port":  graphExprRef("config.port"),
					"index": graphExprRef("i"),
				},
			},
		},
	}

	graph, err := BuildGraph(ctx, w, resolverData, nil)
	require.NoError(t, err)

	assert.Len(t, graph.Actions, 2)

	// Check first iteration
	deploy0 := graph.Actions["deploy[0]"]
	assert.Equal(t, "web", deploy0.MaterializedInputs["name"])
	assert.Equal(t, int64(8080), deploy0.MaterializedInputs["port"])
	assert.Equal(t, int64(0), deploy0.MaterializedInputs["index"])

	// Check second iteration
	deploy1 := graph.Actions["deploy[1]"]
	assert.Equal(t, "api", deploy1.MaterializedInputs["name"])
	assert.Equal(t, int64(9090), deploy1.MaterializedInputs["port"])
	assert.Equal(t, int64(1), deploy1.MaterializedInputs["index"])
}

func TestBuildGraph_ForEachWithActionsReference(t *testing.T) {
	ctx := context.Background()
	resolverData := map[string]any{
		"regions": []any{"us-east", "eu-west"},
	}

	w := &Workflow{
		Actions: map[string]*Action{
			"build": {
				Provider: "shell",
			},
			"deploy": {
				Provider:  "shell",
				DependsOn: []string{"build"},
				ForEach: &spec.ForEachClause{
					Item: "region",
					In:   rslvrRef("regions"),
				},
				Inputs: map[string]*spec.ValueRef{
					"region":   graphExprRef("region"),
					"artifact": graphExprRef("__actions.build.results.artifact"),
				},
			},
		},
	}

	graph, err := BuildGraph(ctx, w, resolverData, nil)
	require.NoError(t, err)

	// Each deploy iteration should have deferred input for artifact
	for _, name := range []string{"deploy[0]", "deploy[1]"} {
		action := graph.Actions[name]
		assert.Contains(t, action.DeferredInputs, "artifact")
		assert.Contains(t, action.Dependencies, "build")
	}
}

func TestActionGraph_Helpers(t *testing.T) {
	ctx := context.Background()
	resolverData := map[string]any{
		"regions": []any{"us-east", "eu-west"},
	}

	w := &Workflow{
		Actions: map[string]*Action{
			"build": {Provider: "shell"},
			"deploy": {
				Provider: "shell",
				ForEach: &spec.ForEachClause{
					In: rslvrRef("regions"),
				},
			},
		},
		Finally: map[string]*Action{
			"cleanup": {Provider: "shell"},
		},
	}

	graph, err := BuildGraph(ctx, w, resolverData, nil)
	require.NoError(t, err)

	// Test GetAllActionNames
	allNames := graph.GetAllActionNames()
	assert.ElementsMatch(t, []string{"build", "cleanup", "deploy[0]", "deploy[1]"}, allNames)

	// Test GetMainActionNames
	mainNames := graph.GetMainActionNames()
	assert.ElementsMatch(t, []string{"build", "deploy[0]", "deploy[1]"}, mainNames)

	// Test GetFinallyActionNames
	finallyNames := graph.GetFinallyActionNames()
	assert.Equal(t, []string{"cleanup"}, finallyNames)

	// Test GetForEachIterations
	deployIterations := graph.GetForEachIterations("deploy")
	assert.ElementsMatch(t, []string{"deploy[0]", "deploy[1]"}, deployIterations)

	// Test TotalPhases
	assert.Equal(t, 1, graph.TotalPhases())
	assert.Equal(t, 1, graph.TotalFinallyPhases())

	// Test GetActionsByPhase
	phase0 := graph.GetActionsByPhase(0)
	assert.Len(t, phase0, 3)

	// Test out of bounds
	assert.Nil(t, graph.GetActionsByPhase(5))
}

func TestExpandedAction_Helpers(t *testing.T) {
	// Test non-forEach action
	regular := &ExpandedAction{
		Action:       &Action{Name: "deploy"},
		ExpandedName: "deploy",
		MaterializedInputs: map[string]any{
			"env": "production",
		},
	}
	assert.False(t, regular.HasDeferredInputs())
	assert.False(t, regular.IsForEachIteration())
	assert.Equal(t, "deploy", regular.GetOriginalName())
	assert.Equal(t, "deploy", regular.GetExpandedName())

	// Test forEach action
	forEach := &ExpandedAction{
		Action:       &Action{Name: "deploy"},
		ExpandedName: "deploy[0]",
		DeferredInputs: map[string]*DeferredValue{
			"artifact": {OriginalExpr: "__actions.build.results", Deferred: true},
		},
		ForEachMetadata: &ForEachExpansionMetadata{
			ExpandedFrom: "deploy",
			Index:        0,
			Item:         "us-east",
		},
	}
	assert.True(t, forEach.HasDeferredInputs())
	assert.True(t, forEach.IsForEachIteration())
	assert.Equal(t, "deploy", forEach.GetOriginalName())
	assert.Equal(t, "deploy[0]", forEach.GetExpandedName())
}

func TestParseActionsRefsForGraph(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "dot notation",
			input:    "__actions.build.results.artifact",
			expected: []string{"build"},
		},
		{
			name:     "bracket notation double quotes",
			input:    `__actions["deploy"].results`,
			expected: []string{"deploy"},
		},
		{
			name:     "bracket notation single quotes",
			input:    `__actions['test'].results`,
			expected: []string{"test"},
		},
		{
			name:     "forEach expanded name",
			input:    `__actions["deploy[0]"].results`,
			expected: []string{"deploy[0]"},
		},
		{
			name:     "multiple references",
			input:    `__actions.build.results + __actions.test.output`,
			expected: []string{"build", "test"},
		},
		{
			name:     "no match",
			input:    `some random string`,
			expected: []string{},
		},
		{
			name:     "template syntax",
			input:    `{{ .__actions.deploy.results }}`,
			expected: []string{"deploy"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseActionsRefsForGraph(tt.input)
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}

func TestEvaluateForEachArray(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		forEach     *spec.ForEachClause
		data        map[string]any
		expected    []any
		expectError bool
	}{
		{
			name: "string array from resolver",
			forEach: &spec.ForEachClause{
				In: rslvrRef("regions"),
			},
			data: map[string]any{
				"regions": []any{"a", "b", "c"},
			},
			expected: []any{"a", "b", "c"},
		},
		{
			name: "array from expression",
			forEach: &spec.ForEachClause{
				In: graphExprRef("[1, 2, 3]"),
			},
			data:     nil,
			expected: []any{int64(1), int64(2), int64(3)},
		},
		{
			name: "nil in value returns error",
			forEach: &spec.ForEachClause{
				In: literalRef(nil),
			},
			data:        nil,
			expected:    nil,
			expectError: true, // literalRef(nil) creates empty value reference which errors
		},
		{
			name:        "nil forEach",
			forEach:     nil,
			expectError: true,
		},
		{
			name:        "nil In field",
			forEach:     &spec.ForEachClause{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := evaluateForEachArray(ctx, tt.forEach, tt.data)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestBuildGraph_EmptyForEach(t *testing.T) {
	ctx := context.Background()
	resolverData := map[string]any{
		"regions": []any{},
	}

	w := &Workflow{
		Actions: map[string]*Action{
			"deploy": {
				Provider: "shell",
				ForEach: &spec.ForEachClause{
					In: rslvrRef("regions"),
				},
			},
		},
	}

	graph, err := BuildGraph(ctx, w, resolverData, nil)
	require.NoError(t, err)

	// Should have no actions since forEach array is empty
	assert.Empty(t, graph.Actions)
	assert.Nil(t, graph.ExecutionOrder)
}

func TestBuildGraph_MultipleForEachChained(t *testing.T) {
	ctx := context.Background()
	resolverData := map[string]any{
		"apps":    []any{"app1", "app2"},
		"regions": []any{"us", "eu"},
	}

	w := &Workflow{
		Actions: map[string]*Action{
			"build": {
				Provider: "shell",
				ForEach: &spec.ForEachClause{
					Item: "app",
					In:   rslvrRef("apps"),
				},
			},
			"deploy": {
				Provider:  "shell",
				DependsOn: []string{"build"},
				ForEach: &spec.ForEachClause{
					Item: "region",
					In:   rslvrRef("regions"),
				},
			},
		},
	}

	graph, err := BuildGraph(ctx, w, resolverData, nil)
	require.NoError(t, err)

	// Should have 4 actions: 2 build + 2 deploy
	assert.Len(t, graph.Actions, 4)

	// Each deploy should depend on all builds
	for _, deployName := range []string{"deploy[0]", "deploy[1]"} {
		deps := graph.Actions[deployName].Dependencies
		assert.ElementsMatch(t, []string{"build[0]", "build[1]"}, deps)
	}

	// Should have 2 phases
	require.Len(t, graph.ExecutionOrder, 2)
	assert.ElementsMatch(t, []string{"build[0]", "build[1]"}, graph.ExecutionOrder[0])
	assert.ElementsMatch(t, []string{"deploy[0]", "deploy[1]"}, graph.ExecutionOrder[1])
}

func TestExtractActionsRefsFromDeferred(t *testing.T) {
	tests := []struct {
		name     string
		deferred *DeferredValue
		expected []string
	}{
		{
			name:     "nil deferred",
			deferred: nil,
			expected: nil,
		},
		{
			name: "expression with single ref",
			deferred: &DeferredValue{
				OriginalExpr: "__actions.build.results",
				Deferred:     true,
			},
			expected: []string{"build"},
		},
		{
			name: "expression with multiple refs",
			deferred: &DeferredValue{
				OriginalExpr: "__actions.build.results + __actions.test.output",
				Deferred:     true,
			},
			expected: []string{"build", "test"},
		},
		{
			name: "template with ref",
			deferred: &DeferredValue{
				OriginalTmpl: "{{ .__actions.deploy.results.url }}",
				Deferred:     true,
			},
			expected: []string{"deploy"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractActionsRefsFromDeferred(tt.deferred)
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}
