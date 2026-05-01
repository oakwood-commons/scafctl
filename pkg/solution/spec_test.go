// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solution

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpec_ResolversToSlice(t *testing.T) {
	tests := []struct {
		name     string
		spec     *Spec
		wantLen  int
		wantNils bool
	}{
		{
			name:    "nil spec",
			spec:    nil,
			wantLen: 0,
		},
		{
			name:    "nil resolvers map",
			spec:    &Spec{Resolvers: nil},
			wantLen: 0,
		},
		{
			name:    "empty resolvers map",
			spec:    &Spec{Resolvers: map[string]*resolver.Resolver{}},
			wantLen: 0,
		},
		{
			name: "single resolver",
			spec: &Spec{
				Resolvers: map[string]*resolver.Resolver{
					"env": {Description: "test resolver"},
				},
			},
			wantLen: 1,
		},
		{
			name: "multiple resolvers",
			spec: &Spec{
				Resolvers: map[string]*resolver.Resolver{
					"env":    {Description: "env resolver"},
					"region": {Description: "region resolver"},
					"app":    {Description: "app resolver"},
				},
			},
			wantLen: 3,
		},
		{
			name: "nil resolver entry is skipped",
			spec: &Spec{
				Resolvers: map[string]*resolver.Resolver{
					"valid":   {Description: "valid resolver"},
					"invalid": nil,
				},
			},
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.spec.ResolversToSlice()
			assert.Len(t, result, tt.wantLen)

			// Verify that names are set correctly for non-nil resolvers
			if tt.spec != nil && tt.spec.Resolvers != nil {
				for _, r := range result {
					assert.NotEmpty(t, r.Name, "resolver name should be set from map key")
					// Verify the name matches a key in the original map
					_, exists := tt.spec.Resolvers[r.Name]
					assert.True(t, exists, "resolver name should exist in original map")
				}
			}
		})
	}
}

