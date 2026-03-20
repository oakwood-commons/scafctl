// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/duration"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
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

func TestCollectReferencedResolvers_DirectRslvr(t *testing.T) {
	resolverName := "myResolver"
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"source": {
					Name: "source",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "parameter",
							Inputs: map[string]*spec.ValueRef{
								"field": {Resolver: &resolverName},
							},
						}},
					},
				},
			},
		},
	}

	refs := collectReferencedResolvers(sol)
	assert.True(t, refs["myResolver"], "should detect direct rslvr reference")
}

func TestCollectReferencedResolvers_NestedRslvrInLiteral(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"source": {
					Name: "source",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "parameter",
							Inputs: map[string]*spec.ValueRef{
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
							},
						}},
					},
				},
			},
		},
	}

	refs := collectReferencedResolvers(sol)
	assert.True(t, refs["emailBody"], "should detect nested rslvr in literal map")
	assert.True(t, refs["emailSubject"], "should detect nested rslvr in literal map")
}

func TestCollectReferencedResolvers_NestedExprInLiteral(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"source": {
					Name: "source",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "parameter",
							Inputs: map[string]*spec.ValueRef{
								"value": {
									Literal: map[string]any{
										"ts": map[string]any{
											"expr": "string(_.timestamp)",
										},
									},
								},
							},
						}},
					},
				},
			},
		},
	}

	refs := collectReferencedResolvers(sol)
	assert.True(t, refs["timestamp"], "should detect resolver ref in nested expr")
}

func TestCollectReferencedResolvers_NestedInArray(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"source": {
					Name: "source",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "parameter",
							Inputs: map[string]*spec.ValueRef{
								"args": {
									Literal: []any{
										map[string]any{"rslvr": "env"},
									},
								},
							},
						}},
					},
				},
			},
		},
	}

	refs := collectReferencedResolvers(sol)
	assert.True(t, refs["env"], "should detect rslvr inside array elements")
}

func TestLintResolverSelfReferences(t *testing.T) {
	validationProv := newFakeProvider("validation", map[string]*jsonschema.Schema{
		"expression": {Type: "string"},
	})
	celProv := newFakeProvider("cel", map[string]*jsonschema.Schema{
		"expression": {Type: "string"},
	})
	staticProv := newFakeProvider("static", map[string]*jsonschema.Schema{
		"value": {Type: "string"},
	})

	reg := provider.NewRegistry()
	_ = reg.Register(validationProv)
	_ = reg.Register(celProv)
	_ = reg.Register(staticProv)

	selfExpr := celexp.Expression("_.publicSiteCheck.statusCode == 200")
	correctExpr := celexp.Expression("__self.statusCode == 200")
	otherExpr := celexp.Expression("_.otherResolver.value == 'ok'")

	tests := []struct {
		name          string
		resolverName  string
		resolver      *resolver.Resolver
		expectFinding bool
		findingRule   string
	}{
		{
			name:         "validate self-reference via _.name triggers finding",
			resolverName: "publicSiteCheck",
			resolver: &resolver.Resolver{
				Type: "object",
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: "test"}}}},
				},
				Validate: &resolver.ValidatePhase{
					With: []resolver.ProviderValidation{{
						Provider: "validation",
						Inputs:   map[string]*spec.ValueRef{"expression": {Expr: &selfExpr}},
					}},
				},
			},
			expectFinding: true,
			findingRule:   "resolver-self-reference",
		},
		{
			name:         "validate using __self is clean",
			resolverName: "publicSiteCheck",
			resolver: &resolver.Resolver{
				Type: "object",
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: "test"}}}},
				},
				Validate: &resolver.ValidatePhase{
					With: []resolver.ProviderValidation{{
						Provider: "validation",
						Inputs:   map[string]*spec.ValueRef{"expression": {Expr: &correctExpr}},
					}},
				},
			},
			expectFinding: false,
		},
		{
			name:         "validate referencing other resolver is fine",
			resolverName: "publicSiteCheck",
			resolver: &resolver.Resolver{
				Type: "object",
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: "test"}}}},
				},
				Validate: &resolver.ValidatePhase{
					With: []resolver.ProviderValidation{{
						Provider: "validation",
						Inputs:   map[string]*spec.ValueRef{"expression": {Expr: &otherExpr}},
					}},
				},
			},
			expectFinding: false,
		},
		{
			name:         "transform self-reference via _.name triggers finding",
			resolverName: "myValue",
			resolver: &resolver.Resolver{
				Type: "string",
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: "test"}}}},
				},
				Transform: &resolver.TransformPhase{
					With: []resolver.ProviderTransform{{
						Provider: "cel",
						Inputs:   map[string]*spec.ValueRef{"expression": {Expr: exprPtr("_.myValue + '-suffix'")}},
					}},
				},
			},
			expectFinding: true,
			findingRule:   "resolver-self-reference",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sol := &solution.Solution{
				Spec: solution.Spec{
					Resolvers: map[string]*resolver.Resolver{
						tt.resolverName: tt.resolver,
					},
				},
			}

			result := Solution(sol, "test.yaml", reg)

			selfRefFindings := filterFindingsByRule(result, tt.findingRule)
			if tt.expectFinding {
				require.NotEmpty(t, selfRefFindings, "expected resolver-self-reference finding")
				assert.Contains(t, selfRefFindings[0].Message, tt.resolverName)
			} else {
				assert.Empty(t, selfRefFindings, "expected no resolver-self-reference finding")
			}
		})
	}
}

