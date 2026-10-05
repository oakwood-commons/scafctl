package catalog

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCliBinaryName(t *testing.T) {
	t.Parallel()

	custom := settings.NewCliParams()
	custom.BinaryName = "mycli"

	tests := []struct {
		name   string
		params *settings.Run
		want   string
	}{
		{name: "nil params fall back to the default", params: nil, want: settings.CliBinaryName},
		{name: "empty binary name falls back to the default", params: &settings.Run{}, want: settings.CliBinaryName},
		{name: "embedder binary name is used", params: custom, want: "mycli"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, cliBinaryName(tt.params))
		})
	}
}

func TestOriginHint(t *testing.T) {
	t.Parallel()

	ref := catalog.Reference{
		Kind:    catalog.ArtifactKindSolution,
		Name:    "my-sol",
		Version: semver.MustParse("1.0.0"),
	}
	ambiguous := &catalog.AmbiguousArtifactError{
		Matches: []catalog.ArtifactInfo{
			{Reference: ref},
			{Reference: ref, Annotations: map[string]string{catalog.AnnotationSourceCanonical: "ghcr.io/org/a"}},
			{Reference: ref, Annotations: map[string]string{catalog.AnnotationSourceCanonical: "ghcr.io/org/a"}},
			{Reference: ref, Annotations: map[string]string{catalog.AnnotationSourceCanonical: "ghcr.io/org/b c;rm"}},
		},
	}

	assert.Equal(t,
		"re-run the same command with one of: --origin built, --origin ghcr.io/org/a, --origin 'ghcr.io/org/b c;rm'",
		originHint(ambiguous),
		"one entry per distinct origin, unsafe values shell-quoted")
}

func TestQuoteIfNeeded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{in: "built", want: "built"},
		{in: "localhost:5000/org/solutions", want: "localhost:5000/org/solutions"},
		{in: "", want: "''"},
		{in: "a b", want: "'a b'"},
		{in: "it's", want: `'it'\''s'`},
		{in: "$(id)", want: "'$(id)'"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, quoteIfNeeded(tt.in), "quoteIfNeeded(%q)", tt.in)
	}
}

func TestValidateVersionConstraint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		nameOrRef  string
		constraint string
		wantErr    string
	}{
		{
			name:       "empty constraint is valid",
			nameOrRef:  "my-app@1.0.0",
			constraint: "",
		},
		{
			name:       "constraint without @version is valid",
			nameOrRef:  "my-app",
			constraint: "^1.0.0",
		},
		{
			name:       "constraint with @version is error",
			nameOrRef:  "my-app@1.0.0",
			constraint: "^1.0.0",
			wantErr:    "cannot use --version with an explicit version",
		},
		{
			name:       "invalid constraint syntax",
			nameOrRef:  "my-app",
			constraint: "not-semver!!",
			wantErr:    "invalid version constraint",
		},
		{
			name:       "valid tilde constraint",
			nameOrRef:  "my-app",
			constraint: "~1.2.0",
		},
		{
			name:       "valid range constraint",
			nameOrRef:  "my-app",
			constraint: ">= 1.0.0, < 2.0.0",
		},
		{
			name:       "wildcard constraint",
			nameOrRef:  "my-app",
			constraint: "*",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateVersionConstraint(tc.nameOrRef, tc.constraint)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestLooksLikeRemoteReference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ref      string
		expected bool
	}{
		{
			name:     "simple name is local",
			ref:      "my-solution@1.0.0",
			expected: false,
		},
		{
			name:     "localhost prefix is remote",
			ref:      "localhost:5000/solutions/my-solution@1.0.0",
			expected: true,
		},
		{
			name:     "host with dot is remote",
			ref:      "ghcr.io/myorg/scafctl/solutions/my-solution@1.0.0",
			expected: true,
		},
		{
			name:     "host with port is remote",
			ref:      "registry:5000/solutions/my-solution@1.0.0",
			expected: true,
		},
		{
			name:     "oci scheme prefix is remote",
			ref:      "oci://ghcr.io/myorg/solutions/my-solution@1.0.0",
			expected: true,
		},
		{
			name:     "docker registry is remote",
			ref:      "docker.io/myorg/my-solution@1.0.0",
			expected: true,
		},
		{
			name:     "no slash is local",
			ref:      "my-solution",
			expected: false,
		},
		{
			name:     "path without dot host is local",
			ref:      "myorg/my-solution",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := looksLikeRemoteReference(tt.ref)
			assert.Equal(t, tt.expected, result, "looksLikeRemoteReference(%q)", tt.ref)
		})
	}
}
