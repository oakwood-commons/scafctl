// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Graph building — phase splitting
// ---------------------------------------------------------------------------

// TestBuildGraph_Exclusive_NoConflictInPhase verifies that when exclusive actions
// are placed in separate phases by their dependsOn relationships, no extra splitting occurs.
func TestBuildGraph_Exclusive_NoConflictInPhase(t *testing.T) {
	ctx := context.Background()
	w := &Workflow{
		Actions: map[string]*Action{
			"migrate": {
				Provider: "shell",
			},
			// updateDB depends on migrate, so they can never be in the same phase.
			// The exclusive declaration is redundant here but must not break anything.
			"updateDB": {
				Provider:  "shell",
				DependsOn: []string{"migrate"},
				Exclusive: []string{"migrate"},
			},
			"notify": {
				Provider: "shell",
			},
		},
	}

	graph, err := BuildGraph(ctx, w, nil, &BuildGraphOptions{SkipInputMaterialization: true})
	require.NoError(t, err)

	// "migrate" and "notify" are in phase 0; "updateDB" is in phase 1 (depends on migrate).
	// No splitting needed.
	assert.Equal(t, 2, len(graph.ExecutionOrder))
	assert.Contains(t, graph.ExecutionOrder[0], "migrate")
	assert.Contains(t, graph.ExecutionOrder[0], "notify")
	assert.Equal(t, []string{"updateDB"}, graph.ExecutionOrder[1])
}

// TestBuildGraph_Exclusive_SplitsPhase verifies that two mutually exclusive actions
// that would normally occupy the same phase are split into sequential sub-phases.
func TestBuildGraph_Exclusive_SplitsPhase(t *testing.T) {
	ctx := context.Background()
	w := &Workflow{
		Actions: map[string]*Action{
			"migrate":  {Provider: "shell"},
			"updateDB": {Provider: "shell", Exclusive: []string{"migrate"}},
			"notify":   {Provider: "shell"},
		},
	}

	graph, err := BuildGraph(ctx, w, nil, &BuildGraphOptions{SkipInputMaterialization: true})
	require.NoError(t, err)

	// All three actions have no dependsOn so Kahn puts them all in one phase.
	// exclusive: migrate ↔ updateDB means they must be split.
	// "notify" has no conflict so it goes into the same sub-phase as one of them.
	//
	// Greedy coloring (sorted: migrate, notify, updateDB):
	//   migrate  → color 0
	//   notify   → color 0  (no conflict with migrate)
	//   updateDB → color 1  (conflicts with migrate which is color 0)
	//
	// Sub-phase 0: [migrate, notify]
	// Sub-phase 1: [updateDB]
	require.Len(t, graph.ExecutionOrder, 2)
	assert.ElementsMatch(t, []string{"migrate", "notify"}, graph.ExecutionOrder[0])
	assert.Equal(t, []string{"updateDB"}, graph.ExecutionOrder[1])
}

// TestBuildGraph_Exclusive_OneWayDeclaration verifies that only one side needs to
// declare exclusivity; the conflict is still enforced bidirectionally.
func TestBuildGraph_Exclusive_OneWayDeclaration(t *testing.T) {
	ctx := context.Background()
	// Only updateDB declares exclusive — migrate does not.
	w := &Workflow{
		Actions: map[string]*Action{
			"migrate":  {Provider: "shell"},
			"updateDB": {Provider: "shell", Exclusive: []string{"migrate"}},
		},
	}

	graph, err := BuildGraph(ctx, w, nil, &BuildGraphOptions{SkipInputMaterialization: true})
	require.NoError(t, err)

	require.Len(t, graph.ExecutionOrder, 2, "exclusive pair must be in separate phases")
}

// TestBuildGraph_Exclusive_ThreeWayConflict verifies that when A conflicts with B and C,
// all three are placed in separate sub-phases because each pair conflicts.
func TestBuildGraph_Exclusive_ThreeWayConflict(t *testing.T) {
	ctx := context.Background()
	w := &Workflow{
		Actions: map[string]*Action{
			// A conflicts with B and C; B also conflicts with C via A's declaration alone
			// doesn't force B↔C, so we need explicit declarations for a full 3-clique.
			"a": {Provider: "shell", Exclusive: []string{"b", "c"}},
			"b": {Provider: "shell", Exclusive: []string{"c"}},
			"c": {Provider: "shell"},
		},
	}

	graph, err := BuildGraph(ctx, w, nil, &BuildGraphOptions{SkipInputMaterialization: true})
	require.NoError(t, err)

	// a↔b, a↔c, b↔c  → chromatic number 3 → three sub-phases
	require.Len(t, graph.ExecutionOrder, 3)
	// Each sub-phase must contain exactly one action
	assert.Len(t, graph.ExecutionOrder[0], 1)
	assert.Len(t, graph.ExecutionOrder[1], 1)
	assert.Len(t, graph.ExecutionOrder[2], 1)
}

