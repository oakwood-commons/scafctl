// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSolution_ComposeField_UnmarshalYAML(t *testing.T) {
	sol := &solution.Solution{}
	err := sol.UnmarshalFromBytes([]byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test
  version: 1.0.0
compose:
  - resolvers.yaml
  - workflow.yaml
`))
	require.NoError(t, err)
	assert.Equal(t, []string{"resolvers.yaml", "workflow.yaml"}, sol.Compose)
}

func TestSolution_BundleField_UnmarshalYAML(t *testing.T) {
	sol := &solution.Solution{}
	err := sol.UnmarshalFromBytes([]byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test
  version: 1.0.0
bundle:
  include:
    - templates/**/*.tmpl
    - configs/*.yaml
`))
	require.NoError(t, err)
	assert.Equal(t, []string{"templates/**/*.tmpl", "configs/*.yaml"}, sol.Bundle.Include)
}

func TestSolution_BundleWithPlugins_UnmarshalYAML(t *testing.T) {
	sol := &solution.Solution{}
	err := sol.UnmarshalFromBytes([]byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test
  version: 1.0.0
bundle:
  include:
    - templates/*.tmpl
  plugins:
    - name: aws-provider
      kind: provider
      version: "^1.5.0"
    - name: vault-auth
      kind: auth-handler
      version: "~1.2.0"
`))
	require.NoError(t, err)
	require.Len(t, sol.Bundle.Plugins, 2)
	assert.Equal(t, "aws-provider", sol.Bundle.Plugins[0].Name)
	assert.Equal(t, solution.PluginKindProvider, sol.Bundle.Plugins[0].Kind)
	assert.Equal(t, "^1.5.0", sol.Bundle.Plugins[0].Version)
	assert.Equal(t, "vault-auth", sol.Bundle.Plugins[1].Name)
	assert.Equal(t, solution.PluginKindAuthHandler, sol.Bundle.Plugins[1].Kind)
}

func TestSolution_BundleWithPluginDefaults_UnmarshalYAML(t *testing.T) {
	sol := &solution.Solution{}
	err := sol.UnmarshalFromBytes([]byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test
  version: 1.0.0
bundle:
  plugins:
    - name: aws-provider
      kind: provider
      version: "^1.5.0"
      defaults:
        region: us-east-1
        output_format: json
`))
	require.NoError(t, err)
	require.Len(t, sol.Bundle.Plugins, 1)
	require.NotNil(t, sol.Bundle.Plugins[0].Defaults)
	assert.Contains(t, sol.Bundle.Plugins[0].Defaults, "region")
	assert.Contains(t, sol.Bundle.Plugins[0].Defaults, "output_format")
}

func TestBundle_IsEmpty(t *testing.T) {
	tests := []struct {
		name     string
		bundle   solution.Bundle
		expected bool
	}{
		{
			name:     "empty bundle",
			bundle:   solution.Bundle{},
			expected: true,
		},
		{
			name: "has includes",
			bundle: solution.Bundle{
				Include: []string{"*.yaml"},
			},
			expected: false,
		},
		{
			name: "has plugins",
			bundle: solution.Bundle{
				Plugins: []solution.PluginDependency{
					{Name: "test", Kind: "provider", Version: "1.0.0"},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.bundle.IsEmpty())
		})
	}
}

func TestSolution_ComposeAndBundle_RoundTrip(t *testing.T) {
	sol := &solution.Solution{}
	err := sol.UnmarshalFromBytes([]byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: roundtrip-test
  version: 2.0.0
compose:
  - resolvers.yaml
bundle:
  include:
    - templates/*.tmpl
  plugins:
    - name: aws-provider
      kind: provider
      version: "^1.5.0"
spec:
  resolvers:
    env:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: environment
`))
	require.NoError(t, err)

	// Marshal to YAML and back
	data, err := sol.ToYAML()
	require.NoError(t, err)

	sol2 := &solution.Solution{}
	err = sol2.UnmarshalFromBytes(data)
	require.NoError(t, err)

	assert.Equal(t, sol.Compose, sol2.Compose)
	assert.Equal(t, sol.Bundle.Include, sol2.Bundle.Include)
	require.Len(t, sol2.Bundle.Plugins, 1)
	assert.Equal(t, "aws-provider", sol2.Bundle.Plugins[0].Name)
}
