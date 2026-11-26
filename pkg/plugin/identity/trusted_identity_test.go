// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package identity

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHasPlaceholders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		want    bool
	}{
		{name: "empty", pattern: "", want: false},
		{name: "static wildcard", pattern: "https://github.com/oakwood-commons/*", want: false},
		{name: "name placeholder", pattern: "https://github.com/my-org/sln-{name}/.github/workflows/*", want: true},
		{name: "kind placeholder", pattern: "https://github.com/my-org/{kind}/.github/workflows/*", want: true},
		{name: "both placeholders", pattern: "https://github.com/my-org/{kind}-{name}/.github/workflows/*", want: true},
		{name: "other token not treated as supported placeholder", pattern: "https://github.com/my-org/{repo}/*", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, HasPlaceholders(tt.pattern))
		})
	}
}

func TestValidateTrustedIdentityPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		pattern     string
		wantErrText string
	}{
		{
			name:    "static org wildcard",
			pattern: "https://github.com/oakwood-commons/*",
		},
		{
			name:    "reusable workflow URI",
			pattern: "https://github.com/oakwood-commons/scafctl/.github/workflows/sign-plugin.yml@refs/tags/sign-plugin/v*",
		},
		{
			name:    "name placeholder",
			pattern: "https://github.com/my-org/sln-{name}/.github/workflows/*",
		},
		{
			name:    "kind and name placeholders",
			pattern: "https://github.com/my-org/scafctl-{kind}-{name}/.github/workflows/release.yml@refs/tags/v*",
		},
		{
			name:    "repeated name placeholder",
			pattern: "https://github.com/my-org/{name}/.github/workflows/{name}.yml@*",
		},
		{
			name:        "empty string",
			pattern:     "",
			wantErrText: "must not be empty",
		},
		{
			name:        "whitespace only",
			pattern:     "   ",
			wantErrText: "must not contain whitespace",
		},
		{
			name:        "leading whitespace",
			pattern:     " https://github.com/my-org/*",
			wantErrText: "must not contain whitespace",
		},
		{
			name:        "trailing whitespace",
			pattern:     "https://github.com/my-org/* ",
			wantErrText: "must not contain whitespace",
		},
		{
			name:        "internal space",
			pattern:     "https://github.com/my org/*",
			wantErrText: "must not contain whitespace",
		},
		{
			name:        "control character newline",
			pattern:     "https://github.com/my-org/*\n",
			wantErrText: "must not contain control characters",
		},
		{
			name:        "control character nul",
			pattern:     "https://github.com/my-org/\x00*",
			wantErrText: "must not contain control characters",
		},
		{
			name:        "control character del",
			pattern:     "https://github.com/my-org/\x7f*",
			wantErrText: "must not contain control characters",
		},
		{
			name:        "invalid utf8",
			pattern:     string([]byte{'h', 't', 't', 'p', 's', ':', '/', '/', 0xff}),
			wantErrText: "invalid UTF-8",
		},
		{
			name:        "unclosed open brace",
			pattern:     "https://github.com/my-org/{name/*",
			wantErrText: "unclosed '{'",
		},
		{
			name:        "trailing open brace",
			pattern:     "https://github.com/my-org/{",
			wantErrText: "unclosed '{'",
		},
		{
			name:        "unmatched close brace",
			pattern:     "https://github.com/my-org/name}/*",
			wantErrText: "unmatched '}'",
		},
		{
			name:        "trailing unmatched close brace after valid placeholder",
			pattern:     "https://github.com/my-org/{name}}/*",
			wantErrText: "unmatched '}'",
		},
		{
			name:        "empty braces",
			pattern:     "https://github.com/my-org/{}/*",
			wantErrText: `empty placeholder "{}"`,
		},
		{
			name:        "nested braces around name",
			pattern:     "https://github.com/my-org/{{name}}/*",
			wantErrText: "nested or unbalanced '{'",
		},
		{
			name:        "nested brace inside token",
			pattern:     "https://github.com/my-org/{foo{name}}/*",
			wantErrText: "nested or unbalanced '{'",
		},
		{
			name:        "unknown placeholder repo",
			pattern:     "https://github.com/my-org/{repo}/*",
			wantErrText: "unknown placeholder {repo}",
		},
		{
			name:        "case sensitive placeholder Name",
			pattern:     "https://github.com/my-org/{Name}/*",
			wantErrText: "unknown placeholder {Name}",
		},
		{
			name:        "case sensitive placeholder KIND",
			pattern:     "https://github.com/my-org/{KIND}/*",
			wantErrText: "unknown placeholder {KIND}",
		},
		{
			name:        "regex quantifier braces rejected",
			pattern:     "https://github.com/my-org/v{1,3}/*",
			wantErrText: "unknown placeholder {1,3}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateTrustedIdentityPattern(tt.pattern)
			if tt.wantErrText == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrInvalidIdentityPattern)
			assert.Contains(t, err.Error(), tt.wantErrText)
		})
	}
}

func TestExpandTrustedIdentityPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		pattern     string
		artName     string
		artKind     string
		want        string
		wantErrText string
	}{
		{
			name:    "static pattern unchanged even with empty name and kind",
			pattern: "https://github.com/oakwood-commons/*",
			want:    "https://github.com/oakwood-commons/*",
		},
		{
			name:    "expands name",
			pattern: "https://github.com/my-org/sln-{name}/.github/workflows/*",
			artName: "agentic-code",
			artKind: "solution",
			want:    "https://github.com/my-org/sln-agentic-code/.github/workflows/*",
		},
		{
			name:    "expands kind and name",
			pattern: "https://github.com/my-org/{kind}/{name}/.github/workflows/*",
			artName: "entra",
			artKind: "auth-handler",
			want:    "https://github.com/my-org/auth-handler/entra/.github/workflows/*",
		},
		{
			name:    "expands repeated placeholders",
			pattern: "{kind}:{name}/{name}",
			artName: "github",
			artKind: "provider",
			want:    "provider:github/github",
		},
		{
			name:    "single pass does not re-expand placeholder tokens inside values",
			pattern: "https://github.com/my-org/{name}/{kind}/*",
			artName: "{kind}",
			artKind: "{name}",
			want:    "https://github.com/my-org/{kind}/{name}/*",
		},
		{
			name:        "missing name when {name} referenced",
			pattern:     "https://github.com/my-org/{name}/*",
			artName:     "",
			artKind:     "provider",
			wantErrText: "requires a non-empty artifact name",
		},
		{
			name:        "missing kind when {kind} referenced",
			pattern:     "https://github.com/my-org/{kind}/*",
			artName:     "github",
			artKind:     "   ",
			wantErrText: "requires a non-empty artifact kind",
		},
		{
			name:        "malformed pattern rejected during expand",
			pattern:     "https://github.com/my-org/{repo}/*",
			artName:     "github",
			artKind:     "provider",
			wantErrText: "unknown placeholder {repo}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ExpandTrustedIdentityPattern(tt.pattern, tt.artName, tt.artKind)
			if tt.wantErrText != "" {
				require.ErrorIs(t, err, ErrInvalidIdentityPattern)
				assert.Contains(t, err.Error(), tt.wantErrText)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func BenchmarkValidateTrustedIdentityPattern(b *testing.B) {
	const pat = "https://github.com/my-org/scafctl-{kind}-{name}/.github/workflows/*"
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := ValidateTrustedIdentityPattern(pat); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExpandTrustedIdentityPattern_Static(b *testing.B) {
	const pat = "https://github.com/oakwood-commons/*"
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := ExpandTrustedIdentityPattern(pat, "github", "provider"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExpandTrustedIdentityPattern_Placeholders(b *testing.B) {
	const pat = "https://github.com/my-org/scafctl-{kind}-{name}/.github/workflows/*"
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := ExpandTrustedIdentityPattern(pat, "github", "provider"); err != nil {
			b.Fatal(err)
		}
	}
}