// TestBuildGraph_Exclusive_NonExclusiveActionsGrouped verifies that non-conflicting
// action pairs end up together while conflicting pairs are separated.
func TestBuildGraph_Exclusive_NonExclusiveActionsGrouped(t *testing.T) {
	ctx := context.Background()
	// a exclusive of b; c and d have no conflicts.
	w := &Workflow{
		Actions: map[string]*Action{
			"a": {Provider: "shell", Exclusive: []string{"b"}},
			"b": {Provider: "shell"},
			"c": {Provider: "shell"},
			"d": {Provider: "shell"},
		},
	}

	graph, err := BuildGraph(ctx, w, nil, &BuildGraphOptions{SkipInputMaterialization: true})
	require.NoError(t, err)

	// Greedy coloring (sorted: a, b, c, d):
	//   a → color 0
	//   b → color 1  (conflicts with a)
	//   c → color 0  (no conflict)
	//   d → color 0  (no conflict)
	//
	// Sub-phase 0: [a, c, d]
	// Sub-phase 1: [b]
	require.Len(t, graph.ExecutionOrder, 2)
	assert.ElementsMatch(t, []string{"a", "c", "d"}, graph.ExecutionOrder[0])
	assert.Equal(t, []string{"b"}, graph.ExecutionOrder[1])
}

// TestBuildGraph_Exclusive_ForEachExpansion verifies that exclusive: [deploy] on
// an action expands to all forEach iterations of deploy.
func TestBuildGraph_Exclusive_ForEachExpansion(t *testing.T) {
	ctx := context.Background()
	w := &Workflow{
		Actions: map[string]*Action{
			"migrate": {
				Provider:  "shell",
				Exclusive: []string{"deploy"},
			},
			"deploy": {
				Provider: "shell",
				ForEach: &spec.ForEachClause{
					In: &spec.ValueRef{Literal: []any{"env1", "env2"}},
				},
			},
		},
	}

	graph, err := BuildGraph(ctx, w, nil, &BuildGraphOptions{SkipInputMaterialization: true})
	require.NoError(t, err)

	// deploy expands to deploy[0] and deploy[1].
	// migrate must not run concurrently with either deploy iteration.
	// Phase topology: migrate, deploy[0], deploy[1] are all independent (no dependsOn),
	// but migrate conflicts with both deploy iterations.
	//
	// Greedy coloring (sorted: deploy[0], deploy[1], migrate):
	//   deploy[0] → color 0
	//   deploy[1] → color 0  (no conflict between iterations)
	//   migrate   → color 1  (conflicts with deploy[0] & deploy[1])
	//
	// Sub-phase 0: [deploy[0], deploy[1]]
	// Sub-phase 1: [migrate]
	require.Len(t, graph.ExecutionOrder, 2)
	assert.ElementsMatch(t, []string{"deploy[0]", "deploy[1]"}, graph.ExecutionOrder[0])
	assert.Equal(t, []string{"migrate"}, graph.ExecutionOrder[1])

	// Verify ExpandedExclusive was populated on migrate
	migrateAction := graph.Actions["migrate"]
	require.NotNil(t, migrateAction)
	assert.ElementsMatch(t, []string{"deploy[0]", "deploy[1]"}, migrateAction.ExpandedExclusive)
}

// TestBuildGraph_Exclusive_FinallySection verifies exclusive works in the finally section.
func TestBuildGraph_Exclusive_FinallySection(t *testing.T) {
	ctx := context.Background()
	w := &Workflow{
		Actions: map[string]*Action{
			"main": {Provider: "shell"},
		},
		Finally: map[string]*Action{
			"cleanup-a": {Provider: "shell", Exclusive: []string{"cleanup-b"}},
			"cleanup-b": {Provider: "shell"},
		},
	}

	graph, err := BuildGraph(ctx, w, nil, &BuildGraphOptions{SkipInputMaterialization: true})
	require.NoError(t, err)

	require.Len(t, graph.FinallyOrder, 2)
}