func exprPtr(s string) *celexp.Expression {
	e := celexp.Expression(s)
	return &e
}

func filterFindingsByRule(result *Result, rule string) []*Finding {
	if rule == "" {
		return nil
	}
	var out []*Finding
	for _, f := range result.Findings {
		if f.RuleName == rule {
			out = append(out, f)
		}
	}
	return out
}

func TestLintNilProviderInput(t *testing.T) {
	staticProv := newFakeProvider("static", map[string]*jsonschema.Schema{
		"value": {Type: "string"},
	})
	reg := provider.NewRegistry()
	_ = reg.Register(staticProv)

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"has-nil-input": {
					Type: "string",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "static",
							Inputs: map[string]*spec.ValueRef{
								"value":        {Literal: "ok"},
								"dangling-key": nil,
							},
						}},
					},
				},
			},
		},
	}

	result := Solution(sol, "test.yaml", reg)

	findings := filterFindingsByRule(result, "nil-provider-input")
	require.Len(t, findings, 1)
	assert.Contains(t, findings[0].Message, "dangling-key")
	assert.Contains(t, findings[0].Message, "no value")
}

func TestLintEmptyTransformWith(t *testing.T) {
	staticProv := newFakeProvider("static", map[string]*jsonschema.Schema{
		"value": {Type: "string"},
	})
	reg := provider.NewRegistry()
	_ = reg.Register(staticProv)

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"empty-transform": {
					Type: "string",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "static",
							Inputs:   map[string]*spec.ValueRef{"value": {Literal: "ok"}},
						}},
					},
					Transform: &resolver.TransformPhase{
						With: []resolver.ProviderTransform{},
					},
				},
			},
		},
	}

	result := Solution(sol, "test.yaml", reg)

	findings := filterFindingsByRule(result, "empty-transform-with")
	require.Len(t, findings, 1)
	assert.Contains(t, findings[0].Message, "empty")
}

