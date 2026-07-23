// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/duration"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	gotmplext "github.com/oakwood-commons/scafctl/pkg/gotmpl/ext"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initExtensionFactory ensures sprig + custom Go template functions are
// registered for lint validation tests. In production this is done by
// RegisterDefaults(); in tests we call the factory directly.
//
// Note: SetExtensionFuncMapFactory is guarded by sync.Once so we call it
// in TestMain to guarantee deterministic setup before any test runs.
func TestMain(m *testing.M) {
	gotmpl.SetExtensionFuncMapFactory(gotmplext.AllFuncMap)
	os.Exit(m.Run())
}

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

func TestCollectReferencedResolvers_StringLiteralWithResolverRef(t *testing.T) {
	// CEL provider's `expression` input is a plain string literal containing
	// _.resolverName and has(_.resolverName) patterns. These must be detected.
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"localIpWindows": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "exec"}},
					},
				},
				"localIpUnix": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "exec"}},
					},
				},
				"localIp": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "cel",
							Inputs: map[string]*spec.ValueRef{
								"expression": {
									Literal: `has(_.localIpWindows) ? _.localIpWindows : has(_.localIpUnix) ? _.localIpUnix : ""`,
								},
							},
						}},
					},
				},
			},
		},
	}

	refs := collectReferencedResolvers(sol)
	assert.True(t, refs["localIpWindows"], "should detect resolver ref in string literal (has pattern)")
	assert.True(t, refs["localIpUnix"], "should detect resolver ref in string literal (has pattern)")
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

func TestLintUndefinedOptionalReference(t *testing.T) {
	staticProv := newFakeProvider("static", map[string]*jsonschema.Schema{
		"value": {Type: "string"},
	})
	celProv := newFakeProvider("cel", map[string]*jsonschema.Schema{
		"expression": {Type: "string"},
	})
	reg := provider.NewRegistry()
	_ = reg.Register(staticProv)
	_ = reg.Register(celProv)

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"profile": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "static",
							Inputs:   map[string]*spec.ValueRef{"value": {Literal: "p"}},
						}},
					},
				},
				"app": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "cel",
							// Optional reference to a defined resolver (profile) must not be
							// flagged; optional reference to an undefined resolver (missing)
							// must produce a single INFO finding.
							Inputs: map[string]*spec.ValueRef{
								"expression": {Expr: exprPtr(`_.?profile.orValue("") + _.?missing.orValue("")`)},
							},
						}},
					},
				},
			},
		},
	}

	result := Solution(sol, "test.yaml", reg)

	findings := filterFindingsByRule(result, "undefined-optional-reference")
	require.Len(t, findings, 1)
	assert.Equal(t, SeverityInfo, findings[0].Severity)
	assert.Contains(t, findings[0].Message, "missing")
	assert.NotContains(t, findings[0].Message, "profile")
}

func TestLintUndefinedOptionalReference_HardRefNotFlagged(t *testing.T) {
	// A hard reference to a defined resolver must never produce an
	// undefined-optional-reference finding.
	staticProv := newFakeProvider("static", map[string]*jsonschema.Schema{
		"value": {Type: "string"},
	})
	celProv := newFakeProvider("cel", map[string]*jsonschema.Schema{
		"expression": {Type: "string"},
	})
	reg := provider.NewRegistry()
	_ = reg.Register(staticProv)
	_ = reg.Register(celProv)

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"profile": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "static",
							Inputs:   map[string]*spec.ValueRef{"value": {Literal: "p"}},
						}},
					},
				},
				"app": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "cel",
							Inputs: map[string]*spec.ValueRef{
								"expression": {Expr: exprPtr(`_.profile`)},
							},
						}},
					},
				},
			},
		},
	}

	result := Solution(sol, "test.yaml", reg)

	findings := filterFindingsByRule(result, "undefined-optional-reference")
	assert.Empty(t, findings)
}

func TestLintUndefinedOptionalReference_InjectedKeyNotFlagged(t *testing.T) {
	// Optional access to an injected context key (e.g. __plan) reachable through
	// the `_` map must not be reported as an undefined resolver.
	staticProv := newFakeProvider("static", map[string]*jsonschema.Schema{
		"value": {Type: "string"},
	})
	celProv := newFakeProvider("cel", map[string]*jsonschema.Schema{
		"expression": {Type: "string"},
	})
	reg := provider.NewRegistry()
	_ = reg.Register(staticProv)
	_ = reg.Register(celProv)

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"app": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "cel",
							Inputs: map[string]*spec.ValueRef{
								"expression": {Expr: exprPtr(`_[?"__plan"].orValue({})`)},
							},
						}},
					},
				},
			},
		},
	}

	result := Solution(sol, "test.yaml", reg)

	findings := filterFindingsByRule(result, "undefined-optional-reference")
	assert.Empty(t, findings)
}

func TestLintUndefinedOptionalReference_NoResolvers(t *testing.T) {
	// A solution with no spec.resolvers block must still have optional
	// references (here in a workflow action's `when` condition) checked
	// against the empty defined set, so an undefined optional reference is
	// reported instead of being silently skipped.
	reg := provider.NewRegistry()
	whenExpr := celexp.Expression(`_.?missing.orValue(false)`)

	sol := &solution.Solution{
		Spec: solution.Spec{
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"main": {
						Name:        "main",
						Description: "main action",
						When:        &spec.Condition{Expr: &whenExpr},
					},
				},
			},
		},
	}

	result := Solution(sol, "test.yaml", reg)

	findings := filterFindingsByRule(result, "undefined-optional-reference")
	require.Len(t, findings, 1)
	assert.Equal(t, SeverityInfo, findings[0].Severity)
	assert.Contains(t, findings[0].Message, "missing")
}

func TestLintResolverUndefinedDependency(t *testing.T) {
	staticProv := newFakeProvider("static", map[string]*jsonschema.Schema{
		"value": {Type: "string"},
	})

	newSol := func(deps []string) *solution.Solution {
		return &solution.Solution{
			Spec: solution.Spec{
				Resolvers: map[string]*resolver.Resolver{
					"base": {
						Resolve: &resolver.ResolvePhase{
							With: []resolver.ProviderSource{{
								Provider: "static",
								Inputs:   map[string]*spec.ValueRef{"value": {Literal: "b"}},
							}},
						},
					},
					"app": {
						DependsOn: deps,
						Resolve: &resolver.ResolvePhase{
							With: []resolver.ProviderSource{{
								Provider: "static",
								Inputs:   map[string]*spec.ValueRef{"value": {Literal: "a"}},
							}},
						},
					},
				},
			},
		}
	}

	reg := provider.NewRegistry()
	_ = reg.Register(staticProv)

	tests := []struct {
		name          string
		deps          []string
		wantFindings  int
		wantSubstring string
	}{
		{name: "defined dependency is not flagged", deps: []string{"base"}, wantFindings: 0},
		{name: "undefined dependency is flagged", deps: []string{"doesNotExist"}, wantFindings: 1, wantSubstring: "doesNotExist"},
		{name: "empty dependency is flagged", deps: []string{""}, wantFindings: 1, wantSubstring: "empty"},
		{name: "self dependency is flagged", deps: []string{"app"}, wantFindings: 1, wantSubstring: "itself"},
		{name: "multiple undefined deps are all flagged", deps: []string{"missingA", "missingB"}, wantFindings: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Solution(newSol(tt.deps), "test.yaml", reg)
			findings := filterFindingsByRule(result, "resolver-undefined-dependency")
			require.Len(t, findings, tt.wantFindings)
			for _, f := range findings {
				assert.Equal(t, SeverityError, f.Severity)
			}
			if tt.wantSubstring != "" {
				require.Len(t, findings, 1)
				assert.Contains(t, findings[0].Message, tt.wantSubstring)
			}
		})
	}
}

