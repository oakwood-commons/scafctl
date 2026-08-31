// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package inspect

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractProviderNames(t *testing.T) {
	t.Run("extracts providers from all phases", func(t *testing.T) {
		r := &resolver.Resolver{
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "http"},
					{Provider: "static"},
				},
			},
			Transform: &resolver.TransformPhase{
				With: []resolver.ProviderTransform{
					{Provider: "jq"},
				},
			},
			Validate: &resolver.ValidatePhase{
				With: []resolver.ProviderValidation{
					{Provider: "schema"},
				},
			},
		}

		providers := extractProviderNames(r)

		assert.Len(t, providers, 4)
		assert.Contains(t, providers, "http")
		assert.Contains(t, providers, "static")
		assert.Contains(t, providers, "jq")
		assert.Contains(t, providers, "schema")
	})

	t.Run("removes duplicates", func(t *testing.T) {
		r := &resolver.Resolver{
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "http"},
					{Provider: "http"},
				},
			},
		}

		providers := extractProviderNames(r)
		assert.Len(t, providers, 1)
		assert.Equal(t, "http", providers[0])
	})

	t.Run("returns empty slice for empty resolver", func(t *testing.T) {
		r := &resolver.Resolver{}

		providers := extractProviderNames(r)
		assert.Empty(t, providers)
	})
}

func TestExtractPhases(t *testing.T) {
	t.Run("identifies all phases", func(t *testing.T) {
		r := &resolver.Resolver{
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "http"}},
			},
			Transform: &resolver.TransformPhase{
				With: []resolver.ProviderTransform{{Provider: "jq"}},
			},
			Validate: &resolver.ValidatePhase{
				With: []resolver.ProviderValidation{{Provider: "schema"}},
			},
		}

		phases := extractPhases(r)
		assert.Len(t, phases, 3)
		assert.Equal(t, []string{"resolve", "transform", "validate"}, phases)
	})

	t.Run("identifies single phase", func(t *testing.T) {
		r := &resolver.Resolver{
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "http"}},
			},
		}

		phases := extractPhases(r)
		assert.Equal(t, []string{"resolve"}, phases)
	})

	t.Run("returns empty slice for empty resolver", func(t *testing.T) {
		r := &resolver.Resolver{}

		phases := extractPhases(r)
		assert.Empty(t, phases)
	})
}

func TestBuildSolutionExplanation_Minimal(t *testing.T) {
	sol := &solution.Solution{}
	sol.Metadata.Name = "test-solution"

	exp := BuildSolutionExplanation(sol)
	require.NotNil(t, exp)
	assert.Equal(t, "test-solution", exp.Name)
	assert.Equal(t, "unknown", exp.Version)
}

func TestBuildSolutionExplanation_WithVersion(t *testing.T) {
	sol := &solution.Solution{}
	sol.Metadata.Name = "versioned"
	sol.Metadata.Version = semver.MustParse("2.1.0")

	exp := BuildSolutionExplanation(sol)
	assert.Equal(t, "2.1.0", exp.Version)
}

func TestBuildSolutionExplanation_WithDisplayName(t *testing.T) {
	sol := &solution.Solution{}
	sol.Metadata.Name = "sol"
	sol.Metadata.DisplayName = "My Solution"
	sol.Metadata.Description = "A test solution"
	sol.Metadata.Category = "infra"
	sol.Metadata.Tags = []string{"a", "b"}
	sol.Metadata.Source = "https://github.com/example/sol"
	sol.Metadata.Annotations = map[string]string{"team": "platform"}

	exp := BuildSolutionExplanation(sol)
	assert.Equal(t, "My Solution", exp.DisplayName)
	assert.Equal(t, "A test solution", exp.Description)
	assert.Equal(t, "infra", exp.Category)
	assert.Equal(t, []string{"a", "b"}, exp.Tags)
	assert.Equal(t, "https://github.com/example/sol", exp.Source)
	assert.Equal(t, map[string]string{"team": "platform"}, exp.Annotations)
}