func TestLintEmptyValidateWith(t *testing.T) {
	staticProv := newFakeProvider("static", map[string]*jsonschema.Schema{
		"value": {Type: "string"},
	})
	reg := provider.NewRegistry()
	_ = reg.Register(staticProv)

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"empty-validate": {
					Type: "string",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "static",
							Inputs:   map[string]*spec.ValueRef{"value": {Literal: "ok"}},
						}},
					},
					Validate: &resolver.ValidatePhase{
						With: []resolver.ProviderValidation{},
					},
				},
			},
		},
	}

	result := Solution(sol, "test.yaml", reg)

	findings := filterFindingsByRule(result, "empty-validate-with")
	require.Len(t, findings, 1)
	assert.Contains(t, findings[0].Message, "empty")
}

func TestLintNullResolverValue(t *testing.T) {
	reg := provider.NewRegistry()
	staticProv := newFakeProvider("static", map[string]*jsonschema.Schema{
		"value": {Type: "string"},
	})
	_ = reg.Register(staticProv)

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"map": nil,
				"hello": {
					Type: "string",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "static",
							Inputs:   map[string]*spec.ValueRef{"value": {Literal: "world"}},
						}},
					},
				},
			},
		},
	}

	result := Solution(sol, "test.yaml", reg)

	findings := filterFindingsByRule(result, "null-resolver")
	require.Len(t, findings, 1)
	assert.Equal(t, SeverityError, findings[0].Severity)
	assert.Contains(t, findings[0].Message, "null value")
	assert.Contains(t, findings[0].Location, "map")
}

func BenchmarkLintNullResolver(b *testing.B) {
	reg := provider.NewRegistry()
	staticProv := newFakeProvider("static", map[string]*jsonschema.Schema{
		"value": {Type: "string"},
	})
	_ = reg.Register(staticProv)

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"null_resolver": nil,
				"valid": {
					Type: "string",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "static",
							Inputs:   map[string]*spec.ValueRef{"value": {Literal: "ok"}},
						}},
					},
				},
			},
		},
	}

	b.ResetTimer()
	for b.Loop() {
		_ = Solution(sol, "bench.yaml", reg)
	}
}

func TestLintSchema_FileNotFound(t *testing.T) {
	result := &Result{Findings: make([]*Finding, 0)}
	lintSchema("/nonexistent/path/solution.yaml", result)
	// Should silently skip when file cannot be read
	assert.Empty(t, result.Findings)
}

func TestLintSchema_ValidYAML(t *testing.T) {
	// Write a valid minimal YAML file (empty map)
	tmpFile := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(tmpFile, []byte("name: test\nkind: Solution\napiVersion: v1\n"), 0o600))
	result := &Result{Findings: make([]*Finding, 0)}
	lintSchema(tmpFile, result)
	// Might have findings or none, but should not panic
}

func TestLintSchema_InvalidYAML(t *testing.T) {
	// Write invalid YAML
	tmpFile := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(tmpFile, []byte("{\ninvalid: [yaml\n"), 0o600))
	result := &Result{Findings: make([]*Finding, 0)}
	lintSchema(tmpFile, result)
	// Should silently skip invalid YAML
	assert.Empty(t, result.Findings)
}

func TestLintExpressions_AllPaths(t *testing.T) {
	tmplExpr := gotmpl.GoTemplatingContent("{{.InvalidTemplate")
	celExpr := celexp.Expression("invalid === CEL")
	celEmpty := celexp.Expression("")
	tmplEmpty := gotmpl.GoTemplatingContent("")

	inputs := map[string]*spec.ValueRef{
		"nilVal":      nil,
		"emptyExpr":   {Expr: &celEmpty},
		"emptyTmpl":   {Tmpl: &tmplEmpty},
		"invalidExpr": {Expr: &celExpr},
		"invalidTmpl": {Tmpl: &tmplExpr},
	}

	result := &Result{Findings: make([]*Finding, 0)}
	lintExpressions(inputs, "test.resolvers.myresolver", result)

	// Should have findings for invalid CEL and invalid template
	findingRules := make([]string, 0, len(result.Findings))
	for _, f := range result.Findings {
		findingRules = append(findingRules, f.RuleName)
	}
	assert.Contains(t, findingRules, "invalid-expression")
	assert.Contains(t, findingRules, "invalid-template")
}