func TestLintResolverUndefinedDependency_NilResolver(t *testing.T) {
	staticProv := newFakeProvider("static", map[string]*jsonschema.Schema{
		"value": {Type: "string"},
	})
	reg := provider.NewRegistry()
	_ = reg.Register(staticProv)

	// A nil resolver entry must be skipped without panicking and must not
	// produce a resolver-undefined-dependency finding.
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"nilResolver": nil,
				"app": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "static",
							Inputs:   map[string]*spec.ValueRef{"value": {Literal: "a"}},
						}},
					},
				},
			},
		},
	}

	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "resolver-undefined-dependency")
	assert.Empty(t, findings)
}

func TestLintResolverUndefinedDependency_NilTargetFlagged(t *testing.T) {
	staticProv := newFakeProvider("static", map[string]*jsonschema.Schema{
		"value": {Type: "string"},
	})
	reg := provider.NewRegistry()
	_ = reg.Register(staticProv)

	// A dependsOn reference to a resolver whose value is null must be flagged:
	// a null resolver is not a valid dependency target and execution fails.
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"nilResolver": nil,
				"app": {
					DependsOn: []string{"nilResolver"},
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "static",
							Inputs:   map[string]*spec.ValueRef{"value": {Literal: "a"}},
						}},
					},
				},
			},
		},
	}

	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "resolver-undefined-dependency")
	require.Len(t, findings, 1)
	assert.Equal(t, SeverityError, findings[0].Severity)
	assert.Contains(t, findings[0].Message, "nilResolver")
}

func TestLintNonValidationProvider(t *testing.T) {
	// "static" has CapabilityFrom, not CapabilityValidation
	staticProv := newFakeProvider("static", map[string]*jsonschema.Schema{
		"value": {Type: "string"},
	})
	validationProv := newFakeProvider("validation", map[string]*jsonschema.Schema{
		"expression": {Type: "string"},
	})
	validationProv.desc.Capabilities = []provider.Capability{provider.CapabilityValidation}

	reg := provider.NewRegistry()
	_ = reg.Register(staticProv)
	_ = reg.Register(validationProv)

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"test-resolver": {
					Type: "string",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "static",
							Inputs:   map[string]*spec.ValueRef{"value": {Literal: "ok"}},
						}},
					},
					Validate: &resolver.ValidatePhase{
						With: []resolver.ProviderValidation{
							{
								Provider: "static",
								Inputs:   map[string]*spec.ValueRef{"expression": {Literal: "true"}},
							},
							{
								Provider: "validation",
								Inputs:   map[string]*spec.ValueRef{"expression": {Literal: "true"}},
							},
						},
					},
				},
			},
		},
	}

	result := Solution(sol, "test.yaml", reg)

	findings := filterFindingsByRule(result, "non-validation-provider")
	require.Len(t, findings, 1)
	assert.Contains(t, findings[0].Message, "static")
	assert.Contains(t, findings[0].Message, "does not declare validation capability")
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
	lintSchema(nil, "/nonexistent/path/solution.yaml", result)
	// Should silently skip when there is no raw content and the file cannot be read.
	assert.Empty(t, result.Findings)
}

func TestLintSchema_ValidYAML(t *testing.T) {
	// Write a valid minimal YAML file (empty map)
	tmpFile := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(tmpFile, []byte("name: test\nkind: Solution\napiVersion: v1\n"), 0o600))
	result := &Result{Findings: make([]*Finding, 0)}
	lintSchema(nil, tmpFile, result)
	// Might have findings or none, but should not panic
}

func TestLintSchema_InvalidYAML(t *testing.T) {
	// Write invalid YAML
	tmpFile := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(tmpFile, []byte("{\ninvalid: [yaml\n"), 0o600))
	result := &Result{Findings: make([]*Finding, 0)}
	lintSchema(nil, tmpFile, result)
	// Should silently skip invalid YAML
	assert.Empty(t, result.Findings)
}

// TestLintSchema_RawContentPreferred verifies that when raw content is present,
// schema validation runs against it regardless of the (possibly bogus) file
// path -- so URL/catalog-sourced solutions with schema violations are caught
// instead of being silently skipped.
func TestLintSchema_RawContentPreferred(t *testing.T) {
	// Unknown top-level field "bogusField" violates the solution schema.
	raw := []byte("name: test\nkind: Solution\napiVersion: v1\nbogusField: nope\n")
	result := &Result{Findings: make([]*Finding, 0)}
	// A file path that does not exist on disk: the old behavior would read
	// this path, fail, and silently skip. With raw content preferred, the
	// violation must still be reported.
	lintSchema(raw, "/nonexistent/from-url/solution.yaml", result)

	found := false
	for _, f := range result.Findings {
		if f.RuleName == "schema-violation" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected a schema-violation finding from raw content, got %+v", result.Findings)
}

// TestLintSchema_RawContentFromNonFileSolution verifies the end-to-end path:
// a solution loaded from bytes (no file on disk) with an unknown top-level
// field still produces a schema-violation finding through lint.Solution.
func TestLintSchema_RawContentFromNonFileSolution(t *testing.T) {
	raw := []byte("apiVersion: scaffolding.oakwood-commons.io/v1\nkind: Solution\nmetadata:\n  name: test-sol\nspec:\n  resolvers:\n    r1:\n      value: hello\nbogusTopLevel: nope\n")
	sol := &solution.Solution{}
	require.NoError(t, sol.FromYAML(raw))

	// Path points at a non-existent file to simulate a URL/catalog source.
	result := Solution(sol, "https://example.com/solution.yaml", nil)

	found := false
	for _, f := range result.Findings {
		if f.RuleName == "schema-violation" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected schema-violation from non-file source, got %+v", result.Findings)
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

func TestLintExpressions_SprigFunctionsNotFalsePositive(t *testing.T) {
	// Sprig functions like replace, upper, lower, trim, default should not
	// trigger invalid-template findings.
	//
	// SetExtensionFuncMapFactory is called in TestMain.

	templates := []string{
		`{{ "hello.tpl" | replace ".tpl" "" }}`,
		`{{ "hello" | upper }}`,
		`{{ "HELLO" | lower }}`,
		`{{ " hello " | trim }}`,
		`{{ .value | default "fallback" }}`,
		`{{ list "a" "b" "c" | join "," }}`,
	}

	for _, tmpl := range templates {
		tmplContent := gotmpl.GoTemplatingContent(tmpl)
		inputs := map[string]*spec.ValueRef{
			"test": {Tmpl: &tmplContent},
		}
		result := &Result{Findings: make([]*Finding, 0)}
		lintExpressions(inputs, "test.resolvers.myresolver", result)

		for _, f := range result.Findings {
			assert.NotEqual(t, "invalid-template", f.RuleName,
				"sprig template %q should not produce invalid-template finding, got: %s", tmpl, f.Message)
		}
	}
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

func TestLintWorkflow_ExplicitOnFinally(t *testing.T) {
	reg := provider.NewRegistry()

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
						Explicit:    true,
					},
				},
			},
		},
	}

	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "explicit-on-finally")
	require.Len(t, findings, 1)
	assert.Contains(t, findings[0].Location, "cleanup")
	assert.Equal(t, SeverityWarning, findings[0].Severity)
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

