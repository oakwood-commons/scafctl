// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solution

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSolution_UnmarshalFromBytes(t *testing.T) {
	tests := []struct {
		name    string
		bytes   []byte
		wantErr bool
	}{
		{
			name: "valid YAML with new structure",
			bytes: []byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
  displayName: Test Solution
  description: A test solution
  category: application
  version: 1.2.3
  tags:
    - tag1
    - tag2
  maintainers:
    - name: John Doe
      email: john.doe@example.com
  links:
    - name: Docs
      url: https://example.com/docs
catalog:
  visibility: public
  beta: false
  disabled: false
`),
			wantErr: false,
		},
		{
			name: "valid JSON with new structure",
			bytes: []byte(`{
				"apiVersion": "scafctl.io/v1",
				"kind": "Solution",
				"metadata": {
					"name": "test-solution",
					"displayName": "Test Solution",
					"description": "A test solution",
					"category": "application",
					"version": "1.2.3",
					"tags": ["tag1", "tag2"],
					"maintainers": [{"name": "John Doe", "email": "john.doe@example.com"}],
					"links": [{"name": "Docs", "url": "https://example.com/docs"}]
				},
				"catalog": {
					"visibility": "public",
					"beta": false,
					"disabled": false
				}
			}`),
			wantErr: false,
		},
		{
			name: "minimal valid YAML",
			bytes: []byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: minimal
  version: 1.0.0
`),
			wantErr: false,
		},
		{
			name: "name too short - minLength constraint not enforced",
			bytes: []byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: ab
  version: 1.0.0
`),
			wantErr: false, // Currently no validation is enforced, so this succeeds
		},
		{
			name:    "invalid data",
			bytes:   []byte(`not a valid yaml or json`),
			wantErr: true,
		},
		{
			name:    "empty input",
			bytes:   []byte(``),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s Solution
			gotErr := s.UnmarshalFromBytes(tt.bytes)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("UnmarshalFromBytes() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("UnmarshalFromBytes() succeeded unexpectedly")
			}

			// Validate structure for successful unmarshaling

			assert.NotEmpty(t, s.APIVersion, "APIVersion should not be empty")
			assert.NotEmpty(t, s.Kind, "Kind should not be empty")
			assert.NotEmpty(t, s.Metadata.Name, "Metadata.Name should not be empty")
			assert.NotNil(t, s.Metadata.Version, "Metadata.Version should not be nil")
		})
	}
}

func TestSolution_UnmarshalSourceAndAnnotations(t *testing.T) {
	yamlBytes := []byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
  version: 1.0.0
  source: https://github.com/example/test-solution
  annotations:
    team: platform
    costCenter: "12345"
`)
	var s Solution
	require.NoError(t, s.UnmarshalFromBytes(yamlBytes))
	assert.Equal(t, "https://github.com/example/test-solution", s.Metadata.Source)
	require.Len(t, s.Metadata.Annotations, 2)
	assert.Equal(t, "platform", s.Metadata.Annotations["team"])
	assert.Equal(t, "12345", s.Metadata.Annotations["costCenter"])
}

func TestSolution_ToJSON(t *testing.T) {
	tests := []struct {
		name           string
		solution       Solution
		wantErr        bool
		checkVersion   bool
		versionPresent bool
	}{
		{
			name: "with nil Version",
			solution: Solution{
				APIVersion: "scafctl.io/v1",
				Kind:       "Solution",
				Metadata: Metadata{
					Name:        "test-solution",
					DisplayName: "Test Solution",
				},
			},
			wantErr:        false,
			checkVersion:   true,
			versionPresent: false,
		},
		{
			name: "with populated Version",
			solution: Solution{
				APIVersion: "scafctl.io/v1",
				Kind:       "Solution",
				Metadata: Metadata{
					Name:        "test-solution",
					DisplayName: "Test Solution",
					Version:     semver.MustParse("1.2.3"),
				},
			},
			wantErr:        false,
			checkVersion:   true,
			versionPresent: true,
		},
		{
			name: "minimal with Version",
			solution: Solution{
				APIVersion: "scafctl.io/v1",
				Kind:       "Solution",
				Metadata: Metadata{
					Name:    "minimal",
					Version: semver.MustParse("0.1.0"),
				},
			},
			wantErr:        false,
			checkVersion:   true,
			versionPresent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := tt.solution.ToJSON()

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, data)
			assert.Contains(t, string(data), "apiVersion")
			assert.Contains(t, string(data), "kind")
			assert.Contains(t, string(data), "metadata")

			if tt.checkVersion {
				if tt.versionPresent {
					assert.Contains(t, string(data), "version")
					if tt.solution.Metadata.Version != nil {
						assert.Contains(t, string(data), tt.solution.Metadata.Version.String())
					}
				} else {
					// When Version is nil and omitempty, it should be absent from JSON
					assert.NotContains(t, string(data), "version")
				}
			}
		})
	}
}

