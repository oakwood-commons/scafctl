// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractDependencies(t *testing.T) {
	tests := []struct {
		name     string
		resolver *Resolver
		want     []string
	}{
		{
			name: "no dependencies",
			resolver: &Resolver{
				Name: "simple",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value": {Literal: "test"},
							},
						},
					},
				},
			},
			want: []string{},
		},
		{
			name: "resolver reference in input",
			resolver: &Resolver{
				Name: "dependent",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "cel",
							Inputs: map[string]*ValueRef{
								"value": {Resolver: stringPtr("base")},
							},
						},
					},
				},
			},
			want: []string{"base"},
		},
		{
			name: "cel expression with underscore variable",
			resolver: &Resolver{
				Name: "dependent",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "cel",
							Inputs: map[string]*ValueRef{
								"expr": {Expr: celExpPtr("_.environment + '-prod'")},
							},
						},
					},
				},
			},
			want: []string{"environment"},
		},
		{
			name: "template with underscore variable",
			resolver: &Resolver{
				Name: "dependent",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value": {Tmpl: tmplPtr("{{ ._.region }}-east")},
							},
						},
					},
				},
			},
			want: []string{"region"},
		},
		{
			name: "when condition with dependency",
			resolver: &Resolver{
				Name: "conditional",
				When: &Condition{
					Expr: celExpPtr("_.enabled == true"),
				},
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value": {Literal: "test"},
							},
						},
					},
				},
			},
			want: []string{"enabled"},
		},
		{
			name: "multiple dependencies from different phases",
			resolver: &Resolver{
				Name: "complex",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "cel",
							Inputs: map[string]*ValueRef{
								"value": {Resolver: stringPtr("base")},
							},
						},
					},
				},
				Transform: &TransformPhase{
					With: []ProviderTransform{
						{
							Provider: "cel",
							Inputs: map[string]*ValueRef{
								"expr": {Expr: celExpPtr("_.region + '-' + __self")},
							},
						},
					},
				},
				Validate: &ValidatePhase{
					With: []ProviderValidation{
						{
							Provider: "validation",
							Inputs: map[string]*ValueRef{
								"rule": {Expr: celExpPtr("__self != _.invalid")},
							},
							Message: &ValueRef{Tmpl: tmplPtr("Invalid: {{ ._.environment }}")},
						},
					},
				},
			},
			// Two-phase validation: validate-phase refs (invalid, environment) are
			// NOT resolution dependencies; only resolve/transform refs remain.
			want: []string{"base", "region"},
		},
		{
			name: "phase-level when conditions",
			resolver: &Resolver{
				Name: "phaseConditional",
				Resolve: &ResolvePhase{
					When: &Condition{
						Expr: celExpPtr("_.enabled == true"),
					},
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value": {Literal: "test"},
							},
						},
					},
				},
				Transform: &TransformPhase{
					When: &Condition{
						Expr: celExpPtr("_.transform_enabled == true"),
					},
					With: []ProviderTransform{
						{
							Provider: "cel",
							Inputs: map[string]*ValueRef{
								"expr": {Literal: "__self + '-suffix'"},
							},
						},
					},
				},
			},
			want: []string{"enabled", "transform_enabled"},
		},
		{
			name: "until condition in resolve phase",
			resolver: &Resolver{
				Name: "withUntil",
				Resolve: &ResolvePhase{
					Until: &Condition{
						Expr: celExpPtr("_.max_retries > 5"),
					},
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value": {Literal: "test"},
							},
						},
					},
				},
			},
			want: []string{"max_retries"},
		},
		{
			name: "source-level when condition",
			resolver: &Resolver{
				Name: "sourceConditional",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "parameter",
							When: &Condition{
								Expr: celExpPtr("_.use_param == true"),
							},
							Inputs: map[string]*ValueRef{
								"name": {Literal: "test"},
							},
						},
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value": {Literal: "default"},
							},
						},
					},
				},
			},
			want: []string{"use_param"},
		},
		{
			name: "multiple cel expressions",
			resolver: &Resolver{
				Name: "multipleExpressions",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "cel",
							Inputs: map[string]*ValueRef{
								"expr": {Expr: celExpPtr("_.env + '-' + _.region + '-' + _.account")},
							},
						},
					},
				},
			},
			want: []string{"env", "region", "account"},
		},
		{
			name: "template with multiple variables",
			resolver: &Resolver{
				Name: "multipleTemplateVars",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value": {Tmpl: tmplPtr("{{ ._.env }}-{{ ._.region }}-{{ ._.account }}")},
							},
						},
					},
				},
			},
			want: []string{"env", "region", "account"},
		},
		{
			name: "__self in template should not be treated as dependency",
			resolver: &Resolver{
				Name: "selfInTemplate",
				Validate: &ValidatePhase{
					With: []ProviderValidation{
						{
							Provider: "validation",
							Inputs: map[string]*ValueRef{
								"match": {Literal: "^[a-z0-9-]+$"},
							},
							Message: &ValueRef{Tmpl: tmplPtr("Invalid value '{{ .__self }}' for environment {{ ._.environment }}")},
						},
					},
				},
			},
			// Two-phase validation: a validate message referencing another resolver
			// (environment) is deferred, not a resolution dependency.
			want: []string{},
		},
		{
			name: "go-template provider with direct root-level references",
			resolver: &Resolver{
				Name: "goTemplateProvider",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "go-template",
							Inputs: map[string]*ValueRef{
								// Direct root-level template references (go-template provider pattern)
								// These use {{.resolverName}} instead of {{._.resolverName}}
								"template": {Literal: "Environment: {{.environment}}\nRegion: {{.region}}"},
								"name":     {Literal: "test-template"},
							},
						},
					},
				},
			},
			want: []string{"environment", "region"},
		},
		{
			name: "go-template provider with nested field access",
			resolver: &Resolver{
				Name: "goTemplateNestedAccess",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "go-template",
							Inputs: map[string]*ValueRef{
								// Template accessing nested fields (e.g., {{.config.host}} should depend on "config")
								"template": {Literal: "Host: {{.config.host}}\nPort: {{.config.port}}"},
								"name":     {Literal: "nested-template"},
							},
						},
					},
				},
			},
			want: []string{"config"},
		},
		{
			name: "go-template provider with range over resolver array",
			resolver: &Resolver{
				Name: "goTemplateRange",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "go-template",
							Inputs: map[string]*ValueRef{
								// Inside range blocks, {{.name}} refers to the element's field,
								// not a top-level resolver. The scope-aware parser correctly
								// excludes it as a dependency.
								"template": {Literal: "{{range .servers}}- {{.name}}{{end}}"},
								"name":     {Literal: "range-template"},
							},
						},
					},
				},
			},
			// Only "servers" is a root-level dependency.
			// ".name" inside the range body is scoped to each element, not a resolver.
			want: []string{"servers"},
		},
		{
			name: "explicit dependsOn only",
			resolver: &Resolver{
				Name:      "explicitDeps",
				DependsOn: []string{"config", "credentials"},
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value": {Literal: "test"},
							},
						},
					},
				},
			},
			want: []string{"config", "credentials"},
		},
		{
			name: "dependsOn merged with auto-extracted",
			resolver: &Resolver{
				Name:      "mergedDeps",
				DependsOn: []string{"explicit-dep"},
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "cel",
							Inputs: map[string]*ValueRef{
								"value": {Resolver: stringPtr("auto-dep")},
							},
						},
					},
				},
			},
			want: []string{"explicit-dep", "auto-dep"},
		},
		{
			name: "dependsOn with duplicates and empty entries",
			resolver: &Resolver{
				Name:      "dedupeDeps",
				DependsOn: []string{"config", "", "config", "other"},
				When: &Condition{
					Expr: celExpPtr("_.config != nil"),
				},
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value": {Literal: "test"},
							},
						},
					},
				},
			},
			// config appears in both dependsOn and when condition - should be deduplicated
			want: []string{"config", "other"},
		},
		{
			name: "self-reference in validate expression is not a dependency",
			resolver: &Resolver{
				Name: "publicSiteCheck",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "http",
							Inputs: map[string]*ValueRef{
								"url":    {Literal: "https://httpbin.org/get"},
								"method": {Literal: "GET"},
							},
						},
					},
				},
				Validate: &ValidatePhase{
					With: []ProviderValidation{
						{
							Provider: "validation",
							Inputs: map[string]*ValueRef{
								"expression": {Expr: celExpPtr("_.publicSiteCheck.statusCode == 200")},
							},
						},
					},
				},
			},
			// _.publicSiteCheck inside publicSiteCheck's validate phase is __self, not a dependency
			want: []string{},
		},
		{
			name: "self-reference in transform expression is not a dependency",
			resolver: &Resolver{
				Name: "myResolver",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value": {Literal: "hello"},
							},
						},
					},
				},
				Transform: &TransformPhase{
					With: []ProviderTransform{
						{
							Provider: "cel",
							Inputs: map[string]*ValueRef{
								"expression": {Expr: celExpPtr("_.myResolver + '-suffix'")},
							},
						},
					},
				},
			},
			// _.myResolver inside myResolver's transform phase is __self, not a dependency
			want: []string{},
		},
		{
			name: "self-reference in resolve phase IS a real dependency",
			resolver: &Resolver{
				Name: "selfRef",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "cel",
							Inputs: map[string]*ValueRef{
								"value": {Resolver: stringPtr("selfRef")},
							},
						},
					},
				},
			},
			// Self-reference in resolve phase is a genuine circular dependency
			want: []string{"selfRef"},
		},
		{
			name: "validate cross-resolver ref is deferred, not a resolution dep",
			resolver: &Resolver{
				Name: "checker",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value": {Literal: "test"},
							},
						},
					},
				},
				Validate: &ValidatePhase{
					With: []ProviderValidation{
						{
							Provider: "validation",
							Inputs: map[string]*ValueRef{
								"expression": {Expr: celExpPtr("_.checker != _.otherResolver")},
							},
						},
					},
				},
			},
			// Two-phase validation: _.otherResolver is referenced only in a validate
			// rule, so it is deferred and does NOT create a resolution-graph edge.
			want: []string{},
		},
		{
			name: "bracket notation CEL expression dependency",
			resolver: &Resolver{
				Name: "bracketTest",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "cel",
							Inputs: map[string]*ValueRef{
								"expression": {Expr: celExpPtr(`_["base"] + "-suffix"`)},
							},
						},
					},
				},
			},
			want: []string{"base"},
		},
		{
			name: "bracket notation mixed with dot notation dependency",
			resolver: &Resolver{
				Name: "mixedNotation",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "cel",
							Inputs: map[string]*ValueRef{
								"expression": {Expr: celExpPtr(`_.dotRef + _["bracketRef"]`)},
							},
						},
					},
				},
			},
			want: []string{"dotRef", "bracketRef"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDependencies(tt.resolver, nil)

			// Convert to map for easier comparison (order doesn't matter)
			gotMap := make(map[string]bool)
			for _, dep := range got {
				gotMap[dep] = true
			}

			wantMap := make(map[string]bool)
			for _, dep := range tt.want {
				wantMap[dep] = true
			}

			assert.Equal(t, wantMap, gotMap, "dependencies should match")
		})
	}
}