func TestLintTemplateUnderscorePrefix(t *testing.T) {
	tests := []struct {
		name        string
		tmpl        string
		expectError bool
		errorCount  int
	}{
		{
			name:        "underscore prefix triggers info",
			tmpl:        "{{ ._.config.appName }}",
			expectError: true,
			errorCount:  1,
		},
		{
			name:        "multiple underscore refs same resolver",
			tmpl:        "{{ ._.config.appName }} {{ ._.config.version }}",
			expectError: true,
			errorCount:  1, // deduplicated per resolver name
		},
		{
			name:        "multiple different underscore refs",
			tmpl:        "{{ ._.config.appName }} {{ ._.env }}",
			expectError: true,
			errorCount:  2,
		},
		{
			name:        "direct access - no error",
			tmpl:        "{{ .config.appName }}",
			expectError: false,
		},
		{
			name:        "no dot-underscore - no error",
			tmpl:        "plain text with no templates",
			expectError: false,
		},
		{
			name:        "__self is not flagged",
			tmpl:        "{{ .__self }}",
			expectError: false,
		},
		{
			name:        "__actions is not flagged",
			tmpl:        "{{ .__actions.build.result }}",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &Result{Findings: make([]*Finding, 0)}
			lintTemplateUnderscorePrefix(tt.tmpl, "resolvers.msg.resolve.with[0].inputs.message", result)
			findings := filterFindingsByRule(result, "tmpl-underscore-prefix")
			if tt.expectError {
				assert.Len(t, findings, tt.errorCount)
				if len(findings) > 0 {
					assert.Equal(t, SeverityInfo, findings[0].Severity)
				}
			} else {
				assert.Empty(t, findings)
			}
		})
	}
}

func TestLintTemplateAccessors(t *testing.T) {
	dataExpr := celexp.Expression(`{"appName": _.appName}`)

	tests := []struct {
		name      string
		resolvers map[string]*resolver.Resolver
		wantNames []string
	}{
		{
			name: "typo accessor with known data keys is flagged",
			resolvers: map[string]*resolver.Resolver{
				"appName": {Name: "appName"},
				"rendered": {
					Name: "rendered",
					Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{
						Provider: "go-template",
						Inputs: map[string]*spec.ValueRef{
							"template": {Literal: `app = "{{ .appNam }}"`},
							"data":     {Expr: &dataExpr},
						},
					}}},
				},
			},
			wantNames: []string{"appNam"},
		},
		{
			name: "valid resolver ref is not flagged",
			resolvers: map[string]*resolver.Resolver{
				"appName": {Name: "appName"},
				"rendered": {
					Name: "rendered",
					Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{
						Provider: "go-template",
						Inputs: map[string]*spec.ValueRef{
							"template": {Literal: `{{ .appName }}`},
						},
					}}},
				},
			},
			wantNames: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sol := &solution.Solution{Spec: solution.Spec{Resolvers: tt.resolvers}}
			result := &Result{Findings: make([]*Finding, 0)}
			lintTemplateAccessors(sol, result)

			findings := filterFindingsByRule(result, "template-unknown-accessor")
			assert.Len(t, findings, len(tt.wantNames))
			for _, name := range tt.wantNames {
				found := false
				for _, f := range findings {
					assert.Equal(t, SeverityWarning, f.Severity)
					if strings.Contains(f.Message, name) {
						found = true
					}
				}
				assert.True(t, found, "expected a finding mentioning %q", name)
			}
		})
	}
}

//nolint:unparam // capability is parameterized for test flexibility
func newStateProvider(name string, capability provider.Capability) *fakeProvider {
	outputSchema := &jsonschema.Schema{Type: "object"}
	if capability == provider.CapabilityState {
		outputSchema = &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"success": {Type: "boolean"},
			},
		}
	}
	return &fakeProvider{
		desc: &provider.Descriptor{
			Name:        name,
			APIVersion:  "v1",
			Version:     semver.MustParse("1.0.0"),
			Description: "State provider",
			Capabilities: []provider.Capability{
				capability,
			},
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				capability: outputSchema,
			},
		},
	}
}

