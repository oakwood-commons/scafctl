// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/prepare"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPluginFetcher_WithDefaultContext(t *testing.T) {
	// With a bare context (no config), BuildCatalogChain should still
	// succeed using the local catalog as a fallback.
	ctx := context.Background()
	fetcher, err := buildPluginFetcher(ctx)
	require.NoError(t, err)
	assert.NotNil(t, fetcher)
}

func TestSolutionMetaFromSolution_Source(t *testing.T) {
	t.Run("uses solution path as provenance source", func(t *testing.T) {
		sol := &solution.Solution{}
		sol.Metadata.Name = "demo"
		sol.SetPath("/path/to/solution.yaml")
		sol.Metadata.Source = "github.com/acme/solutions//demo"

		meta := solutionMetaFromSolution(sol)
		require.NotNil(t, meta)
		assert.Equal(t, "demo", meta.Name)
		assert.Equal(t, "/path/to/solution.yaml", meta.Source)
	})

	t.Run("falls back to metadata source when no path", func(t *testing.T) {
		sol := &solution.Solution{}
		sol.Metadata.Source = "github.com/acme/solutions//demo"

		meta := solutionMetaFromSolution(sol)
		require.NotNil(t, meta)
		assert.Equal(t, "github.com/acme/solutions//demo", meta.Source)
	})
}

func TestBuildPluginFetcher_WithConfig(t *testing.T) {
	cfg := &config.Config{}
	ctx := config.WithConfig(context.Background(), cfg)

	fetcher, err := buildPluginFetcher(ctx)
	require.NoError(t, err)
	assert.NotNil(t, fetcher)
}

func TestAutoResolveProviderByName_NilRegistry(t *testing.T) {
	// With no official registry in context, returns error.
	ctx := context.Background()
	reg := provider.NewRegistry()

	_, err := autoResolveProviderByName(ctx, "exec", reg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "official registry not available")
}

