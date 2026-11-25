// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/staticprovider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func execFallbackTestRegistry(t *testing.T) *provider.Registry {
	t.Helper()
	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(staticprovider.New()))
	require.NoError(t, reg.Register(newFakeProvider("exec", nil)))
	require.NoError(t, reg.Register(newFakeProvider("env", nil)))
	require.NoError(t, reg.Register(newFakeProvider("shell-runner", nil)))
	return reg
}

func TestExecFailOnErrorSatisfied(t *testing.T) {
	t.Parallel()

	strPtr := func(s string) *string { return &s }
	exprPtr := func(s string) *celexp.Expression {
		e := celexp.Expression(s)
		return &e
	}
	tmplPtr := func(s string) *gotmpl.GoTemplatingContent {
		tc := gotmpl.GoTemplatingContent(s)
		return &tc
	}

	tests := []struct {
		name string
		vr   *spec.ValueRef
		want bool
	}{
		{name: "nil ValueRef", vr: nil, want: false},
		{name: "empty ValueRef", vr: &spec.ValueRef{}, want: false},
		{name: "literal true", vr: &spec.ValueRef{Literal: true}, want: true},
		{name: "literal false", vr: &spec.ValueRef{Literal: false}, want: false},
		{name: "literal string true", vr: &spec.ValueRef{Literal: "true"}, want: false},
		{name: "literal int 1", vr: &spec.ValueRef{Literal: 1}, want: false},
		{name: "expr true", vr: &spec.ValueRef{Expr: exprPtr("true")}, want: true},
		{name: "expr dynamic", vr: &spec.ValueRef{Expr: exprPtr("_.strict")}, want: true},
		{name: "expr false", vr: &spec.ValueRef{Expr: exprPtr("false")}, want: false},
		{name: "expr false with whitespace", vr: &spec.ValueRef{Expr: exprPtr("  false \n")}, want: false},
		{name: "resolver ref", vr: &spec.ValueRef{Resolver: strPtr("strictFlag")}, want: true},
		{name: "template ref", vr: &spec.ValueRef{Tmpl: tmplPtr("{{ .strict }}")}, want: true},
		{name: "secret ref", vr: &spec.ValueRef{Secret: strPtr("flag-secret")}, want: true},
		{name: "token ref", vr: &spec.ValueRef{Token: &spec.TokenRef{Handler: "github"}}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, execFailOnErrorSatisfied(tt.vr))
		})
	}
}