func TestSolution_ApplyDefaults(t *testing.T) {
	s := &Solution{}

	s.ApplyDefaults()

	require.Equal(t, DefaultAPIVersion, s.APIVersion)
	require.Equal(t, SolutionKind, s.Kind)
	require.Equal(t, "private", s.Catalog.Visibility)
}

func TestSolution_Validate(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		s := &Solution{
			APIVersion: DefaultAPIVersion,
			Kind:       SolutionKind,
			Metadata: Metadata{
				Name:    "valid",
				Version: semver.MustParse("1.0.0"),
			},
			Catalog: Catalog{Visibility: "public"},
		}
		assert.NoError(t, s.Validate())
	})

	t.Run("invalid apiversion", func(t *testing.T) {
		s := &Solution{APIVersion: "bad", Kind: SolutionKind, Metadata: Metadata{Name: "x", Version: semver.MustParse("1.0.0")}}
		err := s.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "apiVersion")
	})

	t.Run("invalid kind", func(t *testing.T) {
		s := &Solution{APIVersion: DefaultAPIVersion, Kind: "Other", Metadata: Metadata{Name: "x", Version: semver.MustParse("1.0.0")}}
		err := s.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "kind")
	})

	t.Run("missing name", func(t *testing.T) {
		s := &Solution{APIVersion: DefaultAPIVersion, Kind: SolutionKind, Metadata: Metadata{Version: semver.MustParse("1.0.0")}}
		err := s.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "metadata.name")
	})

	t.Run("missing version gets default", func(t *testing.T) {
		s := &Solution{APIVersion: DefaultAPIVersion, Kind: SolutionKind, Metadata: Metadata{Name: "x"}}
		s.ApplyDefaults()
		err := s.Validate()
		require.NoError(t, err)
		assert.Equal(t, "0.0.0-dev", s.Metadata.Version.String())
	})

	t.Run("invalid visibility", func(t *testing.T) {
		s := &Solution{APIVersion: DefaultAPIVersion, Kind: SolutionKind, Metadata: Metadata{Name: "x", Version: semver.MustParse("1.0.0")}, Catalog: Catalog{Visibility: "weird"}}
		err := s.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "catalog.visibility")
	})
}

func TestSolution_ToYAML(t *testing.T) {
	tests := []struct {
		name           string
		solution       Solution
		wantErr        bool
		checkVersion   bool
		versionPresent bool
	}{
		{
			name: "with nil Version",
			solution: Solution{
				APIVersion: "scafctl.io/v1",
				Kind:       "Solution",
				Metadata: Metadata{
					Name:        "test-solution",
					DisplayName: "Test Solution",
				},
			},
			wantErr:        false,
			checkVersion:   true,
			versionPresent: false,
		},
		{
			name: "with populated Version",
			solution: Solution{
				APIVersion: "scafctl.io/v1",
				Kind:       "Solution",
				Metadata: Metadata{
					Name:        "test-solution",
					DisplayName: "Test Solution",
					Version:     semver.MustParse("2.0.1"),
				},
			},
			wantErr:        false,
			checkVersion:   true,
			versionPresent: true,
		},
		{
			name: "with prerelease Version",
			solution: Solution{
				APIVersion: "scafctl.io/v1",
				Kind:       "Solution",
				Metadata: Metadata{
					Name:    "prerelease",
					Version: semver.MustParse("1.0.0-alpha.1"),
				},
			},
			wantErr:        false,
			checkVersion:   true,
			versionPresent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := tt.solution.ToYAML()

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, data)
			assert.Contains(t, string(data), "apiVersion")
			assert.Contains(t, string(data), "kind")
			assert.Contains(t, string(data), "metadata")

			if tt.checkVersion {
				if tt.versionPresent {
					assert.Contains(t, string(data), "version")
					if tt.solution.Metadata.Version != nil {
						assert.Contains(t, string(data), tt.solution.Metadata.Version.String())
					}
				} else {
					// When Version is nil and omitempty, it should be absent from YAML
					assert.NotContains(t, string(data), "version")
				}
			}
		})
	}
}

