// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// helper to create a CEL expression for renderer tests
func rendererCelExpr(s string) *celexp.Expression {
	e := celexp.Expression(s)
	return &e
}

// helper to create a condition for renderer tests
func rendererCondition(expr string) *spec.Condition {
	return &spec.Condition{Expr: rendererCelExpr(expr)}
}

// helper to create a Duration pointer
func durationPtr(d time.Duration) *Duration {
	dur := Duration(d)
	return &dur
}

func TestRender_NilGraph(t *testing.T) {
	data, err := Render(nil, nil)
	require.Error(t, err)
	assert.Nil(t, data)
	assert.Contains(t, err.Error(), "graph cannot be nil")
}

func TestRenderToStruct_NilGraph(t *testing.T) {
	rendered, err := RenderToStruct(nil, nil)
	require.Error(t, err)
	assert.Nil(t, rendered)
	assert.Contains(t, err.Error(), "graph cannot be nil")
}

func TestRender_EmptyGraph(t *testing.T) {
	graph := &Graph{
		Actions: make(map[string]*ExpandedAction),
	}

	data, err := Render(graph, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	// Parse and verify structure
	var rendered RenderedGraph
	err = json.Unmarshal(data, &rendered)
	require.NoError(t, err)

	assert.Equal(t, APIVersion, rendered.APIVersion)
	assert.Equal(t, KindActionGraph, rendered.Kind)
	assert.Empty(t, rendered.Actions)
	assert.Nil(t, rendered.ExecutionOrder)
	assert.Nil(t, rendered.FinallyOrder)
	assert.NotNil(t, rendered.Metadata)
	assert.Equal(t, 0, rendered.Metadata.TotalActions)
	assert.Equal(t, 0, rendered.Metadata.TotalPhases)
	assert.False(t, rendered.Metadata.HasFinally)
}

func TestRender_SingleAction_JSON(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"deploy": {
				Action: &Action{
					Name:        "deploy",
					Provider:    "shell",
					Description: "Deploy the application",
				},
				ExpandedName:       "deploy",
				MaterializedInputs: map[string]any{"command": "kubectl apply -f deployment.yaml"},
				Section:            "actions",
				Dependencies:       []string{},
			},
		},
		ExecutionOrder: [][]string{{"deploy"}},
	}

	opts := &RenderOptions{
		Format:           FormatJSON,
		IncludeTimestamp: false,
		PrettyPrint:      true,
	}

	data, err := Render(graph, opts)
	require.NoError(t, err)

	var rendered RenderedGraph
	err = json.Unmarshal(data, &rendered)
	require.NoError(t, err)

	assert.Equal(t, APIVersion, rendered.APIVersion)
	assert.Equal(t, KindActionGraph, rendered.Kind)
	require.Len(t, rendered.Actions, 1)

	action := rendered.Actions["deploy"]
	require.NotNil(t, action)
	assert.Equal(t, "deploy", action.Name)
	assert.Equal(t, "shell", action.Provider)
	assert.Equal(t, "Deploy the application", action.Description)
	assert.Equal(t, "actions", action.Section)
	assert.Equal(t, "kubectl apply -f deployment.yaml", action.Inputs["command"])
	assert.Empty(t, action.DependsOn)

	// Metadata checks
	assert.NotNil(t, rendered.Metadata)
	assert.Equal(t, 1, rendered.Metadata.TotalActions)
	assert.Equal(t, 1, rendered.Metadata.TotalPhases)
	assert.False(t, rendered.Metadata.HasFinally)
	assert.Empty(t, rendered.Metadata.GeneratedAt) // timestamp disabled
}

func TestRender_SingleAction_YAML(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"build": {
				Action: &Action{
					Name:     "build",
					Provider: "shell",
				},
				ExpandedName:       "build",
				MaterializedInputs: map[string]any{"command": "go build ./..."},
				Section:            "actions",
			},
		},
		ExecutionOrder: [][]string{{"build"}},
	}

	opts := &RenderOptions{
		Format:           FormatYAML,
		IncludeTimestamp: false,
	}

	data, err := Render(graph, opts)
	require.NoError(t, err)

	var rendered RenderedGraph
	err = yaml.Unmarshal(data, &rendered)
	require.NoError(t, err)

	assert.Equal(t, APIVersion, rendered.APIVersion)
	assert.Equal(t, KindActionGraph, rendered.Kind)
	require.Len(t, rendered.Actions, 1)

	action := rendered.Actions["build"]
	require.NotNil(t, action)
	assert.Equal(t, "build", action.Name)
	assert.Equal(t, "shell", action.Provider)
}