func TestAutoResolveProviderByName_UnknownProvider(t *testing.T) {
	officialReg := official.NewRegistry()
	ctx := official.WithRegistry(context.Background(), officialReg)
	reg := provider.NewRegistry()

	_, err := autoResolveProviderByName(ctx, "nonexistent-provider", reg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not an official provider")
}

func TestAutoResolveProviderByName_FetcherFails(t *testing.T) {
	// When the provider is known but no catalog or cache has it available,
	// the function should return a wrapped error.
	officialReg := official.NewRegistryFrom([]official.Provider{
		{Name: "nonexistent-fake", CatalogRef: "nonexistent-fake", DefaultVersion: "latest"},
	})
	ctx := official.WithRegistry(context.Background(), officialReg)
	ctx = config.WithConfig(ctx, &config.Config{})
	reg := provider.NewRegistry()

	// "nonexistent-fake" is in our custom registry but no catalog or cache has it.
	_, err := autoResolveProviderByName(ctx, "nonexistent-fake", reg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "fetching provider")
}

func TestExtractParameterKeys(t *testing.T) {
	strRef := func(s string) *resolver.ValueRef { return &resolver.ValueRef{Literal: s} }
	listRef := func(items ...any) *resolver.ValueRef { return &resolver.ValueRef{Literal: items} }

	tests := []struct {
		name      string
		resolvers []*resolver.Resolver
		want      []string
	}{
		{
			name: "single key input",
			resolvers: []*resolver.Resolver{{
				Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{
					{Provider: "parameter", Inputs: map[string]*resolver.ValueRef{"key": strRef("name")}},
				}},
			}},
			want: []string{"name"},
		},
		{
			name: "keys alias list",
			resolvers: []*resolver.Resolver{{
				Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{
					{Provider: "parameter", Inputs: map[string]*resolver.ValueRef{"keys": listRef("environment", "e", "env")}},
				}},
			}},
			want: []string{"environment", "e", "env"},
		},
		{
			name: "key and keys combined with dedup",
			resolvers: []*resolver.Resolver{{
				Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{
					{Provider: "parameter", Inputs: map[string]*resolver.ValueRef{
						"key":  strRef("environment"),
						"keys": listRef("environment", "e"),
					}},
				}},
			}},
			want: []string{"environment", "e"},
		},
		{
			name: "non-parameter providers ignored",
			resolvers: []*resolver.Resolver{{
				Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{
					{Provider: "static", Inputs: map[string]*resolver.ValueRef{"key": strRef("ignored")}},
				}},
			}},
			want: nil,
		},
		{
			name: "non-string keys entries skipped",
			resolvers: []*resolver.Resolver{{
				Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{
					{Provider: "parameter", Inputs: map[string]*resolver.ValueRef{"keys": listRef("env", 42, "region")}},
				}},
			}},
			want: []string{"env", "region"},
		},
		{
			name: "keys as map contributes its distinct keys",
			resolvers: []*resolver.Resolver{{
				Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{
					{Provider: "parameter", Inputs: map[string]*resolver.ValueRef{
						"keys": listRef("keyA", "keyB"),
						"as":   strRef("map"),
					}},
				}},
			}},
			want: []string{"keyA", "keyB"},
		},
		{
			name: "keys as []string literal",
			resolvers: []*resolver.Resolver{{
				Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{
					{Provider: "parameter", Inputs: map[string]*resolver.ValueRef{
						"keys": {Literal: []string{"keyA", "keyB"}},
					}},
				}},
			}},
			want: []string{"keyA", "keyB"},
		},
		{
			name: "all mode contributes no named keys",
			resolvers: []*resolver.Resolver{{
				Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{
					{Provider: "parameter", Inputs: map[string]*resolver.ValueRef{"all": {Literal: true}}},
				}},
			}},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractParameterKeys(tt.resolvers)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolversAcceptAllParameters(t *testing.T) {
	tests := []struct {
		name      string
		resolvers []*resolver.Resolver
		want      bool
	}{
		{
			name: "all true",
			resolvers: []*resolver.Resolver{{
				Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{
					{Provider: "parameter", Inputs: map[string]*resolver.ValueRef{"all": {Literal: true}}},
				}},
			}},
			want: true,
		},
		{
			name: "all false is inert",
			resolvers: []*resolver.Resolver{{
				Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{
					{Provider: "parameter", Inputs: map[string]*resolver.ValueRef{"all": {Literal: false}}},
				}},
			}},
			want: false,
		},
		{
			name: "named-key resolver only",
			resolvers: []*resolver.Resolver{{
				Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{
					{Provider: "parameter", Inputs: map[string]*resolver.ValueRef{"key": {Literal: "env"}}},
				}},
			}},
			want: false,
		},
		{
			name: "all on non-parameter provider ignored",
			resolvers: []*resolver.Resolver{{
				Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{
					{Provider: "static", Inputs: map[string]*resolver.ValueRef{"all": {Literal: true}}},
				}},
			}},
			want: false,
		},
		{
			name: "non-literal all via resolver ref treated as could-be-true",
			resolvers: []*resolver.Resolver{{
				Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{
					{Provider: "parameter", Inputs: map[string]*resolver.ValueRef{"all": {Resolver: refString("useAll")}}},
				}},
			}},
			want: true,
		},
		{
			name: "non-literal all via expression treated as could-be-true",
			resolvers: []*resolver.Resolver{{
				Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{
					{Provider: "parameter", Inputs: map[string]*resolver.ValueRef{"all": {Expr: refExpr("true")}}},
				}},
			}},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, resolversAcceptAllParameters(tt.resolvers))
		})
	}
}

// refString returns a pointer to s for building ValueRef.Resolver in tests.
func refString(s string) *string { return &s }

// refExpr returns a pointer to a celexp.Expression for building ValueRef.Expr
// in tests.
func refExpr(s string) *celexp.Expression {
	e := celexp.Expression(s)
	return &e
}