func TestLintState_MissingBackendProvider(t *testing.T) {
	sol := &solution.Solution{
		APIVersion: "scafctl.io/v1",
		Kind:       "Solution",
		Metadata:   solution.Metadata{Name: "test"},
		Spec:       solution.Spec{Resolvers: map[string]*resolver.Resolver{"a": {Type: "string", Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static"}}}}}},
		State: &state.Config{
			Enabled: &spec.ValueRef{Literal: true},
			Backend: state.Backend{Provider: ""},
		},
	}
	reg := provider.NewRegistry()
	_ = reg.Register(newFakeProvider("static", nil))

	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "missing-state-backend")
	assert.Len(t, findings, 1)
}

func TestLintState_InvalidBackendProvider(t *testing.T) {
	sol := &solution.Solution{
		APIVersion: "scafctl.io/v1",
		Kind:       "Solution",
		Metadata:   solution.Metadata{Name: "test"},
		Spec:       solution.Spec{Resolvers: map[string]*resolver.Resolver{"a": {Type: "string", Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static"}}}}}},
		State: &state.Config{
			Enabled: &spec.ValueRef{Literal: true},
			Backend: state.Backend{Provider: "nonexistent"},
		},
	}
	reg := provider.NewRegistry()
	_ = reg.Register(newFakeProvider("static", nil))

	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "invalid-state-backend")
	assert.Len(t, findings, 1)
}

func TestLintState_BundlePluginSuppressesFinding(t *testing.T) {
	sol := &solution.Solution{
		APIVersion: "scafctl.io/v1",
		Kind:       "Solution",
		Metadata:   solution.Metadata{Name: "test"},
		Spec:       solution.Spec{Resolvers: map[string]*resolver.Resolver{"a": {Type: "string", Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static"}}}}}},
		Bundle: solution.Bundle{
			Plugins: []solution.PluginDependency{
				{Name: "github", Kind: solution.PluginKindProvider, Version: ">=0.1.0"},
			},
		},
		State: &state.Config{
			Enabled: &spec.ValueRef{Literal: true},
			Backend: state.Backend{
				Provider: "github",
				Inputs:   map[string]*spec.ValueRef{"repo": {Literal: "my-org/my-repo"}},
			},
		},
	}
	reg := provider.NewRegistry()
	_ = reg.Register(newFakeProvider("static", nil))

	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "invalid-state-backend")
	assert.Empty(t, findings, "bundle.plugins-declared provider should not trigger invalid-state-backend")
}

func TestLintBundlePlugins_BuiltinWarning(t *testing.T) {
	sol := &solution.Solution{
		APIVersion: "scafctl.io/v1",
		Kind:       "Solution",
		Metadata:   solution.Metadata{Name: "test"},
		Spec:       solution.Spec{Resolvers: map[string]*resolver.Resolver{"a": {Type: "string", Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "cel"}}}}}},
		Bundle: solution.Bundle{
			Plugins: []solution.PluginDependency{
				{Name: "cel", Kind: solution.PluginKindProvider, Version: "1.0.0"},
			},
		},
	}
	reg := provider.NewRegistry()

	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "builtin-in-bundle-plugins")
	require.Len(t, findings, 1)
	assert.Equal(t, SeverityWarning, findings[0].Severity)
	assert.Contains(t, findings[0].Message, "cel")
	assert.Contains(t, findings[0].Message, "builtin provider")
}

func TestLintBundlePlugins_ExternalNoWarning(t *testing.T) {
	sol := &solution.Solution{
		APIVersion: "scafctl.io/v1",
		Kind:       "Solution",
		Metadata:   solution.Metadata{Name: "test"},
		Spec:       solution.Spec{Resolvers: map[string]*resolver.Resolver{"a": {Type: "string", Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static"}}}}}},
		Bundle: solution.Bundle{
			Plugins: []solution.PluginDependency{
				{Name: "aws-provider", Kind: solution.PluginKindProvider, Version: ">=1.0.0"},
			},
		},
	}
	reg := provider.NewRegistry()
	_ = reg.Register(newFakeProvider("static", nil))

	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "builtin-in-bundle-plugins")
	assert.Empty(t, findings, "external provider should not trigger builtin-in-bundle-plugins")
}

func TestLintBundlePlugins_MultipleBuiltins(t *testing.T) {
	sol := &solution.Solution{
		APIVersion: "scafctl.io/v1",
		Kind:       "Solution",
		Metadata:   solution.Metadata{Name: "test"},
		Spec:       solution.Spec{Resolvers: map[string]*resolver.Resolver{"a": {Type: "string", Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static"}}}}}},
		Bundle: solution.Bundle{
			Plugins: []solution.PluginDependency{
				{Name: "cel", Kind: solution.PluginKindProvider, Version: "1.0.0"},
				{Name: "aws-provider", Kind: solution.PluginKindProvider, Version: ">=1.0.0"},
				{Name: "file", Kind: solution.PluginKindProvider, Version: ">=0.1.0"},
			},
		},
	}
	reg := provider.NewRegistry()
	_ = reg.Register(newFakeProvider("static", nil))

	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "builtin-in-bundle-plugins")
	assert.Len(t, findings, 2, "should warn for both cel and file, not aws-provider")
}

func TestLintBundlePlugins_AuthHandlerNotWarned(t *testing.T) {
	sol := &solution.Solution{
		APIVersion: "scafctl.io/v1",
		Kind:       "Solution",
		Metadata:   solution.Metadata{Name: "test"},
		Spec:       solution.Spec{Resolvers: map[string]*resolver.Resolver{"a": {Type: "string", Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static"}}}}}},
		Bundle: solution.Bundle{
			Plugins: []solution.PluginDependency{
				{Name: "cel", Kind: solution.PluginKindAuthHandler, Version: "1.0.0"},
			},
		},
	}
	reg := provider.NewRegistry()
	_ = reg.Register(newFakeProvider("static", nil))

	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "builtin-in-bundle-plugins")
	assert.Empty(t, findings, "auth handler with builtin name should not trigger warning")
}

func TestLintState_ProviderWithoutCapabilityState(t *testing.T) {
	sol := &solution.Solution{
		APIVersion: "scafctl.io/v1",
		Kind:       "Solution",
		Metadata:   solution.Metadata{Name: "test"},
		Spec:       solution.Spec{Resolvers: map[string]*resolver.Resolver{"a": {Type: "string", Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static"}}}}}},
		State: &state.Config{
			Enabled: &spec.ValueRef{Literal: true},
			Backend: state.Backend{Provider: "static"},
		},
	}
	reg := provider.NewRegistry()
	_ = reg.Register(newFakeProvider("static", nil))

	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "invalid-state-backend")
	assert.Len(t, findings, 1)
}

func TestLintState_ValidConfig(t *testing.T) {
	sol := &solution.Solution{
		APIVersion: "scafctl.io/v1",
		Kind:       "Solution",
		Metadata:   solution.Metadata{Name: "test"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"greeting": {
					Type:    "string",
					Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static"}}},
				},
			},
		},
		State: &state.Config{
			Enabled: &spec.ValueRef{Literal: true},
			Backend: state.Backend{
				Provider: "file",
				Inputs:   map[string]*spec.ValueRef{"path": {Literal: "test.json"}},
			},
		},
	}
	reg := provider.NewRegistry()
	_ = reg.Register(newFakeProvider("static", nil))
	_ = reg.Register(newStateProvider("file", provider.CapabilityState))

	result := Solution(sol, "test.yaml", reg)
	stateFindings := []*Finding{}
	for _, f := range result.Findings {
		if f.Category == "state" {
			stateFindings = append(stateFindings, f)
		}
	}
	assert.Empty(t, stateFindings)
}

func TestLintState_ResolverRefInEnabled(t *testing.T) {
	rslvrName := "my_flag"
	sol := &solution.Solution{
		APIVersion: "scafctl.io/v1",
		Kind:       "Solution",
		Metadata:   solution.Metadata{Name: "test"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"my_flag": {
					Type:    "bool",
					Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static"}}},
				},
			},
		},
		State: &state.Config{
			Enabled: &spec.ValueRef{Resolver: &rslvrName},
			Backend: state.Backend{
				Provider: "file",
				Inputs:   map[string]*spec.ValueRef{"path": {Literal: "test.json"}},
			},
		},
	}
	reg := provider.NewRegistry()
	_ = reg.Register(newFakeProvider("static", nil))
	_ = reg.Register(newStateProvider("file", provider.CapabilityState))

	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "state-resolver-ref")
	assert.Len(t, findings, 1)
	assert.Contains(t, findings[0].Message, "state.enabled")
	assert.Contains(t, findings[0].Message, "my_flag")
}

func TestLintState_ResolverRefInBackendInputs(t *testing.T) {
	rslvrName := "app_name"
	sol := &solution.Solution{
		APIVersion: "scafctl.io/v1",
		Kind:       "Solution",
		Metadata:   solution.Metadata{Name: "test"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"app_name": {
					Type:    "string",
					Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static"}}},
				},
			},
		},
		State: &state.Config{
			Enabled: &spec.ValueRef{Literal: true},
			Backend: state.Backend{
				Provider: "file",
				Inputs: map[string]*spec.ValueRef{
					"path": {Resolver: &rslvrName},
				},
			},
		},
	}
	reg := provider.NewRegistry()
	_ = reg.Register(newFakeProvider("static", nil))
	_ = reg.Register(newStateProvider("file", provider.CapabilityState))

	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "state-resolver-ref")
	assert.Len(t, findings, 1)
	assert.Contains(t, findings[0].Message, "path")
	assert.Contains(t, findings[0].Message, "app_name")
}

func TestLintState_SaveOverrideStateRef(t *testing.T) {
	expr := celexp.Expression("__state.branch")
	sol := &solution.Solution{
		APIVersion: "scafctl.io/v1",
		Kind:       "Solution",
		Metadata:   solution.Metadata{Name: "test"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"env": {
					Type:    "string",
					Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static"}}},
				},
			},
		},
		State: &state.Config{
			Enabled: &spec.ValueRef{Literal: true},
			Backend: state.Backend{
				Provider: "github",
				Inputs:   map[string]*spec.ValueRef{"path": {Literal: "state.json"}},
				SaveOverrides: map[string]*spec.ValueRef{
					"branch": {Expr: &expr},
				},
			},
		},
	}
	reg := provider.NewRegistry()
	_ = reg.Register(newFakeProvider("static", nil))
	_ = reg.Register(newStateProvider("github", provider.CapabilityState))

	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "state-save-override-state-ref")
	assert.Len(t, findings, 1)
	assert.Contains(t, findings[0].Message, "branch")
}

func TestLintState_SaveOverrideRslvrAllowed(t *testing.T) {
	rslvrName := "featureBranch"
	sol := &solution.Solution{
		APIVersion: "scafctl.io/v1",
		Kind:       "Solution",
		Metadata:   solution.Metadata{Name: "test"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"featureBranch": {
					Type:    "string",
					Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static"}}},
				},
			},
		},
		State: &state.Config{
			Enabled: &spec.ValueRef{Literal: true},
			Backend: state.Backend{
				Provider: "github",
				Inputs:   map[string]*spec.ValueRef{"path": {Literal: "state.json"}},
				SaveOverrides: map[string]*spec.ValueRef{
					"branch": {Resolver: &rslvrName},
				},
			},
		},
	}
	reg := provider.NewRegistry()
	_ = reg.Register(newFakeProvider("static", nil))
	_ = reg.Register(newStateProvider("github", provider.CapabilityState))

	result := Solution(sol, "test.yaml", reg)
	// rslvr: is allowed in saveOverrides (unlike inputs)
	findings := filterFindingsByRule(result, "state-resolver-ref")
	assert.Empty(t, findings)
	findings = filterFindingsByRule(result, "state-save-override-state-ref")
	assert.Empty(t, findings)
}

func TestLintState_SaveOverrideNoFalsePositive(t *testing.T) {
	// Expressions using 'state/' as a string prefix must NOT trigger the state-ref rule
	expr := celexp.Expression("'state/' + _.branch")
	sol := &solution.Solution{
		APIVersion: "scafctl.io/v1",
		Kind:       "Solution",
		Metadata:   solution.Metadata{Name: "test"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"branch": {
					Type:    "string",
					Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static"}}},
				},
			},
		},
		State: &state.Config{
			Enabled: &spec.ValueRef{Literal: true},
			Backend: state.Backend{
				Provider: "github",
				Inputs:   map[string]*spec.ValueRef{"path": {Literal: "state.json"}},
				SaveOverrides: map[string]*spec.ValueRef{
					"path": {Expr: &expr},
				},
			},
		},
	}
	reg := provider.NewRegistry()
	_ = reg.Register(newFakeProvider("static", nil))
	_ = reg.Register(newStateProvider("github", provider.CapabilityState))

	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "state-save-override-state-ref")
	assert.Empty(t, findings, "expression using 'state/' string should not trigger false positive")
}