func TestClassifyValidationRule(t *testing.T) {
	tests := []struct {
		name string
		self string
		rule ProviderValidation
		want []string
	}{
		{
			name: "self-only rule has no foreign refs",
			self: "env",
			rule: ProviderValidation{
				Provider: "validation",
				Inputs: map[string]*ValueRef{
					"expression": {Expr: celExpPtr("__self != ''")},
				},
			},
			want: []string{},
		},
		{
			name: "self reference by name is filtered",
			self: "env",
			rule: ProviderValidation{
				Provider: "validation",
				Inputs: map[string]*ValueRef{
					"expression": {Expr: celExpPtr("_.env != ''")},
				},
			},
			want: []string{},
		},
		{
			name: "foreign ref in inputs is returned",
			self: "env",
			rule: ProviderValidation{
				Provider: "validation",
				Inputs: map[string]*ValueRef{
					"expression": {Expr: celExpPtr("_.env != _.other")},
				},
			},
			want: []string{"other"},
		},
		{
			name: "foreign ref in rule-level when is returned",
			self: "env",
			rule: ProviderValidation{
				Provider: "validation",
				When:     &Condition{Expr: celExpPtr("_.enabled == true")},
				Inputs: map[string]*ValueRef{
					"expression": {Expr: celExpPtr("__self != ''")},
				},
			},
			want: []string{"enabled"},
		},
		{
			name: "foreign ref only in message template is returned",
			self: "env",
			rule: ProviderValidation{
				Provider: "validation",
				Inputs: map[string]*ValueRef{
					"match": {Literal: "^[a-z]+$"},
				},
				Message: &ValueRef{Tmpl: tmplPtr("bad value, see {{ ._.other }}")},
			},
			want: []string{"other"},
		},
		{
			name: "multiple foreign refs across inputs and message",
			self: "env",
			rule: ProviderValidation{
				Provider: "validation",
				Inputs: map[string]*ValueRef{
					"expression": {Expr: celExpPtr("_.env != _.a")},
				},
				Message: &ValueRef{Tmpl: tmplPtr("conflict with {{ ._.b }}")},
			},
			want: []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyValidationRule(tt.rule, tt.self, nil)

			wantMap := make(map[string]bool)
			for _, ref := range tt.want {
				wantMap[ref] = true
			}
			assert.Equal(t, wantMap, got, "foreign refs should match")
		})
	}
}