func TestBuildParamFlagHint(t *testing.T) {
	tests := []struct {
		name   string
		params []string
		want   string
	}{
		{
			name:   "single param",
			params: []string{"env"},
			want:   "-r env=<value>",
		},
		{
			name:   "multiple params",
			params: []string{"env", "region"},
			want:   "-r env=<value> -r region=<value>",
		},
		{
			name:   "empty params",
			params: []string{},
			want:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildParamFlagHint(tt.params)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHandleStateLoadError_MissingParams(t *testing.T) {
	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(context.Background(), w)
	opts := &sharedResolverOptions{}

	missingErr := &state.MissingParamsError{
		Missing:  []string{"env", "region"},
		Original: errors.New("CEL eval failed"),
	}

	err := opts.handleStateLoadError(ctx, missingErr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required parameters [env, region]")
	assert.Contains(t, err.Error(), "-r env=<value>")
	assert.Contains(t, err.Error(), "-r region=<value>")
	assert.Equal(t, exitcode.GeneralError, exitcode.GetCode(err))
}

func TestHandleStateLoadError_GenericError(t *testing.T) {
	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(context.Background(), w)
	opts := &sharedResolverOptions{}

	genericErr := errors.New("disk full")

	err := opts.handleStateLoadError(ctx, genericErr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "state load: disk full")
	assert.Equal(t, exitcode.GeneralError, exitcode.GetCode(err))
}

func TestWarnStateSkipped(t *testing.T) {
	stateSol := &solution.Solution{
		Metadata: solution.Metadata{Name: "demo"},
		State:    &state.Config{},
	}

	t.Run("emits notice for state-enabled solution", func(t *testing.T) {
		var buf bytes.Buffer
		ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
		w := writer.New(ioStreams, settings.NewCliParams())
		ctx := writer.WithWriter(context.Background(), w)

		warnStateSkipped(ctx, stateSol)
		assert.Contains(t, buf.String(), "--no-state")
		assert.Contains(t, buf.String(), "demo")
	})

	t.Run("no-op when solution is nil", func(t *testing.T) {
		var buf bytes.Buffer
		ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
		w := writer.New(ioStreams, settings.NewCliParams())
		ctx := writer.WithWriter(context.Background(), w)

		warnStateSkipped(ctx, nil)
		assert.Empty(t, buf.String())
	})

	t.Run("no-op when solution has no state block", func(t *testing.T) {
		var buf bytes.Buffer
		ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
		w := writer.New(ioStreams, settings.NewCliParams())
		ctx := writer.WithWriter(context.Background(), w)

		warnStateSkipped(ctx, &solution.Solution{Metadata: solution.Metadata{Name: "nostate"}})
		assert.Empty(t, buf.String())
	})

	t.Run("no panic when no writer in context", func(t *testing.T) {
		assert.NotPanics(t, func() {
			warnStateSkipped(context.Background(), stateSol)
		})
	})
}

// paramResolverSolution builds a solution whose single resolver reads the
// parameter provider under the given key.
func paramResolverSolution(key string) *solution.Solution {
	return &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				key: {
					Name: key,
					Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{
						{Provider: "parameter", Inputs: map[string]*resolver.ValueRef{
							"key": {Literal: key},
						}},
					}},
				},
			},
		},
	}
}