func TestRender_WithDependencies(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"build": {
				Action:       &Action{Name: "build", Provider: "shell"},
				ExpandedName: "build",
				Section:      "actions",
				Dependencies: []string{},
			},
			"test": {
				Action:       &Action{Name: "test", Provider: "shell", DependsOn: []string{"build"}},
				ExpandedName: "test",
				Section:      "actions",
				Dependencies: []string{"build"},
			},
			"deploy": {
				Action:       &Action{Name: "deploy", Provider: "shell", DependsOn: []string{"test"}},
				ExpandedName: "deploy",
				Section:      "actions",
				Dependencies: []string{"test"},
			},
		},
		ExecutionOrder: [][]string{{"build"}, {"test"}, {"deploy"}},
	}

	rendered, err := RenderToStruct(graph, nil)
	require.NoError(t, err)

	assert.Len(t, rendered.ExecutionOrder, 3)
	assert.Equal(t, []string{"build"}, rendered.ExecutionOrder[0])
	assert.Equal(t, []string{"test"}, rendered.ExecutionOrder[1])
	assert.Equal(t, []string{"deploy"}, rendered.ExecutionOrder[2])

	assert.Empty(t, rendered.Actions["build"].DependsOn)
	assert.Equal(t, []string{"build"}, rendered.Actions["test"].DependsOn)
	assert.Equal(t, []string{"test"}, rendered.Actions["deploy"].DependsOn)
}

func TestRender_WithDeferredInputs(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"build": {
				Action:             &Action{Name: "build", Provider: "shell"},
				ExpandedName:       "build",
				Section:            "actions",
				MaterializedInputs: map[string]any{"command": "go build"},
			},
			"notify": {
				Action:       &Action{Name: "notify", Provider: "http"},
				ExpandedName: "notify",
				Section:      "actions",
				Dependencies: []string{"build"},
				MaterializedInputs: map[string]any{
					"url": "https://hooks.slack.com/services/xxx",
				},
				DeferredInputs: map[string]*DeferredValue{
					"message": {
						OriginalExpr: `"Build completed: " + __actions.build.results.artifact`,
						Deferred:     true,
					},
				},
			},
		},
		ExecutionOrder: [][]string{{"build"}, {"notify"}},
	}

	rendered, err := RenderToStruct(graph, nil)
	require.NoError(t, err)

	notifyAction := rendered.Actions["notify"]
	require.NotNil(t, notifyAction)

	// Check that materialized inputs are present
	assert.Equal(t, "https://hooks.slack.com/services/xxx", notifyAction.Inputs["url"])

	// Check that deferred input is preserved
	messageInput := notifyAction.Inputs["message"]
	require.NotNil(t, messageInput)

	deferredValue, ok := messageInput.(*DeferredValue)
	require.True(t, ok, "expected *DeferredValue, got %T", messageInput)
	assert.True(t, deferredValue.Deferred)
	assert.Contains(t, deferredValue.OriginalExpr, "__actions.build.results")
}

func TestRender_WithCondition(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"deploy": {
				Action: &Action{
					Name:     "deploy",
					Provider: "shell",
					When:     rendererCondition(`_.environment == "prod"`),
				},
				ExpandedName: "deploy",
				Section:      "actions",
			},
		},
		ExecutionOrder: [][]string{{"deploy"}},
	}

	rendered, err := RenderToStruct(graph, nil)
	require.NoError(t, err)

	action := rendered.Actions["deploy"]
	require.NotNil(t, action)
	require.NotNil(t, action.When)

	// Condition should be rendered as a DeferredValue
	deferredCond, ok := action.When.(*DeferredValue)
	require.True(t, ok, "expected *DeferredValue, got %T", action.When)
	assert.True(t, deferredCond.Deferred)
	assert.Equal(t, `_.environment == "prod"`, deferredCond.OriginalExpr)
}