// TestBuildGraph_Exclusive_ExpandedExclusiveField verifies that ExpandedExclusive
// is correctly populated on expanded actions for non-forEach actions.
func TestBuildGraph_Exclusive_ExpandedExclusiveField(t *testing.T) {
	ctx := context.Background()
	w := &Workflow{
		Actions: map[string]*Action{
			"a": {Provider: "shell", Exclusive: []string{"b", "c"}},
			"b": {Provider: "shell"},
			"c": {Provider: "shell"},
		},
	}

	graph, err := BuildGraph(ctx, w, nil, &BuildGraphOptions{SkipInputMaterialization: true})
	require.NoError(t, err)

	aAction := graph.Actions["a"]
	require.NotNil(t, aAction)
	assert.ElementsMatch(t, []string{"b", "c"}, aAction.ExpandedExclusive)

	bAction := graph.Actions["b"]
	require.NotNil(t, bAction)
	assert.Empty(t, bAction.ExpandedExclusive)
}

// ---------------------------------------------------------------------------
// splitPhaseForExclusive unit tests
// ---------------------------------------------------------------------------

func TestSplitPhaseForExclusive_EmptyPhase(t *testing.T) {
	result := splitPhaseForExclusive(nil, map[string]*ExpandedAction{})
	assert.Nil(t, result)
}

func TestSplitPhaseForExclusive_SingleAction(t *testing.T) {
	phase := []string{"a"}
	result := splitPhaseForExclusive(phase, map[string]*ExpandedAction{})
	assert.Equal(t, [][]string{{"a"}}, result)
}

func TestSplitPhaseForExclusive_NoConflicts(t *testing.T) {
	phase := []string{"a", "b", "c"}
	actions := map[string]*ExpandedAction{
		"a": {Action: &Action{Name: "a"}},
		"b": {Action: &Action{Name: "b"}},
		"c": {Action: &Action{Name: "c"}},
	}
	result := splitPhaseForExclusive(phase, actions)
	// No conflicts → returned as-is (one sub-phase)
	assert.Equal(t, [][]string{{"a", "b", "c"}}, result)
}

func TestSplitPhaseForExclusive_ConflictOutsidePhase(t *testing.T) {
	// "a" has an exclusive ref to "z" which is NOT in this phase — no splitting needed
	phase := []string{"a", "b"}
	actions := map[string]*ExpandedAction{
		"a": {Action: &Action{Name: "a"}, ExpandedExclusive: []string{"z"}},
		"b": {Action: &Action{Name: "b"}},
	}
	result := splitPhaseForExclusive(phase, actions)
	assert.Equal(t, [][]string{{"a", "b"}}, result)
}

// ---------------------------------------------------------------------------
// Validation tests
// ---------------------------------------------------------------------------

func TestValidateWorkflow_Exclusive_Valid(t *testing.T) {
	w := &Workflow{
		Actions: map[string]*Action{
			"migrate":  {Provider: "shell"},
			"updateDB": {Provider: "shell", Exclusive: []string{"migrate"}},
		},
	}
	err := ValidateWorkflow(w, nil)
	assert.NoError(t, err)
}