func TestLintState_GitHubNoSaveBranch(t *testing.T) {
	sol := &solution.Solution{
		APIVersion: "scafctl.io/v1",
		Kind:       "Solution",
		Metadata:   solution.Metadata{Name: "test"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"env": {
					Type:    "string",
					Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static"}}},
				},
			},
		},
		State: &state.Config{
			Enabled: &spec.ValueRef{Literal: true},
			Backend: state.Backend{
				Provider: "github",
				Inputs:   map[string]*spec.ValueRef{"path": {Literal: "state.json"}},
			},
		},
	}
	reg := provider.NewRegistry()
	_ = reg.Register(newFakeProvider("static", nil))
	_ = reg.Register(newStateProvider("github", provider.CapabilityState))

	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "state-github-no-save-branch")
	assert.Len(t, findings, 1)
	assert.Contains(t, findings[0].Message, "no save branch")
}

func TestLintState_GitHubWithSaveBranchInOverrides(t *testing.T) {
	rslvrName := "featureBranch"
	sol := &solution.Solution{
		APIVersion: "scafctl.io/v1",
		Kind:       "Solution",
		Metadata:   solution.Metadata{Name: "test"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"featureBranch": {
					Type:    "string",
					Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static"}}},
				},
			},
		},
		State: &state.Config{
			Enabled: &spec.ValueRef{Literal: true},
			Backend: state.Backend{
				Provider: "github",
				Inputs:   map[string]*spec.ValueRef{"path": {Literal: "state.json"}},
				SaveOverrides: map[string]*spec.ValueRef{
					"branch": {Resolver: &rslvrName},
				},
			},
		},
	}
	reg := provider.NewRegistry()
	_ = reg.Register(newFakeProvider("static", nil))
	_ = reg.Register(newStateProvider("github", provider.CapabilityState))

	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "state-github-no-save-branch")
	assert.Empty(t, findings) // branch is configured via saveOverrides
}

func TestLintState_GitHubWithBranchInInputs(t *testing.T) {
	sol := &solution.Solution{
		APIVersion: "scafctl.io/v1",
		Kind:       "Solution",
		Metadata:   solution.Metadata{Name: "test"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"env": {
					Type:    "string",
					Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static"}}},
				},
			},
		},
		State: &state.Config{
			Enabled: &spec.ValueRef{Literal: true},
			Backend: state.Backend{
				Provider: "github",
				Inputs: map[string]*spec.ValueRef{
					"path":   {Literal: "state.json"},
					"branch": {Literal: "main"},
				},
			},
		},
	}
	reg := provider.NewRegistry()
	_ = reg.Register(newFakeProvider("static", nil))
	_ = reg.Register(newStateProvider("github", provider.CapabilityState))

	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "state-github-no-save-branch")
	assert.Empty(t, findings) // branch is configured via inputs
}

