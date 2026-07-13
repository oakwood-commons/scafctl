// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package execute

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// literalRef builds a literal ValueRef.
func literalRef(v any) *spec.ValueRef { return &spec.ValueRef{Literal: v} }

// tmplRef builds a Go-template ValueRef.
func tmplRef(s string) *spec.ValueRef {
	c := gotmpl.GoTemplatingContent(s)
	return &spec.ValueRef{Tmpl: &c}
}

// exprRef builds a CEL ValueRef.
func exprRef(s string) *spec.ValueRef {
	e := celexp.Expression(s)
	return &spec.ValueRef{Expr: &e}
}

func testRegistry(t *testing.T) *provider.Registry {
	t.Helper()
	reg, err := builtin.DefaultRegistry(context.Background())
	require.NoError(t, err)
	return reg
}

// resolverCfg returns a resolver execution config with generous timeouts so
// tests do not spuriously time out.
func resolverCfg() ResolverExecutionConfig {
	return ResolverExecutionConfig{Timeout: 30 * time.Second, PhaseTimeout: 30 * time.Second}
}

// countingProvider records how many times Execute runs so dedup behavior can be
// asserted. It echoes the resolved "value" input back under "value".
type countingProvider struct {
	calls int32
}

func (c *countingProvider) Descriptor() *provider.Descriptor {
	valueOut := schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
		"value": schemahelper.AnyProp("echoed value"),
	})
	return &provider.Descriptor{
		Name:         "counter",
		APIVersion:   "v1",
		Version:      semver.MustParse("1.0.0"),
		Description:  "test counting provider",
		Capabilities: []provider.Capability{provider.CapabilityFrom, provider.CapabilityTransform},
		OutputSchemas: map[provider.Capability]*jsonschema.Schema{
			provider.CapabilityFrom:      valueOut,
			provider.CapabilityTransform: valueOut,
		},
	}
}

func (c *countingProvider) Execute(_ context.Context, input any) (*provider.Output, error) {
	atomic.AddInt32(&c.calls, 1)
	m, _ := input.(map[string]any)
	return &provider.Output{Data: map[string]any{"value": m["value"]}}, nil
}

func TestResolvers_ResolveStepCall(t *testing.T) {
	sol := &solution.Solution{}
	sol.Metadata.Name = "call-resolve"
	sol.Spec.Calls = map[string]*spec.Call{
		"greet": {
			Provider: "static",
			Inputs:   map[string]*spec.ValueRef{"value": tmplRef("{{ .args.prefix }}-{{ .args.name }}")},
			Args: map[string]*spec.ArgDef{
				"prefix": {Type: spec.TypeString, Default: "id"},
				"name":   {Type: spec.TypeString, Required: true},
			},
		},
	}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"thing": {
			Name: "thing",
			Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{
				CallRef: spec.CallRef{Call: "greet", Args: map[string]*spec.ValueRef{"name": literalRef("abc")}},
			}}},
		},
	}

	result, err := Resolvers(context.Background(), sol, nil, testRegistry(t), resolverCfg())
	require.NoError(t, err)

	assert.Equal(t, "id-abc", result.Data["thing"], "default prefix + supplied name should be bound")
}

func TestResolvers_TransformStepCall(t *testing.T) {
	sol := &solution.Solution{}
	sol.Metadata.Name = "call-transform"
	sol.Spec.Calls = map[string]*spec.Call{
		"upper": {
			Provider: "cel",
			// The arg is supplied as __self at the call site, proving the call
			// site resolves args in the transform context. The template renders
			// to a valid CEL expression that concatenates a suffix.
			Inputs: map[string]*spec.ValueRef{"expression": tmplRef("\"{{ .args.text }}\" + \"-x\"")},
			Args:   map[string]*spec.ArgDef{"text": {Type: spec.TypeString, Required: true}},
		},
	}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"name": {
			Name:    "name",
			Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": literalRef("hello")}}}},
			Transform: &resolver.TransformPhase{With: []resolver.ProviderTransform{{
				CallRef: spec.CallRef{Call: "upper", Args: map[string]*spec.ValueRef{"text": exprRef("__self")}},
			}}},
		},
	}

	result, err := Resolvers(context.Background(), sol, nil, testRegistry(t), resolverCfg())
	require.NoError(t, err)

	assert.Equal(t, "hello-x", result.Data["name"])
}