func TestValidateWorkflow_Exclusive_SelfReference(t *testing.T) {
	w := &Workflow{
		Actions: map[string]*Action{
			"migrate": {Provider: "shell", Exclusive: []string{"migrate"}},
		},
	}
	err := ValidateWorkflow(w, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot exclude itself")
}

func TestValidateWorkflow_Exclusive_ActionNotFound(t *testing.T) {
	w := &Workflow{
		Actions: map[string]*Action{
			"migrate": {Provider: "shell", Exclusive: []string{"nonexistent"}},
		},
	}
	err := ValidateWorkflow(w, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"nonexistent" not found`)
}

func TestValidateWorkflow_Exclusive_CrossSectionReference(t *testing.T) {
	// "actions" section cannot reference "finally" section actions in exclusive
	w := &Workflow{
		Actions: map[string]*Action{
			"migrate": {Provider: "shell", Exclusive: []string{"cleanup"}},
		},
		Finally: map[string]*Action{
			"cleanup": {Provider: "shell"},
		},
	}
	err := ValidateWorkflow(w, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"cleanup" not found in actions section`)
}

func TestValidateWorkflow_Exclusive_FinallyReferencesActions(t *testing.T) {
	// "finally" section cannot reference "actions" section actions in exclusive
	w := &Workflow{
		Actions: map[string]*Action{
			"migrate": {Provider: "shell"},
		},
		Finally: map[string]*Action{
			"cleanup": {Provider: "shell", Exclusive: []string{"migrate"}},
		},
	}
	err := ValidateWorkflow(w, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"migrate" not found in finally section`)
}

func TestValidateWorkflow_Exclusive_ValidFinally(t *testing.T) {
	w := &Workflow{
		Finally: map[string]*Action{
			"cleanup-a": {Provider: "shell", Exclusive: []string{"cleanup-b"}},
			"cleanup-b": {Provider: "shell"},
		},
	}
	err := ValidateWorkflow(w, nil)
	assert.NoError(t, err)
}

func TestValidateWorkflow_Exclusive_MultipleErrors(t *testing.T) {
	w := &Workflow{
		Actions: map[string]*Action{
			"a": {
				Provider:  "shell",
				Exclusive: []string{"a", "missing"},
			},
		},
	}
	err := ValidateWorkflow(w, nil)
	require.Error(t, err)

	var aggregated *AggregatedValidationError
	require.ErrorAs(t, err, &aggregated)
	assert.Len(t, aggregated.Errors, 2, "should report self-reference and missing action")
}

// ---------------------------------------------------------------------------
// Executor integration — exclusive actions run serially
// ---------------------------------------------------------------------------

// TestExecutor_Execute_ExclusiveActionsRunSerially verifies that exclusive actions
// never execute concurrently. We detect overlap by recording start/end timestamps
// under a mutex and asserting the intervals do not intersect.
func TestExecutor_Execute_ExclusiveActionsRunSerially(t *testing.T) {
	type interval struct {
		name  string
		start time.Time
		end   time.Time
	}

	var mu sync.Mutex
	var intervals []interval

	registry := newExecMockRegistry()
	registry.register(&execMockProvider{
		name: "test-provider",
		execute: func(_ context.Context, input any) (*provider.Output, error) {
			inputs, _ := input.(map[string]any)
			name, _ := inputs["name"].(string)

			start := time.Now()
			// Small sleep to give concurrent actions a chance to overlap
			time.Sleep(30 * time.Millisecond)
			end := time.Now()

			mu.Lock()
			intervals = append(intervals, interval{name: name, start: start, end: end})
			mu.Unlock()

			return &provider.Output{Data: map[string]any{"done": true}}, nil
		},
	})

	executor := NewExecutor(
		WithRegistry(registry),
		WithDefaultTimeout(10*time.Second),
	)

	workflow := &Workflow{
		Actions: map[string]*Action{
			"migrate": {
				Provider:  "test-provider",
				Exclusive: []string{"updateDB"},
				Inputs:    map[string]*spec.ValueRef{"name": {Literal: "migrate"}},
			},
			"updateDB": {
				Provider: "test-provider",
				Inputs:   map[string]*spec.ValueRef{"name": {Literal: "updateDB"}},
			},
			// notify has no exclusive constraints — it may run with either
			"notify": {
				Provider: "test-provider",
				Inputs:   map[string]*spec.ValueRef{"name": {Literal: "notify"}},
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)
	require.NoError(t, err)
	assert.Equal(t, ExecutionSucceeded, result.FinalStatus)

	// Locate the migrate and updateDB intervals
	var migrateIv, updateDBIv *interval
	for i := range intervals {
		switch intervals[i].name {
		case "migrate":
			migrateIv = &intervals[i]
		case "updateDB":
			updateDBIv = &intervals[i]
		}
	}

	require.NotNil(t, migrateIv, "migrate must have executed")
	require.NotNil(t, updateDBIv, "updateDB must have executed")

	// The two intervals must not overlap:
	// one must have ended before the other started.
	overlaps := migrateIv.start.Before(updateDBIv.end) && updateDBIv.start.Before(migrateIv.end)
	assert.False(t, overlaps, "migrate and updateDB should not run concurrently")
}