func TestLintExecFallbackSources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		yaml          string
		wantLocations []string
	}{
		{
			name: "non-final exec source without failOnError is flagged",
			yaml: `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: exec-fallback
spec:
  resolvers:
    token:
      description: Fetch token from CLI with env fallback
      resolve:
        with:
          - provider: exec
            inputs:
              command: some-cli create token
              raw: true
          - provider: env
            inputs:
              operation: get
              name: FEDERATED_TOKEN
`,
			wantLocations: []string{"resolvers.token.resolve.with[0]"},
		},
		{
			name: "non-final exec source with failOnError true is clean",
			yaml: `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: exec-fallback-clean
spec:
  resolvers:
    token:
      description: Fetch token from CLI with env fallback
      resolve:
        with:
          - provider: exec
            inputs:
              command: some-cli create token
              raw: true
              failOnError: true
          - provider: env
            inputs:
              operation: get
              name: FEDERATED_TOKEN
`,
			wantLocations: nil,
		},
		{
			name: "non-final exec source with explicit failOnError false is flagged",
			yaml: `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: exec-fallback-explicit-false
spec:
  resolvers:
    token:
      description: Fetch token
      resolve:
        with:
          - provider: exec
            inputs:
              command: some-cli create token
              failOnError: false
          - provider: static
            inputs:
              value: fallback
`,
			wantLocations: []string{"resolvers.token.resolve.with[0]"},
		},
		{
			name: "sole exec source without failOnError is not flagged",
			yaml: `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: exec-sole
spec:
  resolvers:
    branch:
      description: Single exec source
      resolve:
        with:
          - provider: exec
            inputs:
              command: git rev-parse --abbrev-ref HEAD
`,
			wantLocations: nil,
		},
		{
			name: "final exec fallback source without failOnError is not flagged",
			yaml: `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: exec-final
spec:
  resolvers:
    clusterID:
      description: Parameter with exec fallback
      resolve:
        with:
          - provider: env
            inputs:
              operation: get
              name: CLUSTER_ID
          - provider: exec
            inputs:
              command: uuidgen
`,
			wantLocations: nil,
		},
		{
			name: "multi-step chain flags only non-final exec steps missing failOnError",
			yaml: `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: exec-multi-chain
spec:
  resolvers:
    token:
      description: Multi-step exec chain
      resolve:
        with:
          - provider: exec
            inputs:
              command: cli-one token
          - provider: exec
            inputs:
              command: cli-two token
              failOnError: true
          - provider: exec
            inputs:
              command: cli-three token
          - provider: exec
            inputs:
              command: cli-final token
`,
			wantLocations: []string{
				"resolvers.token.resolve.with[0]",
				"resolvers.token.resolve.with[2]",
			},
		},
		{
			name: "transform.with and workflow.actions exec steps are never flagged",
			yaml: `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: exec-transform-and-actions
spec:
  resolvers:
    data:
      description: Sequential transform pipeline
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
      transform:
        with:
          - provider: exec
            inputs:
              command: tr a-z A-Z
          - provider: exec
            inputs:
              command: tr -d '\n'
  workflow:
    actions:
      stepOne:
        description: First action
        provider: exec
        inputs:
          command: echo one
      stepTwo:
        description: Second action
        dependsOn: [stepOne]
        provider: exec
        inputs:
          command: echo two
    finally:
      cleanup:
        description: Cleanup
        provider: exec
        inputs:
          command: echo done
`,
			wantLocations: nil,
		},
		{
			name: "forEach on non-final resolve.with exec step is flagged",
			yaml: `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: exec-foreach
spec:
  resolvers:
    hosts:
      description: Host list
      resolve:
        with:
          - provider: static
            inputs:
              value: [a, b]
    probed:
      description: Probe each host with fallback
      resolve:
        with:
          - provider: exec
            forEach:
              item: host
              in:
                rslvr: hosts
            inputs:
              command: ping -c 1
          - provider: static
            inputs:
              value: []
`,
			wantLocations: []string{"resolvers.probed.resolve.with[0]"},
		},
		{
			name: "bundle.plugins defaults failOnError true satisfies inline omission",
			yaml: `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: exec-plugin-defaults-true
bundle:
  plugins:
    - name: exec
      kind: provider
      version: latest
      defaults:
        failOnError: true
spec:
  resolvers:
    token:
      description: Uses plugin default failOnError
      resolve:
        with:
          - provider: exec
            inputs:
              command: some-cli token
          - provider: static
            inputs:
              value: fallback
`,
			wantLocations: nil,
		},
		{
			name: "inline failOnError false overrides bundle.plugins defaults failOnError true",
			yaml: `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: exec-plugin-defaults-overridden
bundle:
  plugins:
    - name: exec
      kind: provider
      version: latest
      defaults:
        failOnError: true
spec:
  resolvers:
    token:
      description: Overrides plugin default with false
      resolve:
        with:
          - provider: exec
            inputs:
              command: some-cli token
              failOnError: false
          - provider: static
            inputs:
              value: fallback
`,
			wantLocations: []string{"resolvers.token.resolve.with[0]"},
		},
		{
			name: "exec plugin published under custom OCI artifact name is still flagged",
			yaml: `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: exec-custom-artifact-flagged
bundle:
  plugins:
    - name: exec
      kind: provider
      version: ^2.0.0
      source:
        registry: ghcr.io/example/providers
        artifact: custom-exec-provider
spec:
  resolvers:
    token:
      description: Uses exec provider fetched from custom OCI artifact name
      resolve:
        with:
          - provider: exec
            inputs:
              command: some-cli token
          - provider: static
            inputs:
              value: fallback
`,
			wantLocations: []string{"resolvers.token.resolve.with[0]"},
		},
		{
			name: "exec plugin published under custom OCI artifact name honors bundle.plugins defaults",
			yaml: `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: exec-custom-artifact-defaults
bundle:
  plugins:
    - name: exec
      kind: provider
      version: ^2.0.0
      source:
        registry: ghcr.io/example/providers
        artifact: custom-exec-provider
      defaults:
        failOnError: true
spec:
  resolvers:
    token:
      description: Uses exec provider with defaults on custom OCI artifact
      resolve:
        with:
          - provider: exec
            inputs:
              command: some-cli token
          - provider: static
            inputs:
              value: fallback
`,
			wantLocations: nil,
		},
		{
			name: "non-exec runtime provider whose OCI artifact happens to be named exec is not flagged",
			yaml: `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: non-exec-provider-with-exec-artifact
bundle:
  plugins:
    - name: shell-runner
      kind: provider
      version: ^1.0.0
      source:
        registry: ghcr.io/example/providers
        artifact: exec
spec:
  resolvers:
    token:
      description: Uses non-exec runtime provider shell-runner
      resolve:
        with:
          - provider: shell-runner
            inputs:
              command: some-cli token
          - provider: static
            inputs:
              value: fallback
`,
			wantLocations: nil,
		},
		{
			name: "auth-handler plugin named exec does not supply defaults to exec provider",
			yaml: `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: exec-auth-handler-same-name
bundle:
  plugins:
    - name: exec
      kind: auth-handler
      version: ^1.0.0
      source:
        registry: ghcr.io/example/auth
        artifact: custom-auth
      defaults:
        failOnError: true
spec:
  resolvers:
    token:
      description: Auth-handler with local name exec must not supply provider defaults
      resolve:
        with:
          - provider: exec
            inputs:
              command: some-cli token
          - provider: static
            inputs:
              value: fallback
`,
			wantLocations: []string{"resolvers.token.resolve.with[0]"},
		},
		{
			name: "spec.calls invoking exec without failOnError is flagged even when bundle.plugins sets default",
			yaml: `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: exec-call-fallback
bundle:
  plugins:
    - name: exec
      kind: provider
      version: latest
      defaults:
        failOnError: true
spec:
  calls:
    mintToken:
      provider: exec
      inputs:
        command: some-cli token
  resolvers:
    token:
      description: Uses call in fallback chain
      resolve:
        with:
          - call: mintToken
          - provider: static
            inputs:
              value: fallback
`,
			wantLocations: []string{"resolvers.token.resolve.with[0]"},
		},
		{
			name: "spec.calls invoking exec with failOnError true in call inputs is clean",
			yaml: `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: exec-call-fallback-clean
spec:
  calls:
    mintToken:
      provider: exec
      inputs:
        command: some-cli token
        failOnError: true
  resolvers:
    token:
      description: Uses call with failOnError true
      resolve:
        with:
          - call: mintToken
          - provider: static
            inputs:
              value: fallback
`,
			wantLocations: nil,
		},
		{
			name: "deterministic ordering across multiple resolvers",
			yaml: `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: exec-deterministic-order
spec:
  resolvers:
    zeta:
      description: Second alphabetically
      resolve:
        with:
          - provider: exec
            inputs:
              command: cmd-z
          - provider: static
            inputs:
              value: z
    alpha:
      description: First alphabetically
      resolve:
        with:
          - provider: exec
            inputs:
              command: cmd-a
          - provider: static
            inputs:
              value: a
`,
			wantLocations: []string{
				"resolvers.alpha.resolve.with[0]",
				"resolvers.zeta.resolve.with[0]",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reg := execFallbackTestRegistry(t)

			sol, err := LoadComposedFromBytes([]byte(tt.yaml), "solution.yaml")
			require.NoError(t, err)

			res := Solution(sol, "solution.yaml", reg)
			findings := filterFindingsByRule(res, ruleExecSourceWithoutFailOnError)

			var gotLocations []string
			for _, f := range findings {
				assert.Equal(t, SeverityWarning, f.Severity)
				assert.Equal(t, "provider", f.Category)
				assert.NotContains(t, f.Message, "scafctl", "message must not hardcode binary name")
				assert.NotContains(t, f.Suggestion, "scafctl", "suggestion must not hardcode binary name")
				assert.Positive(t, f.Line, "finding should be enriched with source line")
				gotLocations = append(gotLocations, f.Location)
			}
			assert.Equal(t, tt.wantLocations, gotLocations)
		})
	}
}

