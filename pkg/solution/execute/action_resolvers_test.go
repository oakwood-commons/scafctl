// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package execute

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/celprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/gotmplprovider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testStrPtr(s string) *string { return &s }

func testExprPtr(s string) *celexp.Expression {
	e := celexp.Expression(s)
	return &e
}

func testTmplPtr(s string) *gotmpl.GoTemplatingContent {
	t := gotmpl.GoTemplatingContent(s)
	return &t
}

func testStaticResolver(name string, expr *celexp.Expression) *resolver.Resolver {
	vr := &spec.ValueRef{Literal: "val-" + name}
	if expr != nil {
		vr = &spec.ValueRef{Expr: expr}
	}
	return &resolver.Resolver{
		Name: name,
		Resolve: &resolver.ResolvePhase{
			With: []resolver.ProviderSource{
				{
					Provider: "static",
					Inputs:   map[string]*spec.ValueRef{"value": vr},
				},
			},
		},
	}
}

func TestActionResolverRefs_AllLocations(t *testing.T) {
	t.Parallel()

	gotmplDesc := gotmplprovider.NewGoTemplateProvider().Descriptor()
	celDesc := celprovider.NewCelProvider().Descriptor()
	lookup := func(name string) *provider.Descriptor {
		switch name {
		case gotmplDesc.Name:
			return gotmplDesc
		case celDesc.Name:
			return celDesc
		default:
			return nil
		}
	}

	baseSolution := func() *solution.Solution {
		return &solution.Solution{
			Metadata: solution.Metadata{Name: "test-sol"},
			Spec: solution.Spec{
				Resolvers: map[string]*resolver.Resolver{
					"directInput":  testStaticResolver("directInput", nil),
					"celInput":     testStaticResolver("celInput", nil),
					"tmplExplicit": testStaticResolver("tmplExplicit", nil),
					"tmplBare":     testStaticResolver("tmplBare", nil),
					"nestedRslvr":  testStaticResolver("nestedRslvr", nil),
					"nestedExpr":   testStaticResolver("nestedExpr", nil),
					"callArgRslvr": testStaticResolver("callArgRslvr", nil),
					"callArgExpr":  testStaticResolver("callArgExpr", nil),
					"fnArgA":       testStaticResolver("fnArgA", nil),
					"fnArgB":       testStaticResolver("fnArgB", nil),
					"forEachSrc":   testStaticResolver("forEachSrc", nil),
					"forEachErr":   testStaticResolver("forEachErr", nil),
					"whenFlag":     testStaticResolver("whenFlag", nil),
					"errFlag":      testStaticResolver("errFlag", nil),
					"retryFlag":    testStaticResolver("retryFlag", nil),
					"optDefined":   testStaticResolver("optDefined", nil),
					"dataHost":     testStaticResolver("dataHost", nil),
					"unrelated":    testStaticResolver("unrelated", nil),
				},
				Functions: map[string]*spec.Function{
					"formatBanner": {
						Params: []*spec.ParamDef{{Name: "a"}, {Name: "b"}},
						Cel:    `_.args.a + ":" + _.args.b`,
					},
					"zeroArgHelper": {
						Cel: `"constant"`,
					},
				},
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"build": {
							Name:     "build",
							Alias:    "buildStep",
							Provider: "cel",
						},
					},
					Finally: map[string]*action.Action{
						"audit": {
							Name:     "audit",
							Alias:    "auditStep",
							Provider: "cel",
						},
					},
				},
			},
		}
	}

	tests := []struct {
		name      string
		action    *action.Action
		want      []string
		expectErr string
	}{
		{
			name:   "nil action returns nil",
			action: nil,
			want:   nil,
		},
		{
			name: "zero-reference literal action",
			action: &action.Action{
				Provider: "exec",
				Inputs: map[string]*spec.ValueRef{
					"command": {Literal: "rm -rf ./dist"},
				},
			},
			want: []string{},
		},
		{
			name: "direct inputs rslvr, expr, tmpl, and nested literal maps/arrays",
			action: &action.Action{
				Provider: "exec",
				Inputs: map[string]*spec.ValueRef{
					"direct":  {Resolver: testStrPtr("directInput")},
					"command": {Expr: testExprPtr(`"run " + _.celInput + " " + _.?optDefined.orValue("x") + " " + _.?optMissing.orValue("y")`)},
					"banner":  {Tmpl: testTmplPtr(`{{ ._.tmplExplicit }} {{ .tmplBare }} {{ .__actions.build.results }}`)},
					"env": {
						Literal: map[string]any{
							"R": map[string]any{"rslvr": "nestedRslvr"},
							"L": []any{map[string]any{"expr": "_.nestedExpr"}},
							"S": map[string]any{"scrt": "my-secret"},
						},
					},
				},
			},
			want: []string{"celInput", "directInput", "nestedExpr", "nestedRslvr", "optDefined", "tmplBare", "tmplExplicit"},
		},
		{
			name: "spec.calls action extracts from args",
			action: &action.Action{
				CallRef: spec.CallRef{
					Call: "deployService",
					Args: map[string]*spec.ValueRef{
						"target":  {Resolver: testStrPtr("callArgRslvr")},
						"payload": {Expr: testExprPtr(`{"v": _.callArgExpr}`)},
					},
				},
			},
			want: []string{"callArgExpr", "callArgRslvr"},
		},
		{
			name: "spec.functions call sites extract argument refs and exclude function names",
			action: &action.Action{
				Provider: "exec",
				Inputs: map[string]*spec.ValueRef{
					"msg": {Tmpl: testTmplPtr(`{{ formatBanner .fnArgA ._.fnArgB }} {{ zeroArgHelper }} {{ .fnArgA | formatBanner "prefix" }}`)},
				},
			},
			want: []string{"fnArgA", "fnArgB"},
		},
		{
			name: "forEach.in vs item/index aliases and forEach.continueOnError",
			action: &action.Action{
				Provider: "exec",
				ForEach: &spec.ForEachClause{
					In:              &spec.ValueRef{Resolver: testStrPtr("forEachSrc")},
					Item:            "region",
					Index:           "idx",
					ContinueOnError: &spec.Condition{Expr: testExprPtr("_.forEachErr == true")},
				},
				Inputs: map[string]*spec.ValueRef{
					"cmd": {Tmpl: testTmplPtr(`deploy {{ ._.directInput }} to {{ .region }} #{{ .idx }} ({{ .__item }}/{{ .__index }})`)},
				},
			},
			want: []string{"directInput", "forEachErr", "forEachSrc"},
		},
		{
			name: "forEach.in is evaluated in outer scope so item name in forEach.in still resolves",
			action: &action.Action{
				Provider: "exec",
				ForEach: &spec.ForEachClause{
					In:   &spec.ValueRef{Tmpl: testTmplPtr(`{{ .forEachSrc }}`)},
					Item: "forEachSrc",
				},
				Inputs: map[string]*spec.ValueRef{
					"cmd": {Tmpl: testTmplPtr(`echo {{ .forEachSrc }}`)},
				},
			},
			want: []string{"forEachSrc"},
		},
		{
			name: "when, continueOnError, and retry.retryIf conditions",
			action: &action.Action{
				Provider:        "exec",
				When:            &spec.Condition{Expr: testExprPtr("_.whenFlag")},
				ContinueOnError: &spec.Condition{Expr: testExprPtr("_.errFlag && __error.exitCode != 0")},
				Retry: &action.RetryConfig{
					MaxAttempts: 3,
					RetryIf:     testExprPtr("_.retryFlag"),
				},
				Inputs: map[string]*spec.ValueRef{
					"cmd": {Literal: "echo ok"},
				},
			},
			want: []string{"errFlag", "retryFlag", "whenFlag"},
		},
		{
			name: "workflow action aliases excluded from bare template refs",
			action: &action.Action{
				Provider: "exec",
				Inputs: map[string]*spec.ValueRef{
					"cmd": {Tmpl: testTmplPtr(`{{ .buildStep.results.artifact }} {{ .auditStep.status }} {{ ._.directInput }}`)},
				},
			},
			want: []string{"directInput"},
		},
		{
			name: "go-template provider descriptor excludes data keys and extracts wrapped data refs",
			action: &action.Action{
				Provider: "go-template",
				Inputs: map[string]*spec.ValueRef{
					"name": {Literal: "cfg"},
					"data": {
						Literal: map[string]any{
							"host": map[string]any{"rslvr": "dataHost"},
						},
					},
					"template": {Literal: "host={{ .host }} explicit={{ ._.tmplExplicit }}"},
				},
			},
			want: []string{"dataHost", "tmplExplicit"},
		},
		{
			name: "go-template provider with dynamic data rslvr and tmpl ValueRefs on template and pathTemplate",
			action: &action.Action{
				Provider: "go-template",
				Inputs: map[string]*spec.ValueRef{
					"name":         {Literal: "cfg"},
					"data":         {Resolver: testStrPtr("dataHost")},
					"template":     {Tmpl: testTmplPtr("{{ .fnArgB }} {{ ._.tmplExplicit }}")},
					"pathTemplate": {Tmpl: testTmplPtr("{{ .fnArgA }}.yaml")},
				},
			},
			want: []string{"dataHost", "fnArgA", "fnArgB", "tmplExplicit"},
		},
		{
			name: "tmpl ValueRef wrapper ignores sibling literal data keys because it renders before provider runs",
			action: &action.Action{
				Provider: "go-template",
				Inputs: map[string]*spec.ValueRef{
					"name":     {Literal: "cfg"},
					"data":     {Literal: map[string]any{"dataHost": "local"}},
					"template": {Tmpl: testTmplPtr("{{ .dataHost }}")},
				},
			},
			want: []string{"dataHost"},
		},
		{
			name: "go-template render-tree action scans entry content alongside dynamic data rslvr",
			action: &action.Action{
				Provider: "go-template",
				Inputs: map[string]*spec.ValueRef{
					"operation": {Literal: "render-tree"},
					"data":      {Resolver: testStrPtr("dataHost")},
					"entries": {
						Literal: []any{
							map[string]any{
								"path":    "a.txt",
								"content": "{{ ._.tmplExplicit }}",
							},
						},
					},
				},
			},
			want: []string{"dataHost", "tmplExplicit"},
		},
		{
			name: "inert literal strings on message actions do not trigger resolver references",
			action: &action.Action{
				Provider: "message",
				Inputs: map[string]*spec.ValueRef{
					"message": {Literal: "literal {{ .unrelated }} and _.unrelated"},
				},
			},
			want: []string{},
		},
		{
			name: "go-template data expr preserves optional CEL partitioning without promoting missing optional refs to hard",
			action: &action.Action{
				Provider: "go-template",
				Inputs: map[string]*spec.ValueRef{
					"data":     {Expr: testExprPtr(`{"local": _.?optMissing.orValue("x") + _.?optDefined.orValue("y")}`)},
					"template": {Literal: "{{ .local }} {{ .tmplDirect }}"},
				},
			},
			want: []string{"optDefined", "tmplDirect"},
		},
		{
			name: "cel provider partitions optional-only undefined refs out of hard refs",
			action: &action.Action{
				Provider: "cel",
				Inputs: map[string]*spec.ValueRef{
					"expression": {Literal: `_.?optMissing.orValue("x") + _.?optDefined.orValue("y") + _.celInput`},
				},
			},
			want: []string{"celInput", "optDefined"},
		},
		{
			name: "multi-key literal maps with rslvr/tmpl keys do not short-circuit or invent resolver refs",
			action: &action.Action{
				Provider: "exec",
				Inputs: map[string]*spec.ValueRef{
					"cfg": {
						Literal: map[string]any{
							"tmpl": "header.html",
							"env":  map[string]any{"rslvr": "nestedRslvr"},
							"meta": map[string]any{
								"rslvr": "not-a-resolver-name",
								"extra": "plain-literal",
							},
						},
					},
				},
			},
			want: []string{"nestedRslvr"},
		},
		{
			name: "malformed when condition returns error in strict mode",
			action: &action.Action{
				Provider: "exec",
				When:     &spec.Condition{Expr: testExprPtr("_.whenFlag == true &&& invalid")},
			},
			expectErr: `failed to parse when condition for action "deploy"`,
		},
		{
			name: "malformed continueOnError condition returns error in strict mode",
			action: &action.Action{
				Provider:        "exec",
				ContinueOnError: &spec.Condition{Expr: testExprPtr("&&& invalid")},
			},
			expectErr: `failed to parse continueOnError condition for action "deploy"`,
		},
		{
			name: "malformed retryIf condition returns error in strict mode",
			action: &action.Action{
				Provider: "exec",
				Retry:    &action.RetryConfig{MaxAttempts: 2, RetryIf: testExprPtr("&&& invalid")},
			},
			expectErr: `failed to parse retryIf condition for action "deploy"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sol := baseSolution()
			got, err := ActionResolverRefs(sol, "deploy", tt.action, lookup)
			if tt.expectErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNamedActionsResolverRefs(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{
		Metadata: solution.Metadata{Name: "named-actions-sol"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"buildVer": testStaticResolver("buildVer", nil),
				"slackUrl": testStaticResolver("slackUrl", nil),
				"unused":   testStaticResolver("unused", nil),
			},
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"clean": {
						Name:     "clean",
						Provider: "exec",
						Inputs:   map[string]*spec.ValueRef{"command": {Literal: "rm -rf dist"}},
					},
					"build": {
						Name:     "build",
						Alias:    "b",
						Provider: "exec",
						Inputs:   map[string]*spec.ValueRef{"command": {Resolver: testStrPtr("buildVer")}},
					},
				},
				Finally: map[string]*action.Action{
					"notify": {
						Name:     "notify",
						Provider: "exec",
						Inputs:   map[string]*spec.ValueRef{"url": {Resolver: testStrPtr("slackUrl")}},
					},
				},
			},
		},
	}

	t.Run("resolves action by alias and finally name", func(t *testing.T) {
		t.Parallel()
		refs, err := NamedActionsResolverRefs(sol, []string{"b", "notify"}, nil)
		require.NoError(t, err)
		assert.Equal(t, []string{"buildVer", "slackUrl"}, refs)
	})

	t.Run("zero-ref action returns empty slice without error", func(t *testing.T) {
		t.Parallel()
		refs, err := NamedActionsResolverRefs(sol, []string{"clean"}, nil)
		require.NoError(t, err)
		assert.Empty(t, refs)
	})

	t.Run("unknown action returns error listing available actions", func(t *testing.T) {
		t.Parallel()
		_, err := NamedActionsResolverRefs(sol, []string{"missing"}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `action "missing" not found (available: build, clean, notify)`)
	})

	t.Run("solution without workflow returns error", func(t *testing.T) {
		t.Parallel()
		noWf := &solution.Solution{Metadata: solution.Metadata{Name: "no-wf"}}
		_, err := NamedActionsResolverRefs(noWf, []string{"clean"}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `requires a solution with a workflow section`)
	})
}

func TestResolversForWorkflow(t *testing.T) {
	t.Parallel()

	t.Run("zero-ref workflow returns non-nil empty slice", func(t *testing.T) {
		t.Parallel()
		sol := &solution.Solution{
			Spec: solution.Spec{
				Resolvers: map[string]*resolver.Resolver{
					"broken": testStaticResolver("broken", nil),
				},
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"clean": {
							Name:     "clean",
							Provider: "exec",
							Inputs:   map[string]*spec.ValueRef{"command": {Literal: "echo clean"}},
						},
					},
				},
			},
		}

		filteredWf, err := action.FilterWorkflowActions(sol.Spec.Workflow, []string{"clean"})
		require.NoError(t, err)

		got := ResolversForWorkflow(sol, filteredWf, nil, nil)
		require.NotNil(t, got)
		assert.Empty(t, got)
	})

	t.Run("unions transitive action deps, finally, state config, and cross-resolver validate deps", func(t *testing.T) {
		t.Parallel()
		sol := &solution.Solution{
			Spec: solution.Spec{
				Resolvers: map[string]*resolver.Resolver{
					"baseVer":     testStaticResolver("baseVer", nil),
					"imageTag":    testStaticResolver("imageTag", testExprPtr(`"app:" + _.baseVer`)),
					"allowedTags": testStaticResolver("allowedTags", nil),
					"deployUrl": {
						Name: "deployUrl",
						Resolve: &resolver.ResolvePhase{
							With: []resolver.ProviderSource{{
								Provider: "static",
								Inputs:   map[string]*spec.ValueRef{"value": {Literal: "https://example.com"}},
							}},
						},
						Validate: &resolver.ValidatePhase{
							With: []resolver.ProviderValidation{{
								Provider: "cel",
								Inputs: map[string]*spec.ValueRef{
									"expression": {Expr: testExprPtr(`_.allowedTags != ""`)},
								},
							}},
						},
					},
					"finallyHook": testStaticResolver("finallyHook", nil),
					"statePath":   testStaticResolver("statePath", nil),
					"unrelated":   testStaticResolver("unrelated", nil),
				},
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"build": {
							Name:     "build",
							Provider: "exec",
							Inputs:   map[string]*spec.ValueRef{"tag": {Resolver: testStrPtr("imageTag")}},
						},
						"deploy": {
							Name:      "deploy",
							Provider:  "exec",
							DependsOn: []string{"build"},
							Inputs:    map[string]*spec.ValueRef{"url": {Resolver: testStrPtr("deployUrl")}},
						},
						"lint": {
							Name:     "lint",
							Provider: "exec",
							Inputs:   map[string]*spec.ValueRef{"cmd": {Resolver: testStrPtr("unrelated")}},
						},
					},
					Finally: map[string]*action.Action{
						"cleanup": {
							Name:     "cleanup",
							Provider: "exec",
							Inputs:   map[string]*spec.ValueRef{"hook": {Resolver: testStrPtr("finallyHook")}},
						},
					},
				},
			},
		}

		stateCfg := &state.Config{
			Save: []state.SaveTarget{{
				Provider: "file",
				Inputs:   map[string]*spec.ValueRef{"path": {Resolver: testStrPtr("statePath")}},
			}},
		}

		filteredWf, err := action.FilterWorkflowActions(sol.Spec.Workflow, []string{"deploy"})
		require.NoError(t, err)

		got := ResolversForWorkflow(sol, filteredWf, stateCfg, nil)
		names := make([]string, 0, len(got))
		for _, r := range got {
			names = append(names, r.Name)
		}
		assert.ElementsMatch(t, []string{
			"baseVer",
			"imageTag",
			"deployUrl",
			"allowedTags",
			"finallyHook",
			"statePath",
		}, names)
		assert.NotContains(t, names, "unrelated")
	})

	t.Run("nil solution returns non-nil empty slice", func(t *testing.T) {
		t.Parallel()
		got := ResolversForWorkflow(nil, nil, nil, nil)
		require.NotNil(t, got)
		assert.Empty(t, got)
	})

	t.Run("workflow referencing only undefined resolver returns non-nil empty slice", func(t *testing.T) {
		t.Parallel()
		sol := &solution.Solution{
			Spec: solution.Spec{
				Resolvers: map[string]*resolver.Resolver{
					"known": testStaticResolver("known", nil),
				},
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"ghost": {
							Name:     "ghost",
							Provider: "exec",
							Inputs:   map[string]*spec.ValueRef{"x": {Resolver: testStrPtr("nonexistent.field")}},
						},
					},
				},
			},
		}
		got := ResolversForWorkflow(sol, sol.Spec.Workflow, nil, nil)
		require.NotNil(t, got)
		assert.Empty(t, got)
	})
}

func TestActionResolverRefs_EdgeCasesAndProviderInputs(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{
		Metadata: solution.Metadata{Name: "edge-cases"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"rslvrDot": testStaticResolver("rslvrDot", nil),
				"exprData": testStaticResolver("exprData", nil),
				"dynData":  testStaticResolver("dynData", nil),
				"strCel":   testStaticResolver("strCel", nil),
				"strTmpl":  testStaticResolver("strTmpl", nil),
				"tplName":  testStaticResolver("tplName", nil),
			},
		},
	}

	t.Run("provider-evaluated raw strings vs inert literals and explicit tmpl wrappers", func(t *testing.T) {
		t.Parallel()
		fileAct := &action.Action{
			Provider: "file",
			Inputs: map[string]*spec.ValueRef{
				"operation":  {Literal: "write-tree"},
				"outputPath": {Literal: "{{ .strTmpl }}/{{ .__fileName }}"},
				"content":    {Literal: "inert {{ .exprData }} _.dynData"},
			},
		}
		fileRefs, err := ActionResolverRefs(sol, "fileAct", fileAct, nil)
		require.NoError(t, err)
		assert.Equal(t, []string{"strTmpl"}, fileRefs)

		act := &action.Action{
			Provider: "go-template",
			Inputs: map[string]*spec.ValueRef{
				"verbatim": {Resolver: testStrPtr("rslvrDot.nested")},
				"inertStr": {Literal: "_.strCel + '!' and {{ .dynData }}"},
				"data":     {Expr: testExprPtr(`{"boundKey": _.exprData}`)},
				"template": {Literal: "{{ .boundKey }} {{ .strTmpl }}"},
				"nestedLit": {
					Literal: map[string]any{
						"wrapped": map[string]any{"tmpl": "{{ .strCel }}"},
						"token":   map[string]any{"tkn": "github"},
					},
				},
			},
		}

		refs, err := ActionResolverRefs(sol, "customAct", act, nil)
		require.NoError(t, err)
		assert.Equal(t, []string{"exprData", "rslvrDot.nested", "strCel", "strTmpl"}, refs)
	})

	t.Run("WorkflowResolverRefs tolerates malformed CEL in conditions without failing", func(t *testing.T) {
		t.Parallel()
		wf := &action.Workflow{
			Actions: map[string]*action.Action{
				"bad": {
					Name:     "bad",
					Provider: "exec",
					When:     &spec.Condition{Expr: testExprPtr("&&& invalid")},
					Inputs:   map[string]*spec.ValueRef{"cmd": {Resolver: testStrPtr("strCel")}},
				},
			},
		}
		assert.Equal(t, []string{"strCel"}, WorkflowResolverRefs(sol, wf, nil, nil))
	})

	t.Run("provider ExtractDependencies nil fallback and scrt/tkn/expr/rslvr input conversion", func(t *testing.T) {
		t.Parallel()
		nilReturningLookup := func(_ string) *provider.Descriptor {
			return &provider.Descriptor{
				Name: "plugin-prov",
				ExtractDependencies: func(_ map[string]any) []string {
					return nil
				},
			}
		}
		customLookup := func(_ string) *provider.Descriptor {
			return &provider.Descriptor{
				Name: "plugin-prov",
				ExtractDependencies: func(_ map[string]any) []string {
					return []string{"strTmpl"}
				},
			}
		}

		secName := "api-key"
		act := &action.Action{
			Provider: "plugin-prov",
			Inputs: map[string]*spec.ValueRef{
				"secret":       {Secret: &secName},
				"token":        {Token: &spec.TokenRef{Handler: "github"}},
				"template":     {Resolver: testStrPtr("tplName")},
				"pathTemplate": {Expr: testExprPtr("_.exprData")},
				"data":         {Resolver: testStrPtr("dynData")},
			},
		}

		refsFallback, err := ActionResolverRefs(sol, "act1", act, nilReturningLookup)
		require.NoError(t, err)
		assert.Equal(t, []string{"dynData", "exprData", "tplName"}, refsFallback)

		refsCustom, err := ActionResolverRefs(sol, "act2", act, customLookup)
		require.NoError(t, err)
		assert.Equal(t, []string{"dynData", "exprData", "strTmpl", "tplName"}, refsCustom)
	})

	t.Run("malformed forEach.continueOnError returns error", func(t *testing.T) {
		t.Parallel()
		act := &action.Action{
			Provider: "exec",
			ForEach: &spec.ForEachClause{
				In:              &spec.ValueRef{Literal: []any{"a"}},
				ContinueOnError: &spec.Condition{Expr: testExprPtr("&&& bad")},
			},
		}
		_, err := ActionResolverRefs(sol, "loopAct", act, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `failed to parse forEach.continueOnError condition for action "loopAct"`)
	})

	t.Run("solution provider ignores inert literal strings even when lookup provides ExtractDependencies", func(t *testing.T) {
		t.Parallel()
		naiveSolutionLookup := func(name string) *provider.Descriptor {
			if name != "solution" {
				return nil
			}
			return &provider.Descriptor{
				Name: "solution",
				ExtractDependencies: func(_ map[string]any) []string {
					return []string{"strCel"}
				},
			}
		}
		act := &action.Action{
			Provider: "solution",
			Inputs: map[string]*spec.ValueRef{
				"source": {Literal: "./sub/solution.yaml"},
				"inputs": {Literal: map[string]any{
					"note": "literal _.strCel should not trigger",
					"env":  map[string]any{"rslvr": "exprData"},
				}},
			},
		}
		refs, err := ActionResolverRefs(sol, "subSol", act, naiveSolutionLookup)
		require.NoError(t, err)
		assert.Equal(t, []string{"exprData"}, refs)
	})

	t.Run("whole-namespace template or CEL access conservatively retains all defined resolvers", func(t *testing.T) {
		t.Parallel()
		wholeSol := &solution.Solution{
			Spec: solution.Spec{
				Resolvers: map[string]*resolver.Resolver{
					"alpha": testStaticResolver("alpha", nil),
					"beta":  testStaticResolver("beta", nil),
				},
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"dumpTmpl": {
							Name:     "dumpTmpl",
							Provider: "message",
							Inputs: map[string]*spec.ValueRef{
								"message": {Tmpl: testTmplPtr(`{{ printf "%v" ._ }}`)},
							},
						},
						"dumpCEL": {
							Name:     "dumpCEL",
							Provider: "message",
							Inputs: map[string]*spec.ValueRef{
								"message": {Expr: testExprPtr(`string(size(_))`)},
							},
						},
					},
				},
			},
		}

		wfTmpl, err := action.FilterWorkflowActions(wholeSol.Spec.Workflow, []string{"dumpTmpl"})
		require.NoError(t, err)
		gotTmpl := ResolversForWorkflow(wholeSol, wfTmpl, nil, nil)
		require.Len(t, gotTmpl, 2)
		assert.ElementsMatch(t, []string{"alpha", "beta"}, []string{gotTmpl[0].Name, gotTmpl[1].Name})

		wfCEL, err := action.FilterWorkflowActions(wholeSol.Spec.Workflow, []string{"dumpCEL"})
		require.NoError(t, err)
		gotCEL := ResolversForWorkflow(wholeSol, wfCEL, nil, nil)
		require.Len(t, gotCEL, 2)
		assert.ElementsMatch(t, []string{"alpha", "beta"}, []string{gotCEL[0].Name, gotCEL[1].Name})
	})

	t.Run("filtered-out action aliases do not shadow resolvers, dynamic template inputs retain all resolvers, and file outputPath gates on write-tree", func(t *testing.T) {
		t.Parallel()
		aliasSol := &solution.Solution{
			Spec: solution.Spec{
				Resolvers: map[string]*resolver.Resolver{
					"config":       testStaticResolver("config", nil),
					"templateText": testStaticResolver("templateText", nil),
					"host":         testStaticResolver("host", nil),
				},
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"unselected": {
							Name:     "unselected",
							Alias:    "config",
							Provider: "message",
							Inputs:   map[string]*spec.ValueRef{"message": {Literal: "unused"}},
						},
						"selected": {
							Name:     "selected",
							Provider: "message",
							Inputs:   map[string]*spec.ValueRef{"message": {Tmpl: testTmplPtr("{{ .config }}")}},
						},
						"dynTmpl": {
							Name:     "dynTmpl",
							Provider: "go-template",
							Inputs: map[string]*spec.ValueRef{
								"template": {Resolver: testStrPtr("templateText")},
							},
						},
						"fileWrite": {
							Name:     "fileWrite",
							Provider: "file",
							Inputs: map[string]*spec.ValueRef{
								"operation":  {Literal: "write"},
								"path":       {Literal: "out.txt"},
								"outputPath": {Literal: "{{ .host }}"},
							},
						},
					},
				},
			},
		}

		filteredSelected, err := action.FilterWorkflowActions(aliasSol.Spec.Workflow, []string{"selected"})
		require.NoError(t, err)
		gotSelected := ResolversForWorkflow(aliasSol, filteredSelected, nil, nil)
		require.Len(t, gotSelected, 1)
		assert.Equal(t, "config", gotSelected[0].Name)

		filteredDyn, err := action.FilterWorkflowActions(aliasSol.Spec.Workflow, []string{"dynTmpl"})
		require.NoError(t, err)
		gotDyn := ResolversForWorkflow(aliasSol, filteredDyn, nil, nil)
		assert.Len(t, gotDyn, 3, "dynamically supplied template content conservatively retains all defined resolvers")

		filteredWrite, err := action.FilterWorkflowActions(aliasSol.Spec.Workflow, []string{"fileWrite"})
		require.NoError(t, err)
		assert.Empty(t, ResolversForWorkflow(aliasSol, filteredWrite, nil, nil), "file write operation ignores outputPath")
	})

	t.Run("expression-generated CEL input and dynamic go-template data fallback", func(t *testing.T) {
		t.Parallel()
		sol := &solution.Solution{
			Spec: solution.Spec{
				Resolvers: map[string]*resolver.Resolver{
					"config":   testStaticResolver("config", nil),
					"host":     testStaticResolver("host", nil),
					"shadowed": testStaticResolver("shadowed", nil),
					"unused":   testStaticResolver("unused", nil),
				},
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"celExprGenerated": {
							Name:     "celExprGenerated",
							Provider: "cel",
							Inputs: map[string]*spec.ValueRef{
								"expression": {Expr: testExprPtr(`"_.host"`)},
							},
						},
						"debugExprGenerated": {
							Name:     "debugExprGenerated",
							Provider: "debug",
							Inputs: map[string]*spec.ValueRef{
								"expression": {Expr: testExprPtr(`"_.host"`)},
							},
						},
						"fileTmplGeneratedOutputPath": {
							Name:     "fileTmplGeneratedOutputPath",
							Provider: "file",
							Inputs: map[string]*spec.ValueRef{
								"operation":  {Literal: "write-tree"},
								"outputPath": {Tmpl: testTmplPtr(`{{ ._.config }}/{{ "{{ .host }}" }}`)},
							},
						},
						"tmplDynamicData": {
							Name:     "tmplDynamicData",
							Provider: gotmplprovider.ProviderName,
							Inputs: map[string]*spec.ValueRef{
								"data":     {Resolver: testStrPtr("config")},
								"template": {Literal: "{{ .host }}:{{ .dynamicKeyOnlyInConfig }}"},
							},
						},
						"treeDynamicData": {
							Name:     "treeDynamicData",
							Provider: gotmplprovider.ProviderName,
							Inputs: map[string]*spec.ValueRef{
								"operation": {Literal: gotmplprovider.OperationRenderTree},
								"entries": {
									Literal: []any{
										map[string]any{
											"path":    "out.txt",
											"data":    map[string]any{"rslvr": "config"},
											"content": "{{ .host }}:{{ .dynamicKeyOnlyInConfig }}",
										},
									},
								},
							},
						},
						"treeDynamicSharedStaticEntry": {
							Name:     "treeDynamicSharedStaticEntry",
							Provider: gotmplprovider.ProviderName,
							Inputs: map[string]*spec.ValueRef{
								"operation":    {Literal: gotmplprovider.OperationRenderTree},
								"data":         {Resolver: testStrPtr("config")},
								"forEach":      {Literal: map[string]any{"item": "env", "in": []any{"dev"}}},
								"pathTemplate": {Literal: "{{ .shadowed }}/{{ .env }}/{{ .__fileName }}"},
								"entries": {
									Literal: []any{
										map[string]any{
											"path":    "out.txt",
											"data":    map[string]any{"shadowed": "local"},
											"content": "{{ .shadowed }}:{{ .host }}:{{ .dynamicKeyOnlyInConfig }}",
										},
									},
								},
							},
						},
						"treeStaticSharedDynamicEntry": {
							Name:     "treeStaticSharedDynamicEntry",
							Provider: gotmplprovider.ProviderName,
							Inputs: map[string]*spec.ValueRef{
								"operation":    {Literal: gotmplprovider.OperationRenderTree},
								"data":         {Literal: map[string]any{"shadowed": "local"}},
								"forEach":      {Literal: map[string]any{"item": "env", "in": []any{"dev"}}},
								"pathTemplate": {Literal: "{{ .shadowed }}/{{ .env }}/{{ .__fileName }}"},
								"entries": {
									Literal: []any{
										map[string]any{
											"path":    "out.txt",
											"data":    map[string]any{"rslvr": "config"},
											"content": "{{ .shadowed }}:{{ .host }}:{{ .dynamicKeyOnlyInConfig }}",
										},
									},
								},
							},
						},
						"treeMultiKeyLiteralEntryData": {
							Name:     "treeMultiKeyLiteralEntryData",
							Provider: gotmplprovider.ProviderName,
							Inputs: map[string]*spec.ValueRef{
								"operation":    {Literal: gotmplprovider.OperationRenderTree},
								"data":         {Resolver: testStrPtr("config")},
								"forEach":      {Literal: map[string]any{"item": "env", "in": []any{"dev"}}},
								"pathTemplate": {Literal: "{{ .shadowed }}/{{ .env }}/{{ .__fileName }}"},
								"entries": {
									Literal: []any{
										map[string]any{
											"path":    "out.txt",
											"data":    map[string]any{"expr": "text", "shadowed": "local"},
											"content": "{{ .shadowed }}:{{ .expr }}:{{ .host }}",
										},
									},
								},
							},
						},
					},
				},
			},
		}

		for _, dynAct := range []string{"celExprGenerated", "debugExprGenerated", "fileTmplGeneratedOutputPath"} {
			wf, err := action.FilterWorkflowActions(sol.Spec.Workflow, []string{dynAct})
			require.NoError(t, err)
			got := ResolversForWorkflow(sol, wf, nil, nil)
			assert.Len(t, got, 4, "%s with dynamic template/expression ValueRef conservatively retains all defined resolvers", dynAct)
		}

		for _, tmplAct := range []string{"tmplDynamicData", "treeDynamicData", "treeDynamicSharedStaticEntry", "treeStaticSharedDynamicEntry", "treeMultiKeyLiteralEntryData"} {
			wf, err := action.FilterWorkflowActions(sol.Spec.Workflow, []string{tmplAct})
			require.NoError(t, err)
			got := ResolversForWorkflow(sol, wf, nil, nil)
			names := make([]string, 0, len(got))
			for _, r := range got {
				names = append(names, r.Name)
			}
			assert.ElementsMatch(t, []string{"config", "host"}, names, "%s retains fallback defined resolver while excluding shadowed and unused resolvers", tmplAct)
		}

		// Direct candidate-partitioning checks, including soft-before-hard reference order.
		hardDeps, candidateDeps := gotmplprovider.ExtractRawTemplateDependenciesWithCandidates(map[string]any{
			"data":     map[string]any{"rslvr": "config"},
			"template": "{{ .host }} {{ .port }} {{ ._.host }}",
		})
		assert.Equal(t, []string{"host"}, hardDeps)
		assert.Equal(t, []string{"port"}, candidateDeps)

		treeHardDeps, treeCandidateDeps := gotmplprovider.ExtractRawTemplateDependenciesWithCandidates(map[string]any{
			"operation": gotmplprovider.OperationRenderTree,
			"entries": []any{
				map[string]any{
					"path":    "first.txt",
					"data":    map[string]any{"rslvr": "config"},
					"content": "{{ .host }} {{ .fallbackOnly }}",
				},
				map[string]any{
					"path":    "second.txt",
					"content": "{{ ._.host }}",
				},
			},
		})
		assert.Equal(t, []string{"host"}, treeHardDeps)
		assert.Equal(t, []string{"fallbackOnly"}, treeCandidateDeps)
	})
}

func BenchmarkResolversForWorkflow(b *testing.B) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"baseVer":     testStaticResolver("baseVer", nil),
				"imageTag":    testStaticResolver("imageTag", testExprPtr(`"app:" + _.baseVer`)),
				"deployEnv":   testStaticResolver("deployEnv", nil),
				"finallyHook": testStaticResolver("finallyHook", nil),
				"unusedOne":   testStaticResolver("unusedOne", nil),
				"unusedTwo":   testStaticResolver("unusedTwo", nil),
			},
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"build": {
						Name:     "build",
						Provider: "exec",
						Inputs:   map[string]*spec.ValueRef{"tag": {Resolver: testStrPtr("imageTag")}},
					},
					"deploy": {
						Name:      "deploy",
						Provider:  "exec",
						DependsOn: []string{"build"},
						When:      &spec.Condition{Expr: testExprPtr(`_.deployEnv == "prod"`)},
						Inputs:    map[string]*spec.ValueRef{"cmd": {Tmpl: testTmplPtr("deploy {{ ._.imageTag }}")}},
					},
				},
				Finally: map[string]*action.Action{
					"notify": {
						Name:     "notify",
						Provider: "exec",
						Inputs:   map[string]*spec.ValueRef{"url": {Resolver: testStrPtr("finallyHook")}},
					},
				},
			},
		},
	}
	wf, err := action.FilterWorkflowActions(sol.Spec.Workflow, []string{"deploy"})
	require.NoError(b, err)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = ResolversForWorkflow(sol, wf, nil, nil)
	}
}