func TestValidateResolverParams(t *testing.T) {
	sol := paramResolverSolution("name")

	newCtx := func() (context.Context, *bytes.Buffer) {
		var buf bytes.Buffer
		ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
		w := writer.New(ioStreams, settings.NewCliParams())
		return writer.WithWriter(context.Background(), w), &buf
	}

	t.Run("known key passes regardless of policy", func(t *testing.T) {
		for _, policy := range []string{"error", "warn", "ignore"} {
			ctx, buf := newCtx()
			opts := &sharedResolverOptions{OnUnknownResolver: policy}
			err := opts.validateResolverParams(ctx, sol, map[string]any{"name": "x"})
			require.NoError(t, err, "policy %s", policy)
			assert.Empty(t, buf.String(), "policy %s should not warn on known key", policy)
		}
	})

	t.Run("unknown key with error policy rejects", func(t *testing.T) {
		ctx, _ := newCtx()
		opts := &sharedResolverOptions{OnUnknownResolver: "error"}
		err := opts.validateResolverParams(ctx, sol, map[string]any{"foo": "bar"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "foo")
	})

	t.Run("unknown key defaults to error when policy empty", func(t *testing.T) {
		ctx, _ := newCtx()
		opts := &sharedResolverOptions{}
		err := opts.validateResolverParams(ctx, sol, map[string]any{"foo": "bar"})
		require.Error(t, err)
	})

	t.Run("unknown key with warn policy proceeds and warns", func(t *testing.T) {
		ctx, buf := newCtx()
		opts := &sharedResolverOptions{OnUnknownResolver: "warn"}
		err := opts.validateResolverParams(ctx, sol, map[string]any{"foo": "bar"})
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "foo")
	})

	t.Run("unknown key with ignore policy is silent", func(t *testing.T) {
		ctx, buf := newCtx()
		opts := &sharedResolverOptions{OnUnknownResolver: "ignore"}
		err := opts.validateResolverParams(ctx, sol, map[string]any{"foo": "bar"})
		require.NoError(t, err)
		assert.Empty(t, buf.String())
	})

	t.Run("invalid policy errors even without params", func(t *testing.T) {
		ctx, _ := newCtx()
		opts := &sharedResolverOptions{OnUnknownResolver: "loud"}
		err := opts.validateResolverParams(ctx, sol, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "valid: error, warn, ignore")
	})

	t.Run("all-mode solution accepts any key", func(t *testing.T) {
		ctx, _ := newCtx()
		allSol := &solution.Solution{
			Spec: solution.Spec{
				Resolvers: map[string]*resolver.Resolver{
					"params": {
						Name: "params",
						Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{
							{Provider: "parameter", Inputs: map[string]*resolver.ValueRef{
								"all": {Literal: true},
							}},
						}},
					},
				},
			},
		}
		opts := &sharedResolverOptions{OnUnknownResolver: "error"}
		err := opts.validateResolverParams(ctx, allSol, map[string]any{"anything": "goes"})
		require.NoError(t, err)
	})
}

func TestEffectiveUnknownResolverPolicy(t *testing.T) {
	t.Run("flagsChanged nil uses options value", func(t *testing.T) {
		opts := &sharedResolverOptions{OnUnknownResolver: "warn"}
		got, err := opts.effectiveUnknownResolverPolicy(context.Background())
		require.NoError(t, err)
		assert.Equal(t, settings.UnknownResolverWarn, got)
	})

	t.Run("flag not set falls back to config default", func(t *testing.T) {
		cfg := &config.Config{Resolver: config.ResolverConfig{OnUnknownResolver: "ignore"}}
		ctx := config.WithConfig(context.Background(), cfg)
		opts := &sharedResolverOptions{
			OnUnknownResolver: string(settings.DefaultUnknownResolverPolicy),
			flagsChanged:      map[string]bool{},
		}
		got, err := opts.effectiveUnknownResolverPolicy(ctx)
		require.NoError(t, err)
		assert.Equal(t, settings.UnknownResolverIgnore, got)
	})

	t.Run("explicit flag overrides config", func(t *testing.T) {
		cfg := &config.Config{Resolver: config.ResolverConfig{OnUnknownResolver: "ignore"}}
		ctx := config.WithConfig(context.Background(), cfg)
		opts := &sharedResolverOptions{
			OnUnknownResolver: "warn",
			flagsChanged:      map[string]bool{"on-unknown-resolver": true},
		}
		got, err := opts.effectiveUnknownResolverPolicy(ctx)
		require.NoError(t, err)
		assert.Equal(t, settings.UnknownResolverWarn, got)
	})

	t.Run("invalid config value errors", func(t *testing.T) {
		cfg := &config.Config{Resolver: config.ResolverConfig{OnUnknownResolver: "bogus"}}
		ctx := config.WithConfig(context.Background(), cfg)
		opts := &sharedResolverOptions{
			OnUnknownResolver: string(settings.DefaultUnknownResolverPolicy),
			flagsChanged:      map[string]bool{},
		}
		_, err := opts.effectiveUnknownResolverPolicy(ctx)
		require.Error(t, err)
	})
}