func TestRender_WithTimeout(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"deploy": {
				Action: &Action{
					Name:     "deploy",
					Provider: "shell",
					Timeout:  durationPtr(30 * time.Second),
				},
				ExpandedName: "deploy",
				Section:      "actions",
			},
		},
		ExecutionOrder: [][]string{{"deploy"}},
	}

	rendered, err := RenderToStruct(graph, nil)
	require.NoError(t, err)

	action := rendered.Actions["deploy"]
	assert.Equal(t, "30s", action.Timeout)
}

func TestRender_WithRetryConfig(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"deploy": {
				Action: &Action{
					Name:     "deploy",
					Provider: "shell",
					Retry: &RetryConfig{
						MaxAttempts:  3,
						Backoff:      BackoffExponential,
						InitialDelay: durationPtr(1 * time.Second),
						MaxDelay:     durationPtr(30 * time.Second),
					},
				},
				ExpandedName: "deploy",
				Section:      "actions",
			},
		},
		ExecutionOrder: [][]string{{"deploy"}},
	}

	rendered, err := RenderToStruct(graph, nil)
	require.NoError(t, err)

	action := rendered.Actions["deploy"]
	require.NotNil(t, action.Retry)
	assert.Equal(t, 3, action.Retry.MaxAttempts)
	assert.Equal(t, "exponential", action.Retry.Backoff)
	assert.Equal(t, "1s", action.Retry.InitialDelay)
	assert.Equal(t, "30s", action.Retry.MaxDelay)
}

func TestRender_WithOnError(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"cleanup": {
				Action: &Action{
					Name:     "cleanup",
					Provider: "shell",
					OnError:  spec.OnErrorContinue,
				},
				ExpandedName: "cleanup",
				Section:      "actions",
			},
		},
		ExecutionOrder: [][]string{{"cleanup"}},
	}

	rendered, err := RenderToStruct(graph, nil)
	require.NoError(t, err)

	action := rendered.Actions["cleanup"]
	assert.Equal(t, "continue", action.OnError)
}

func TestRender_WithForEachExpansion(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"deploy[0]": {
				Action: &Action{
					Name:     "deploy",
					Provider: "shell",
					ForEach: &spec.ForEachClause{
						Concurrency: 2,
						OnError:     spec.OnErrorContinue,
					},
				},
				ExpandedName:       "deploy[0]",
				MaterializedInputs: map[string]any{"env": "dev"},
				Section:            "actions",
				ForEachMetadata: &ForEachExpansionMetadata{
					ExpandedFrom: "deploy",
					Index:        0,
					Item:         "dev",
				},
			},
			"deploy[1]": {
				Action: &Action{
					Name:     "deploy",
					Provider: "shell",
					ForEach: &spec.ForEachClause{
						Concurrency: 2,
						OnError:     spec.OnErrorContinue,
					},
				},
				ExpandedName:       "deploy[1]",
				MaterializedInputs: map[string]any{"env": "staging"},
				Section:            "actions",
				ForEachMetadata: &ForEachExpansionMetadata{
					ExpandedFrom: "deploy",
					Index:        1,
					Item:         "staging",
				},
			},
			"deploy[2]": {
				Action: &Action{
					Name:     "deploy",
					Provider: "shell",
					ForEach: &spec.ForEachClause{
						Concurrency: 2,
						OnError:     spec.OnErrorContinue,
					},
				},
				ExpandedName:       "deploy[2]",
				MaterializedInputs: map[string]any{"env": "prod"},
				Section:            "actions",
				ForEachMetadata: &ForEachExpansionMetadata{
					ExpandedFrom: "deploy",
					Index:        2,
					Item:         "prod",
				},
			},
		},
		ExecutionOrder: [][]string{{"deploy[0]", "deploy[1]", "deploy[2]"}},
	}

	rendered, err := RenderToStruct(graph, nil)
	require.NoError(t, err)

	// Check metadata has forEach expansions
	require.NotNil(t, rendered.Metadata.ForEachExpansions)
	assert.Equal(t, []string{"deploy[0]", "deploy[1]", "deploy[2]"}, rendered.Metadata.ForEachExpansions["deploy"])

	// Check each expanded action
	for i, name := range []string{"deploy[0]", "deploy[1]", "deploy[2]"} {
		action := rendered.Actions[name]
		require.NotNil(t, action, "missing action %s", name)
		assert.Equal(t, name, action.Name)
		assert.Equal(t, "deploy", action.OriginalName)

		require.NotNil(t, action.ForEach)
		assert.Equal(t, "deploy", action.ForEach.ExpandedFrom)
		assert.Equal(t, i, action.ForEach.Index)
		assert.Equal(t, 2, action.ForEach.Concurrency)
		assert.Equal(t, "continue", action.ForEach.OnError)
	}

	// Check items
	assert.Equal(t, "dev", rendered.Actions["deploy[0]"].ForEach.Item)
	assert.Equal(t, "staging", rendered.Actions["deploy[1]"].ForEach.Item)
	assert.Equal(t, "prod", rendered.Actions["deploy[2]"].ForEach.Item)
}