func TestLintState_NonGitHubNoSaveBranchHint(t *testing.T) {
	sol := &solution.Solution{
		APIVersion: "scafctl.io/v1",
		Kind:       "Solution",
		Metadata:   solution.Metadata{Name: "test"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"env": {
					Type:    "string",
					Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static"}}},
				},
			},
		},
		State: &state.Config{
			Enabled: &spec.ValueRef{Literal: true},
			Backend: state.Backend{
				Provider: "file",
				Inputs:   map[string]*spec.ValueRef{"path": {Literal: "state.json"}},
			},
		},
	}
	reg := provider.NewRegistry()
	_ = reg.Register(newFakeProvider("static", nil))
	_ = reg.Register(newStateProvider("file", provider.CapabilityState))

	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "state-github-no-save-branch")
	assert.Empty(t, findings) // hint only fires for github provider
}

func TestLintResolveForEach(t *testing.T) {
	prov := newFakeProvider("http", map[string]*jsonschema.Schema{
		"url": {Type: "string"},
	})
	reg := provider.NewRegistry()
	_ = reg.Register(prov)

	newResolveForEach := func(withIn bool) *resolver.ForEachClause {
		fe := &resolver.ForEachClause{Item: "item"}
		if withIn {
			expr := celexp.Expression("_.items")
			fe.In = &spec.ValueRef{Expr: &expr}
		}
		return fe
	}
	urlsResolver := "urls"

	tests := []struct {
		name         string
		sol          *solution.Solution
		wantFindings int
	}{
		{
			name: "resolve forEach with in is valid (no finding)",
			sol: &solution.Solution{Spec: solution.Spec{Resolvers: map[string]*resolver.Resolver{
				"testResolver": {Type: "array", Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{
					Provider: "http",
					Inputs:   map[string]*spec.ValueRef{"url": {Literal: "https://example.com"}},
					ForEach:  newResolveForEach(true),
				}}}},
			}}},
			wantFindings: 0,
		},
		{
			name: "resolve forEach missing in is an error",
			sol: &solution.Solution{Spec: solution.Spec{Resolvers: map[string]*resolver.Resolver{
				"testResolver": {Type: "array", Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{
					Provider: "http",
					Inputs:   map[string]*spec.ValueRef{"url": {Literal: "https://example.com"}},
					ForEach:  newResolveForEach(false),
				}}}},
			}}},
			wantFindings: 1,
		},
		{
			name: "transform forEach without in is fine (defaults to __self)",
			sol: &solution.Solution{Spec: solution.Spec{Resolvers: map[string]*resolver.Resolver{
				"testResolver": {Type: "array", Transform: &resolver.TransformPhase{With: []resolver.ProviderTransform{{
					Provider: "http",
					Inputs:   map[string]*spec.ValueRef{"url": {Literal: "https://example.com"}},
					ForEach:  &resolver.ForEachClause{Item: "item"},
				}}}},
			}}},
			wantFindings: 0,
		},
		{
			name: "resolve forEach with rslvr in is valid (no finding)",
			sol: &solution.Solution{Spec: solution.Spec{Resolvers: map[string]*resolver.Resolver{
				"testResolver": {Type: "array", Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{
					Provider: "http",
					Inputs:   map[string]*spec.ValueRef{"url": {Literal: "https://example.com"}},
					ForEach:  &resolver.ForEachClause{Item: "item", In: &spec.ValueRef{Resolver: &urlsResolver}},
				}}}},
			}}},
			wantFindings: 0,
		},
		{
			name: "only the resolve step missing in is flagged (mixed steps)",
			sol: &solution.Solution{Spec: solution.Spec{Resolvers: map[string]*resolver.Resolver{
				"testResolver": {Type: "array", Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{
					{
						Provider: "http",
						Inputs:   map[string]*spec.ValueRef{"url": {Literal: "https://example.com"}},
						ForEach:  newResolveForEach(true), // has in -> ok
					},
					{
						Provider: "http",
						Inputs:   map[string]*spec.ValueRef{"url": {Literal: "https://example.com"}},
						ForEach:  newResolveForEach(false), // missing in -> flagged
					},
				}}},
			}}},
			wantFindings: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Solution(tt.sol, "test.yaml", reg)
			findings := filterFindingsByRule(result, "resolve-foreach-missing-in")
			require.Len(t, findings, tt.wantFindings)
			if tt.wantFindings > 0 {
				assert.Contains(t, findings[0].Message, "requires 'in'")
				assert.Equal(t, SeverityError, findings[0].Severity)
			}
		})
	}
}

func TestLintRedundantDependsOn(t *testing.T) {
	celProv := newFakeProvider("cel", map[string]*jsonschema.Schema{
		"expression": {Type: "string"},
	})
	staticProv := newFakeProvider("static", map[string]*jsonschema.Schema{
		"value": {Type: "string"},
	})
	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(celProv))
	require.NoError(t, reg.Register(staticProv))

	tests := []struct {
		name          string
		resolvers     map[string]*resolver.Resolver
		expectFinding bool
		msgContains   string
	}{
		{
			name: "all dependsOn entries are redundant (inferred from expr)",
			resolvers: map[string]*resolver.Resolver{
				"registry":  {Type: "string", Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: "docker.io"}}}}}},
				"namespace": {Type: "string", Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: "myns"}}}}}},
				"imageRef": {
					Type:      "string",
					DependsOn: []string{"registry", "namespace"},
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "cel",
							Inputs:   map[string]*spec.ValueRef{"expression": {Expr: exprPtr("_.registry + '/' + _.namespace")}},
						}},
					},
				},
			},
			expectFinding: true,
			msgContains:   "all listed dependencies are already inferred",
		},
		{
			name: "partial redundancy (some dependsOn are not inferred)",
			resolvers: map[string]*resolver.Resolver{
				"registry":  {Type: "string", Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: "docker.io"}}}}}},
				"namespace": {Type: "string", Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: "myns"}}}}}},
				"setup":     {Type: "string", Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: "done"}}}}}},
				"imageRef": {
					Type:      "string",
					DependsOn: []string{"registry", "setup"},
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "cel",
							Inputs:   map[string]*spec.ValueRef{"expression": {Expr: exprPtr("_.registry + '/image'")}},
						}},
					},
				},
			},
			expectFinding: true,
			msgContains:   "redundant entries",
		},
		{
			name: "no redundancy (dependsOn not inferred)",
			resolvers: map[string]*resolver.Resolver{
				"setup": {Type: "string", Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: "done"}}}}}},
				"imageRef": {
					Type:      "string",
					DependsOn: []string{"setup"},
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "static",
							Inputs:   map[string]*spec.ValueRef{"value": {Literal: "myimage"}},
						}},
					},
				},
			},
			expectFinding: false,
		},
		{
			name: "no dependsOn field at all",
			resolvers: map[string]*resolver.Resolver{
				"registry": {Type: "string", Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: "docker.io"}}}}}},
				"imageRef": {
					Type: "string",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "cel",
							Inputs:   map[string]*spec.ValueRef{"expression": {Expr: exprPtr("_.registry + '/image'")}},
						}},
					},
				},
			},
			expectFinding: false,
		},
		{
			name: "dependsOn inferred from rslvr reference",
			resolvers: map[string]*resolver.Resolver{
				"env": {Type: "string", Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: "prod"}}}}}},
				"config": {
					Type:      "string",
					DependsOn: []string{"env"},
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "static",
							Inputs:   map[string]*spec.ValueRef{"value": {Resolver: strPtr("env")}},
						}},
					},
				},
			},
			expectFinding: true,
			msgContains:   "all listed dependencies are already inferred",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sol := &solution.Solution{
				Spec: solution.Spec{
					Resolvers: tt.resolvers,
				},
			}

			result := Solution(sol, "test.yaml", reg)

			findings := filterFindingsByRule(result, "redundant-depends-on")
			if tt.expectFinding {
				require.NotEmpty(t, findings, "expected redundant-depends-on finding")
				assert.Contains(t, findings[0].Message, tt.msgContains)
				assert.Equal(t, SeverityInfo, findings[0].Severity)
			} else {
				assert.Empty(t, findings, "expected no redundant-depends-on finding")
			}
		})
	}
}