func TestLintExecFallbackSources_SuppressionAndCompose(t *testing.T) {
	t.Parallel()
	reg := execFallbackTestRegistry(t)

	t.Run("inline scafctl-lint-ignore suppresses finding cleanly", func(t *testing.T) {
		t.Parallel()
		raw := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: exec-suppressed
spec:
  resolvers:
    token:
      description: Deliberately suppressed exec step
      resolve:
        with:
          # scafctl-lint-ignore: exec-source-without-fail-on-error
          - provider: exec
            inputs:
              command: some-cli token
          - provider: static
            inputs:
              value: fallback
`
		sol, err := LoadComposedFromBytes([]byte(raw), "solution.yaml")
		require.NoError(t, err)

		res := Solution(sol, "solution.yaml", reg)
		assert.Empty(t, filterFindingsByRule(res, ruleExecSourceWithoutFailOnError))
		assert.Empty(t, filterFindingsByRule(res, "unused-suppression"))
	})

	t.Run("composed partial attributes source file and respects partial suppression", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		primaryPath := filepath.Join(tmpDir, "solution.yaml")
		partialPath := filepath.Join(tmpDir, "resolvers.yaml")

		primaryYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: composed-exec-fallback
compose:
  - resolvers.yaml
spec: {}
`
		partialYAML := `spec:
  resolvers:
    unsuppressed:
      description: Flagged in partial
      resolve:
        with:
          - provider: exec
            inputs:
              command: some-cli token
          - provider: static
            inputs:
              value: fallback
    suppressed:
      description: Suppressed in partial
      resolve:
        with:
          # scafctl-lint-ignore: exec-source-without-fail-on-error
          - provider: exec
            inputs:
              command: some-cli other
          - provider: static
            inputs:
              value: fallback
`
		require.NoError(t, os.WriteFile(primaryPath, []byte(primaryYAML), 0o600))
		require.NoError(t, os.WriteFile(partialPath, []byte(partialYAML), 0o600))

		sol, err := LoadComposedFromBytes([]byte(primaryYAML), primaryPath)
		require.NoError(t, err)

		res := Solution(sol, primaryPath, reg)
		findings := filterFindingsByRule(res, ruleExecSourceWithoutFailOnError)
		require.Len(t, findings, 1)
		assert.Equal(t, "resolvers.unsuppressed.resolve.with[0]", findings[0].Location)
		assert.Equal(t, 7, findings[0].Line)
		assert.True(t, sameSourceFile(partialPath, findings[0].SourceFile), "expected SourceFile %q to match %q", findings[0].SourceFile, partialPath)
		assert.Empty(t, filterFindingsByRule(res, "unused-suppression"))
	})
}

