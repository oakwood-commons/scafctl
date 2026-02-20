// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"context"
	"regexp"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProvider implements provider.Provider for testing.
type fakeProvider struct {
	desc *provider.Descriptor
}

func (f *fakeProvider) Descriptor() *provider.Descriptor { return f.desc }

func (f *fakeProvider) Execute(_ context.Context, _ any) (*provider.Output, error) {
	return nil, nil
}

func newFakeProvider(name string, props map[string]*jsonschema.Schema) *fakeProvider {
	return &fakeProvider{
		desc: &provider.Descriptor{
			Name:       name,
			APIVersion: "v1",
			Version:    semver.MustParse("1.0.0"),
			Schema: &jsonschema.Schema{
				Type:       "object",
				Properties: props,
			},
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom: {Type: "object"},
			},
			Description:  "Test provider",
			MockBehavior: "Returns test data",
			Capabilities: []provider.Capability{provider.CapabilityFrom},
		},
	}
}

func TestLintProviderInputs_UnknownInput(t *testing.T) {
	reg := provider.NewRegistry()
	fp := newFakeProvider("test-provider", map[string]*jsonschema.Schema{
		"name": {Type: "string"},
		"url":  {Type: "string"},
	})
	require.NoError(t, reg.Register(fp))

	expr := celexp.Expression("_.env")
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"data": {
					Name: "data",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "test-provider",
								Inputs: map[string]*spec.ValueRef{
									"name":          {Literal: "hello"},
									"unknown_field": {Literal: "oops"},
									"url":           {Expr: &expr},
								},
							},
						},
					},
				},
			},
		},
	}

	result := &Result{Findings: make([]*Finding, 0)}
	lintProviderInputs(sol, result, reg)

	var unknownFindings []*Finding
	for _, f := range result.Findings {
		if f.RuleName == "unknown-provider-input" {
			unknownFindings = append(unknownFindings, f)
		}
	}
	require.Len(t, unknownFindings, 1)
	assert.Contains(t, unknownFindings[0].Message, "unknown_field")
	assert.Contains(t, unknownFindings[0].Message, "test-provider")
}

func TestLintProviderInputs_InvalidLiteralType(t *testing.T) {
	reg := provider.NewRegistry()
	fp := newFakeProvider("test-provider", map[string]*jsonschema.Schema{
		"count": {Type: "integer"},
	})
	require.NoError(t, reg.Register(fp))

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"data": {
					Name: "data",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "test-provider",
								Inputs: map[string]*spec.ValueRef{
									"count": {Literal: "not-a-number"},
								},
							},
						},
					},
				},
			},
		},
	}

	result := &Result{Findings: make([]*Finding, 0)}
	lintProviderInputs(sol, result, reg)

	var typeFindings []*Finding
	for _, f := range result.Findings {
		if f.RuleName == "invalid-provider-input-type" {
			typeFindings = append(typeFindings, f)
		}
	}
	require.Len(t, typeFindings, 1)
	assert.Contains(t, typeFindings[0].Message, "count")
}

func TestLintProviderInputs_ValidLiteral(t *testing.T) {
	reg := provider.NewRegistry()
	fp := newFakeProvider("test-provider", map[string]*jsonschema.Schema{
		"name": {Type: "string"},
	})
	require.NoError(t, reg.Register(fp))

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"data": {
					Name: "data",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "test-provider",
								Inputs: map[string]*spec.ValueRef{
									"name": {Literal: "valid-string"},
								},
							},
						},
					},
				},
			},
		},
	}

	result := &Result{Findings: make([]*Finding, 0)}
	lintProviderInputs(sol, result, reg)

	for _, f := range result.Findings {
		assert.NotEqual(t, "unknown-provider-input", f.RuleName)
		assert.NotEqual(t, "invalid-provider-input-type", f.RuleName)
	}
}

func TestLintProviderInputs_SkipsDynamicValues(t *testing.T) {
	reg := provider.NewRegistry()
	fp := newFakeProvider("test-provider", map[string]*jsonschema.Schema{
		"count": {Type: "integer"},
	})
	require.NoError(t, reg.Register(fp))

	expr := celexp.Expression("1 + 2")
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"data": {
					Name: "data",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "test-provider",
								Inputs: map[string]*spec.ValueRef{
									"count": {Expr: &expr},
								},
							},
						},
					},
				},
			},
		},
	}

	result := &Result{Findings: make([]*Finding, 0)}
	lintProviderInputs(sol, result, reg)

	for _, f := range result.Findings {
		assert.NotEqual(t, "invalid-provider-input-type", f.RuleName)
	}
}