func TestRender_WithFinallySection(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"build": {
				Action:       &Action{Name: "build", Provider: "shell"},
				ExpandedName: "build",
				Section:      "actions",
			},
			"cleanup": {
				Action:       &Action{Name: "cleanup", Provider: "shell"},
				ExpandedName: "cleanup",
				Section:      "finally",
			},
			"notify": {
				Action:       &Action{Name: "notify", Provider: "http"},
				ExpandedName: "notify",
				Section:      "finally",
				Dependencies: []string{"cleanup"},
			},
		},
		ExecutionOrder: [][]string{{"build"}},
		FinallyOrder:   [][]string{{"cleanup"}, {"notify"}},
	}

	rendered, err := RenderToStruct(graph, nil)
	require.NoError(t, err)

	// Check metadata
	assert.True(t, rendered.Metadata.HasFinally)
	assert.Equal(t, 3, rendered.Metadata.TotalPhases)

	// Check execution order
	assert.Equal(t, [][]string{{"build"}}, rendered.ExecutionOrder)
	assert.Equal(t, [][]string{{"cleanup"}, {"notify"}}, rendered.FinallyOrder)

	// Check sections
	assert.Equal(t, "actions", rendered.Actions["build"].Section)
	assert.Equal(t, "finally", rendered.Actions["cleanup"].Section)
	assert.Equal(t, "finally", rendered.Actions["notify"].Section)
}

func TestRender_WithSensitiveFlag(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"setSecret": {
				Action: &Action{
					Name:      "setSecret",
					Provider:  "vault",
					Sensitive: true,
				},
				ExpandedName: "setSecret",
				Section:      "actions",
			},
		},
		ExecutionOrder: [][]string{{"setSecret"}},
	}

	rendered, err := RenderToStruct(graph, nil)
	require.NoError(t, err)

	action := rendered.Actions["setSecret"]
	assert.True(t, action.Sensitive)
}

func TestRender_WithDisplayName(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"deploy_prod": {
				Action: &Action{
					Name:        "deploy_prod",
					Provider:    "shell",
					DisplayName: "Deploy to Production",
				},
				ExpandedName: "deploy_prod",
				Section:      "actions",
			},
		},
		ExecutionOrder: [][]string{{"deploy_prod"}},
	}

	rendered, err := RenderToStruct(graph, nil)
	require.NoError(t, err)

	action := rendered.Actions["deploy_prod"]
	assert.Equal(t, "Deploy to Production", action.DisplayName)
}

func TestRender_WithTimestamp(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"test": {
				Action:       &Action{Name: "test", Provider: "shell"},
				ExpandedName: "test",
				Section:      "actions",
			},
		},
		ExecutionOrder: [][]string{{"test"}},
	}

	opts := &RenderOptions{
		Format:           FormatJSON,
		IncludeTimestamp: true,
	}

	rendered, err := RenderToStruct(graph, opts)
	require.NoError(t, err)

	assert.NotEmpty(t, rendered.Metadata.GeneratedAt)

	// Validate timestamp format
	_, parseErr := time.Parse(time.RFC3339, rendered.Metadata.GeneratedAt)
	assert.NoError(t, parseErr, "timestamp should be RFC3339 format")
}

