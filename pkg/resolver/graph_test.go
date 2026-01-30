package resolver

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
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
			want: []string{"base", "region", "invalid", "environment"},
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
			want: []string{"environment"},
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
								// Note: Inside range blocks, {{.name}} refers to the element's field
								// but the parser can't distinguish this context, so "name" is also extracted
								"template": {Literal: "{{range .servers}}- {{.name}}{{end}}"},
								"name":     {Literal: "range-template"},
							},
						},
					},
				},
			},
			// servers is extracted from .servers, name is extracted from .name inside range
			// (even though .name refers to a field, not a resolver, the parser can't distinguish)
			want: []string{"servers", "name"},
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