func TestResolveValidationPolicy(t *testing.T) {
	t.Run("empty resolves to safe default (error)", func(t *testing.T) {
		opts := &sharedResolverOptions{}
		got, err := opts.resolveValidationPolicy(context.Background())
		require.NoError(t, err)
		assert.Equal(t, settings.ValidationError, got)
		assert.Equal(t, settings.ValidationError, opts.validationPolicy,
			"resolved policy must be stored on the options")
	})

	t.Run("per-command default is honored when flag and config unset", func(t *testing.T) {
		opts := &sharedResolverOptions{ValidationPolicyDefault: settings.ValidationWarn}
		got, err := opts.resolveValidationPolicy(context.Background())
		require.NoError(t, err)
		assert.Equal(t, settings.ValidationWarn, got)
	})

	t.Run("explicit struct value is honored when flagsChanged nil", func(t *testing.T) {
		opts := &sharedResolverOptions{
			OnValidationError:       "ignore",
			ValidationPolicyDefault: settings.ValidationWarn,
		}
		got, err := opts.resolveValidationPolicy(context.Background())
		require.NoError(t, err)
		assert.Equal(t, settings.ValidationIgnore, got)
	})

	t.Run("flag not set falls back to config over per-command default", func(t *testing.T) {
		cfg := &config.Config{Resolver: config.ResolverConfig{OnValidationError: "ignore"}}
		ctx := config.WithConfig(context.Background(), cfg)
		opts := &sharedResolverOptions{
			ValidationPolicyDefault: settings.ValidationWarn,
			flagsChanged:            map[string]bool{},
		}
		got, err := opts.resolveValidationPolicy(ctx)
		require.NoError(t, err)
		assert.Equal(t, settings.ValidationIgnore, got)
	})

	t.Run("explicit flag overrides config", func(t *testing.T) {
		cfg := &config.Config{Resolver: config.ResolverConfig{OnValidationError: "ignore"}}
		ctx := config.WithConfig(context.Background(), cfg)
		opts := &sharedResolverOptions{
			OnValidationError: "error",
			flagsChanged:      map[string]bool{"on-validation-error": true},
		}
		got, err := opts.resolveValidationPolicy(ctx)
		require.NoError(t, err)
		assert.Equal(t, settings.ValidationError, got)
	})

	t.Run("invalid flag value errors", func(t *testing.T) {
		opts := &sharedResolverOptions{OnValidationError: "loud"}
		_, err := opts.resolveValidationPolicy(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "valid: error, warn, ignore")
	})
}

// TestParseCLILockMode pins the CLI --lock-mode parser. Unlike the API's
// parseLockMode (which maps "" to strict), the CLI parser rejects the empty
// string: an unset flag is handled by the caller (which simply appends no
// WithLockMode option and lets applyDefaultLockMode pick a source-dependent
// default), so prepare.ParseLockMode is only ever called with a non-empty value.
func TestParseCLILockMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    prepare.LockMode
		wantErr bool
	}{
		{name: "strict", input: "strict", want: prepare.LockModeStrict},
		{name: "constrained", input: "constrained", want: prepare.LockModeConstrained},
		{name: "bestEffort", input: "bestEffort", want: prepare.LockModeBestEffort},
		{name: "empty is rejected", input: "", wantErr: true},
		{name: "unknown is rejected", input: "bogus", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := prepare.ParseLockMode(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "must be one of: strict, constrained, bestEffort")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestValidateLockMode covers the shared early-validation helper used by the
// run commands (run resolver, run action, run solution). An empty value is
// valid (source-based default); an unknown value must be rejected so callers
// can fail fast with InvalidInput instead of a misleading FileNotFound.
func TestValidateLockMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "empty is valid (source default)", input: ""},
		{name: "strict", input: "strict"},
		{name: "constrained", input: "constrained"},
		{name: "bestEffort", input: "bestEffort"},
		{name: "unknown is rejected", input: "bogus", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			o := &sharedResolverOptions{LockMode: tt.input}
			err := o.validateLockMode()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "must be one of: strict, constrained, bestEffort")
				return
			}
			require.NoError(t, err)
		})
	}
}
