package solution

import (
	"testing"

	"github.com/Masterminds/semver/v3"
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
					// When Version is nil, it should still be present in JSON (not omitempty)
					assert.Contains(t, string(data), "version")
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

	t.Run("missing version", func(t *testing.T) {
		s := &Solution{APIVersion: DefaultAPIVersion, Kind: SolutionKind, Metadata: Metadata{Name: "x"}}
		err := s.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "metadata.version")
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
					// When Version is nil, check YAML representation
					// YAML will show "version: null" or similar
					assert.Contains(t, string(data), "version")
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
}