func TestSolution_GetSetPath(t *testing.T) {
	s := Solution{}
	path := "/path/to/solution.yaml"

	s.SetPath(path)
	assert.Equal(t, path, s.GetPath())
}

func TestSolution_Provenance(t *testing.T) {
	t.Run("prefers file system path", func(t *testing.T) {
		s := Solution{}
		s.SetPath("/path/to/solution.yaml")
		s.Metadata.Source = "github.com/acme/solutions//demo"
		assert.Equal(t, "/path/to/solution.yaml", s.Provenance())
	})

	t.Run("falls back to metadata source when path empty", func(t *testing.T) {
		s := Solution{}
		s.Metadata.Source = "github.com/acme/solutions//demo"
		assert.Equal(t, "github.com/acme/solutions//demo", s.Provenance())
	})

	t.Run("empty when neither path nor source known", func(t *testing.T) {
		s := Solution{}
		assert.Empty(t, s.Provenance())
	})
}

func TestSolution_LoadFromBytes(t *testing.T) {
	t.Run("applies defaults and validates", func(t *testing.T) {
		data := []byte(`metadata:
  name: my-solution
  version: 1.0.0
`)
		var s Solution
		require.NoError(t, s.LoadFromBytes(data))
		assert.Equal(t, DefaultAPIVersion, s.APIVersion)
		assert.Equal(t, SolutionKind, s.Kind)
		assert.Equal(t, "my-solution", s.Metadata.Name)
		assert.NotNil(t, s.Metadata.Version)
		assert.Equal(t, "private", s.Catalog.Visibility)
	})

	t.Run("fails on invalid bytes", func(t *testing.T) {
		var s Solution
		err := s.LoadFromBytes([]byte("nope"))
		require.Error(t, err)
	})

	t.Run("nil receiver", func(t *testing.T) {
		var s *Solution
		err := s.LoadFromBytes([]byte("{}"))
		require.Error(t, err)
	})

	t.Run("defaults plugin kind to provider", func(t *testing.T) {
		data := []byte(`metadata:
  name: plugin-test
  version: 1.0.0
bundle:
  plugins:
    - name: oci
      version: ">=0.5.0"
`)
		var s Solution
		require.NoError(t, s.LoadFromBytes(data))
		require.Len(t, s.Bundle.Plugins, 1)
		assert.Equal(t, PluginKindProvider, s.Bundle.Plugins[0].Kind)
	})
}

func TestSolution_LoadFromBytesLenient(t *testing.T) {
	// A solution whose dependsOn references an undefined resolver fails the
	// strict LoadFromBytes but must load successfully in lenient mode so
	// advisory tooling can report the problem as a finding.
	data := []byte(`metadata:
  name: lenient-test
  version: 1.0.0
spec:
  resolvers:
    app:
      dependsOn: [doesNotExist]
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
`)

	t.Run("strict load fails on undefined dependsOn", func(t *testing.T) {
		var s Solution
		err := s.LoadFromBytes(data)
		require.Error(t, err)
	})

	t.Run("lenient load parses and defaults without validating spec", func(t *testing.T) {
		var s Solution
		require.NoError(t, s.LoadFromBytesLenient(data))
		assert.Equal(t, DefaultAPIVersion, s.APIVersion)
		assert.Equal(t, SolutionKind, s.Kind)
		assert.Equal(t, "lenient-test", s.Metadata.Name)
		require.Contains(t, s.Spec.Resolvers, "app")
	})

	t.Run("lenient load still returns parse errors", func(t *testing.T) {
		var s Solution
		err := s.LoadFromBytesLenient([]byte("nope"))
		require.Error(t, err)
	})

	t.Run("nil receiver", func(t *testing.T) {
		var s *Solution
		err := s.LoadFromBytesLenient([]byte("{}"))
		require.Error(t, err)
	})
}

