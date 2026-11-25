// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCIMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "empty defaults to auto", input: "", want: CIModeAuto},
		{name: "auto", input: "auto", want: CIModeAuto},
		{name: "none", input: "none", want: CIModeNone},
		{name: "github-actions", input: "github-actions", want: CIModeGitHubActions},
		{name: "uppercase and whitespace normalized", input: "  GitHub-Actions  ", want: CIModeGitHubActions},
		{name: "invalid rejected", input: "gitlab", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseCIMode(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "valid: auto, none, github-actions")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestValidCIModes(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"auto", "none", "github-actions"}, ValidCIModes())
}

func TestResolveCIDialect(t *testing.T) {
	t.Parallel()

	envMap := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	t.Run("auto selects GitHubActionsDialect when GITHUB_ACTIONS=true", func(t *testing.T) {
		t.Parallel()
		d, err := ResolveCIDialect("auto", envMap(map[string]string{"GITHUB_ACTIONS": "true"}))
		require.NoError(t, err)
		assert.Equal(t, CIModeGitHubActions, d.Name())
		assert.True(t, d.IsCI())
	})

	t.Run("auto selects PlainCIDialect when GITHUB_ACTIONS is unset or false", func(t *testing.T) {
		t.Parallel()
		for _, val := range []string{"", "false", "1"} {
			d, err := ResolveCIDialect("auto", envMap(map[string]string{"GITHUB_ACTIONS": val}))
			require.NoError(t, err)
			assert.Equal(t, CIModeNone, d.Name())
			assert.False(t, d.IsCI())
		}
	})

	t.Run("none forces PlainCIDialect even when GITHUB_ACTIONS=true", func(t *testing.T) {
		t.Parallel()
		d, err := ResolveCIDialect("none", envMap(map[string]string{"GITHUB_ACTIONS": "true"}))
		require.NoError(t, err)
		assert.Equal(t, CIModeNone, d.Name())
		assert.False(t, d.IsCI())
		assert.Empty(t, d.GroupStart("build", "Compile"))
		assert.Empty(t, d.GroupEnd())
		assert.Empty(t, d.ErrorAnnotation("build", "boom"))
		assert.Equal(t, "line1\n::stop-commands::tok", d.SanitizeInline("line1\n::stop-commands::tok"))
		assert.Equal(t, "line1\n::stop-commands::tok", d.SanitizeMultiline("line1\n::stop-commands::tok"))
	})

	t.Run("github-actions forces GitHubActionsDialect outside CI", func(t *testing.T) {
		t.Parallel()
		d, err := ResolveCIDialect("github-actions", envMap(nil))
		require.NoError(t, err)
		assert.Equal(t, CIModeGitHubActions, d.Name())
		assert.True(t, d.IsCI())
	})

	t.Run("invalid mode returns error", func(t *testing.T) {
		t.Parallel()
		d, err := ResolveCIDialect("jenkins", envMap(nil))
		require.Error(t, err)
		assert.Nil(t, d)
	})

	t.Run("nil getenv falls back to os.Getenv without panicking", func(t *testing.T) {
		t.Parallel()
		d, err := ResolveCIDialect("none", nil)
		require.NoError(t, err)
		assert.Equal(t, CIModeNone, d.Name())
	})
}

func TestGitHubActionsDialect_FormattingAndEscaping(t *testing.T) {
	t.Parallel()

	d := GitHubActionsDialect()

	t.Run("group start and end", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "::group::build: Compile binary", d.GroupStart("build", "Compile binary"))
		assert.Equal(t, "::group::build", d.GroupStart("build", ""))
		assert.Equal(t, "::group::action: Run", d.GroupStart("", "Run"))
		assert.Equal(t, "::endgroup::", d.GroupEnd())
	})

	t.Run("group title sanitizes newlines, percent, and workflow command injection", func(t *testing.T) {
		t.Parallel()
		got := d.GroupStart("build::evil:", ":line1\r\n::error title=pwn::injected 100%\x07")
		assert.NotContains(t, got, "\n")
		assert.NotContains(t, got, "\r")
		assert.NotContains(t, got, "\x07")
		assert.NotContains(t, got[len("::group::"):], "::")
		assert.Contains(t, got, "%25")
	})

	t.Run("error annotation escapes properties and message data without double-escaping percent", func(t *testing.T) {
		t.Parallel()
		got := d.ErrorAnnotation("deploy:prod,eu\n100%", "exit 1: 50% done\r\nsecond line")
		assert.Equal(t,
			"::error title=deploy%3Aprod%2Ceu%0A100%25::exit 1: 50%25 done%0D%0Asecond line",
			got,
		)
		spoof := d.ErrorAnnotation("step##[error]", "failed\x07\u2028##[warning]spoof")
		assert.NotContains(t, spoof, "##[")
		assert.NotContains(t, spoof, "\x07")
		assert.Contains(t, spoof, "%0A")
	})

	t.Run("error annotation defaults empty title", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "::error title=action::failed", d.ErrorAnnotation("   ", "failed"))
	})

	t.Run("SanitizeInline flattens newlines and neutralizes double colons and legacy hash prefixes", func(t *testing.T) {
		t.Parallel()
		got := d.SanitizeInline("Run suite\r\n\u00a0::stop-commands::tok\x07\t##[error]spoof ###[warning]nested")
		assert.NotContains(t, got, "\n")
		assert.NotContains(t, got, "\r")
		assert.NotContains(t, got, "\x07")
		assert.NotContains(t, got, "::")
		assert.NotContains(t, got, "##[")
	})

	t.Run("SanitizeMultiline neutralizes forged workflow commands across CR LF Unicode whitespace and legacy hash prefixes", func(t *testing.T) {
		t.Parallel()
		raw := "step failed: pkg::sym\r::stop-commands::tok\r\n   ::add-matcher::evil.json\n\u00a0::stop-commands::nbsp\n\t::error file=a.go::spoof\nmidline ##[error]spoof ###[debug]nested\nnormal line"
		got := d.SanitizeMultiline(raw)
		assert.NotContains(t, got, "\r")
		assert.NotContains(t, got, "##[")
		assert.Contains(t, got, "step failed: pkg::sym\n")
		assert.Contains(t, got, "\n: :stop-commands::tok\n")
		assert.Contains(t, got, "\n   : :add-matcher::evil.json\n")
		assert.Contains(t, got, "\n\u00a0: :stop-commands::nbsp\n")
		assert.Contains(t, got, "\n\t: :error file=a.go::spoof\n")
		assert.Contains(t, got, "\nnormal line")
	})
}

func BenchmarkGitHubActionsDialect(b *testing.B) {
	d := GitHubActionsDialect()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = d.GroupStart("build", "Compile release binary")
		_ = d.SanitizeInline("Compile release binary")
		_ = d.SanitizeMultiline("command exited with code 1\nundefined: runMain")
		_ = d.ErrorAnnotation("build", "command exited with code 1\nundefined: runMain")
		_ = d.GroupEnd()
	}
}