func TestLintSolution_NoResolversNoWorkflow(t *testing.T) {
	reg := provider.NewRegistry()
	sol := &solution.Solution{
		Spec: solution.Spec{},
	}
	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "empty-solution")
	require.Len(t, findings, 1)
	assert.Equal(t, SeverityError, findings[0].Severity)
}

func TestLintResolverReservedName(t *testing.T) {
	reg := provider.NewRegistry()
	staticProv := newFakeProvider("static", map[string]*jsonschema.Schema{
		"value": {Type: "string"},
	})
	_ = reg.Register(staticProv)

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"__actions": {
					Type: "string",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "static",
							Inputs:   map[string]*spec.ValueRef{"value": {Literal: "x"}},
						}},
					},
				},
			},
		},
	}

	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "reserved-name")
	require.Len(t, findings, 1)
	assert.Contains(t, findings[0].Message, "__actions")
}

func TestLintResolvers_NilResolvers(t *testing.T) {
	reg := provider.NewRegistry()
	// lintResolvers should early-return when Resolvers is nil
	// Use a solution with a workflow only so we don't hit empty-solution
	sol := &solution.Solution{
		Spec: solution.Spec{
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"my-action": {
						Name:        "my-action",
						Description: "does something",
						Provider:    "",
					},
				},
			},
		},
	}
	result := Solution(sol, "test.yaml", reg)
	// No reserved-name or null-resolver findings expected
	for _, f := range result.Findings {
		assert.NotEqual(t, "reserved-name", f.RuleName)
		assert.NotEqual(t, "null-resolver", f.RuleName)
	}
}

func TestLintResolvers_MissingProviderInResolveStep(t *testing.T) {
	reg := provider.NewRegistry()
	// The provider "nonexistent" is not registered
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"data": {
					Type:        "string",
					Description: "fetches data",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "nonexistent",
							Inputs:   map[string]*spec.ValueRef{},
						}},
					},
				},
			},
		},
	}
	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "missing-provider")
	assert.NotEmpty(t, findings)
}

func TestLintResolverSelfReferences_MessageExprAndTmpl(t *testing.T) {
	reg := provider.NewRegistry()
	staticProv := newFakeProvider("static", map[string]*jsonschema.Schema{
		"value": {Type: "string"},
	})
	_ = reg.Register(staticProv)

	celExpr := celexp.Expression("_.myresolver.isValid()")
	tmplExpr := gotmpl.GoTemplatingContent("{{_.myresolver}}")

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"myresolver": {
					Type:        "string",
					Description: "with self-refs",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "static",
							Inputs:   map[string]*spec.ValueRef{"value": {Literal: "x"}},
						}},
					},
					Validate: &resolver.ValidatePhase{
						With: []resolver.ProviderValidation{
							{
								Provider: "static",
								Inputs:   map[string]*spec.ValueRef{"value": {Literal: "ok"}},
								Message: &spec.ValueRef{
									Expr: &celExpr,
								},
							},
							{
								Provider: "static",
								Inputs:   map[string]*spec.ValueRef{"value": {Literal: "ok"}},
								Message: &spec.ValueRef{
									Tmpl: &tmplExpr,
								},
							},
						},
					},
				},
			},
		},
	}

	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "resolver-self-reference")
	assert.GreaterOrEqual(t, len(findings), 2)
}

func TestLintWorkflow_FinallyWithForEach(t *testing.T) {
	reg := provider.NewRegistry()
	celExpr := celexp.Expression("['a','b']")

	sol := &solution.Solution{
		Spec: solution.Spec{
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"main": {
						Name:        "main",
						Description: "main action",
					},
				},
				Finally: map[string]*action.Action{
					"cleanup": {
						Name:        "cleanup",
						Description: "cleanup action",
						ForEach: &spec.ForEachClause{
							In: &spec.ValueRef{Expr: &celExpr},
						},
					},
				},
			},
		},
	}

	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "finally-with-foreach")
	require.Len(t, findings, 1)
	assert.Contains(t, findings[0].Location, "cleanup")
}

