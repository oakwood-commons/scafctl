// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseReference(t *testing.T) {
	tests := []struct {
		name     string
		kind     ArtifactKind
		input    string
		wantName string
		wantVer  string
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "simple name without version",
			kind:     ArtifactKindSolution,
			input:    "my-solution",
			wantName: "my-solution",
			wantVer:  "",
		},
		{
			name:     "name with version",
			kind:     ArtifactKindSolution,
			input:    "my-solution@1.0.0",
			wantName: "my-solution",
			wantVer:  "1.0.0",
		},
		{
			name:     "name with semver v prefix",
			kind:     ArtifactKindSolution,
			input:    "my-app@v2.1.0",
			wantName: "my-app",
			wantVer:  "2.1.0",
		},
		{
			name:     "name with prerelease",
			kind:     ArtifactKindProvider,
			input:    "echo@1.0.0-alpha.1",
			wantName: "echo",
			wantVer:  "1.0.0-alpha.1",
		},
		{
			name:     "single character name",
			kind:     ArtifactKindSolution,
			input:    "a",
			wantName: "a",
		},
		{
			name:    "empty string",
			kind:    ArtifactKindSolution,
			input:   "",
			wantErr: true,
			errMsg:  "cannot be empty",
		},
		{
			name:    "name with uppercase",
			kind:    ArtifactKindSolution,
			input:   "My-Solution",
			wantErr: true,
			errMsg:  "lowercase alphanumeric",
		},
		{
			name:    "name starting with number",
			kind:    ArtifactKindSolution,
			input:   "123-solution",
			wantErr: true,
			errMsg:  "lowercase alphanumeric",
		},
		{
			name:    "name ending with hyphen",
			kind:    ArtifactKindSolution,
			input:   "my-solution-",
			wantErr: true,
			errMsg:  "lowercase alphanumeric",
		},
		{
			name:    "name with double hyphen",
			kind:    ArtifactKindSolution,
			input:   "my--solution",
			wantErr: true,
			errMsg:  "lowercase alphanumeric",
		},
		{
			name:    "invalid version",
			kind:    ArtifactKindSolution,
			input:   "my-solution@invalid",
			wantErr: true,
			errMsg:  "invalid version",
		},
		{
			name:    "multiple @ symbols",
			kind:    ArtifactKindSolution,
			input:   "my@solution@1.0.0",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := ParseReference(tt.kind, tt.input)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.kind, ref.Kind)
			assert.Equal(t, tt.wantName, ref.Name)

			if tt.wantVer != "" {
				require.NotNil(t, ref.Version)
				assert.Equal(t, tt.wantVer, ref.Version.String())
			} else {
				assert.Nil(t, ref.Version)
			}
		})
	}
}

func TestReference_String(t *testing.T) {
	tests := []struct {
		name string
		ref  Reference
		want string
	}{
		{
			name: "with version",
			ref: Reference{
				Kind:    ArtifactKindSolution,
				Name:    "my-solution",
				Version: semver.MustParse("1.0.0"),
			},
			want: "my-solution@1.0.0",
		},
		{
			name: "without version",
			ref: Reference{
				Kind: ArtifactKindProvider,
				Name: "echo",
			},
			want: "echo",
		},
		{
			name: "with digest",
			ref: Reference{
				Kind:   ArtifactKindSolution,
				Name:   "my-solution",
				Digest: "sha256:abc123",
			},
			want: "my-solution@sha256:abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.ref.String())
		})
	}
}

func TestReference_HasVersion(t *testing.T) {
	t.Run("has version", func(t *testing.T) {
		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "test",
			Version: semver.MustParse("1.0.0"),
		}
		assert.True(t, ref.HasVersion())
	})

	t.Run("no version", func(t *testing.T) {
		ref := Reference{
			Kind: ArtifactKindSolution,
			Name: "test",
		}
		assert.False(t, ref.HasVersion())
	})
}

func TestReference_HasDigest(t *testing.T) {
	t.Run("has digest", func(t *testing.T) {
		ref := Reference{
			Kind:   ArtifactKindSolution,
			Name:   "test",
			Digest: "sha256:abc123",
		}
		assert.True(t, ref.HasDigest())
	})

	t.Run("no digest", func(t *testing.T) {
		ref := Reference{
			Kind: ArtifactKindSolution,
			Name: "test",
		}
		assert.False(t, ref.HasDigest())
	})
}

func TestIsValidName(t *testing.T) {
	valid := []string{"a", "abc", "my-solution", "my-app-v2", "app123", "a1"}

	for _, name := range valid {
		t.Run("valid: "+name, func(t *testing.T) {
			assert.True(t, IsValidName(name))
		})
	}

	invalid := []string{"", "My-Solution", "123abc", "my_solution", "my--solution", "-mysolution", "mysolution-", "my.solution"}

	for _, name := range invalid {
		t.Run("invalid: "+name, func(t *testing.T) {
			assert.False(t, IsValidName(name))
		})
	}
}