func TestResolvers_ValidateStepCall(t *testing.T) {
	build := func(pattern, value string) *solution.Solution {
		sol := &solution.Solution{}
		sol.Metadata.Name = "call-validate"
		sol.Spec.Calls = map[string]*spec.Call{
			"matches": {
				Provider: "validation",
				Inputs: map[string]*spec.ValueRef{
					"value": exprRef("__self"),
					"match": tmplRef("{{ .args.pattern }}"),
				},
				Args: map[string]*spec.ArgDef{"pattern": {Type: spec.TypeString, Required: true}},
			},
		}
		sol.Spec.Resolvers = map[string]*resolver.Resolver{
			"env": {
				Name:    "env",
				Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": literalRef(value)}}}},
				Validate: &resolver.ValidatePhase{With: []resolver.ProviderValidation{{
					CallRef: spec.CallRef{Call: "matches", Args: map[string]*spec.ValueRef{"pattern": literalRef(pattern)}},
				}}},
			},
		}
		return sol
	}

	t.Run("passes when value matches", func(t *testing.T) {
		result, err := Resolvers(context.Background(), build("^[a-z]+$", "prod"), nil, testRegistry(t), resolverCfg())
		require.NoError(t, err)
		assert.Equal(t, "prod", result.Data["env"])
	})

	t.Run("fails when value does not match", func(t *testing.T) {
		_, err := Resolvers(context.Background(), build("^[a-z]+$", "PROD-123"), nil, testRegistry(t), resolverCfg())
		require.Error(t, err)
	})
}

func TestResolvers_DedupReusesResult(t *testing.T) {
	counter := &countingProvider{}
	reg := testRegistry(t)
	require.NoError(t, reg.Register(counter))

	sol := &solution.Solution{}
	sol.Metadata.Name = "call-dedup"
	sol.Spec.Calls = map[string]*spec.Call{
		"count": {
			Provider: "counter",
			Dedup:    true,
			Inputs:   map[string]*spec.ValueRef{"value": exprRef("_.args.id")},
			Args:     map[string]*spec.ArgDef{"id": {Type: spec.TypeString, Required: true}},
		},
	}
	// Two resolvers invoke the same call with identical args; dedup should run
	// the provider exactly once.
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"a": {
			Name: "a",
			Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{
				CallRef: spec.CallRef{Call: "count", Args: map[string]*spec.ValueRef{"id": literalRef("same")}},
			}}},
		},
		"b": {
			Name: "b",
			Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{
				CallRef: spec.CallRef{Call: "count", Args: map[string]*spec.ValueRef{"id": literalRef("same")}},
			}}},
		},
	}

	result, err := Resolvers(context.Background(), sol, nil, reg, resolverCfg())
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&counter.calls), "dedup should invoke provider once for identical args")

	a := result.Data["a"].(map[string]any)
	b := result.Data["b"].(map[string]any)
	assert.Equal(t, "same", a["value"])
	assert.Equal(t, "same", b["value"])
}

func TestActions_Call(t *testing.T) {
	sol := &solution.Solution{}
	sol.Metadata.Name = "call-action"
	sol.Spec.Calls = map[string]*spec.Call{
		"echo": {
			Provider: "static",
			Inputs:   map[string]*spec.ValueRef{"value": tmplRef("done-{{ .args.what }}")},
			Args:     map[string]*spec.ArgDef{"what": {Type: spec.TypeString, Required: true}},
		},
	}
	sol.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"run": {
				Name:    "run",
				CallRef: spec.CallRef{Call: "echo", Args: map[string]*spec.ValueRef{"what": literalRef("it")}},
			},
		},
	}

	cfg := ActionExecutionConfig{DefaultTimeout: 5 * time.Second, GracePeriod: time.Second}
	result, err := Actions(context.Background(), sol, nil, testRegistry(t), cfg)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Result)
	assert.Equal(t, action.ExecutionSucceeded, result.Result.FinalStatus)
}

// TestActions_CallCelArgsNamespace guards that a call invoked from an action
// exposes the args namespace to providers that evaluate against the resolver
// context (e.g. cel), not only to templates resolved during input binding.
// Regression: _.args.* in an action call's cel provider previously failed with
// "no such key: args" because the enriched resolver context was not installed
// on the provider execution context.
func TestActions_CallCelArgsNamespace(t *testing.T) {
	sol := &solution.Solution{}
	sol.Metadata.Name = "call-action-cel"
	sol.Spec.Calls = map[string]*spec.Call{
		"announce": {
			Provider: "cel",
			Inputs:   map[string]*spec.ValueRef{"expression": literalRef("'reached-' + _.args.stage")},
			Args:     map[string]*spec.ArgDef{"stage": {Type: spec.TypeString, Required: true}},
		},
	}
	sol.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"run": {
				Name:    "run",
				CallRef: spec.CallRef{Call: "announce", Args: map[string]*spec.ValueRef{"stage": literalRef("build")}},
			},
		},
	}

	cfg := ActionExecutionConfig{DefaultTimeout: 5 * time.Second, GracePeriod: time.Second}
	result, err := Actions(context.Background(), sol, nil, testRegistry(t), cfg)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Result)
	assert.Equal(t, action.ExecutionSucceeded, result.Result.FinalStatus)
	require.Contains(t, result.Result.Actions, "run")
	assert.Equal(t, "reached-build", result.Result.Actions["run"].Results, "cel provider in an action call must read _.args.* from the enriched resolver context")
}
