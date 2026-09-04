// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package executionregistry

import (
	"context"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProvider is a minimal Provider that satisfies both the builtin registry's
// descriptor validation (Name, APIVersion, Version, Description, one Capability,
// and an OutputSchema for it) and the external tier's requirement (non-nil
// Version). The tag lets a test assert exactly which registered instance a
// lookup returned.
type fakeProvider struct {
	name    string
	version string
	tag     string
}

func (f *fakeProvider) Descriptor() *provider.Descriptor {
	return &provider.Descriptor{
		Name:         f.name,
		APIVersion:   "v1",
		Version:      semver.MustParse(f.version),
		Description:  "fake provider for executionregistry tests",
		Capabilities: []provider.Capability{provider.CapabilityFrom},
		OutputSchemas: map[provider.Capability]*jsonschema.Schema{
			provider.CapabilityFrom: {
				Type:       "object",
				Properties: map[string]*jsonschema.Schema{"result": {Type: "string"}},
			},
		},
	}
}

func (f *fakeProvider) Execute(_ context.Context, _ any) (*provider.Output, error) {
	return &provider.Output{Data: f.tag}, nil
}

// tagOf runs the provider and returns its tag, so tests can identify which
// concrete instance a Get returned without reaching into unexported state.
func tagOf(t *testing.T, p provider.Provider) string {
	t.Helper()
	out, err := p.Execute(context.Background(), nil)
	require.NoError(t, err)
	tag, ok := out.Data.(string)
	require.True(t, ok, "fake provider must return a string tag")
	return tag
}

func providerDep(name, catalog, version string) solution.PluginDependency { //nolint:unparam // test helper uses constant catalog for clarity
	return solution.PluginDependency{
		Name:    name,
		Kind:    solution.PluginKindProvider,
		Catalog: catalog,
		Version: version,
	}
}

// namesOf extracts provider names from a List() result so tests can assert on
// identity without depending on ordering or reaching into concrete instances.
func namesOf(providers []provider.Provider) []string {
	names := make([]string, 0, len(providers))
	for _, p := range providers {
		names = append(names, p.Descriptor().Name)
	}
	return names
}

func TestExecutionRegistry_Get(t *testing.T) {
	t.Parallel()

	t.Run("name in resolution routes to the external tier", func(t *testing.T) {
		t.Parallel()
		shared := provider.NewCompositeRegistry()
		require.NoError(t, shared.RegisterExternal(
			&fakeProvider{name: "echo", version: "1.2.0", tag: "external"},
			provider.WithCatalogName("ghcr.io/org"),
			provider.WithRegistrationVersion(semver.MustParse("1.2.0")),
		))

		sr := NewExecutionRegistry(shared, map[string]PluginArtifact{
			"echo": providerDep("echo", "ghcr.io/org", "1.2.0"),
		})

		got, ok := sr.Get("echo")
		require.True(t, ok)
		assert.Equal(t, "external", tagOf(t, got))
	})

	t.Run("resolution wins even when a builtin of the same name exists", func(t *testing.T) {
		t.Parallel()
		shared := provider.NewCompositeRegistry()
		// Same name registered in BOTH tiers with distinguishable tags.
		require.NoError(t, shared.RegisterBase(
			&fakeProvider{name: "echo", version: "9.9.9", tag: "builtin"},
		))
		require.NoError(t, shared.RegisterExternal(
			&fakeProvider{name: "echo", version: "1.2.0", tag: "external"},
			provider.WithCatalogName("ghcr.io/org"),
			provider.WithRegistrationVersion(semver.MustParse("1.2.0")),
		))

		sr := NewExecutionRegistry(shared, map[string]PluginArtifact{
			"echo": providerDep("echo", "ghcr.io/org", "1.2.0"),
		})

		got, ok := sr.Get("echo")
		require.True(t, ok)
		assert.Equal(t, "external", tagOf(t, got),
			"a resolved ref must resolve via the external tier, never the builtin")
	})

	t.Run("external lookup uses the resolved catalog and version", func(t *testing.T) {
		t.Parallel()
		shared := provider.NewCompositeRegistry()
		// Two versions under the same catalog; resolution pins 2.0.0.
		require.NoError(t, shared.RegisterExternal(
			&fakeProvider{name: "tf", version: "1.0.0", tag: "v1"},
			provider.WithCatalogName("ghcr.io/org"),
			provider.WithRegistrationVersion(semver.MustParse("1.0.0")),
		))
		require.NoError(t, shared.RegisterExternal(
			&fakeProvider{name: "tf", version: "2.0.0", tag: "v2"},
			provider.WithCatalogName("ghcr.io/org"),
			provider.WithRegistrationVersion(semver.MustParse("2.0.0")),
		))

		sr := NewExecutionRegistry(shared, map[string]PluginArtifact{
			"tf": providerDep("tf", "ghcr.io/org", "2.0.0"),
		})

		got, ok := sr.Get("tf")
		require.True(t, ok)
		assert.Equal(t, "v2", tagOf(t, got), "the pinned version must be selected")
	})

	t.Run("resolved ref missing from the external tier does not fall back to builtin", func(t *testing.T) {
		t.Parallel()
		shared := provider.NewCompositeRegistry()
		// A builtin named "echo" exists, but the resolved ref points at an
		// external entry that was never registered.
		require.NoError(t, shared.RegisterBase(
			&fakeProvider{name: "echo", version: "9.9.9", tag: "builtin"},
		))

		sr := NewExecutionRegistry(shared, map[string]PluginArtifact{
			"echo": providerDep("echo", "ghcr.io/org", "1.2.0"),
		})

		_, ok := sr.Get("echo")
		assert.False(t, ok, "a resolved ref must not silently resolve to a builtin")
	})

	t.Run("name absent from resolution routes to the builtin tier", func(t *testing.T) {
		t.Parallel()
		shared := provider.NewCompositeRegistry()
		require.NoError(t, shared.RegisterBase(
			&fakeProvider{name: "cel", version: "1.0.0", tag: "builtin"},
		))

		sr := NewExecutionRegistry(shared, map[string]PluginArtifact{
			"echo": providerDep("echo", "ghcr.io/org", "1.2.0"),
		})

		got, ok := sr.Get("cel")
		require.True(t, ok)
		assert.Equal(t, "builtin", tagOf(t, got))
	})

	t.Run("unknown name is not found in either tier", func(t *testing.T) {
		t.Parallel()
		shared := provider.NewCompositeRegistry()

		sr := NewExecutionRegistry[PluginArtifact](shared, nil)

		_, ok := sr.Get("nope")
		assert.False(t, ok)
	})

	t.Run("nil resolution routes everything to the builtin tier", func(t *testing.T) {
		t.Parallel()
		shared := provider.NewCompositeRegistry()
		require.NoError(t, shared.RegisterBase(
			&fakeProvider{name: "cel", version: "1.0.0", tag: "builtin"},
		))

		sr := NewExecutionRegistry[PluginArtifact](shared, nil)

		got, ok := sr.Get("cel")
		require.True(t, ok)
		assert.Equal(t, "builtin", tagOf(t, got))
	})
}

func TestExecutionRegistry_Has(t *testing.T) {
	t.Parallel()

	t.Run("mirrors Get across both tiers", func(t *testing.T) {
		t.Parallel()
		shared := provider.NewCompositeRegistry()
		require.NoError(t, shared.RegisterBase(
			&fakeProvider{name: "cel", version: "1.0.0", tag: "builtin"},
		))
		require.NoError(t, shared.RegisterExternal(
			&fakeProvider{name: "echo", version: "1.2.0", tag: "external"},
			provider.WithCatalogName("ghcr.io/org"),
			provider.WithRegistrationVersion(semver.MustParse("1.2.0")),
		))

		sr := NewExecutionRegistry(shared, map[string]PluginArtifact{
			"echo": providerDep("echo", "ghcr.io/org", "1.2.0"),
		})

		assert.True(t, sr.Has("echo"), "resolved external ref is present")
		assert.True(t, sr.Has("cel"), "unresolved builtin ref is present")
		assert.False(t, sr.Has("missing"), "unknown ref is absent")
	})

	t.Run("resolved ref missing from the external tier is absent", func(t *testing.T) {
		t.Parallel()
		shared := provider.NewCompositeRegistry()
		// Builtin of the same name exists but must not satisfy a resolved ref.
		require.NoError(t, shared.RegisterBase(
			&fakeProvider{name: "echo", version: "9.9.9", tag: "builtin"},
		))

		sr := NewExecutionRegistry(shared, map[string]PluginArtifact{
			"echo": providerDep("echo", "ghcr.io/org", "1.2.0"),
		})

		assert.False(t, sr.Has("echo"),
			"a resolved ref must not be reported present via the builtin tier")
	})

	t.Run("wrong pinned version is absent", func(t *testing.T) {
		t.Parallel()
		shared := provider.NewCompositeRegistry()
		require.NoError(t, shared.RegisterExternal(
			&fakeProvider{name: "tf", version: "1.0.0", tag: "v1"},
			provider.WithCatalogName("ghcr.io/org"),
			provider.WithRegistrationVersion(semver.MustParse("1.0.0")),
		))

		// Resolution pins a version that was never registered.
		sr := NewExecutionRegistry(shared, map[string]PluginArtifact{
			"tf": providerDep("tf", "ghcr.io/org", "2.0.0"),
		})

		assert.False(t, sr.Has("tf"))
	})
}

func TestExecutionRegistry_DescriptorLookup(t *testing.T) {
	t.Parallel()

	t.Run("resolved ref returns the external descriptor", func(t *testing.T) {
		t.Parallel()
		shared := provider.NewCompositeRegistry()
		require.NoError(t, shared.RegisterExternal(
			&fakeProvider{name: "echo", version: "1.2.0", tag: "external"},
			provider.WithCatalogName("ghcr.io/org"),
			provider.WithRegistrationVersion(semver.MustParse("1.2.0")),
		))

		sr := NewExecutionRegistry(shared, map[string]PluginArtifact{
			"echo": providerDep("echo", "ghcr.io/org", "1.2.0"),
		})

		desc := sr.DescriptorLookup()("echo")
		require.NotNil(t, desc)
		assert.Equal(t, "echo", desc.Name)
		assert.Equal(t, "1.2.0", desc.Version.String(),
			"the external, resolved-version descriptor must be returned")
	})

	t.Run("resolved ref selects the pinned version", func(t *testing.T) {
		t.Parallel()
		shared := provider.NewCompositeRegistry()
		require.NoError(t, shared.RegisterExternal(
			&fakeProvider{name: "tf", version: "1.0.0", tag: "v1"},
			provider.WithCatalogName("ghcr.io/org"),
			provider.WithRegistrationVersion(semver.MustParse("1.0.0")),
		))
		require.NoError(t, shared.RegisterExternal(
			&fakeProvider{name: "tf", version: "2.0.0", tag: "v2"},
			provider.WithCatalogName("ghcr.io/org"),
			provider.WithRegistrationVersion(semver.MustParse("2.0.0")),
		))

		sr := NewExecutionRegistry[PluginArtifact](shared, map[string]PluginArtifact{
			"tf": providerDep("tf", "ghcr.io/org", "2.0.0"),
		})

		desc := sr.DescriptorLookup()("tf")
		require.NotNil(t, desc)
		assert.Equal(t, "2.0.0", desc.Version.String())
	})

	t.Run("resolution wins over a same-named builtin", func(t *testing.T) {
		t.Parallel()
		shared := provider.NewCompositeRegistry()
		require.NoError(t, shared.RegisterBase(
			&fakeProvider{name: "echo", version: "9.9.9", tag: "builtin"},
		))
		require.NoError(t, shared.RegisterExternal(
			&fakeProvider{name: "echo", version: "1.2.0", tag: "external"},
			provider.WithCatalogName("ghcr.io/org"),
			provider.WithRegistrationVersion(semver.MustParse("1.2.0")),
		))

		sr := NewExecutionRegistry[PluginArtifact](shared, map[string]PluginArtifact{
			"echo": providerDep("echo", "ghcr.io/org", "1.2.0"),
		})

		desc := sr.DescriptorLookup()("echo")
		require.NotNil(t, desc)
		assert.Equal(t, "1.2.0", desc.Version.String(),
			"a resolved ref must return the external descriptor, not the builtin")
	})

	t.Run("name absent from resolution returns the builtin descriptor", func(t *testing.T) {
		t.Parallel()
		shared := provider.NewCompositeRegistry()
		require.NoError(t, shared.RegisterBase(
			&fakeProvider{name: "cel", version: "1.0.0", tag: "builtin"},
		))

		sr := NewExecutionRegistry[PluginArtifact](shared, map[string]PluginArtifact{
			"echo": providerDep("echo", "ghcr.io/org", "1.2.0"),
		})

		desc := sr.DescriptorLookup()("cel")
		require.NotNil(t, desc)
		assert.Equal(t, "cel", desc.Name)
		assert.Equal(t, "1.0.0", desc.Version.String())
	})

	t.Run("resolved ref missing externally falls back to the builtin descriptor", func(t *testing.T) {
		t.Parallel()
		shared := provider.NewCompositeRegistry()
		// A builtin named "echo" exists; the resolved external entry does not.
		// Unlike Get, DescriptorLookup falls through to the builtin tier.
		require.NoError(t, shared.RegisterBase(
			&fakeProvider{name: "echo", version: "9.9.9", tag: "builtin"},
		))

		sr := NewExecutionRegistry[PluginArtifact](shared, map[string]PluginArtifact{
			"echo": providerDep("echo", "ghcr.io/org", "1.2.0"),
		})

		desc := sr.DescriptorLookup()("echo")
		require.NotNil(t, desc)
		assert.Equal(t, "9.9.9", desc.Version.String(),
			"a missing external ref falls back to the builtin descriptor")
	})

	t.Run("unknown name returns nil", func(t *testing.T) {
		t.Parallel()
		shared := provider.NewCompositeRegistry()

		sr := NewExecutionRegistry[PluginArtifact](shared, nil)

		assert.Nil(t, sr.DescriptorLookup()("nope"))
	})
}

func TestExecutionRegistry_List(t *testing.T) {
	t.Parallel()

	t.Run("lists all builtins and resolved external refs", func(t *testing.T) {
		t.Parallel()
		shared := provider.NewCompositeRegistry()
		require.NoError(t, shared.RegisterBase(
			&fakeProvider{name: "cel", version: "1.0.0", tag: "builtin"},
		))
		require.NoError(t, shared.RegisterBase(
			&fakeProvider{name: "env", version: "1.0.0", tag: "builtin"},
		))
		require.NoError(t, shared.RegisterExternal(
			&fakeProvider{name: "echo", version: "1.2.0", tag: "external"},
			provider.WithCatalogName("ghcr.io/org"),
			provider.WithRegistrationVersion(semver.MustParse("1.2.0")),
		))

		sr := NewExecutionRegistry[PluginArtifact](shared, map[string]PluginArtifact{
			"echo": providerDep("echo", "ghcr.io/org", "1.2.0"),
		})

		assert.ElementsMatch(t, []string{"cel", "env", "echo"}, namesOf(sr.List()))
	})

	t.Run("excludes resolved refs that do not resolve externally", func(t *testing.T) {
		t.Parallel()
		shared := provider.NewCompositeRegistry()
		require.NoError(t, shared.RegisterBase(
			&fakeProvider{name: "cel", version: "1.0.0", tag: "builtin"},
		))
		// "tf" is pinned to a version that was never registered externally.
		require.NoError(t, shared.RegisterExternal(
			&fakeProvider{name: "tf", version: "1.0.0", tag: "v1"},
			provider.WithCatalogName("ghcr.io/org"),
			provider.WithRegistrationVersion(semver.MustParse("1.0.0")),
		))

		sr := NewExecutionRegistry[PluginArtifact](shared, map[string]PluginArtifact{
			"tf": providerDep("tf", "ghcr.io/org", "2.0.0"),
		})

		got := namesOf(sr.List())
		assert.ElementsMatch(t, []string{"cel"}, got)
		assert.NotContains(t, got, "tf",
			"an unresolvable external ref must be omitted")
	})

	t.Run("returns only builtins when resolution is nil", func(t *testing.T) {
		t.Parallel()
		shared := provider.NewCompositeRegistry()
		require.NoError(t, shared.RegisterBase(
			&fakeProvider{name: "cel", version: "1.0.0", tag: "builtin"},
		))

		sr := NewExecutionRegistry[PluginArtifact](shared, nil)

		assert.Equal(t, []string{"cel"}, namesOf(sr.List()))
	})

	t.Run("returns empty for an empty registry and no resolution", func(t *testing.T) {
		t.Parallel()
		shared := provider.NewCompositeRegistry()

		sr := NewExecutionRegistry[PluginArtifact](shared, nil)

		assert.Empty(t, sr.List())
	})
}