func TestPartitionValidatePhase(t *testing.T) {
	tests := []struct {
		name              string
		resolver          *Resolver
		wantInline        []int
		wantDeferred      []int
		wantDeferredRefs  []string
		wantPhaseDeferred bool
	}{
		{
			name:         "nil resolver yields empty partition",
			resolver:     nil,
			wantInline:   nil,
			wantDeferred: nil,
		},
		{
			name: "nil validate phase yields empty partition",
			resolver: &Resolver{
				Name: "env",
			},
			wantInline:   nil,
			wantDeferred: nil,
		},
		{
			name: "self-only rules stay inline",
			resolver: &Resolver{
				Name: "env",
				Validate: &ValidatePhase{
					With: []ProviderValidation{
						{Provider: "validation", Inputs: map[string]*ValueRef{"expression": {Expr: celExpPtr("__self != ''")}}},
						{Provider: "validation", Inputs: map[string]*ValueRef{"expression": {Expr: celExpPtr("_.env.size() > 0")}}},
					},
				},
			},
			wantInline:   []int{0, 1},
			wantDeferred: nil,
		},
		{
			name: "foreign ref defers only that rule",
			resolver: &Resolver{
				Name: "env",
				Validate: &ValidatePhase{
					With: []ProviderValidation{
						{Provider: "validation", Inputs: map[string]*ValueRef{"expression": {Expr: celExpPtr("__self != ''")}}},
						{Provider: "validation", Inputs: map[string]*ValueRef{"expression": {Expr: celExpPtr("_.env != _.other")}}},
					},
				},
			},
			wantInline:       []int{0},
			wantDeferred:     []int{1},
			wantDeferredRefs: []string{"other"},
		},
		{
			name: "message-only foreign ref defers the rule",
			resolver: &Resolver{
				Name: "env",
				Validate: &ValidatePhase{
					With: []ProviderValidation{
						{
							Provider: "validation",
							Inputs:   map[string]*ValueRef{"match": {Literal: "^[a-z]+$"}},
							Message:  &ValueRef{Tmpl: tmplPtr("conflicts with {{ ._.other }}")},
						},
					},
				},
			},
			wantInline:       nil,
			wantDeferred:     []int{0},
			wantDeferredRefs: []string{"other"},
		},
		{
			name: "phase-level foreign when forces whole block to defer",
			resolver: &Resolver{
				Name: "env",
				Validate: &ValidatePhase{
					When: &Condition{Expr: celExpPtr("_.gate == true")},
					With: []ProviderValidation{
						{Provider: "validation", Inputs: map[string]*ValueRef{"expression": {Expr: celExpPtr("__self != ''")}}},
						{Provider: "validation", Inputs: map[string]*ValueRef{"expression": {Expr: celExpPtr("_.env != _.other")}}},
					},
				},
			},
			wantInline:        nil,
			wantDeferred:      []int{0, 1},
			wantDeferredRefs:  []string{"gate", "other"},
			wantPhaseDeferred: true,
		},
		{
			name: "phase-level self-only when does not force deferral",
			resolver: &Resolver{
				Name: "env",
				Validate: &ValidatePhase{
					When: &Condition{Expr: celExpPtr("_.env != ''")},
					With: []ProviderValidation{
						{Provider: "validation", Inputs: map[string]*ValueRef{"expression": {Expr: celExpPtr("__self != ''")}}},
					},
				},
			},
			wantInline:   []int{0},
			wantDeferred: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := partitionValidatePhase(tt.resolver, nil)

			assert.Equal(t, tt.wantInline, got.InlineRules, "inline rule indices")
			assert.Equal(t, tt.wantDeferred, got.DeferredRules, "deferred rule indices")
			assert.Equal(t, tt.wantPhaseDeferred, got.PhaseWhenDeferred, "phase-when deferred flag")

			wantRefs := make(map[string]bool)
			for _, ref := range tt.wantDeferredRefs {
				wantRefs[ref] = true
			}
			assert.Equal(t, wantRefs, got.DeferredRefs, "deferred refs")
			assert.Equal(t, len(tt.wantDeferred) > 0, got.HasDeferred(), "HasDeferred")
		})
	}
}

func TestExtractInferredDependencies(t *testing.T) {
	tests := []struct {
		name     string
		resolver *Resolver
		want     []string
	}{
		{
			name: "explicit dependsOn excluded from inferred",
			resolver: &Resolver{
				Name:      "target",
				DependsOn: []string{"config", "credentials"},
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value": {Literal: "test"},
							},
						},
					},
				},
			},
			want: []string{},
		},
		{
			name: "inferred from expr without explicit dependsOn",
			resolver: &Resolver{
				Name:      "target",
				DependsOn: []string{"explicit"},
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "cel",
							Inputs: map[string]*ValueRef{
								"expression": {Expr: celExpPtr("_.registry + '/' + _.namespace")},
							},
						},
					},
				},
			},
			want: []string{"registry", "namespace"},
		},
		{
			name: "inferred from rslvr reference",
			resolver: &Resolver{
				Name:      "target",
				DependsOn: []string{"other"},
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value": {Resolver: stringPtr("env")},
							},
						},
					},
				},
			},
			want: []string{"env"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractInferredDependencies(tt.resolver, nil)

			gotMap := make(map[string]bool)
			for _, dep := range got {
				gotMap[dep] = true
			}

			wantMap := make(map[string]bool)
			for _, dep := range tt.want {
				wantMap[dep] = true
			}

			assert.Equal(t, wantMap, gotMap, "inferred dependencies should match")
		})
	}
}