func TestBuildSolutionExplanation_WithLinks(t *testing.T) {
	sol := &solution.Solution{}
	sol.Metadata.Name = "sol"
	sol.Metadata.Links = []solution.Link{{Name: "Docs", URL: "https://example.com"}}
	sol.Metadata.Maintainers = []solution.Contact{{Name: "Alice", Email: "alice@example.com"}}

	exp := BuildSolutionExplanation(sol)
	require.Len(t, exp.Links, 1)
	assert.Equal(t, "Docs", exp.Links[0].Name)
	require.Len(t, exp.Maintainers, 1)
	assert.Equal(t, "Alice", exp.Maintainers[0].Name)
}

func TestBuildResolverInfos_Empty(t *testing.T) {
	sol := &solution.Solution{}
	infos := buildResolverInfos(sol, nil)
	assert.Empty(t, infos)
}

func TestBuildResolverInfos_WithResolvers(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"alpha": {
					Name: "alpha",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "parameter"}},
					},
				},
			},
		},
	}
	infos := buildResolverInfos(sol, nil)
	require.Len(t, infos, 1)
	assert.Equal(t, "alpha", infos[0].Name)
	assert.Contains(t, infos[0].Providers, "parameter")
}

func TestBuildActionInfos_Empty(t *testing.T) {
	infos := buildActionInfos(nil, nil, "spec.workflow.actions")
	assert.Nil(t, infos)
}

func TestBuildActionInfos_WithActions(t *testing.T) {
	actions := map[string]*action.Action{
		"deploy": {
			Name:     "deploy",
			Provider: "shell",
		},
	}
	infos := buildActionInfos(actions, nil, "spec.workflow.actions")
	require.Len(t, infos, 1)
	assert.Equal(t, "deploy", infos[0].Name)
	assert.Equal(t, "shell", infos[0].Provider)
}

func TestBuildActionInfos_UnknownProvider(t *testing.T) {
	actions := map[string]*action.Action{
		"run": {Name: "run"},
	}
	infos := buildActionInfos(actions, nil, "spec.workflow.actions")
	require.Len(t, infos, 1)
	assert.Equal(t, "unknown", infos[0].Provider)
}

func TestLookupProvider_Found(t *testing.T) {
	reg := provider.NewRegistry()
	v := semver.MustParse("1.0.0")
	p := &testProvider{
		desc: &provider.Descriptor{
			Name:         "test-prov",
			APIVersion:   "v1",
			Version:      v,
			Description:  "test provider",
			Capabilities: []provider.Capability{provider.CapabilityFrom},
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom: {
					Type: "object",
					Properties: map[string]*jsonschema.Schema{
						"result": {Type: "string"},
					},
				},
			},
		},
	}
	require.NoError(t, reg.Register(p))

	desc, err := LookupProvider(context.Background(), "test-prov", reg)
	require.NoError(t, err)
	assert.Equal(t, "test-prov", desc.Name)
}

func TestLookupProvider_NotFound(t *testing.T) {
	reg := provider.NewRegistry()

	_, err := LookupProvider(context.Background(), "nonexistent", reg)
	assert.Error(t, err)
}

// testProvider is a minimal provider for testing LookupProvider.
type testProvider struct {
	desc *provider.Descriptor
}

func (p *testProvider) Descriptor() *provider.Descriptor { return p.desc }
func (p *testProvider) Execute(_ context.Context, _ any) (*provider.Output, error) {
	return &provider.Output{}, nil
}

func TestBuildSolutionExplanation_NoState(t *testing.T) {
	sol := &solution.Solution{}
	sol.Metadata.Name = "no-state"

	exp := BuildSolutionExplanation(sol)
	assert.Nil(t, exp.State)
}

func TestBuildSolutionExplanation_WithState(t *testing.T) {
	sol := &solution.Solution{
		State: &state.Config{
			Enabled: &spec.ValueRef{Literal: true},
			Backend: state.Backend{
				Provider: "file",
				Inputs: map[string]*spec.ValueRef{
					"path": {Literal: "state.json"},
				},
			},
		},
	}
	sol.Metadata.Name = "with-state"

	exp := BuildSolutionExplanation(sol)
	require.NotNil(t, exp.State)
	assert.True(t, exp.State.Enabled)
	assert.Equal(t, "file", exp.State.Provider)
	assert.Equal(t, []string{"path"}, exp.State.InputKeys)
	assert.Nil(t, exp.State.OverrideKeys)
}