func TestLintAction_MissingProviderAndLongTimeout(t *testing.T) {
	reg := provider.NewRegistry()

	longTimeout := duration.New(15 * time.Minute)

	sol := &solution.Solution{
		Spec: solution.Spec{
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"deploy": {
						Name:        "deploy",
						Description: "deploys",
						Provider:    "nonexistent-provider",
						Timeout:     &longTimeout,
					},
				},
			},
		},
	}

	result := Solution(sol, "test.yaml", reg)

	// Should have missing-provider finding
	mpFindings := filterFindingsByRule(result, "missing-provider")
	assert.NotEmpty(t, mpFindings)

	// Should have long-timeout finding
	ltFindings := filterFindingsByRule(result, "long-timeout")
	assert.NotEmpty(t, ltFindings)
}

func TestLintProviderInputsForStep_EmptyProviderName(t *testing.T) {
	reg := provider.NewRegistry()
	result := &Result{Findings: make([]*Finding, 0)}
	// providerName="" → early return → no findings
	lintProviderInputsForStep("", map[string]*spec.ValueRef{"key": {Literal: "val"}}, "loc", result, reg)
	assert.Empty(t, result.Findings)
}

func TestLintProviderInputsForStep_NilInputs(t *testing.T) {
	reg := provider.NewRegistry()
	result := &Result{Findings: make([]*Finding, 0)}
	// inputs=nil → early return → no findings
	lintProviderInputsForStep("static", nil, "loc", result, reg)
	assert.Empty(t, result.Findings)
}

func TestLintProviderInputsForStep_AdditionalProperties(t *testing.T) {
	// Provider schema allows additional properties → unknown keys are skipped
	prov := &fakeProvider{
		desc: &provider.Descriptor{
			Name:       "flexible",
			APIVersion: "v1",
			Version:    semver.MustParse("1.0.0"),
			Schema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"known": {Type: "string"},
				},
				AdditionalProperties: &jsonschema.Schema{},
			},
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom: {Type: "object"},
			},
			Description:  "flexible provider",
			Capabilities: []provider.Capability{provider.CapabilityFrom},
		},
	}
	reg := provider.NewRegistry()
	_ = reg.Register(prov)

	result := &Result{Findings: make([]*Finding, 0)}
	lintProviderInputsForStep("flexible", map[string]*spec.ValueRef{
		"unknown-key": {Literal: "value"},
	}, "loc", result, reg)

	// No unknown-provider-input should be reported because additionalProperties allows it
	for _, f := range result.Findings {
		assert.NotEqual(t, "unknown-provider-input", f.RuleName)
	}
}

func TestLintProviderInputsForStep_ExecCommandInjection(t *testing.T) {
	// Create an exec provider (or a provider named "exec")
	celExpr := celexp.Expression("_.myresolver")
	execProv := &fakeProvider{
		desc: &provider.Descriptor{
			Name:       "exec",
			APIVersion: "v1",
			Version:    semver.MustParse("1.0.0"),
			Schema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"command": {Type: "string"},
					"args":    {Type: "array"},
				},
			},
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom: {Type: "object"},
			},
			Description:  "exec provider",
			Capabilities: []provider.Capability{provider.CapabilityFrom},
		},
	}
	reg := provider.NewRegistry()
	_ = reg.Register(execProv)

	result := &Result{Findings: make([]*Finding, 0)}
	lintProviderInputsForStep("exec", map[string]*spec.ValueRef{
		"command": {Expr: &celExpr},
	}, "resolvers.myresolver.resolve.with[0]", result, reg)

	findings := filterFindingsByRule(result, "exec-command-injection")
	assert.Len(t, findings, 1)
}
