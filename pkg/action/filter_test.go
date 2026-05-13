// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterWorkflowActions(t *testing.T) {
	t.Parallel()

	baseWorkflow := &Workflow{
		Actions: map[string]*Action{
			"setup":  {Name: "setup"},
			"build":  {Name: "build", DependsOn: []string{"setup"}},
			"test":   {Name: "test", DependsOn: []string{"build"}},
			"lint":   {Name: "lint", DependsOn: []string{"setup"}},
			"deploy": {Name: "deploy", DependsOn: []string{"test", "lint"}, Alias: "d"},
		},
		Finally: map[string]*Action{
			"cleanup": {Name: "cleanup"},
		},
		ResultSchemaMode: ResultSchemaModeError,
	}

	tests := []struct {
		name        string
		targets     []string
		wantActions []string
		wantFinally []string
		wantErr     string
	}{
		{
			name:        "empty targets returns all",
			targets:     nil,
			wantActions: []string{"setup", "build", "test", "lint", "deploy"},
			wantFinally: []string{"cleanup"},
		},
		{
			name:        "single action no deps",
			targets:     []string{"setup"},
			wantActions: []string{"setup"},
			wantFinally: []string{"cleanup"},
		},
		{
			name:        "single action with transitive deps",
			targets:     []string{"test"},
			wantActions: []string{"setup", "build", "test"},
			wantFinally: []string{"cleanup"},
		},
		{
			name:        "multiple targets merge deps",
			targets:     []string{"test", "lint"},
			wantActions: []string{"setup", "build", "test", "lint"},
			wantFinally: []string{"cleanup"},
		},
		{
			name:        "alias resolves to action",
			targets:     []string{"d"},
			wantActions: []string{"setup", "build", "test", "lint", "deploy"},
			wantFinally: []string{"cleanup"},
		},
		{
			name:    "unknown action errors",
			targets: []string{"nonexistent"},
			wantErr: `action "nonexistent" not found in workflow`,
		},
		{
			name:        "finally always included",
			targets:     []string{"setup"},
			wantActions: []string{"setup"},
			wantFinally: []string{"cleanup"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := FilterWorkflowActions(baseWorkflow, tt.targets)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result)

			// Check actions
			gotActions := make([]string, 0, len(result.Actions))
			for name := range result.Actions {
				gotActions = append(gotActions, name)
			}
			assert.ElementsMatch(t, tt.wantActions, gotActions)

			// Check finally always present
			gotFinally := make([]string, 0, len(result.Finally))
			for name := range result.Finally {
				gotFinally = append(gotFinally, name)
			}
			assert.ElementsMatch(t, tt.wantFinally, gotFinally)

			// Check result schema mode preserved
			assert.Equal(t, baseWorkflow.ResultSchemaMode, result.ResultSchemaMode)
		})
	}
}

func TestFilterWorkflowActions_NilWorkflow(t *testing.T) {
	t.Parallel()
	result, err := FilterWorkflowActions(nil, []string{"test"})
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestFilterWorkflowActions_MissingDependency(t *testing.T) {
	t.Parallel()

	// Action "build" depends on "missing" which doesn't exist in the workflow.
	// collectDeps should skip missing deps without adding nil entries.
	w := &Workflow{
		Actions: map[string]*Action{
			"build": {Name: "build", DependsOn: []string{"missing"}},
		},
	}

	result, err := FilterWorkflowActions(w, []string{"build"})
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should only contain "build", not "missing"
	assert.Len(t, result.Actions, 1)
	assert.NotNil(t, result.Actions["build"])
	assert.Nil(t, result.Actions["missing"], "missing dep should not appear in filtered actions")
}

func TestFilterWorkflowActions_ExplicitExcluded(t *testing.T) {
	t.Parallel()

	w := &Workflow{
		Actions: map[string]*Action{
			"build": {Name: "build"},
			"test":  {Name: "test", DependsOn: []string{"build"}},
			"info":  {Name: "info", Explicit: true},
		},
		Finally: map[string]*Action{
			"cleanup": {Name: "cleanup"},
		},
	}

	// Bare invocation excludes explicit
	result, err := FilterWorkflowActions(w, nil)
	require.NoError(t, err)
	assert.Len(t, result.Actions, 2)
	assert.NotNil(t, result.Actions["build"])
	assert.NotNil(t, result.Actions["test"])
	assert.Nil(t, result.Actions["info"])
	assert.Len(t, result.Finally, 1)

	// Explicit naming includes explicit
	result, err = FilterWorkflowActions(w, []string{"info"})
	require.NoError(t, err)
	assert.Len(t, result.Actions, 1)
	assert.NotNil(t, result.Actions["info"])
}

func TestFilterWorkflowActions_ExplicitAsDepIncluded(t *testing.T) {
	t.Parallel()

	w := &Workflow{
		Actions: map[string]*Action{
			"setup":  {Name: "setup", Explicit: true},
			"deploy": {Name: "deploy", DependsOn: []string{"setup"}},
		},
	}

	// Naming "deploy" pulls in explicit "setup" via deps
	result, err := FilterWorkflowActions(w, []string{"deploy"})
	require.NoError(t, err)
	assert.Len(t, result.Actions, 2)
	assert.NotNil(t, result.Actions["setup"])
	assert.NotNil(t, result.Actions["deploy"])
}

func TestFilterWorkflowActions_AllExplicit(t *testing.T) {
	t.Parallel()

	w := &Workflow{
		Actions: map[string]*Action{
			"info":  {Name: "info", Explicit: true},
			"debug": {Name: "debug", Explicit: true},
		},
	}

	// Bare invocation with all explicit results in empty actions
	result, err := FilterWorkflowActions(w, nil)
	require.NoError(t, err)
	assert.Len(t, result.Actions, 0)
}

func TestFilterWorkflowActions_ExplicitDepPreserved(t *testing.T) {
	t.Parallel()

	w := &Workflow{
		Actions: map[string]*Action{
			"setup":  {Name: "setup", Explicit: true},
			"deploy": {Name: "deploy", DependsOn: []string{"setup"}},
		},
	}

	// Bare invocation: "deploy" is non-explicit and depends on "setup" (explicit).
	// The dependency closure must pull "setup" in so the graph is valid.
	result, err := FilterWorkflowActions(w, nil)
	require.NoError(t, err)
	assert.Len(t, result.Actions, 2)
	assert.NotNil(t, result.Actions["setup"], "explicit dep should be preserved")
	assert.NotNil(t, result.Actions["deploy"])
}

func TestFilterWorkflowActions_NoExplicitUnchanged(t *testing.T) {
	t.Parallel()

	w := &Workflow{
		Actions: map[string]*Action{
			"build": {Name: "build"},
			"test":  {Name: "test"},
		},
	}

	// No explicit actions — returns original workflow
	result, err := FilterWorkflowActions(w, nil)
	require.NoError(t, err)
	assert.Equal(t, w, result, "should return original workflow when no explicit actions exist")
}