func TestRender_CompactJSON(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"test": {
				Action:       &Action{Name: "test", Provider: "shell"},
				ExpandedName: "test",
				Section:      "actions",
			},
		},
		ExecutionOrder: [][]string{{"test"}},
	}

	optsCompact := &RenderOptions{
		Format:           FormatJSON,
		IncludeTimestamp: false,
		PrettyPrint:      false,
	}

	optsPretty := &RenderOptions{
		Format:           FormatJSON,
		IncludeTimestamp: false,
		PrettyPrint:      true,
	}

	compact, err := Render(graph, optsCompact)
	require.NoError(t, err)

	pretty, err := Render(graph, optsPretty)
	require.NoError(t, err)

	// Pretty print should be longer (has indentation)
	assert.Greater(t, len(pretty), len(compact))

	// Both should parse to the same structure
	var compactGraph, prettyGraph RenderedGraph
	require.NoError(t, json.Unmarshal(compact, &compactGraph))
	require.NoError(t, json.Unmarshal(pretty, &prettyGraph))

	assert.Equal(t, compactGraph.APIVersion, prettyGraph.APIVersion)
	assert.Equal(t, len(compactGraph.Actions), len(prettyGraph.Actions))
}

func TestRender_UnsupportedFormat(t *testing.T) {
	graph := &Graph{
		Actions: make(map[string]*ExpandedAction),
	}

	opts := &RenderOptions{
		Format: "xml",
	}

	data, err := Render(graph, opts)
	require.Error(t, err)
	assert.Nil(t, data)
	assert.Contains(t, err.Error(), "unsupported output format")
}

func TestGetFormat(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		wantErr  bool
	}{
		{"", FormatJSON, false},
		{"json", FormatJSON, false},
		{"JSON", FormatJSON, false},
		{"yaml", FormatYAML, false},
		{"YAML", FormatYAML, false},
		{"yml", FormatYAML, false},
		{"YML", FormatYAML, false},
		{"xml", "", true},
		{"toml", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			format, err := GetFormat(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected, format)
			}
		})
	}
}

func TestDefaultRenderOptions(t *testing.T) {
	opts := DefaultRenderOptions()
	assert.Equal(t, FormatJSON, opts.Format)
	assert.True(t, opts.IncludeTimestamp)
	assert.True(t, opts.PrettyPrint)
}

func TestRender_ComplexGraph(t *testing.T) {
	// Build a complex graph using BuildGraph to ensure integration works
	ctx := context.Background()
	w := &Workflow{
		Actions: map[string]*Action{
			"build": {
				Provider:    "shell",
				Description: "Build the application",
				Inputs: map[string]*spec.ValueRef{
					"command": {Literal: "go build ./..."},
				},
			},
			"test": {
				Provider:  "shell",
				DependsOn: []string{"build"},
				Inputs: map[string]*spec.ValueRef{
					"command": {Literal: "go test ./..."},
				},
			},
			"deploy": {
				Provider:  "shell",
				DependsOn: []string{"test"},
				When:      rendererCondition(`_.deploy_enabled`),
				Timeout:   durationPtr(5 * time.Minute),
				Retry: &RetryConfig{
					MaxAttempts:  3,
					Backoff:      BackoffExponential,
					InitialDelay: durationPtr(5 * time.Second),
				},
				Inputs: map[string]*spec.ValueRef{
					"command": {Literal: "kubectl apply -f k8s/"},
				},
			},
		},
		Finally: map[string]*Action{
			"notify": {
				Provider: "http",
				Inputs: map[string]*spec.ValueRef{
					"url": {Literal: "https://hooks.slack.com/services/xxx"},
				},
			},
		},
	}

	resolverData := map[string]any{
		"deploy_enabled": true,
	}

	graph, err := BuildGraph(ctx, w, resolverData, nil)
	require.NoError(t, err)

	// Render to JSON
	data, err := Render(graph, &RenderOptions{
		Format:           FormatJSON,
		IncludeTimestamp: false,
		PrettyPrint:      true,
	})
	require.NoError(t, err)

	var rendered RenderedGraph
	err = json.Unmarshal(data, &rendered)
	require.NoError(t, err)

	// Verify structure
	assert.Equal(t, APIVersion, rendered.APIVersion)
	assert.Equal(t, KindActionGraph, rendered.Kind)
	assert.Len(t, rendered.Actions, 4) // build, test, deploy, notify

	// Check execution order
	assert.Len(t, rendered.ExecutionOrder, 3) // 3 phases for main actions
	assert.Len(t, rendered.FinallyOrder, 1)   // 1 phase for finally

	// Check deploy action has all its fields
	deploy := rendered.Actions["deploy"]
	require.NotNil(t, deploy)
	assert.NotNil(t, deploy.When)
	assert.Equal(t, "5m0s", deploy.Timeout)
	assert.NotNil(t, deploy.Retry)
	assert.Equal(t, 3, deploy.Retry.MaxAttempts)
	assert.Equal(t, "exponential", deploy.Retry.Backoff)
}

