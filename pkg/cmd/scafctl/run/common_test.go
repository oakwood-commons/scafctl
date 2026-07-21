// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
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

func TestLoadLockPlugins_MissingFile(t *testing.T) {
	result := loadLockPlugins("/nonexistent/path/solution.yaml")
	assert.Nil(t, result)
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractParameterKeys(tt.resolvers)
			assert.Equal(t, tt.want, got)
		})
	}
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