func strPtr(s string) *string {
	return &s
}

func TestLintImmutableResolvers_NoStateBlock(t *testing.T) {
	sol := &solution.Solution{
		APIVersion: "scafctl.io/v1",
		Kind:       "Solution",
		Metadata:   solution.Metadata{Name: "test"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"cluster_id": {
					Type:      "string",
					Immutable: true,
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{Provider: "exec"},
						},
					},
				},
			},
		},
		// No State block
	}
	reg := provider.NewRegistry()
	_ = reg.Register(newFakeProvider("exec", nil))

	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "immutable-requires-state")
	require.Len(t, findings, 1)
	assert.Contains(t, findings[0].Message, "cluster_id")
	assert.Contains(t, findings[0].Message, "no state block")
	assert.Equal(t, SeverityError, findings[0].Severity)
}

func TestLintImmutableResolvers_WithStateBlock(t *testing.T) {
	sol := &solution.Solution{
		APIVersion: "scafctl.io/v1",
		Kind:       "Solution",
		Metadata:   solution.Metadata{Name: "test"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"cluster_id": {
					Type:      "string",
					Immutable: true,
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{Provider: "parameter"},
							{Provider: "exec"},
						},
					},
				},
			},
		},
		State: &state.Config{
			Enabled: &spec.ValueRef{Literal: true},
			Backend: state.Backend{
				Provider: "file",
				Inputs:   map[string]*spec.ValueRef{"path": {Literal: "state.json"}},
			},
		},
	}
	reg := provider.NewRegistry()
	_ = reg.Register(newFakeProvider("exec", nil))
	_ = reg.Register(newFakeProvider("parameter", nil))
	_ = reg.Register(newStateProvider("file", provider.CapabilityState))

	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "immutable-requires-state")
	assert.Empty(t, findings)
}

func TestLintImmutableResolvers_NotImmutable(t *testing.T) {
	sol := &solution.Solution{
		APIVersion: "scafctl.io/v1",
		Kind:       "Solution",
		Metadata:   solution.Metadata{Name: "test"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"cluster_id": {
					Type:      "string",
					Immutable: false,
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{Provider: "exec"},
						},
					},
				},
			},
		},
		// No State block, but resolver is not immutable so no finding expected
	}
	reg := provider.NewRegistry()
	_ = reg.Register(newFakeProvider("exec", nil))

	result := Solution(sol, "test.yaml", reg)
	findings := filterFindingsByRule(result, "immutable-requires-state")
	assert.Empty(t, findings)
}

func newHTTPFakeProvider() *fakeProvider {
	return &fakeProvider{
		desc: &provider.Descriptor{
			Name:       "http",
			APIVersion: "v1",
			Version:    semver.MustParse("1.0.0"),
			Schema: &jsonschema.Schema{
				Type:       "object",
				Properties: map[string]*jsonschema.Schema{},
			},
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom: {
					Type: "object",
					Properties: map[string]*jsonschema.Schema{
						"statusCode": {Type: "integer"},
						"body":       {Type: "string"},
						"headers":    {Type: "object"},
					},
				},
			},
			Description:  "HTTP provider",
			Capabilities: []provider.Capability{provider.CapabilityFrom},
		},
	}
}