func TestSpec_HasResolvers(t *testing.T) {
	tests := []struct {
		name string
		spec *Spec
		want bool
	}{
		{
			name: "nil spec",
			spec: nil,
			want: false,
		},
		{
			name: "nil resolvers map",
			spec: &Spec{Resolvers: nil},
			want: false,
		},
		{
			name: "empty resolvers map",
			spec: &Spec{Resolvers: map[string]*resolver.Resolver{}},
			want: false,
		},
		{
			name: "has resolvers",
			spec: &Spec{
				Resolvers: map[string]*resolver.Resolver{
					"test": {},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.spec.HasResolvers()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSolution_UnmarshalWithSpec(t *testing.T) {
	tests := []struct {
		name          string
		yaml          string
		wantErr       bool
		wantResolvers int
	}{
		{
			name: "solution with spec containing resolvers",
			yaml: `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
  version: 1.0.0
spec:
  resolvers:
    environment:
      description: "Resolve environment"
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "env"
    region:
      description: "Resolve region"
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "us-east-1"
`,
			wantErr:       false,
			wantResolvers: 2,
		},
		{
			name: "solution without spec",
			yaml: `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: minimal-solution
  version: 1.0.0
`,
			wantErr:       false,
			wantResolvers: 0,
		},
		{
			name: "solution with empty spec",
			yaml: `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: empty-spec-solution
  version: 1.0.0
spec: {}
`,
			wantErr:       false,
			wantResolvers: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sol Solution
			err := sol.UnmarshalFromBytes([]byte(tt.yaml))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			resolvers := sol.Spec.ResolversToSlice()
			assert.Len(t, resolvers, tt.wantResolvers)

			// Verify names are set from map keys
			for _, r := range resolvers {
				assert.NotEmpty(t, r.Name)
			}
		})
	}
}

func TestSolution_ToYAMLWithSpec(t *testing.T) {
	sol := &Solution{
		APIVersion: DefaultAPIVersion,
		Kind:       SolutionKind,
		Metadata: Metadata{
			Name:    "test-solution",
			Version: mustParseSemver("1.0.0"),
		},
		Spec: Spec{
			Resolvers: map[string]*resolver.Resolver{
				"env": {
					Description: "Environment resolver",
					Type:        resolver.TypeString,
				},
			},
		},
	}

	data, err := sol.ToYAML()
	require.NoError(t, err)
	assert.Contains(t, string(data), "spec:")
	assert.Contains(t, string(data), "resolvers:")
	assert.Contains(t, string(data), "env:")
}

func TestSolution_ToJSONWithSpec(t *testing.T) {
	sol := &Solution{
		APIVersion: DefaultAPIVersion,
		Kind:       SolutionKind,
		Metadata: Metadata{
			Name:    "test-solution",
			Version: mustParseSemver("1.0.0"),
		},
		Spec: Spec{
			Resolvers: map[string]*resolver.Resolver{
				"region": {
					Description: "Region resolver",
					Type:        resolver.TypeString,
				},
			},
		},
	}

	data, err := sol.ToJSON()
	require.NoError(t, err)
	assert.Contains(t, string(data), `"spec"`)
	assert.Contains(t, string(data), `"resolvers"`)
	assert.Contains(t, string(data), `"region"`)
}

func TestSolution_ResolverWithCondition(t *testing.T) {
	yaml := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: conditional-solution
  version: 1.0.0
spec:
  resolvers:
    prod-only:
      description: "Only runs in production"
      type: string
      when:
        expr: '_.environment == "prod"'
      resolve:
        with:
          - provider: static
            inputs:
              value: "production-value"
`
	var sol Solution
	err := sol.UnmarshalFromBytes([]byte(yaml))
	require.NoError(t, err)

	resolvers := sol.Spec.ResolversToSlice()
	require.Len(t, resolvers, 1)

	r := resolvers[0]
	assert.Equal(t, "prod-only", r.Name)
	assert.NotNil(t, r.When)
	assert.NotNil(t, r.When.Expr)
}

func TestSolution_ResolverWithDependencies(t *testing.T) {
	// Create a CEL expression that references another resolver
	expr := celexp.Expression(`_.environment`)

	yaml := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: dependent-solution
  version: 1.0.0
spec:
  resolvers:
    environment:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "env"
    region:
      type: string
      resolve:
        with:
          - provider: cel
            inputs:
              expression:
                expr: '_.environment == "prod" ? "us-east-1" : "us-west-2"'
`
	var sol Solution
	err := sol.UnmarshalFromBytes([]byte(yaml))
	require.NoError(t, err)

	resolvers := sol.Spec.ResolversToSlice()
	assert.Len(t, resolvers, 2)

	// Verify both resolvers exist
	names := make(map[string]bool)
	for _, r := range resolvers {
		names[r.Name] = true
	}
	assert.True(t, names["environment"])
	assert.True(t, names["region"])

	// Just verify expr is valid
	_ = expr
}

func TestSpec_HasWorkflow(t *testing.T) {
	tests := []struct {
		name string
		spec *Spec
		want bool
	}{
		{
			name: "nil spec",
			spec: nil,
			want: false,
		},
		{
			name: "nil workflow",
			spec: &Spec{Workflow: nil},
			want: false,
		},
		{
			name: "has workflow",
			spec: &Spec{Workflow: &action.Workflow{}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.spec.HasWorkflow()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSpec_HasActions(t *testing.T) {
	tests := []struct {
		name string
		spec *Spec
		want bool
	}{
		{
			name: "nil spec",
			spec: nil,
			want: false,
		},
		{
			name: "nil workflow",
			spec: &Spec{Workflow: nil},
			want: false,
		},
		{
			name: "empty workflow",
			spec: &Spec{Workflow: &action.Workflow{}},
			want: false,
		},
		{
			name: "has regular actions",
			spec: &Spec{
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"build": {Provider: "shell"},
					},
				},
			},
			want: true,
		},
		{
			name: "has finally actions only",
			spec: &Spec{
				Workflow: &action.Workflow{
					Finally: map[string]*action.Action{
						"cleanup": {Provider: "shell"},
					},
				},
			},
			want: true,
		},
		{
			name: "has both regular and finally actions",
			spec: &Spec{
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"build": {Provider: "shell"},
					},
					Finally: map[string]*action.Action{
						"cleanup": {Provider: "shell"},
					},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.spec.HasActions()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSolution_UnmarshalWithWorkflow(t *testing.T) {
	yamlContent := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: workflow-solution
  version: 1.0.0
spec:
  resolvers:
    environment:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "prod"
  workflow:
    actions:
      build:
        provider: shell
        inputs:
          command:
            literal: "go build"
      deploy:
        provider: kubernetes
        dependsOn:
          - build
    finally:
      cleanup:
        provider: shell
        inputs:
          command:
            literal: "rm -rf tmp"
`
	var sol Solution
	err := sol.UnmarshalFromBytes([]byte(yamlContent))
	require.NoError(t, err)

	assert.True(t, sol.Spec.HasResolvers())
	assert.True(t, sol.Spec.HasWorkflow())
	assert.True(t, sol.Spec.HasActions())

	assert.Len(t, sol.Spec.Workflow.Actions, 2)
	assert.Len(t, sol.Spec.Workflow.Finally, 1)

	// Verify action details
	build := sol.Spec.Workflow.Actions["build"]
	require.NotNil(t, build)
	assert.Equal(t, "shell", build.Provider)

	deploy := sol.Spec.Workflow.Actions["deploy"]
	require.NotNil(t, deploy)
	assert.Equal(t, []string{"build"}, deploy.DependsOn)

	cleanup := sol.Spec.Workflow.Finally["cleanup"]
	require.NotNil(t, cleanup)
	assert.Equal(t, "shell", cleanup.Provider)
}

func mustParseSemver(v string) *semver.Version {
	ver, err := semver.NewVersion(v)
	if err != nil {
		panic(err)
	}
	return ver
}

func TestSpec_HasTests(t *testing.T) {
	tests := []struct {
		name     string
		spec     *Spec
		expected bool
	}{
		{"nil spec", nil, false},
		{"empty spec", &Spec{}, false},
		{"nil testing", &Spec{Testing: nil}, false},
		{"nil cases", &Spec{Testing: &soltesting.TestSuite{Cases: nil}}, false},
		{"empty cases map", &Spec{Testing: &soltesting.TestSuite{Cases: map[string]*soltesting.TestCase{}}}, false},
		{"has cases", &Spec{Testing: &soltesting.TestSuite{Cases: map[string]*soltesting.TestCase{
			"test1": {Name: "test1"},
		}}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.spec.HasTests())
		})
	}
}

func TestSpec_HasTestConfig(t *testing.T) {
	tests := []struct {
		name     string
		spec     *Spec
		expected bool
	}{
		{"nil spec", nil, false},
		{"empty spec", &Spec{}, false},
		{"nil testing", &Spec{Testing: nil}, false},
		{"nil config", &Spec{Testing: &soltesting.TestSuite{Config: nil}}, false},
		{"has config", &Spec{Testing: &soltesting.TestSuite{Config: &soltesting.TestConfig{}}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.spec.HasTestConfig())
		})
	}
}

func TestSolution_UnmarshalWithTests(t *testing.T) {
	yamlContent := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: tested-solution
  version: 1.0.0
spec:
  resolvers:
    env:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "env"
  testing:
    cases:
      render-test:
        description: "Test rendering"
        command:
          - render
          - solution
        assertions:
          - contains: "hello"
        tags:
          - smoke
      _base-template:
        description: "Base template"
        command:
          - render
          - solution
    config:
      skipBuiltins: false
      env:
        MY_VAR: "value"
      setup:
        - command: "echo setup"
`
	var sol Solution
	err := sol.UnmarshalFromBytes([]byte(yamlContent))
	require.NoError(t, err)

	assert.True(t, sol.Spec.HasTests())
	assert.True(t, sol.Spec.HasTestConfig())
	require.NotNil(t, sol.Spec.Testing)
	require.Len(t, sol.Spec.Testing.Cases, 2)

	renderTest := sol.Spec.Testing.Cases["render-test"]
	require.NotNil(t, renderTest)
	assert.Equal(t, "Test rendering", renderTest.Description)
	assert.Equal(t, []string{"render", "solution"}, renderTest.Command)
	assert.Len(t, renderTest.Assertions, 1)
	assert.Equal(t, "hello", renderTest.Assertions[0].Contains)
	assert.Equal(t, []string{"smoke"}, renderTest.Tags)

	baseTemplate := sol.Spec.Testing.Cases["_base-template"]
	require.NotNil(t, baseTemplate)
	assert.Equal(t, "Base template", baseTemplate.Description)

	require.NotNil(t, sol.Spec.Testing.Config)
	assert.False(t, sol.Spec.Testing.Config.SkipBuiltins.All)
	assert.Equal(t, "value", sol.Spec.Testing.Config.Env["MY_VAR"])
	require.Len(t, sol.Spec.Testing.Config.Setup, 1)
	assert.Equal(t, "echo setup", sol.Spec.Testing.Config.Setup[0].Command)
}

func TestSolution_RoundTrip_WithTests(t *testing.T) {
	yamlContent := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: roundtrip-test
  version: 1.0.0
spec:
  resolvers:
    env:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "env"
  testing:
    cases:
      my-test:
        description: "A test"
        command:
          - render
          - solution
        assertions:
          - contains: "output"
    config:
      skipBuiltins: true
`
	var sol Solution
	err := sol.UnmarshalFromBytes([]byte(yamlContent))
	require.NoError(t, err)

	// Marshal back to YAML
	data, err := sol.ToYAML()
	require.NoError(t, err)

	// Parse again
	var sol2 Solution
	err = sol2.UnmarshalFromBytes(data)
	require.NoError(t, err)

	// Verify tests survived round-trip
	assert.True(t, sol2.Spec.HasTests())
	require.Contains(t, sol2.Spec.Testing.Cases, "my-test")
	assert.Equal(t, "A test", sol2.Spec.Testing.Cases["my-test"].Description)

	// Verify testConfig survived round-trip (including SkipBuiltinsValue)
	assert.True(t, sol2.Spec.HasTestConfig())
	assert.True(t, sol2.Spec.Testing.Config.SkipBuiltins.All)
}

func TestSolution_BackwardCompat_NoTests(t *testing.T) {
	// Existing solutions without tests should still parse fine
	yamlContent := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: no-tests
  version: 1.0.0
spec:
  resolvers:
    env:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "env"
`
	var sol Solution
	err := sol.UnmarshalFromBytes([]byte(yamlContent))
	require.NoError(t, err)

	assert.False(t, sol.Spec.HasTests())
	assert.False(t, sol.Spec.HasTestConfig())
	assert.Nil(t, sol.Spec.Testing)
}

func TestSpec_ReferencedProviderNames(t *testing.T) {
	tests := []struct {
		name string
		spec *Spec
		want []string
	}{
		{
			name: "nil spec",
			spec: nil,
			want: nil,
		},
		{
			name: "empty spec",
			spec: &Spec{},
			want: []string{},
		},
		{
			name: "resolve phase only",
			spec: &Spec{
				Resolvers: map[string]*resolver.Resolver{
					"r1": {
						Resolve: &resolver.ResolvePhase{
							With: []resolver.ProviderSource{
								{Provider: "parameter"},
								{Provider: "env"},
							},
						},
					},
				},
			},
			want: []string{"env", "parameter"},
		},
		{
			name: "transform phase only",
			spec: &Spec{
				Resolvers: map[string]*resolver.Resolver{
					"r1": {
						Transform: &resolver.TransformPhase{
							With: []resolver.ProviderTransform{
								{Provider: "cel"},
							},
						},
					},
				},
			},
			want: []string{"cel"},
		},
		{
			name: "validate phase only",
			spec: &Spec{
				Resolvers: map[string]*resolver.Resolver{
					"r1": {
						Validate: &resolver.ValidatePhase{
							With: []resolver.ProviderValidation{
								{Provider: "validation"},
							},
						},
					},
				},
			},
			want: []string{"validation"},
		},
		{
			name: "workflow actions and finally",
			spec: &Spec{
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"build": {Provider: "exec"},
					},
					Finally: map[string]*action.Action{
						"cleanup": {Provider: "shell"},
					},
				},
			},
			want: []string{"exec", "shell"},
		},
		{
			name: "deduplication across phases",
			spec: &Spec{
				Resolvers: map[string]*resolver.Resolver{
					"r1": {
						Resolve: &resolver.ResolvePhase{
							With: []resolver.ProviderSource{
								{Provider: "parameter"},
							},
						},
						Transform: &resolver.TransformPhase{
							With: []resolver.ProviderTransform{
								{Provider: "parameter"},
							},
						},
					},
				},
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"run": {Provider: "parameter"},
					},
				},
			},
			want: []string{"parameter"},
		},
		{
			name: "all phases combined, sorted",
			spec: &Spec{
				Resolvers: map[string]*resolver.Resolver{
					"r1": {
						Resolve: &resolver.ResolvePhase{
							With: []resolver.ProviderSource{
								{Provider: "git"},
								{Provider: "env"},
							},
						},
						Transform: &resolver.TransformPhase{
							With: []resolver.ProviderTransform{
								{Provider: "cel"},
							},
						},
						Validate: &resolver.ValidatePhase{
							With: []resolver.ProviderValidation{
								{Provider: "validation"},
							},
						},
					},
				},
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"deploy": {Provider: "exec"},
					},
					Finally: map[string]*action.Action{
						"notify": {Provider: "env"},
					},
				},
			},
			want: []string{"cel", "env", "exec", "git", "validation"},
		},
		{
			name: "empty provider names skipped",
			spec: &Spec{
				Resolvers: map[string]*resolver.Resolver{
					"r1": {
						Resolve: &resolver.ResolvePhase{
							With: []resolver.ProviderSource{
								{Provider: ""},
								{Provider: "parameter"},
							},
						},
					},
				},
			},
			want: []string{"parameter"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.spec.ReferencedProviderNames()
			assert.Equal(t, tt.want, got)
		})
	}
}