func TestBundle_IsEmpty(t *testing.T) {
	b := Bundle{}
	assert.True(t, b.IsEmpty())

	b2 := Bundle{Include: []string{"something"}}
	assert.False(t, b2.IsEmpty())
}

func TestBundle_PartitionPlugins(t *testing.T) {
	t.Run("empty bundle", func(t *testing.T) {
		unsourced, sourced := Bundle{}.PartitionPlugins()
		assert.Nil(t, unsourced)
		assert.Nil(t, sourced)
	})

	t.Run("splits and preserves order", func(t *testing.T) {
		b := Bundle{Plugins: []PluginDependency{
			{Name: "local-a"},
			{Name: "remote-a", Source: &PluginSource{Registry: "ghcr.io/myorg", Artifact: "remote-a"}},
			{Name: "local-b"},
			{Name: "remote-b", Source: &PluginSource{Registry: "registry.example.com", Artifact: "remote-b"}},
		}}

		unsourced, sourced := b.PartitionPlugins()

		assert.Equal(t, []PluginDependency{
			{Name: "local-a"},
			{Name: "local-b"},
		}, unsourced)
		assert.Equal(t, []PluginDependency{
			{Name: "remote-a", Source: &PluginSource{Registry: "ghcr.io/myorg", Artifact: "remote-a"}},
			{Name: "remote-b", Source: &PluginSource{Registry: "registry.example.com", Artifact: "remote-b"}},
		}, sourced)
	})

	t.Run("all unsourced", func(t *testing.T) {
		b := Bundle{Plugins: []PluginDependency{{Name: "a"}, {Name: "b"}}}
		unsourced, sourced := b.PartitionPlugins()
		assert.Len(t, unsourced, 2)
		assert.Nil(t, sourced)
	})

	t.Run("all include registry", func(t *testing.T) {
		b := Bundle{Plugins: []PluginDependency{
			{Name: "a", Source: &PluginSource{Registry: "ghcr.io/org", Artifact: "a"}},
			{Name: "b", Source: &PluginSource{Registry: "ghcr.io/org", Artifact: "b"}},
		}}
		unsourced, sourced := b.PartitionPlugins()
		assert.Nil(t, unsourced)
		assert.Len(t, sourced, 2)
	})
}

func TestPluginKind_IsValid(t *testing.T) {
	assert.True(t, PluginKindProvider.IsValid())
	assert.True(t, PluginKindAuthHandler.IsValid())
	assert.False(t, PluginKind("invalid").IsValid())
	assert.False(t, PluginKind("").IsValid())
}

func TestPluginDependency_ArtifactName(t *testing.T) {
	t.Run("local plugin falls back to name", func(t *testing.T) {
		assert.Equal(t, "exec", PluginDependency{Name: "exec"}.ArtifactName())
	})
	t.Run("sourced plugin uses source artifact", func(t *testing.T) {
		dep := PluginDependency{Name: "my-exec", Source: &PluginSource{Registry: "ghcr.io/myorg", Artifact: "scafctl-exec-provider"}}
		assert.Equal(t, "scafctl-exec-provider", dep.ArtifactName())
	})
	t.Run("local name returns the alias", func(t *testing.T) {
		dep := PluginDependency{Name: "my-exec", Source: &PluginSource{Registry: "ghcr.io/myorg", Artifact: "scafctl-exec-provider"}}
		assert.Equal(t, "my-exec", dep.LocalName())
	})
}