func TestLintTransformShapeMismatch(t *testing.T) {
	bodyExpr := celexp.Expression("__self.body.items")
	guardExpr := celexp.Expression("type(__self) == map_type && has(__self.body)")
	credExpr := celexp.Expression("has(_.credentials)")
	statusExpr := celexp.Expression("__self.statusCode == 200")
	tmplContent := gotmpl.GoTemplatingContent("{{ .__self.body }}")
	safeExpr := celexp.Expression("size(__self) > 0")

	tests := []struct {
		name        string
		resolver    *resolver.Resolver
		expectRule  bool
		description string
	}{
		{
			name: "flagged: http + static, transform accesses __self.body without guard",
			resolver: &resolver.Resolver{
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{
						{Provider: "http", When: &resolver.Condition{Expr: &credExpr}},
						{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: []any{}}}},
					},
				},
				Transform: &resolver.TransformPhase{
					With: []resolver.ProviderTransform{
						{Provider: "cel", Inputs: map[string]*spec.ValueRef{"expression": {Expr: &bodyExpr}}},
					},
				},
			},
			expectRule: true,
		},
		{
			name: "flagged: http + static, transform accesses __self.statusCode",
			resolver: &resolver.Resolver{
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{
						{Provider: "http"},
						{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: ""}}},
					},
				},
				Transform: &resolver.TransformPhase{
					With: []resolver.ProviderTransform{
						{Provider: "cel", Inputs: map[string]*spec.ValueRef{"expression": {Expr: &statusExpr}}},
					},
				},
			},
			expectRule: true,
		},
		{
			name: "flagged: http + static, go template accesses __self.body",
			resolver: &resolver.Resolver{
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{
						{Provider: "http"},
						{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: ""}}},
					},
				},
				Transform: &resolver.TransformPhase{
					With: []resolver.ProviderTransform{
						{Provider: "go-template", Inputs: map[string]*spec.ValueRef{"template": {Tmpl: &tmplContent}}},
					},
				},
			},
			expectRule: true,
		},
		{
			name: "flagged: http + static, transform accesses __self.body via literal string",
			resolver: &resolver.Resolver{
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{
						{Provider: "http", When: &resolver.Condition{Expr: &credExpr}},
						{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: []any{}}}},
					},
				},
				Transform: &resolver.TransformPhase{
					With: []resolver.ProviderTransform{
						{Provider: "cel", Inputs: map[string]*spec.ValueRef{"expression": {Literal: "__self.body"}}},
					},
				},
			},
			expectRule: true,
		},
		{
			name: "not flagged: static fallback is a map with same shape",
			resolver: &resolver.Resolver{
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{
						{Provider: "http", When: &resolver.Condition{Expr: &credExpr}},
						{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: map[string]any{"body": "", "statusCode": 0}}}},
					},
				},
				Transform: &resolver.TransformPhase{
					With: []resolver.ProviderTransform{
						{Provider: "cel", Inputs: map[string]*spec.ValueRef{"expression": {Expr: &bodyExpr}}},
					},
				},
			},
			expectRule: false,
		},
		{
			name: "not flagged: transform has a when guard",
			resolver: &resolver.Resolver{
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{
						{Provider: "http", When: &resolver.Condition{Expr: &credExpr}},
						{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: []any{}}}},
					},
				},
				Transform: &resolver.TransformPhase{
					With: []resolver.ProviderTransform{
						{
							Provider: "cel",
							When:     &resolver.Condition{Expr: &guardExpr},
							Inputs:   map[string]*spec.ValueRef{"expression": {Expr: &bodyExpr}},
						},
					},
				},
			},
			expectRule: false,
		},
		{
			name: "not flagged: single resolve source",
			resolver: &resolver.Resolver{
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{
						{Provider: "http"},
					},
				},
				Transform: &resolver.TransformPhase{
					With: []resolver.ProviderTransform{
						{Provider: "cel", Inputs: map[string]*spec.ValueRef{"expression": {Expr: &bodyExpr}}},
					},
				},
			},
			expectRule: false,
		},
		{
			name: "not flagged: both sources are same provider type",
			resolver: &resolver.Resolver{
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{
						{Provider: "http", When: &resolver.Condition{Expr: &credExpr}},
						{Provider: "http"},
					},
				},
				Transform: &resolver.TransformPhase{
					With: []resolver.ProviderTransform{
						{Provider: "cel", Inputs: map[string]*spec.ValueRef{"expression": {Expr: &bodyExpr}}},
					},
				},
			},
			expectRule: false,
		},
		{
			name: "not flagged: phase-level when guard makes shape access valid",
			resolver: &resolver.Resolver{
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{
						{Provider: "http", When: &resolver.Condition{Expr: &credExpr}},
						{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: []any{}}}},
					},
				},
				Transform: &resolver.TransformPhase{
					When: &resolver.Condition{Expr: &guardExpr},
					With: []resolver.ProviderTransform{
						{Provider: "cel", Inputs: map[string]*spec.ValueRef{"expression": {Expr: &bodyExpr}}},
					},
				},
			},
			expectRule: false,
		},
		{
			name: "not flagged: transform does not access structured fields",
			resolver: &resolver.Resolver{
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{
						{Provider: "http"},
						{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: ""}}},
					},
				},
				Transform: &resolver.TransformPhase{
					With: []resolver.ProviderTransform{
						{Provider: "cel", Inputs: map[string]*spec.ValueRef{"expression": {Expr: &safeExpr}}},
					},
				},
			},
			expectRule: false,
		},
		{
			name: "not flagged: no transform phase",
			resolver: &resolver.Resolver{
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{
						{Provider: "http"},
						{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: ""}}},
					},
				},
			},
			expectRule: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sol := &solution.Solution{
				APIVersion: "scafctl.io/v1",
				Kind:       "Solution",
				Metadata:   solution.Metadata{Name: "test"},
				Spec: solution.Spec{
					Resolvers: map[string]*resolver.Resolver{
						"api_data": tt.resolver,
					},
				},
			}

			reg := provider.NewRegistry()
			require.NoError(t, reg.Register(newHTTPFakeProvider()))
			require.NoError(t, reg.Register(newFakeProvider("static", map[string]*jsonschema.Schema{"value": {Type: "string"}})))
			require.NoError(t, reg.Register(newFakeProvider("cel", map[string]*jsonschema.Schema{"expression": {Type: "string"}})))
			require.NoError(t, reg.Register(newFakeProvider("go-template", map[string]*jsonschema.Schema{"template": {Type: "string"}})))

			result := Solution(sol, "test.yaml", reg)
			findings := filterFindingsByRule(result, "transform-shape-mismatch")

			if tt.expectRule {
				assert.NotEmpty(t, findings, "expected transform-shape-mismatch finding")
				assert.Equal(t, SeverityWarning, findings[0].Severity)
			} else {
				assert.Empty(t, findings, "expected no transform-shape-mismatch finding")
			}
		})
	}
}

func TestLintAction_FingerprintWithoutSources(t *testing.T) {
	t.Parallel()
	reg := provider.NewRegistry()

	tests := []struct {
		name       string
		action     *action.Action
		expectRule bool
	}{
		{
			name: "fingerprint with sources",
			action: &action.Action{
				Name:     "build",
				Provider: "test",
				Sources:  []string{"*.go"},
				Fingerprint: &action.FingerprintConfig{
					Scope: action.FingerprintScopeFiles,
				},
			},
			expectRule: false,
		},
		{
			name: "fingerprint without sources",
			action: &action.Action{
				Name:     "deploy",
				Provider: "test",
				Fingerprint: &action.FingerprintConfig{
					Scope: action.FingerprintScopeFiles,
				},
			},
			expectRule: true,
		},
		{
			name: "no fingerprint no sources",
			action: &action.Action{
				Name:     "deploy",
				Provider: "test",
			},
			expectRule: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sol := &solution.Solution{
				Spec: solution.Spec{
					Workflow: &action.Workflow{
						Actions: map[string]*action.Action{
							tt.action.Name: tt.action,
						},
					},
				},
			}
			result := Solution(sol, "test.yaml", reg)
			findings := filterFindingsByRule(result, "fingerprint-without-sources")
			if tt.expectRule {
				require.NotEmpty(t, findings)
				assert.Equal(t, SeverityWarning, findings[0].Severity)
			} else {
				assert.Empty(t, findings)
			}
		})
	}
}

func TestLintAction_InvalidFingerprintScope(t *testing.T) {
	t.Parallel()
	reg := provider.NewRegistry()

	tests := []struct {
		name       string
		scope      action.FingerprintScope
		expectRule bool
	}{
		{"valid scope all", action.FingerprintScopeAll, false},
		{"valid scope files", action.FingerprintScopeFiles, false},
		{"valid scope empty", "", false},
		{"invalid scope", action.FingerprintScope("none"), true},
		{"invalid scope typo", action.FingerprintScope("file"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sol := &solution.Solution{
				Spec: solution.Spec{
					Workflow: &action.Workflow{
						Actions: map[string]*action.Action{
							"build": {
								Name:     "build",
								Provider: "test",
								Sources:  []string{"*.go"},
								Fingerprint: &action.FingerprintConfig{
									Scope: tt.scope,
								},
							},
						},
					},
				},
			}
			result := Solution(sol, "test.yaml", reg)
			findings := filterFindingsByRule(result, "invalid-fingerprint-scope")
			if tt.expectRule {
				require.NotEmpty(t, findings)
				assert.Equal(t, SeverityError, findings[0].Severity)
				assert.Contains(t, findings[0].Message, string(tt.scope))
			} else {
				assert.Empty(t, findings)
			}
		})
	}
}
