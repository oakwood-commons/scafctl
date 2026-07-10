// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// actionEchoProvider is an action-capable provider (nil schema) that returns the
// "value" input verbatim and tracks invocation count.
type actionEchoProvider struct {
	name    string
	counter *atomic.Int32
}

func (p *actionEchoProvider) Descriptor() *provider.Descriptor {
	return &provider.Descriptor{
		Name:         p.name,
		APIVersion:   "v1",
		Version:      semver.MustParse("1.0.0"),
		Description:  "Action echo provider for testing",
		Capabilities: []provider.Capability{provider.CapabilityAction},
	}
}

func (p *actionEchoProvider) Execute(_ context.Context, input any) (*provider.Output, error) {
	if p.counter != nil {
		p.counter.Add(1)
	}
	inputs, _ := input.(map[string]any)
	return &provider.Output{Data: inputs["value"]}, nil
}

func actionExpr(s string) *spec.ValueRef {
	e := celexp.Expression(s)
	return &spec.ValueRef{Expr: &e}
}

func actionTmpl(s string) *spec.ValueRef {
	t := gotmpl.GoTemplatingContent(s)
	return &spec.ValueRef{Tmpl: &t}
}

func newCallExecutor(t *testing.T, calls map[string]*spec.Call, counter *atomic.Int32, resolverData map[string]any) *Executor {
	t.Helper()
	registry := newExecMockRegistry()
	registry.register(&actionEchoProvider{name: "http", counter: counter})
	opts := []ExecutorOption{
		WithRegistry(registry),
		WithCalls(calls),
		WithDefaultTimeout(5 * time.Second),
	}
	if resolverData != nil {
		opts = append(opts, WithResolverData(resolverData))
	}
	return NewExecutor(opts...)
}