func TestIsValidDigest(t *testing.T) {
	t.Run("valid digest", func(t *testing.T) {
		assert.True(t, IsValidDigest("sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"))
	})

	invalid := []string{
		"",
		"sha256:",
		"sha256:abc",
		"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b85", // 63 chars
		"sha512:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}

	for _, digest := range invalid {
		name := "invalid"
		if digest != "" {
			name = "invalid: " + digest[:min(20, len(digest))]
		}
		t.Run(name, func(t *testing.T) {
			assert.False(t, IsValidDigest(digest))
		})
	}
}

func TestParseRemoteReference(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantRegistry   string
		wantRepository string
		wantKind       ArtifactKind
		wantName       string
		wantTag        string
		wantErr        bool
		errMsg         string
	}{
		{
			name:           "full path with solutions",
			input:          "ghcr.io/myorg/scafctl/solutions/my-solution@1.0.0",
			wantRegistry:   "ghcr.io",
			wantRepository: "myorg/scafctl",
			wantKind:       ArtifactKindSolution,
			wantName:       "my-solution",
			wantTag:        "1.0.0",
		},
		{
			name:           "full path with providers",
			input:          "ghcr.io/myorg/scafctl/providers/echo@2.0.0",
			wantRegistry:   "ghcr.io",
			wantRepository: "myorg/scafctl",
			wantKind:       ArtifactKindProvider,
			wantName:       "echo",
			wantTag:        "2.0.0",
		},
		{
			name:           "with oci:// prefix",
			input:          "oci://ghcr.io/myorg/scafctl/solutions/my-solution@1.0.0",
			wantRegistry:   "ghcr.io",
			wantRepository: "myorg/scafctl",
			wantKind:       ArtifactKindSolution,
			wantName:       "my-solution",
			wantTag:        "1.0.0",
		},
		{
			name:           "without version tag",
			input:          "ghcr.io/myorg/scafctl/solutions/my-solution",
			wantRegistry:   "ghcr.io",
			wantRepository: "myorg/scafctl",
			wantKind:       ArtifactKindSolution,
			wantName:       "my-solution",
			wantTag:        "",
		},
		{
			name:           "simple repository path",
			input:          "ghcr.io/myorg/my-solution@1.0.0",
			wantRegistry:   "ghcr.io",
			wantRepository: "myorg",
			wantKind:       "",
			wantName:       "my-solution",
			wantTag:        "1.0.0",
		},
		{
			name:           "localhost registry with port",
			input:          "localhost:5000/scafctl/solutions/test@1.0.0",
			wantRegistry:   "localhost:5000",
			wantRepository: "scafctl",
			wantKind:       ArtifactKindSolution,
			wantName:       "test",
			wantTag:        "1.0.0",
		},
		{
			name:           "Docker Hub style with colon tag",
			input:          "docker.io/myorg/myimage:1.0.0",
			wantRegistry:   "docker.io",
			wantRepository: "myorg",
			wantKind:       "",
			wantName:       "myimage",
			wantTag:        "1.0.0",
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
			errMsg:  "cannot be empty",
		},
		{
			name:    "registry only",
			input:   "ghcr.io",
			wantErr: true,
			errMsg:  "must include registry and repository",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := ParseRemoteReference(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantRegistry, ref.Registry)
			assert.Equal(t, tt.wantRepository, ref.Repository)
			assert.Equal(t, tt.wantKind, ref.Kind)
			assert.Equal(t, tt.wantName, ref.Name)
			assert.Equal(t, tt.wantTag, ref.Tag)
		})
	}
}

func TestRemoteReference_ToReference(t *testing.T) {
	tests := []struct {
		name       string
		remote     RemoteReference
		wantKind   ArtifactKind
		wantName   string
		wantVer    string
		wantDigest string
		wantErr    bool
	}{
		{
			name: "with version tag",
			remote: RemoteReference{
				Kind: ArtifactKindSolution,
				Name: "my-solution",
				Tag:  "1.0.0",
			},
			wantKind: ArtifactKindSolution,
			wantName: "my-solution",
			wantVer:  "1.0.0",
		},
		{
			name: "without tag",
			remote: RemoteReference{
				Kind: ArtifactKindProvider,
				Name: "echo",
			},
			wantKind: ArtifactKindProvider,
			wantName: "echo",
		},
		{
			name: "with digest tag",
			remote: RemoteReference{
				Kind: ArtifactKindSolution,
				Name: "my-solution",
				Tag:  "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			},
			wantKind:   ArtifactKindSolution,
			wantName:   "my-solution",
			wantDigest: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name: "invalid version tag",
			remote: RemoteReference{
				Kind: ArtifactKindSolution,
				Name: "my-solution",
				Tag:  "invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := tt.remote.ToReference()
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantKind, ref.Kind)
			assert.Equal(t, tt.wantName, ref.Name)
			if tt.wantVer != "" {
				assert.Equal(t, tt.wantVer, ref.Version.String())
			}
			if tt.wantDigest != "" {
				assert.Equal(t, tt.wantDigest, ref.Digest)
			}
		})
	}
}