func TestSolution_ToJSONPretty(t *testing.T) {
	s := &Solution{}
	s.Metadata.Name = "test-solution"
	data, err := s.ToJSONPretty()
	require.NoError(t, err)
	assert.Contains(t, string(data), "test-solution")
	assert.Contains(t, string(data), "\n") // pretty-printed
}

func TestSolution_FromJSON(t *testing.T) {
	s := &Solution{}
	jsonData := `{"apiVersion":"scafctl.oakwood-commons.io/v1","kind":"Solution","metadata":{"name":"my-sol"}}`
	err := s.FromJSON([]byte(jsonData))
	require.NoError(t, err)
	assert.Equal(t, "my-sol", s.Metadata.Name)
}

func TestSolution_FromJSON_Invalid(t *testing.T) {
	s := &Solution{}
	err := s.FromJSON([]byte("not json"))
	require.Error(t, err)
}

func TestSolution_SourceMap_And_SetSourceMap(t *testing.T) {
	s := &Solution{}
	assert.Nil(t, s.SourceMap())

	s.SetSourceMap(nil)
	assert.Nil(t, s.SourceMap())
}

func TestSpec_HasTesting(t *testing.T) {
	var sp *Spec
	assert.False(t, sp.HasTesting())

	sp = &Spec{}
	assert.False(t, sp.HasTesting())

	sp.Testing = &soltesting.TestSuite{}
	assert.True(t, sp.HasTesting())
}

func TestDefaultVersion(t *testing.T) {
	t.Parallel()

	v := DefaultVersion()
	require.NotNil(t, v)
	assert.Equal(t, "0.0.0-dev", v.String())

	// Verify it returns a distinct copy (mutating one doesn't affect the other)
	v2 := DefaultVersion()
	assert.Equal(t, v, v2)
	assert.NotSame(t, v, v2, "DefaultVersion must return distinct pointers")

	// Mutate v and verify v2 is unchanged
	*v, _ = v.SetPrerelease("mutated")
	assert.Equal(t, "0.0.0-dev", v2.String(), "mutating one copy must not affect the other")
}

func TestSolution_RawContent(t *testing.T) {
	t.Parallel()

	t.Run("nil when not loaded from bytes", func(t *testing.T) {
		t.Parallel()
		s := &Solution{}
		assert.Nil(t, s.RawContent())
	})

	t.Run("preserves original YAML", func(t *testing.T) {
		t.Parallel()
		yaml := []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
spec: {}
`)
		s := &Solution{}
		require.NoError(t, s.FromYAML(yaml))

		raw := s.RawContent()
		assert.Equal(t, yaml, raw)
	})

	t.Run("returns a copy not a reference", func(t *testing.T) {
		t.Parallel()
		yaml := []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
spec: {}
`)
		s := &Solution{}
		require.NoError(t, s.FromYAML(yaml))

		raw1 := s.RawContent()
		raw2 := s.RawContent()
		raw1[0] = 'X'
		assert.NotEqual(t, raw1, raw2, "mutating one copy should not affect the other")
	})

	t.Run("preserves original JSON", func(t *testing.T) {
		t.Parallel()
		jsonData := []byte(`{"apiVersion":"scafctl.io/v1","kind":"Solution","metadata":{"name":"test"}}`)
		s := &Solution{}
		require.NoError(t, s.FromJSON(jsonData))

		raw := s.RawContent()
		assert.Equal(t, jsonData, raw)
	})
}

func TestSolution_ApplyDefaults_SetsDefaultVersion(t *testing.T) {
	t.Parallel()

	s := &Solution{}
	s.ApplyDefaults()

	require.NotNil(t, s.Metadata.Version)
	assert.Equal(t, "0.0.0-dev", s.Metadata.Version.String())
}

func TestSolution_ApplyDefaults_PreservesExistingVersion(t *testing.T) {
	t.Parallel()

	s := &Solution{
		Metadata: Metadata{
			Version: semver.MustParse("1.2.3"),
		},
	}
	s.ApplyDefaults()
	assert.Equal(t, "1.2.3", s.Metadata.Version.String())
}