func TestExecutor_ActionCall_Basic(t *testing.T) {
	calls := map[string]*spec.Call{
		"get": {
			Provider: "http",
			Args:     map[string]*spec.ArgDef{"id": {Type: spec.TypeString, Required: true}},
			Inputs:   map[string]*spec.ValueRef{"value": actionExpr(`_.args.id`)},
		},
	}
	executor := newCallExecutor(t, calls, nil, nil)

	workflow := &Workflow{
		Actions: map[string]*Action{
			"fetch": {
				CallRef: spec.CallRef{
					Call: "get",
					Args: map[string]*spec.ValueRef{"id": {Literal: "abc"}},
				},
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)
	require.NoError(t, err)
	assert.Equal(t, ExecutionSucceeded, result.FinalStatus)
	require.Contains(t, result.Actions, "fetch")
	assert.Equal(t, StatusSucceeded, result.Actions["fetch"].Status)
	assert.Equal(t, "abc", result.Actions["fetch"].Results)
}

func TestExecutor_ActionCall_Dedup(t *testing.T) {
	var count atomic.Int32
	calls := map[string]*spec.Call{
		"get": {
			Provider: "http",
			Dedup:    true,
			Args:     map[string]*spec.ArgDef{"id": {Type: spec.TypeString}},
			Inputs:   map[string]*spec.ValueRef{"value": actionExpr(`_.args.id`)},
		},
	}
	executor := newCallExecutor(t, calls, &count, nil)

	workflow := &Workflow{
		Actions: map[string]*Action{
			"a": {CallRef: spec.CallRef{Call: "get", Args: map[string]*spec.ValueRef{"id": {Literal: "same"}}}},
			"b": {CallRef: spec.CallRef{Call: "get", Args: map[string]*spec.ValueRef{"id": {Literal: "same"}}}},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)
	require.NoError(t, err)
	assert.Equal(t, ExecutionSucceeded, result.FinalStatus)
	assert.Equal(t, "same", result.Actions["a"].Results)
	assert.Equal(t, "same", result.Actions["b"].Results)
	assert.Equal(t, int32(1), count.Load(), "dedup should collapse identical action invocations to one provider call")
}

func TestExecutor_ActionCall_ArgFromActions(t *testing.T) {
	calls := map[string]*spec.Call{
		"get": {
			Provider: "http",
			Args:     map[string]*spec.ArgDef{"id": {Type: spec.TypeString}},
			Inputs:   map[string]*spec.ValueRef{"value": actionExpr(`_.args.id`)},
		},
	}
	executor := newCallExecutor(t, calls, nil, nil)

	workflow := &Workflow{
		Actions: map[string]*Action{
			"seed": {
				CallRef: spec.CallRef{Call: "get", Args: map[string]*spec.ValueRef{"id": {Literal: "root"}}},
			},
			"downstream": {
				CallRef: spec.CallRef{
					Call: "get",
					Args: map[string]*spec.ValueRef{"id": actionExpr(`string(__actions.seed.results)`)},
				},
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)
	require.NoError(t, err)
	assert.Equal(t, ExecutionSucceeded, result.FinalStatus)
	assert.Equal(t, "root", result.Actions["seed"].Results)
	assert.Equal(t, "root", result.Actions["downstream"].Results)
}

func TestExecutor_ActionCall_ArgFromActionsTmpl(t *testing.T) {
	calls := map[string]*spec.Call{
		"get": {
			Provider: "http",
			Args:     map[string]*spec.ArgDef{"id": {Type: spec.TypeString}},
			Inputs:   map[string]*spec.ValueRef{"value": actionExpr(`_.args.id`)},
		},
	}
	executor := newCallExecutor(t, calls, nil, nil)

	workflow := &Workflow{
		Actions: map[string]*Action{
			"seed": {
				CallRef: spec.CallRef{Call: "get", Args: map[string]*spec.ValueRef{"id": {Literal: "root"}}},
			},
			"downstream": {
				CallRef: spec.CallRef{
					Call: "get",
					Args: map[string]*spec.ValueRef{"id": actionTmpl(`{{ .__actions.seed.results }}`)},
				},
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)
	require.NoError(t, err)
	assert.Equal(t, ExecutionSucceeded, result.FinalStatus)
	assert.Equal(t, "root", result.Actions["downstream"].Results)
}

func TestExecutor_ActionCall_NotFound(t *testing.T) {
	executor := newCallExecutor(t, map[string]*spec.Call{}, nil, nil)

	workflow := &Workflow{
		Actions: map[string]*Action{
			"fetch": {CallRef: spec.CallRef{Call: "missing"}},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)
	require.Error(t, err)
	assert.Equal(t, ExecutionFailed, result.FinalStatus)
	assert.Contains(t, result.FailedActions, "fetch")
}

func TestExecutor_ActionCall_MissingRequiredArg(t *testing.T) {
	calls := map[string]*spec.Call{
		"get": {
			Provider: "http",
			Args:     map[string]*spec.ArgDef{"id": {Type: spec.TypeString, Required: true}},
			Inputs:   map[string]*spec.ValueRef{"value": actionExpr(`_.args.id`)},
		},
	}
	executor := newCallExecutor(t, calls, nil, nil)

	workflow := &Workflow{
		Actions: map[string]*Action{
			"fetch": {CallRef: spec.CallRef{Call: "get"}}, // required "id" omitted
		},
	}

	result, err := executor.Execute(context.Background(), workflow)
	require.Error(t, err)
	assert.Contains(t, result.FailedActions, "fetch")
}

func TestExecutor_ActionCall_NilArg(t *testing.T) {
	calls := map[string]*spec.Call{
		"get": {
			Provider: "http",
			Args:     map[string]*spec.ArgDef{"id": {Type: spec.TypeString}},
			Inputs:   map[string]*spec.ValueRef{"value": actionExpr(`_.args.id`)},
		},
	}
	executor := newCallExecutor(t, calls, nil, nil)

	workflow := &Workflow{
		Actions: map[string]*Action{
			"fetch": {CallRef: spec.CallRef{Call: "get", Args: map[string]*spec.ValueRef{"id": nil}}},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)
	require.Error(t, err)
	assert.Contains(t, result.FailedActions, "fetch")
}

func TestExecutor_ActionCall_ProviderNotFound(t *testing.T) {
	calls := map[string]*spec.Call{
		"get": {
			Provider: "missing-provider",
			Args:     map[string]*spec.ArgDef{"id": {Type: spec.TypeString}},
			Inputs:   map[string]*spec.ValueRef{"value": actionExpr(`_.args.id`)},
		},
	}
	executor := newCallExecutor(t, calls, nil, nil)

	workflow := &Workflow{
		Actions: map[string]*Action{
			"fetch": {CallRef: spec.CallRef{Call: "get", Args: map[string]*spec.ValueRef{"id": {Literal: "1"}}}},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)
	require.Error(t, err)
	assert.Contains(t, result.FailedActions, "fetch")
}

func TestExecutor_ActionCall_DefinitionInputNil(t *testing.T) {
	calls := map[string]*spec.Call{
		"get": {
			Provider: "http",
			Args:     map[string]*spec.ArgDef{"id": {Type: spec.TypeString}},
			Inputs:   map[string]*spec.ValueRef{"value": nil},
		},
	}
	executor := newCallExecutor(t, calls, nil, nil)

	workflow := &Workflow{
		Actions: map[string]*Action{
			"fetch": {CallRef: spec.CallRef{Call: "get", Args: map[string]*spec.ValueRef{"id": {Literal: "1"}}}},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)
	require.Error(t, err)
	assert.Contains(t, result.FailedActions, "fetch")
}

func TestExecutor_ActionCall_ResolverDataArg(t *testing.T) {
	calls := map[string]*spec.Call{
		"get": {
			Provider: "http",
			Args:     map[string]*spec.ArgDef{"id": {Type: spec.TypeString}},
			Inputs:   map[string]*spec.ValueRef{"value": actionExpr(`_.args.id`)},
		},
	}
	executor := newCallExecutor(t, calls, nil, map[string]any{"appName": "myapp"})

	workflow := &Workflow{
		Actions: map[string]*Action{
			"fetch": {
				CallRef: spec.CallRef{
					Call: "get",
					Args: map[string]*spec.ValueRef{"id": actionExpr(`_.appName`)},
				},
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)
	require.NoError(t, err)
	assert.Equal(t, ExecutionSucceeded, result.FinalStatus)
	assert.Equal(t, "myapp", result.Actions["fetch"].Results)
}