func TestValidateAlias(t *testing.T) {
	tests := []struct {
		name    string
		alias   string
		wantErr string
	}{
		{name: "valid alias - stable", alias: "stable"},
		{name: "valid alias - latest", alias: "latest"},
		{name: "valid alias - production", alias: "production"},
		{name: "valid alias - with dots", alias: "v1.release"},
		{name: "valid alias - with hyphens", alias: "pre-release"},
		{name: "valid alias - with underscores", alias: "staging_v2"},
		{name: "valid alias - uppercase", alias: "STABLE"},
		{name: "empty alias", alias: "", wantErr: "cannot be empty"},
		{name: "semver version - rejected", alias: "1.0.0", wantErr: "looks like a semver version"},
		{name: "semver with prerelease - rejected", alias: "1.2.3-alpha.1", wantErr: "looks like a semver version"},
		{name: "contains slash", alias: "foo/bar", wantErr: "invalid character"},
		{name: "contains space", alias: "foo bar", wantErr: "invalid character"},
		{name: "contains colon", alias: "foo:bar", wantErr: "invalid character"},
		{name: "starts with dot", alias: ".hidden", wantErr: "must start with"},
		{name: "starts with hyphen", alias: "-flag", wantErr: "must start with"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAlias(tt.alias)
			if tt.wantErr != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestIsValidTagChar(t *testing.T) {
	valid := []rune{'a', 'z', 'A', 'Z', '0', '9', '_', '.', '-'}
	for _, ch := range valid {
		assert.True(t, IsValidTagChar(ch), "expected %q to be valid", string(ch))
	}

	invalid := []rune{'/', ':', ' ', '@', '#', '!'}
	for _, ch := range invalid {
		assert.False(t, IsValidTagChar(ch), "expected %q to be invalid", string(ch))
	}
}

func BenchmarkValidateAlias(b *testing.B) {
	for b.Loop() {
		_ = ValidateAlias("production")
	}
}

func TestArtifactKind_Plural(t *testing.T) {
	assert.Equal(t, "solutions", ArtifactKindSolution.Plural())
	assert.Equal(t, "providers", ArtifactKindProvider.Plural())
	assert.Equal(t, "auth-handlers", ArtifactKindAuthHandler.Plural())
	assert.Equal(t, "unknowns", ArtifactKind("unknown").Plural())
}

func TestParseArtifactKindFromPlural(t *testing.T) {
	kind, ok := ParseArtifactKindFromPlural("solutions")
	assert.True(t, ok)
	assert.Equal(t, ArtifactKindSolution, kind)

	kind, ok = ParseArtifactKindFromPlural("providers")
	assert.True(t, ok)
	assert.Equal(t, ArtifactKindProvider, kind)

	kind, ok = ParseArtifactKindFromPlural("auth-handlers")
	assert.True(t, ok)
	assert.Equal(t, ArtifactKindAuthHandler, kind)

	_, ok = ParseArtifactKindFromPlural("invalid")
	assert.False(t, ok)
}

func TestAnnotationBuilder_SetTags(t *testing.T) {
	b := NewAnnotationBuilder()
	b.SetTags([]string{"alpha", "beta"})
	annotations := b.Build()
	assert.Equal(t, "alpha,beta", annotations[AnnotationTags])
}

func TestAnnotationBuilder_SetTags_Empty(t *testing.T) {
	b := NewAnnotationBuilder()
	b.SetTags(nil)
	annotations := b.Build()
	_, exists := annotations[AnnotationTags]
	assert.False(t, exists)
}

func TestGetTags(t *testing.T) {
	tags := GetTags(map[string]string{AnnotationTags: "x,y,z"})
	assert.Equal(t, []string{"x", "y", "z"}, tags)
}

func TestGetTags_Empty(t *testing.T) {
	tags := GetTags(map[string]string{})
	assert.Nil(t, tags)
}

func TestNormalizeRegistryHost(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"docker.io", "docker.io"},
		{"index.docker.io", "docker.io"},
		{"registry-1.docker.io", "docker.io"},
		{"ghcr.io", "ghcr.io"},
		{"my-registry.example.com", "my-registry.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, NormalizeRegistryHost(tt.input))
		})
	}
}

func TestRemoteReference_String(t *testing.T) {
	tests := []struct {
		name     string
		ref      *RemoteReference
		expected string
	}{
		{
			name:     "full reference",
			ref:      &RemoteReference{Registry: "ghcr.io", Repository: "my-org", Kind: ArtifactKindSolution, Name: "my-sol", Tag: "1.0.0"},
			expected: "ghcr.io/my-org/solutions/my-sol@1.0.0",
		},
		{
			name:     "no repository",
			ref:      &RemoteReference{Registry: "ghcr.io", Kind: ArtifactKindSolution, Name: "my-sol"},
			expected: "ghcr.io/solutions/my-sol",
		},
		{
			name:     "no tag",
			ref:      &RemoteReference{Registry: "ghcr.io", Repository: "my-org", Kind: ArtifactKindSolution, Name: "my-sol"},
			expected: "ghcr.io/my-org/solutions/my-sol",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.ref.String())
		})
	}
}