func TestRender_DeferredTemplateInput(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"notify": {
				Action:       &Action{Name: "notify", Provider: "http"},
				ExpandedName: "notify",
				Section:      "actions",
				DeferredInputs: map[string]*DeferredValue{
					"message": {
						OriginalTmpl: `Build status: {{ .__actions.build.status }}`,
						Deferred:     true,
					},
				},
			},
		},
		ExecutionOrder: [][]string{{"notify"}},
	}

	rendered, err := RenderToStruct(graph, nil)
	require.NoError(t, err)

	action := rendered.Actions["notify"]
	messageInput := action.Inputs["message"]
	require.NotNil(t, messageInput)

	deferredValue, ok := messageInput.(*DeferredValue)
	require.True(t, ok)
	assert.True(t, deferredValue.Deferred)
	assert.Contains(t, deferredValue.OriginalTmpl, "__actions.build")
}

func TestRender_JSONOutputIsValidJSON(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"test": {
				Action:       &Action{Name: "test", Provider: "shell"},
				ExpandedName: "test",
				Section:      "actions",
				MaterializedInputs: map[string]any{
					"command": "echo 'hello world'",
					"count":   42,
					"enabled": true,
					"tags":    []string{"a", "b", "c"},
				},
			},
		},
		ExecutionOrder: [][]string{{"test"}},
	}

	data, err := Render(graph, &RenderOptions{Format: FormatJSON})
	require.NoError(t, err)

	// Should be valid JSON
	assert.True(t, json.Valid(data), "output should be valid JSON")
}

func TestRender_YAMLOutputIsValidYAML(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"test": {
				Action:       &Action{Name: "test", Provider: "shell"},
				ExpandedName: "test",
				Section:      "actions",
				MaterializedInputs: map[string]any{
					"command": "echo 'hello world'",
					"count":   42,
					"enabled": true,
				},
			},
		},
		ExecutionOrder: [][]string{{"test"}},
	}

	data, err := Render(graph, &RenderOptions{Format: FormatYAML})
	require.NoError(t, err)

	// Should be valid YAML that can unmarshal
	var rendered RenderedGraph
	err = yaml.Unmarshal(data, &rendered)
	require.NoError(t, err)
	assert.Equal(t, APIVersion, rendered.APIVersion)
}

func TestRender_NilActionInGraph(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"valid": {
				Action:       &Action{Name: "valid", Provider: "shell"},
				ExpandedName: "valid",
				Section:      "actions",
			},
			"nilAction": nil, // This shouldn't cause a panic
		},
		ExecutionOrder: [][]string{{"valid"}},
	}

	rendered, err := RenderToStruct(graph, nil)
	require.NoError(t, err)

	// The nil action should result in nil rendered action
	assert.Nil(t, rendered.Actions["nilAction"])
	assert.NotNil(t, rendered.Actions["valid"])
}

func TestRender_RetryWithMinimalConfig(t *testing.T) {
	graph := &Graph{
		Actions: map[string]*ExpandedAction{
			"retry": {
				Action: &Action{
					Name:     "retry",
					Provider: "shell",
					Retry: &RetryConfig{
						MaxAttempts: 2,
						// No backoff, initialDelay, or maxDelay
					},
				},
				ExpandedName: "retry",
				Section:      "actions",
			},
		},
		ExecutionOrder: [][]string{{"retry"}},
	}

	rendered, err := RenderToStruct(graph, nil)
	require.NoError(t, err)

	action := rendered.Actions["retry"]
	require.NotNil(t, action.Retry)
	assert.Equal(t, 2, action.Retry.MaxAttempts)
	assert.Empty(t, action.Retry.Backoff)
	assert.Empty(t, action.Retry.InitialDelay)
	assert.Empty(t, action.Retry.MaxDelay)
}