func TestBuildSolutionExplanation_WithSaveOverrides(t *testing.T) {
	rslvrName := "featureBranch"
	sol := &solution.Solution{
		State: &state.Config{
			Enabled: &spec.ValueRef{Literal: true},
			Backend: state.Backend{
				Provider: "github",
				Inputs: map[string]*spec.ValueRef{
					"owner": {Literal: "my-org"},
					"repo":  {Literal: "my-repo"},
					"path":  {Literal: "state.json"},
				},
				SaveOverrides: map[string]*spec.ValueRef{
					"branch":  {Resolver: &rslvrName},
					"message": {Literal: "commit msg"},
				},
			},
		},
	}
	sol.Metadata.Name = "with-overrides"

	exp := BuildSolutionExplanation(sol)
	require.NotNil(t, exp.State)
	assert.Equal(t, "github", exp.State.Provider)
	assert.Equal(t, []string{"owner", "path", "repo"}, exp.State.InputKeys)
	assert.Equal(t, []string{"branch", "message"}, exp.State.OverrideKeys)
}

func TestBuildSolutionExplanation_StateDisabled(t *testing.T) {
	sol := &solution.Solution{
		State: &state.Config{
			Enabled: &spec.ValueRef{Literal: false},
			Backend: state.Backend{
				Provider: "file",
				Inputs:   map[string]*spec.ValueRef{"path": {Literal: "state.json"}},
			},
		},
	}
	sol.Metadata.Name = "disabled-state"

	exp := BuildSolutionExplanation(sol)
	require.NotNil(t, exp.State)
	assert.False(t, exp.State.Enabled)
}

func TestBuildSolutionExplanation_StateEnabledNil(t *testing.T) {
	sol := &solution.Solution{
		State: &state.Config{
			Enabled: nil,
			Backend: state.Backend{
				Provider: "file",
				Inputs:   map[string]*spec.ValueRef{"path": {Literal: "state.json"}},
			},
		},
	}
	sol.Metadata.Name = "nil-enabled"

	exp := BuildSolutionExplanation(sol)
	require.NotNil(t, exp.State)
	assert.True(t, exp.State.Enabled, "nil Enabled defaults to true")
}

func TestSortedKeys(t *testing.T) {
	t.Run("nil map", func(t *testing.T) {
		result := sortedKeys[string](nil)
		assert.Nil(t, result)
	})

	t.Run("empty map", func(t *testing.T) {
		result := sortedKeys(map[string]string{})
		assert.Nil(t, result)
	})

	t.Run("returns sorted", func(t *testing.T) {
		result := sortedKeys(map[string]int{"c": 3, "a": 1, "b": 2})
		assert.Equal(t, []string{"a", "b", "c"}, result)
	})
}

func TestLoadSolution_LocalFile(t *testing.T) {
	const solYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: load-inspect
  version: 1.0.0
spec:
  resolvers: {}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "solution.yaml")
	require.NoError(t, os.WriteFile(path, []byte(solYAML), 0o600))

	sol, err := LoadSolution(context.Background(), path)
	require.NoError(t, err)
	require.NotNil(t, sol)
	assert.Equal(t, "load-inspect", sol.Metadata.Name)
}

func TestLoadSolution_NotFound(t *testing.T) {
	_, err := LoadSolution(context.Background(), filepath.Join(t.TempDir(), "missing.yaml"))
	require.Error(t, err)
}

func TestLoadSolutionWithLock_LocalFileNoLock(t *testing.T) {
	const solYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: lock-inspect
  version: 1.0.0
spec:
  resolvers: {}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "solution.yaml")
	require.NoError(t, os.WriteFile(path, []byte(solYAML), 0o600))

	sol, lock, err := LoadSolutionWithLock(context.Background(), path)
	require.NoError(t, err)
	require.NotNil(t, sol)
	assert.Equal(t, "lock-inspect", sol.Metadata.Name)
	assert.Nil(t, lock) // local files carry no lock layer
}

func TestLoadSolutionWithLock_NotFound(t *testing.T) {
	_, _, err := LoadSolutionWithLock(context.Background(), filepath.Join(t.TempDir(), "missing.yaml"))
	require.Error(t, err)
}