func TestExtractDepsFromExpression(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want []string
	}{
		{
			name: "no underscore variables",
			expr: "true && false",
			want: []string{},
		},
		{
			name: "single underscore variable",
			expr: "_.environment == 'prod'",
			want: []string{"environment"},
		},
		{
			name: "multiple underscore variables",
			expr: "_.env + '-' + _.region + '-' + _.account",
			want: []string{"env", "region", "account"},
		},
		{
			name: "nested expressions",
			expr: "(_.enabled == true) && (_.region != '') && (_.account != '')",
			want: []string{"enabled", "region", "account"},
		},
		{
			name: "bracket notation single",
			expr: `_["environment"]`,
			want: []string{"environment"},
		},
		{
			name: "bracket notation multiple",
			expr: `_["env"] + '-' + _["region"]`,
			want: []string{"env", "region"},
		},
		{
			name: "bracket notation mixed with dot",
			expr: `_.env + _["region"]`,
			want: []string{"env", "region"},
		},
		{
			name: "optional select",
			expr: `_.?platformProfileID.orValue("")`,
			want: []string{"platformProfileID"},
		},
		{
			name: "optional select mixed with plain select",
			expr: `_.?optionalDep.orValue("") + _.plainDep`,
			want: []string{"optionalDep", "plainDep"},
		},
		{
			name: "optional index bracket notation",
			expr: `_[?"my-resolver"].orValue("")`,
			want: []string{"my-resolver"},
		},
		{
			name: "invalid expression (should not panic)",
			expr: "this is not valid CEL",
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := make(map[string]bool)
			extractDepsFromExpression(tt.expr, deps)

			got := make([]string, 0, len(deps))
			for dep := range deps {
				got = append(got, dep)
			}

			// Convert to maps for comparison
			gotMap := make(map[string]bool)
			for _, dep := range got {
				gotMap[dep] = true
			}

			wantMap := make(map[string]bool)
			for _, dep := range tt.want {
				wantMap[dep] = true
			}

			assert.Equal(t, wantMap, gotMap, "extracted dependencies should match")
		})
	}
}

func TestExtractDepsFromTemplate(t *testing.T) {
	tests := []struct {
		name string
		tmpl string
		want []string
	}{
		{
			name: "direct root-level variable",
			tmpl: "{{ .value }}",
			// Now extracts root-level references for go-template provider compatibility
			want: []string{"value"},
		},
		{
			name: "single underscore variable with dot prefix",
			tmpl: "{{ ._.environment }}",
			want: []string{"environment"},
		},
		{
			name: "multiple underscore variables",
			tmpl: "{{ ._.env }}-{{ ._.region }}-{{ ._.account }}",
			want: []string{"env", "region", "account"},
		},
		{
			name: "underscore variable with spaces",
			tmpl: "{{  ._.environment  }}",
			want: []string{"environment"},
		},
		{
			name: "mixed root-level and underscore variables",
			tmpl: "{{ .value }} - {{ ._.environment }} - {{ .other }}",
			// Now extracts both root-level refs and underscore refs
			want: []string{"value", "environment", "other"},
		},
		{
			name: "multiple direct root-level variables",
			tmpl: "{{ .environment }} - {{ .region }} - {{ .cluster }}",
			want: []string{"environment", "region", "cluster"},
		},
		{
			name: "nested access extracts top-level",
			tmpl: "{{ .config.host }}:{{ .config.port }}",
			// Nested access should extract the top-level resolver name
			want: []string{"config"},
		},
		{
			name: "__self should not be extracted",
			tmpl: "{{ .__self }} - {{ .environment }}",
			want: []string{"environment"},
		},
		{
			name: "__item and __index should not be extracted",
			tmpl: "{{ .__item }} at {{ .__index }}",
			want: []string{},
		},
		{
			name: "with block scopes inner references",
			tmpl: `{{ with .platformAssets.body.data }}{{ .kubeNamespaces }}{{ end }}`,
			want: []string{"platformAssets"},
		},
		{
			name: "range block scopes inner references",
			tmpl: `{{ range .items }}{{ .name }}{{ end }}`,
			want: []string{"items"},
		},
		{
			name: "nested with/range blocks",
			tmpl: `{{ with .config }}{{ range .servers }}{{ .host }}{{ end }}{{ end }}`,
			want: []string{"config"},
		},
		{
			name: "if block does not scope references",
			tmpl: `{{ if .enabled }}{{ .value }}{{ end }}`,
			want: []string{"enabled", "value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := make(map[string]bool)
			extractDepsFromTemplate(tt.tmpl, deps)

			got := make([]string, 0, len(deps))
			for dep := range deps {
				got = append(got, dep)
			}

			// Convert to maps for comparison
			gotMap := make(map[string]bool)
			for _, dep := range got {
				gotMap[dep] = true
			}

			wantMap := make(map[string]bool)
			for _, dep := range tt.want {
				wantMap[dep] = true
			}

			assert.Equal(t, wantMap, gotMap, "extracted dependencies should match")
		})
	}
}

func TestExtractDepsFromValueRef(t *testing.T) {
	tests := []struct {
		name string
		ref  *ValueRef
		want []string
	}{
		{
			name: "nil value ref",
			ref:  nil,
			want: []string{},
		},
		{
			name: "literal value",
			ref:  &ValueRef{Literal: "test"},
			want: []string{},
		},
		{
			name: "resolver reference",
			ref:  &ValueRef{Resolver: stringPtr("base")},
			want: []string{"base"},
		},
		{
			name: "cel expression",
			ref:  &ValueRef{Expr: celExpPtr("_.environment + '-prod'")},
			want: []string{"environment"},
		},
		{
			name: "template",
			ref:  &ValueRef{Tmpl: tmplPtr("{{ ._.region }}-east")},
			want: []string{"region"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := make(map[string]bool)
			extractDepsFromValueRef(tt.ref, deps)

			got := make([]string, 0, len(deps))
			for dep := range deps {
				got = append(got, dep)
			}

			// Convert to maps for comparison
			gotMap := make(map[string]bool)
			for _, dep := range got {
				gotMap[dep] = true
			}

			wantMap := make(map[string]bool)
			for _, dep := range tt.want {
				wantMap[dep] = true
			}

			assert.Equal(t, wantMap, gotMap, "extracted dependencies should match")
		})
	}
}