func TestLintExecFallbackSources_NilSafety(t *testing.T) {
	t.Parallel()

	result := &Result{}
	lintExecFallbackSources(nil, result)
	assert.Empty(t, result.Findings)

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"nilRes":     nil,
				"nilResolve": {},
			},
		},
	}
	lintExecFallbackSources(sol, result)
	assert.Empty(t, result.Findings)

	// UnmarshalFromBytes without ApplyDefaults (e.g. LSP document sync) leaves
	// omitted plugin Kind empty; defaults and aliases must still be honored.
	unnormalized := &solution.Solution{
		Bundle: solution.Bundle{
			Plugins: []solution.PluginDependency{
				{
					Name: "exec",
					Defaults: map[string]*spec.ValueRef{
						"failOnError": {Literal: true},
					},
				},
			},
		},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"token": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{Provider: "exec", Inputs: map[string]*spec.ValueRef{"command": {Literal: "cli token"}}},
							{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: "fallback"}}},
						},
					},
				},
			},
		},
	}
	lintExecFallbackSources(unnormalized, result)
	assert.Empty(t, result.Findings)
}

func BenchmarkLintExecFallbackSources(b *testing.B) {
	sol := &solution.Solution{
		Bundle: solution.Bundle{
			Plugins: []solution.PluginDependency{
				{Name: "exec", Kind: solution.PluginKindProvider, Version: "latest"},
			},
		},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"gitBranch": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "exec",
								Inputs: map[string]*spec.ValueRef{
									"command": {Literal: "git rev-parse --abbrev-ref HEAD"},
								},
							},
							{
								Provider: "static",
								Inputs: map[string]*spec.ValueRef{
									"value": {Literal: "main"},
								},
							},
						},
					},
				},
				"gitCommit": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "exec",
								Inputs: map[string]*spec.ValueRef{
									"command":     {Literal: "git rev-parse --short HEAD"},
									"failOnError": {Literal: true},
								},
							},
							{
								Provider: "static",
								Inputs: map[string]*spec.ValueRef{
									"value": {Literal: "0000000"},
								},
							},
						},
					},
				},
			},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		res := &Result{Findings: make([]*Finding, 0, 1)}
		lintExecFallbackSources(sol, res)
	}
}