func TestLintProviderInputs_ActionInputs(t *testing.T) {
	reg := provider.NewRegistry()
	fp := newFakeProvider("shell", map[string]*jsonschema.Schema{
		"command": {Type: "string"},
	})
	fp.desc.Capabilities = []provider.Capability{provider.CapabilityAction}
	fp.desc.OutputSchemas = map[provider.Capability]*jsonschema.Schema{
		provider.CapabilityAction: {
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"success": {Type: "boolean"},
			},
			Required: []string{"success"},
		},
	}
	require.NoError(t, reg.Register(fp))

	sol := &solution.Solution{
		Spec: solution.Spec{
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"run": {
						Name:     "run",
						Provider: "shell",
						Inputs: map[string]*spec.ValueRef{
							"command":     {Literal: "echo hello"},
							"bogus_input": {Literal: "oops"},
						},
					},
				},
			},
		},
	}

	result := &Result{Findings: make([]*Finding, 0)}
	lintProviderInputs(sol, result, reg)

	var unknownFindings []*Finding
	for _, f := range result.Findings {
		if f.RuleName == "unknown-provider-input" {
			unknownFindings = append(unknownFindings, f)
		}
	}
	require.Len(t, unknownFindings, 1)
	assert.Contains(t, unknownFindings[0].Message, "bogus_input")
}

func TestLintProviderInputs_MissingProviderSkipped(t *testing.T) {
	reg := provider.NewRegistry()

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"data": {
					Name: "data",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "nonexistent",
								Inputs: map[string]*spec.ValueRef{
									"key": {Literal: "value"},
								},
							},
						},
					},
				},
			},
		},
	}

	result := &Result{Findings: make([]*Finding, 0)}
	lintProviderInputs(sol, result, reg)

	for _, f := range result.Findings {
		assert.NotEqual(t, "unknown-provider-input", f.RuleName)
	}
}

func TestScanInputsForResolverRefs_DirectRslvr(t *testing.T) {
	refs := make(map[string]bool)
	pattern := regexp.MustCompile(`_\.([a-zA-Z_][a-zA-Z0-9_]*)`)

	resolverName := "myResolver"
	inputs := map[string]*spec.ValueRef{
		"field": {Resolver: &resolverName},
	}

	scanInputsForResolverRefs(inputs, pattern, refs)
	assert.True(t, refs["myResolver"], "should detect direct rslvr reference")
}

func TestScanInputsForResolverRefs_NestedRslvrInLiteral(t *testing.T) {
	refs := make(map[string]bool)
	pattern := regexp.MustCompile(`_\.([a-zA-Z_][a-zA-Z0-9_]*)`)

	// Simulates: value: { body: { rslvr: "emailBody" } }
	inputs := map[string]*spec.ValueRef{
		"value": {
			Literal: map[string]any{
				"body": map[string]any{
					"rslvr": "emailBody",
				},
				"subject": map[string]any{
					"rslvr": "emailSubject",
				},
			},
		},
	}

	scanInputsForResolverRefs(inputs, pattern, refs)
	assert.True(t, refs["emailBody"], "should detect nested rslvr in literal map")
	assert.True(t, refs["emailSubject"], "should detect nested rslvr in literal map")
}

func TestScanInputsForResolverRefs_NestedExprInLiteral(t *testing.T) {
	refs := make(map[string]bool)
	pattern := regexp.MustCompile(`_\.([a-zA-Z_][a-zA-Z0-9_]*)`)

	// Simulates: value: { ts: { expr: "_.timestamp" } }
	inputs := map[string]*spec.ValueRef{
		"value": {
			Literal: map[string]any{
				"ts": map[string]any{
					"expr": "string(_.timestamp)",
				},
			},
		},
	}

	scanInputsForResolverRefs(inputs, pattern, refs)
	assert.True(t, refs["timestamp"], "should detect resolver ref in nested expr")
}

func TestScanInputsForResolverRefs_NestedInArray(t *testing.T) {
	refs := make(map[string]bool)
	pattern := regexp.MustCompile(`_\.([a-zA-Z_][a-zA-Z0-9_]*)`)

	// Simulates: args: [ { rslvr: "env" } ]
	inputs := map[string]*spec.ValueRef{
		"args": {
			Literal: []any{
				map[string]any{"rslvr": "env"},
			},
		},
	}

	scanInputsForResolverRefs(inputs, pattern, refs)
	assert.True(t, refs["env"], "should detect rslvr inside array elements")
}
