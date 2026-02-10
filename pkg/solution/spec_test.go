// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solution

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
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