func TestBuildGraph(t *testing.T) {
	tests := []struct {
		name      string
		resolvers []*Resolver
		wantErr   bool
		validate  func(t *testing.T, graph *Graph)
	}{
		{
			name:      "empty resolvers",
			resolvers: []*Resolver{},
			wantErr:   false,
			validate: func(t *testing.T, graph *Graph) {
				assert.Equal(t, 0, len(graph.Nodes))
				assert.Equal(t, 0, len(graph.Edges))
				assert.Equal(t, 0, len(graph.Phases))
			},
		},
		{
			name: "single resolver no dependencies",
			resolvers: []*Resolver{
				{
					Name: "simple",
					Type: TypeString,
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "static",
								Inputs: map[string]*ValueRef{
									"value": {Literal: "test"},
								},
							},
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, graph *Graph) {
				require.Equal(t, 1, len(graph.Nodes))
				assert.Equal(t, "simple", graph.Nodes[0].Name)
				assert.Equal(t, TypeString, graph.Nodes[0].Type)
				assert.Equal(t, 1, graph.Nodes[0].Phase)
				assert.Equal(t, 0, len(graph.Edges))
			},
		},
		{
			name: "two resolvers with dependency",
			resolvers: []*Resolver{
				{
					Name: "base",
					Type: TypeString,
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "static",
								Inputs: map[string]*ValueRef{
									"value": {Literal: "base"},
								},
							},
						},
					},
				},
				{
					Name: "dependent",
					Type: TypeString,
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "cel",
								Inputs: map[string]*ValueRef{
									"value": {Resolver: stringPtr("base")},
								},
							},
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, graph *Graph) {
				require.Equal(t, 2, len(graph.Nodes))
				require.Equal(t, 2, len(graph.Phases))
				assert.Equal(t, 1, len(graph.Edges))

				// Verify stats
				assert.Equal(t, 2, graph.Stats.TotalResolvers)
				assert.Equal(t, 2, graph.Stats.TotalPhases)
			},
		},
		{
			name: "dependency via optional access expression",
			resolvers: []*Resolver{
				{
					Name: "base",
					Type: TypeString,
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "static",
								Inputs: map[string]*ValueRef{
									"value": {Literal: "base"},
								},
							},
						},
					},
				},
				{
					Name: "dependent",
					Type: TypeString,
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "cel",
								Inputs: map[string]*ValueRef{
									"value": {Expr: celExpPtr(`_.?base.orValue("")`)},
								},
							},
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, graph *Graph) {
				require.Equal(t, 2, len(graph.Nodes))
				// Optional access _.?base must produce a dependency edge so that
				// base is ordered before dependent.
				require.Equal(t, 2, len(graph.Phases))
				assert.Equal(t, 1, len(graph.Edges))

				phaseOf := make(map[string]int)
				for _, n := range graph.Nodes {
					phaseOf[n.Name] = n.Phase
				}
				assert.Less(t, phaseOf["base"], phaseOf["dependent"],
					"base must resolve before dependent when referenced via optional access")
			},
		},
		{
			name: "conditional resolver",
			resolvers: []*Resolver{
				{
					Name: "conditional",
					Type: TypeString,
					When: &Condition{
						Expr: celExpPtr("_.enabled == true"),
					},
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "static",
								Inputs: map[string]*ValueRef{
									"value": {Literal: "test"},
								},
							},
						},
					},
				},
				{
					Name: "enabled",
					Type: TypeBool,
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "static",
								Inputs: map[string]*ValueRef{
									"value": {Literal: true},
								},
							},
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, graph *Graph) {
				conditionalNode := graph.findNode("conditional")
				require.NotNil(t, conditionalNode)
				assert.True(t, conditionalNode.Conditional)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			graph, err := BuildGraph(tt.resolvers, nil)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, graph)

			if tt.validate != nil {
				tt.validate(t, graph)
			}
		})
	}
}

func TestCriticalPath(t *testing.T) {
	tests := []struct {
		name              string
		resolvers         []*Resolver
		wantCriticalPath  []string
		wantCriticalDepth int
	}{
		{
			name:              "empty graph",
			resolvers:         []*Resolver{},
			wantCriticalPath:  nil,
			wantCriticalDepth: 0,
		},
		{
			name: "single resolver",
			resolvers: []*Resolver{
				{
					Name: "only",
					Resolve: &ResolvePhase{
						With: []ProviderSource{{Provider: "static", Inputs: map[string]*ValueRef{"value": {Literal: "x"}}}},
					},
				},
			},
			wantCriticalPath:  []string{"only"},
			wantCriticalDepth: 1,
		},
		{
			name: "linear chain a->b->c",
			resolvers: []*Resolver{
				{
					Name: "a",
					Resolve: &ResolvePhase{
						With: []ProviderSource{{Provider: "static", Inputs: map[string]*ValueRef{"value": {Literal: "a"}}}},
					},
				},
				{
					Name: "b",
					Resolve: &ResolvePhase{
						With: []ProviderSource{{Provider: "cel", Inputs: map[string]*ValueRef{"value": {Resolver: stringPtr("a")}}}},
					},
				},
				{
					Name: "c",
					Resolve: &ResolvePhase{
						With: []ProviderSource{{Provider: "cel", Inputs: map[string]*ValueRef{"value": {Resolver: stringPtr("b")}}}},
					},
				},
			},
			wantCriticalPath:  []string{"a", "b", "c"},
			wantCriticalDepth: 3,
		},
		{
			name: "diamond: a->b, a->c, b->d, c->d - path length is 3",
			resolvers: []*Resolver{
				{
					Name: "a",
					Resolve: &ResolvePhase{
						With: []ProviderSource{{Provider: "static", Inputs: map[string]*ValueRef{"value": {Literal: "a"}}}},
					},
				},
				{
					Name: "b",
					Resolve: &ResolvePhase{
						With: []ProviderSource{{Provider: "cel", Inputs: map[string]*ValueRef{"value": {Resolver: stringPtr("a")}}}},
					},
				},
				{
					Name: "c",
					Resolve: &ResolvePhase{
						With: []ProviderSource{{Provider: "cel", Inputs: map[string]*ValueRef{"value": {Resolver: stringPtr("a")}}}},
					},
				},
				{
					Name:      "d",
					DependsOn: []string{"b", "c"},
					Resolve: &ResolvePhase{
						With: []ProviderSource{{Provider: "static", Inputs: map[string]*ValueRef{"value": {Literal: "d"}}}},
					},
				},
			},
			wantCriticalPath:  []string{"a", "b", "d"},
			wantCriticalDepth: 3,
		},
		{
			name: "parallel independent resolvers - critical path is 1",
			resolvers: []*Resolver{
				{
					Name: "x",
					Resolve: &ResolvePhase{
						With: []ProviderSource{{Provider: "static", Inputs: map[string]*ValueRef{"value": {Literal: "x"}}}},
					},
				},
				{
					Name: "y",
					Resolve: &ResolvePhase{
						With: []ProviderSource{{Provider: "static", Inputs: map[string]*ValueRef{"value": {Literal: "y"}}}},
					},
				},
			},
			wantCriticalDepth: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			graph, err := BuildGraph(tt.resolvers, nil)
			require.NoError(t, err)

			if tt.wantCriticalPath != nil {
				assert.Equal(t, tt.wantCriticalPath, graph.Stats.CriticalPath)
			}
			assert.Equal(t, tt.wantCriticalDepth, graph.Stats.CriticalDepth)
		})
	}
}