func TestSolution_ApplyDefaults_PluginKindDefaultsToProvider(t *testing.T) {
	t.Parallel()

	s := &Solution{
		Bundle: Bundle{
			Plugins: []PluginDependency{
				{Name: "oci", Version: ">=0.5.0"},
				{Name: "myauth", Kind: PluginKindAuthHandler, Version: "1.0.0"},
				{Name: "exec", Kind: PluginKindProvider, Version: "^2.0.0"},
			},
		},
	}
	s.ApplyDefaults()

	assert.Equal(t, PluginKindProvider, s.Bundle.Plugins[0].Kind, "empty kind should default to provider")
	assert.Equal(t, PluginKindAuthHandler, s.Bundle.Plugins[1].Kind, "explicit auth-handler kind should be preserved")
	assert.Equal(t, PluginKindProvider, s.Bundle.Plugins[2].Kind, "explicit provider kind should be preserved")
}

// TestSolution_ValidateStateConfig exercises stateConfigProblems through the
// public Validate() gate: every structural state problem must abort validation,
// and a load/save pair must pass cleanly.
func TestSolution_ValidateStateConfig(t *testing.T) {
	tests := []struct {
		name    string
		state   *state.Config
		wantErr string // empty means Validate must succeed
	}{
		{
			name:    "valid load and extends save target",
			state:   &state.Config{Load: &state.LoadConfig{Provider: "file"}, Save: []state.SaveTarget{{Extends: state.ExtendsLoad}}},
			wantErr: "",
		},
		{
			name:    "valid save-only config",
			state:   &state.Config{Save: []state.SaveTarget{{Provider: "file"}}},
			wantErr: "",
		},
		{
			name:    "valid load-only config",
			state:   &state.Config{Load: &state.LoadConfig{Provider: "file"}},
			wantErr: "",
		},
		{
			name:    "load block without provider",
			state:   &state.Config{Load: &state.LoadConfig{}},
			wantErr: "state.load.provider is required when state.load is configured",
		},
		{
			name:    "save target without provider or extends",
			state:   &state.Config{Save: []state.SaveTarget{{Inputs: map[string]*spec.ValueRef{"path": {Literal: "state.json"}}}}},
			wantErr: "state.save[0].provider is required unless extends is set",
		},
		{
			name:    "unsupported extends value",
			state:   &state.Config{Load: &state.LoadConfig{Provider: "file"}, Save: []state.SaveTarget{{Extends: "elsewhere"}}},
			wantErr: `state.save[0].extends must be "load"`,
		},
		{
			name:    "extends combined with provider",
			state:   &state.Config{Load: &state.LoadConfig{Provider: "file"}, Save: []state.SaveTarget{{Extends: state.ExtendsLoad, Provider: "file"}}},
			wantErr: "state.save[0]: extends and provider are mutually exclusive",
		},
		{
			name:    "extends without a load block",
			state:   &state.Config{Save: []state.SaveTarget{{Extends: state.ExtendsLoad}}},
			wantErr: `state.save[0].extends: load requires a state.load block`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Solution{
				APIVersion: DefaultAPIVersion,
				Kind:       SolutionKind,
				Metadata:   Metadata{Name: "state-config"},
				State:      tt.state,
			}

			err := s.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSolution_ReferencedProviderNames(t *testing.T) {
	t.Parallel()

	specWithProviders := func(providers ...string) Spec {
		with := make([]resolver.ProviderSource, 0, len(providers))
		for _, p := range providers {
			with = append(with, resolver.ProviderSource{Provider: p})
		}
		return Spec{
			Resolvers: map[string]*resolver.Resolver{
				"r1": {Resolve: &resolver.ResolvePhase{With: with}},
			},
		}
	}

	tests := []struct {
		name string
		sol  *Solution
		want []string
	}{
		{
			name: "nil solution",
			sol:  nil,
			want: nil,
		},
		{
			name: "no state falls back to spec providers",
			sol:  &Solution{Spec: specWithProviders("env", "parameter")},
			want: []string{"env", "parameter"},
		},
		{
			name: "state load provider is added and sorted",
			sol: &Solution{
				Spec:  specWithProviders("parameter"),
				State: &state.Config{Load: &state.LoadConfig{Provider: "github"}},
			},
			want: []string{"github", "parameter"},
		},
		{
			name: "state provider deduplicated when also spec-referenced",
			sol: &Solution{
				Spec:  specWithProviders("file", "parameter"),
				State: &state.Config{Load: &state.LoadConfig{Provider: "file"}},
			},
			want: []string{"file", "parameter"},
		},
		{
			name: "empty state providers are ignored",
			sol: &Solution{
				Spec:  specWithProviders("parameter"),
				State: &state.Config{Load: &state.LoadConfig{}, Save: []state.SaveTarget{{Extends: state.ExtendsLoad}}},
			},
			want: []string{"parameter"},
		},
		{
			name: "save target providers are added, extends adds nothing new",
			sol: &Solution{
				Spec: specWithProviders("parameter"),
				State: &state.Config{
					Load: &state.LoadConfig{Provider: "github"},
					Save: []state.SaveTarget{{Extends: state.ExtendsLoad}, {Provider: "file"}, {Provider: "github"}},
				},
			},
			want: []string{"file", "github", "parameter"},
		},
		{
			name: "save-only state with no spec references",
			sol: &Solution{
				State: &state.Config{Save: []state.SaveTarget{{Provider: "file"}}},
			},
			want: []string{"file"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.sol.ReferencedProviderNames())
		})
	}
}

// TestSolution_LegacyStateKeysRejected verifies the decode-time rejection of
// state keys removed by the load/save split: every legacy key fails with
// state.ErrLegacyStateConfig and names the offending key.
func TestSolution_LegacyStateKeysRejected(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantKey string
	}{
		{
			name: "state.backend",
			data: `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: legacy
state:
  enabled: true
  backend:
    provider: file
    inputs:
      path: state.json
`,
			wantKey: "state.backend",
		},
		{
			name: "state.emit",
			data: `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: legacy
state:
  load:
    provider: file
  emit:
    - provider: file
      format: intent
`,
			wantKey: "state.emit",
		},
		{
			name: "state.load.saveOverrides",
			data: `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: legacy
state:
  load:
    provider: file
    saveOverrides:
      branch: main
`,
			wantKey: "state.load.saveOverrides",
		},
		{
			name: "state.load.format",
			data: `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: legacy
state:
  load:
    provider: file
    format: intent
`,
			wantKey: "state.load.format",
		},
		{
			name: "state.load.parameters",
			data: `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: legacy
state:
  load:
    provider: file
    parameters:
      exclude: [mode]
`,
			wantKey: "state.load.parameters",
		},
		{
			name: "state.save saveOverrides",
			data: `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: legacy
state:
  load:
    provider: file
  save:
    - extends: load
      saveOverrides:
        branch: main
`,
			wantKey: "state.save[].saveOverrides",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/yaml", func(t *testing.T) {
			s := &Solution{}
			err := s.FromYAML([]byte(tt.data))
			require.Error(t, err, "legacy state key %q must be rejected at decode time", tt.wantKey)
			assert.True(t, errors.Is(err, state.ErrLegacyStateConfig),
				"error must wrap state.ErrLegacyStateConfig, got: %v", err)
			assert.Contains(t, err.Error(), tt.wantKey, "error must name the offending key")
		})
	}

	t.Run("state.backend/json", func(t *testing.T) {
		s := &Solution{}
		err := s.FromJSON([]byte(`{"apiVersion":"scafctl.io/v1","kind":"Solution","metadata":{"name":"legacy"},"state":{"backend":{"provider":"file"}}}`))
		require.Error(t, err)
		assert.True(t, errors.Is(err, state.ErrLegacyStateConfig),
			"error must wrap state.ErrLegacyStateConfig, got: %v", err)
		assert.Contains(t, err.Error(), "state.backend")
	})

	t.Run("new load/save split still loads", func(t *testing.T) {
		s := &Solution{}
		err := s.FromYAML([]byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: modern
state:
  load:
    provider: file
    inputs:
      path: state.json
  save:
    - extends: load
`))
		require.NoError(t, err)
		require.NotNil(t, s.State)
		require.NotNil(t, s.State.Load)
		assert.Equal(t, "file", s.State.Load.Provider)
		require.Len(t, s.State.Save, 1)
		assert.Equal(t, state.ExtendsLoad, s.State.Save[0].Extends)
	})
}

// TestSolution_ReferencedProviderNames_ReflectionCoverage guards against a new
// field being added that carries a data-provider handle but is not wired into
// Solution.ReferencedProviderNames. It walks the Solution type graph via
// reflection and collects every reachable exported struct field named exactly
// "Provider" of kind string -- the data-provider handle convention across the
// spec (auth-handler "AuthProvider" fields are intentionally NOT matched, since
// they are not registrable data providers). It then asserts the set of owning
// types is exactly the set ReferencedProviderNames reads.
//
// If this test fails after adding a provider field, wire the new field into
// Solution.ReferencedProviderNames (and Spec.ReferencedProviderNames if it lives
// under the spec). If the field is deliberately not a registrable data provider,
// add its owning type to the ignored set below with a rationale.
func TestSolution_ReferencedProviderNames_ReflectionCoverage(t *testing.T) {
	t.Parallel()

	const (
		scafctlPkgPrefix  = "github.com/oakwood-commons/scafctl/"
		providerFieldName = "Provider"
	)

	// collected lists the types whose Provider field ReferencedProviderNames
	// (via Spec.ReferencedProviderNames plus the state load and save providers) actually reads.
	// Keyed by reflect.Type.String() (e.g. "resolver.ProviderSource").
	collected := map[string]struct{}{
		"resolver.ProviderSource":     {},
		"resolver.ProviderTransform":  {},
		"resolver.ProviderValidation": {},
		"spec.Call":                   {},
		"action.Action":               {},
		"state.LoadConfig":            {},
		"state.SaveTarget":            {},
	}

	// ignored lists reachable Provider-bearing types that intentionally do NOT
	// contribute registrable data providers. Add entries here (with a reason)
	// only when a new "Provider" field is genuinely not a data-provider handle.
	ignored := map[string]struct{}{}

	found := map[string][]string{} // reflect type string -> field paths
	visited := map[reflect.Type]bool{}

	var walk func(rt reflect.Type, path string)
	walk = func(rt reflect.Type, path string) {
		// Unwrap containers to reach the underlying struct type. Map keys cannot
		// hold provider fields we care about, so following Elem() (the value
		// type) is sufficient.
		for rt.Kind() == reflect.Pointer || rt.Kind() == reflect.Slice ||
			rt.Kind() == reflect.Array || rt.Kind() == reflect.Map {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct || visited[rt] {
			return
		}
		visited[rt] = true
		// Provider handles only live on scafctl-owned spec types; never descend
		// into third-party types (semver, jsonschema, time, ...).
		if !strings.HasPrefix(rt.PkgPath(), scafctlPkgPrefix) {
			return
		}
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if f.PkgPath != "" {
				continue // unexported: not part of the serializable graph
			}
			fpath := path + "." + f.Name
			if f.Name == providerFieldName && f.Type.Kind() == reflect.String {
				found[rt.String()] = append(found[rt.String()], fpath)
				continue
			}
			walk(f.Type, fpath)
		}
	}
	walk(reflect.TypeOf(Solution{}), "Solution")

	// Every expected owner must still be reachable; otherwise the collected set
	// has silently rotted (e.g. a field was renamed or removed).
	for owner := range collected {
		assert.Containsf(t, found, owner,
			"expected Provider owner %q to be reachable from Solution but reflection did not find it; update the collected set", owner)
	}

	// Every reachable Provider owner must be either collected or explicitly
	// ignored -- otherwise ReferencedProviderNames is silently missing a field.
	for owner, paths := range found {
		if _, ok := collected[owner]; ok {
			continue
		}
		if _, ok := ignored[owner]; ok {
			continue
		}
		t.Errorf("provider field on type %q (paths: %v) is not handled by Solution.ReferencedProviderNames; "+
			"wire it into the method or add %q to the ignored set with a rationale", owner, paths, owner)
	}
}