// Helper functions
func stringPtr(s string) *string {
	return &s
}

func celExpPtr(expr string) *celexp.Expression {
	e := celexp.Expression(expr)
	return &e
}

func tmplPtr(tmpl string) *gotmpl.GoTemplatingContent {
	t := gotmpl.GoTemplatingContent(tmpl)
	return &t
}

func TestIsTransitiveDependency(t *testing.T) {
	resolvers := map[string]*Resolver{
		"a": {DependsOn: []string{"b"}},
		"b": {DependsOn: []string{"c"}},
		"c": {},
		"d": {DependsOn: []string{"a"}},
	}

	tests := []struct {
		name      string
		target    string
		candidate string
		expected  bool
	}{
		{"direct dependency", "a", "b", true},
		{"transitive dependency", "a", "c", true},
		{"no dependency", "a", "d", false},
		{"non-existent target", "x", "a", false},
		{"non-existent candidate", "a", "x", false},
		{"self", "a", "a", false},
		{"deep transitive", "d", "c", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsTransitiveDependency(resolvers, tt.target, tt.candidate)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsTransitiveDependency_CycleProtection(t *testing.T) {
	// Create a cycle: a → b → a
	resolvers := map[string]*Resolver{
		"a": {DependsOn: []string{"b"}},
		"b": {DependsOn: []string{"a"}},
	}

	// Should not infinite loop; b is a direct dep of a
	assert.True(t, IsTransitiveDependency(resolvers, "a", "b"))
	// a is a dep of b (through the cycle)
	assert.True(t, IsTransitiveDependency(resolvers, "b", "a"))
	// c doesn't exist
	assert.False(t, IsTransitiveDependency(resolvers, "a", "c"))
}

func BenchmarkIsTransitiveDependency(b *testing.B) {
	resolvers := map[string]*Resolver{
		"a": {DependsOn: []string{"b", "c"}},
		"b": {DependsOn: []string{"d", "e"}},
		"c": {DependsOn: []string{"f"}},
		"d": {},
		"e": {},
		"f": {},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IsTransitiveDependency(resolvers, "a", "f")
	}
}

func TestExtractDepsFromProviderInputs_NilFallback(t *testing.T) {
	rslvrRef := "dep-resolver"
	inputs := map[string]*ValueRef{
		"key": {Resolver: &rslvrRef},
	}

	tests := []struct {
		name     string
		lookup   DescriptorLookup
		wantUsed bool
		wantDeps []string
	}{
		{
			name: "nil return signals fallback",
			lookup: func(_ string) *provider.Descriptor {
				return &provider.Descriptor{
					ExtractDependencies: func(_ map[string]any) []string {
						return nil // simulate RPC failure
					},
				}
			},
			wantUsed: false,
		},
		{
			name: "empty slice means provider handled it with no deps",
			lookup: func(_ string) *provider.Descriptor {
				return &provider.Descriptor{
					ExtractDependencies: func(_ map[string]any) []string {
						return []string{}
					},
				}
			},
			wantUsed: true,
			wantDeps: []string{},
		},
		{
			name: "non-nil deps are collected",
			lookup: func(_ string) *provider.Descriptor {
				return &provider.Descriptor{
					ExtractDependencies: func(_ map[string]any) []string {
						return []string{"from-plugin"}
					},
				}
			},
			wantUsed: true,
			wantDeps: []string{"from-plugin"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := make(map[string]bool)
			used := extractDepsFromProviderInputs("test-provider", inputs, nil, deps, tt.lookup)
			assert.Equal(t, tt.wantUsed, used)

			if tt.wantDeps != nil {
				gotDeps := make([]string, 0, len(deps))
				for d := range deps {
					gotDeps = append(gotDeps, d)
				}
				assert.ElementsMatch(t, tt.wantDeps, gotDeps)
			}
		})
	}
}

func TestExtractDepsFromTemplateCtx(t *testing.T) {
	tests := []struct {
		name    string
		tmpl    string
		exclude map[string]bool
		want    []string
	}{
		{
			name:    "no exclusions",
			tmpl:    "{{ .config }} {{ .region }}",
			exclude: nil,
			want:    []string{"config", "region"},
		},
		{
			name:    "exclude data key",
			tmpl:    "{{ .config.host }}:{{ .region }}",
			exclude: map[string]bool{"config": true},
			want:    []string{"region"},
		},
		{
			name:    "exclude all keys",
			tmpl:    "{{ toYaml .config }}",
			exclude: map[string]bool{"config": true},
			want:    []string{},
		},
		{
			name:    "underscore refs not affected by exclusions",
			tmpl:    "{{ ._.environment }} {{ .config }}",
			exclude: map[string]bool{"config": true},
			want:    []string{"environment"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := make(map[string]bool)
			extractDepsFromTemplateCtx(tt.tmpl, deps, scanCtxFromExclude(tt.exclude))

			wantMap := make(map[string]bool)
			for _, dep := range tt.want {
				wantMap[dep] = true
			}

			assert.Equal(t, wantMap, deps, "extracted dependencies should match")
		})
	}
}

func TestBuildTmplScanCtx_DataKeys(t *testing.T) {
	tests := []struct {
		name   string
		inputs map[string]*ValueRef
		want   map[string]bool
	}{
		{
			name:   "no data input",
			inputs: map[string]*ValueRef{"template": {Literal: "{{ .name }}"}},
			want:   nil,
		},
		{
			name: "data input with map literal",
			inputs: map[string]*ValueRef{
				"template": {Literal: "{{ .config }}"},
				"data":     {Literal: map[string]any{"config": map[string]any{"port": 8080}, "name": "test"}},
			},
			want: map[string]bool{"config": true, "name": true},
		},
		{
			name: "data input is not a map",
			inputs: map[string]*ValueRef{
				"data": {Literal: "not a map"},
			},
			want: nil,
		},
		{
			name: "data input is resolver ref",
			inputs: map[string]*ValueRef{
				"data": {Resolver: stringPtr("myresolver")},
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildTmplScanCtx(tt.inputs, nil).dataKeys
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValueRefDataScanKeys(t *testing.T) {
	tests := []struct {
		name         string
		ref          *ValueRef
		wantKeys     map[string]bool
		wantComplete bool
	}{
		{
			name:         "literal map",
			ref:          &ValueRef{Literal: map[string]any{"a": 1, "b": 2}},
			wantKeys:     map[string]bool{"a": true, "b": true},
			wantComplete: true,
		},
		{
			name:         "literal non-map is dynamic",
			ref:          &ValueRef{Literal: "scalar"},
			wantKeys:     nil,
			wantComplete: false,
		},
		{
			name:         "expr map literal is complete",
			ref:          &ValueRef{Expr: celExpPtr(`{"appName": _.appName, "env": _.env}`)},
			wantKeys:     map[string]bool{"appName": true, "env": true},
			wantComplete: true,
		},
		{
			name:         "expr non-map literal is dynamic",
			ref:          &ValueRef{Expr: celExpPtr(`map.merge(_.base, _.extra)`)},
			wantKeys:     nil,
			wantComplete: false,
		},
		{
			name:         "resolver ref is dynamic",
			ref:          &ValueRef{Resolver: stringPtr("vars")},
			wantKeys:     nil,
			wantComplete: false,
		},
		{
			name:         "tmpl ref is dynamic",
			ref:          &ValueRef{Tmpl: tmplPtr("{{ .x }}")},
			wantKeys:     nil,
			wantComplete: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, complete := valueRefDataScanKeys(tt.ref)
			assert.Equal(t, tt.wantComplete, complete)
			assert.Equal(t, tt.wantKeys, keys)
		})
	}
}

func TestLiteralDataKeys(t *testing.T) {
	tests := []struct {
		name string
		data any
		want map[string]bool
	}{
		{
			name: "plain literal map",
			data: map[string]any{"a": 1, "b": 2},
			want: map[string]bool{"a": true, "b": true},
		},
		{
			name: "expr map literal",
			data: map[string]any{"expr": `{"appName": _.appName}`},
			want: map[string]bool{"appName": true},
		},
		{
			name: "expr non-map literal is unknown",
			data: map[string]any{"expr": `map.merge(a, b)`},
			want: nil,
		},
		{
			name: "rslvr ref is unknown",
			data: map[string]any{"rslvr": "vars"},
			want: nil,
		},
		{
			name: "tmpl ref is unknown",
			data: map[string]any{"tmpl": "{{ .x }}"},
			want: nil,
		},
		{
			name: "non-map is unknown",
			data: "scalar",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, literalDataKeys(tt.data))
		})
	}
}

func TestUnresolvedTemplateAccessors(t *testing.T) {
	tests := []struct {
		name      string
		resolvers []*Resolver
		want      []TemplateAccessor
	}{
		{
			name: "typo accessor with known data keys is flagged",
			resolvers: []*Resolver{
				{Name: "appName"},
				{
					Name: "rendered",
					Resolve: &ResolvePhase{With: []ProviderSource{{
						Provider: "go-template",
						Inputs: map[string]*ValueRef{
							"template": {Literal: `app = "{{ .appNam }}"`},
							"data":     {Expr: celExpPtr(`{"appName": _.appName}`)},
						},
					}}},
				},
			},
			want: []TemplateAccessor{{Resolver: "rendered", Step: "resolve", Name: "appNam"}},
		},
		{
			name: "known resolver reference is not flagged",
			resolvers: []*Resolver{
				{Name: "appName"},
				{
					Name: "rendered",
					Resolve: &ResolvePhase{With: []ProviderSource{{
						Provider: "go-template",
						Inputs: map[string]*ValueRef{
							"template": {Literal: `{{ .appName }}`},
						},
					}}},
				},
			},
			want: nil,
		},
		{
			name: "forEach alias is not flagged",
			resolvers: []*Resolver{
				{
					Name: "rendered",
					Resolve: &ResolvePhase{With: []ProviderSource{{
						Provider: "go-template",
						ForEach:  &ForEachClause{Item: "proj", In: &ValueRef{Resolver: stringPtr("projects")}},
						Inputs: map[string]*ValueRef{
							"template": {Literal: `{{ .proj.name }}`},
						},
					}}},
				},
				{Name: "projects"},
			},
			want: nil,
		},
		{
			name: "dynamic data input suppresses bare field flagging",
			resolvers: []*Resolver{
				{
					Name: "rendered",
					Resolve: &ResolvePhase{With: []ProviderSource{{
						Provider: "go-template",
						Inputs: map[string]*ValueRef{
							"template": {Literal: `{{ .anything }}`},
							"data":     {Resolver: stringPtr("vars")},
						},
					}}},
				},
				{Name: "vars"},
			},
			want: nil,
		},
		{
			name: "transform step accessor is flagged",
			resolvers: []*Resolver{
				{
					Name: "rendered",
					Transform: &TransformPhase{With: []ProviderTransform{{
						Provider: "go-template",
						Inputs: map[string]*ValueRef{
							"template": {Literal: `{{ .missing }}`},
						},
					}}},
				},
			},
			want: []TemplateAccessor{{Resolver: "rendered", Step: "transform", Name: "missing"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UnresolvedTemplateAccessors(tt.resolvers)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractDeps_GoTemplateDataExclusion(t *testing.T) {
	// Integration test: a resolver using go-template with data input should
	// not create false dependencies on data keys.
	resolver := &Resolver{
		Name: "my-template",
		Resolve: &ResolvePhase{
			With: []ProviderSource{
				{
					Provider: "go-template",
					Inputs: map[string]*ValueRef{
						"name":     {Literal: "test"},
						"template": {Literal: "{{ toYaml .config }} {{ .appName }}"},
						"data": {Literal: map[string]any{
							"config":  map[string]any{"port": 8080},
							"appName": "myapp",
						}},
					},
				},
			},
		},
	}

	deps := extractDependencies(resolver, nil)

	// config and appName are provided by data, not resolvers
	for _, dep := range deps {
		assert.NotEqual(t, "config", dep, "config should not be a dependency (provided by data)")
		assert.NotEqual(t, "appName", dep, "appName should not be a dependency (provided by data)")
	}
}

func TestExtractRefsFromValueRefs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		inputs map[string]*ValueRef
		want   map[string]bool
	}{
		{
			name:   "nil inputs",
			inputs: nil,
			want:   map[string]bool{},
		},
		{
			name:   "empty inputs",
			inputs: map[string]*ValueRef{},
			want:   map[string]bool{},
		},
		{
			name: "direct resolver reference",
			inputs: map[string]*ValueRef{
				"target": {Resolver: stringPtr("environment")},
			},
			want: map[string]bool{"environment": true},
		},
		{
			name: "CEL expression reference",
			inputs: map[string]*ValueRef{
				"url": {Expr: celExpPtr("_.config.host + ':' + string(_.config.port)")},
			},
			want: map[string]bool{"config": true},
		},
		{
			name: "template reference",
			inputs: map[string]*ValueRef{
				"greeting": {Tmpl: tmplPtr("Hello {{ ._.username }}")},
			},
			want: map[string]bool{"username": true},
		},
		{
			name: "multiple mixed references",
			inputs: map[string]*ValueRef{
				"target":  {Resolver: stringPtr("environment")},
				"url":     {Expr: celExpPtr("_.config.host")},
				"message": {Tmpl: tmplPtr("{{ ._.greeting }}")},
				"literal": {Literal: "static-value"},
			},
			want: map[string]bool{"environment": true, "config": true, "greeting": true},
		},
		{
			name: "literal value produces no refs",
			inputs: map[string]*ValueRef{
				"name": {Literal: "hello"},
			},
			want: map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractRefsFromValueRefs(tt.inputs)
			gotMap := make(map[string]bool, len(got))
			for _, dep := range got {
				gotMap[dep] = true
			}
			assert.Equal(t, tt.want, gotMap)
		})
	}
}

func TestExtractRefsFromValueRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ref  *ValueRef
		want map[string]bool
	}{
		{
			name: "nil ref is a no-op",
			ref:  nil,
			want: map[string]bool{},
		},
		{
			name: "direct resolver reference",
			ref:  &ValueRef{Resolver: stringPtr("environment")},
			want: map[string]bool{"environment": true},
		},
		{
			name: "cel expression reference",
			ref:  &ValueRef{Expr: celExpPtr("_.config.host + ':' + string(_.config.port)")},
			want: map[string]bool{"config": true},
		},
		{
			name: "template reference",
			ref:  &ValueRef{Tmpl: tmplPtr("Hello {{ ._.username }}")},
			want: map[string]bool{"username": true},
		},
		{
			name: "nested literal map reference",
			ref: &ValueRef{Literal: map[string]any{
				"APP_NAME": map[string]any{"rslvr": "appName"},
			}},
			want: map[string]bool{"appName": true},
		},
		{
			name: "literal value produces no refs",
			ref:  &ValueRef{Literal: "static"},
			want: map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			deps := make(map[string]bool)
			ExtractRefsFromValueRef(tt.ref, deps)
			assert.Equal(t, tt.want, deps)
		})
	}
}

func TestExtractRefsFromValueRef_NilDepsIsNoOp(t *testing.T) {
	t.Parallel()

	// A nil deps map must be a no-op and must not panic, even with a
	// non-nil ref that would otherwise produce dependencies.
	assert.NotPanics(t, func() {
		ExtractRefsFromValueRef(&ValueRef{Resolver: stringPtr("environment")}, nil)
	})
}

func TestExtractOptionalRefsFromValueRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ref  *ValueRef
		want map[string]bool
	}{
		{
			name: "nil ref is a no-op",
			ref:  nil,
			want: map[string]bool{},
		},
		{
			name: "optional select in cel expression",
			ref:  &ValueRef{Expr: celExpPtr(`_.?platformProfileID.orValue("")`)},
			want: map[string]bool{"platformProfileID": true},
		},
		{
			name: "optional index in cel expression",
			ref:  &ValueRef{Expr: celExpPtr(`_[?"my-resolver"].orValue("")`)},
			want: map[string]bool{"my-resolver": true},
		},
		{
			name: "hard reference contributes no optional",
			ref:  &ValueRef{Expr: celExpPtr("_.config.host")},
			want: map[string]bool{},
		},
		{
			name: "hard dominates within one expression",
			ref:  &ValueRef{Expr: celExpPtr(`_.a + _.?a.orValue("")`)},
			want: map[string]bool{},
		},
		{
			name: "direct resolver reference is hard",
			ref:  &ValueRef{Resolver: stringPtr("environment")},
			want: map[string]bool{},
		},
		{
			name: "template reference is hard",
			ref:  &ValueRef{Tmpl: tmplPtr("Hello {{ ._.username }}")},
			want: map[string]bool{},
		},
		{
			name: "optional select nested in literal map expr",
			ref: &ValueRef{Literal: map[string]any{
				"ID": map[string]any{"expr": `_.?profile.orValue("")`},
			}},
			want: map[string]bool{"profile": true},
		},
		{
			name: "optional select in literal string",
			ref:  &ValueRef{Literal: `_.?zone.orValue("us")`},
			want: map[string]bool{"zone": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			optional := make(map[string]bool)
			ExtractOptionalRefsFromValueRef(tt.ref, optional)
			assert.Equal(t, tt.want, optional)
		})
	}
}

func TestExtractOptionalRefsFromValueRef_NilOptionalIsNoOp(t *testing.T) {
	t.Parallel()

	assert.NotPanics(t, func() {
		ExtractOptionalRefsFromValueRef(&ValueRef{Expr: celExpPtr(`_.?x.orValue("")`)}, nil)
	})
}

func TestExtractRefsFromValueRefs_NestedValueRefMaps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		inputs map[string]*ValueRef
		want   []string
	}{
		{
			name: "nested rslvr in literal map",
			inputs: map[string]*ValueRef{
				"env": {Literal: map[string]any{
					"APP_NAME": map[string]any{"rslvr": "appName"},
					"APP_PORT": "8080",
				}},
			},
			want: []string{"appName"},
		},
		{
			name: "nested expr in literal map",
			inputs: map[string]*ValueRef{
				"config": {Literal: map[string]any{
					"url": map[string]any{"expr": "_.host + ':' + string(_.port)"},
				}},
			},
			want: []string{"host", "port"},
		},
		{
			name: "nested tmpl in literal map",
			inputs: map[string]*ValueRef{
				"labels": {Literal: map[string]any{
					"version": map[string]any{"tmpl": "{{ ._.appVersion }}"},
				}},
			},
			want: []string{"appVersion"},
		},
		{
			name: "mixed nested and top-level refs",
			inputs: map[string]*ValueRef{
				"direct": {Resolver: stringPtr("directRef")},
				"nested": {Literal: map[string]any{
					"inner": map[string]any{"rslvr": "nestedRef"},
				}},
			},
			want: []string{"directRef", "nestedRef"},
		},
		{
			name: "deeply nested rslvr in array",
			inputs: map[string]*ValueRef{
				"items": {Literal: []any{
					map[string]any{"rslvr": "first"},
					map[string]any{"rslvr": "second"},
					"plain-string",
				}},
			},
			want: []string{"first", "second"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractRefsFromValueRefs(tt.inputs)
			assert.Equal(t, tt.want, got)
		})
	}
}
