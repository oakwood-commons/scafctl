// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalogindex

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

func wantHash(t *testing.T, canonical string) string {
	t.Helper()
	h := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(h[:registryHashLen])
}

func testConfig() *config.Config {
	return &config.Config{
		Catalogs: []config.CatalogConfig{
			{Name: "Prod", URL: "oci://GHCR.io/Acme/Plugins"},
			{Name: "staging", URL: "https://reg.example.com/team"},
			{Name: "local", Path: "/Home/User/Catalog"},
			{Name: "empty"}, // neither URL nor Path -> skipped
		},
	}
}

func TestFromConfig_RemoteLookups(t *testing.T) {
	idx := FromConfig(testConfig())

	// AliasForRegistry: case-insensitive origin match, kind-strict.
	alias, ok := idx.AliasForRegistry("ghcr.io/acme/plugins")
	assert.True(t, ok)
	assert.Equal(t, "Prod", alias)

	// Mixed-case query resolves the same way.
	alias, ok = idx.AliasForRegistry("GHCR.IO/ACME/PLUGINS")
	assert.True(t, ok)
	assert.Equal(t, "Prod", alias)

	// RegistryForAlias returns the lowercased canonical (OCI names are
	// case-insensitive).
	origin, ok := idx.RegistryForAlias("prod")
	assert.True(t, ok)
	assert.Equal(t, "ghcr.io/acme/plugins", origin)

	// Unknown origin: strict false (reject, do not fetch).
	_, ok = idx.AliasForRegistry("unknown.io/nope")
	assert.False(t, ok)
}

func TestFromConfig_RemoteExcludesLocal(t *testing.T) {
	idx := FromConfig(testConfig())

	// A local catalog's alias must not resolve via the remote accessor.
	_, ok := idx.RegistryForAlias("local")
	assert.False(t, ok)

	// A local path must not resolve via the remote accessor.
	_, ok = idx.AliasForRegistry("/Home/User/Catalog")
	assert.False(t, ok)
}

func TestFromConfig_LocalLookups(t *testing.T) {
	idx := FromConfig(testConfig())

	// AliasForFile is case-sensitive on the path.
	alias, ok := idx.AliasForFile("/Home/User/Catalog")
	assert.True(t, ok)
	assert.Equal(t, "local", alias)

	// Wrong case does not match a filesystem path.
	_, ok = idx.AliasForFile("/home/user/catalog")
	assert.False(t, ok)

	// FileForAlias returns the original path.
	path, ok := idx.FileForAlias("local")
	assert.True(t, ok)
	assert.Equal(t, "/Home/User/Catalog", path)

	// A remote alias must not resolve via the local accessor.
	_, ok = idx.FileForAlias("prod")
	assert.False(t, ok)
}

func TestFromConfig_IdentityLookups(t *testing.T) {
	idx := FromConfig(testConfig())

	id, ok := idx.IdentityForAlias("staging")
	assert.True(t, ok)
	assert.Equal(t, "staging", id.Alias)
	assert.Equal(t, "reg.example.com/team", id.Canonical)
	assert.True(t, id.IsRemote())

	// Canonical lookup, kind-agnostic, case-insensitive for remote.
	id, ok = idx.IdentityForCanonical("REG.EXAMPLE.COM/TEAM")
	assert.True(t, ok)
	assert.Equal(t, "staging", id.Alias)

	// Local canonical resolves case-sensitively.
	id, ok = idx.IdentityForCanonical("/Home/User/Catalog")
	assert.True(t, ok)
	assert.Equal(t, "local", id.Alias)
	assert.True(t, id.IsLocal())

	_, ok = idx.IdentityForAlias("does-not-exist")
	assert.False(t, ok)

	_, ok = idx.IdentityForCanonical("nope.io/missing")
	assert.False(t, ok)
}

func TestFromConfig_RegistryHash(t *testing.T) {
	idx := FromConfig(testConfig())

	id, ok := idx.IdentityForAlias("prod")
	assert.True(t, ok)
	// Remote canonical is lowercased at construction; the hash is over the
	// lowercased form.
	assert.Equal(t, wantHash(t, "ghcr.io/acme/plugins"), id.RegistryHash())
	assert.NotEqual(t, wantHash(t, "GHCR.io/Acme/Plugins"), id.RegistryHash())
}

func TestFromConfig_All(t *testing.T) {
	idx := FromConfig(testConfig())

	got := idx.All()
	aliases := make([]string, 0, len(got))
	for _, id := range got {
		aliases = append(aliases, id.Alias)
	}
	// Config-definition order preserved; the "empty" catalog (no URL/Path) is
	// excluded.
	assert.Equal(t, []string{"Prod", "staging", "local"}, aliases)
}

func TestFromConfig_FirstWinsOnSharedOrigin(t *testing.T) {
	cfg := &config.Config{
		Catalogs: []config.CatalogConfig{
			{Name: "first", URL: "oci://ghcr.io/acme/plugins"},
			{Name: "second", URL: "oci://ghcr.io/acme/plugins"},
		},
	}
	idx := FromConfig(cfg)

	alias, ok := idx.AliasForRegistry("ghcr.io/acme/plugins")
	assert.True(t, ok)
	assert.Equal(t, "first", alias)
}

func TestFromConfig_NilConfig(t *testing.T) {
	idx := FromConfig(nil)
	assert.NotNil(t, idx)

	_, ok := idx.AliasForRegistry("ghcr.io/acme/plugins")
	assert.False(t, ok)
	assert.Empty(t, idx.All())
}

func TestFromConfig_RegistryWithoutRepository(t *testing.T) {
	cfg := &config.Config{
		Catalogs: []config.CatalogConfig{
			{Name: "hostonly", URL: "oci://localhost:5000"},
		},
	}
	idx := FromConfig(cfg)

	alias, ok := idx.AliasForRegistry("localhost:5000")
	assert.True(t, ok)
	assert.Equal(t, "hostonly", alias)

	origin, ok := idx.RegistryForAlias("hostonly")
	assert.True(t, ok)
	assert.Equal(t, "localhost:5000", origin)
}

func TestNilIndex_AllLookupsMiss(t *testing.T) {
	var idx *Index

	_, ok := idx.AliasForRegistry("ghcr.io/x")
	assert.False(t, ok)
	_, ok = idx.RegistryForAlias("x")
	assert.False(t, ok)
	_, ok = idx.AliasForFile("/x")
	assert.False(t, ok)
	_, ok = idx.FileForAlias("x")
	assert.False(t, ok)
	_, ok = idx.IdentityForAlias("x")
	assert.False(t, ok)
	_, ok = idx.IdentityForCanonical("x")
	assert.False(t, ok)
	assert.Nil(t, idx.All())
}

func TestCatalogIdentity_Helpers(t *testing.T) {
	var zero CatalogIdentity
	assert.True(t, zero.IsZero())
	assert.Empty(t, zero.RegistryHash())

	remote := newIdentity("prod", "ghcr.io/acme/plugins", CatalogKindRemote)
	assert.False(t, remote.IsZero())
	assert.True(t, remote.IsRemote())
	assert.False(t, remote.IsLocal())
	assert.Equal(t, "ghcr.io/acme/plugins (prod)", remote.String())

	local := newIdentity("cat", "/tmp/cat", CatalogKindLocal)
	assert.True(t, local.IsLocal())
	assert.False(t, local.IsRemote())

	// Alias equal to canonical: no parenthetical.
	same := newIdentity("ghcr.io/x", "ghcr.io/x", CatalogKindRemote)
	assert.Equal(t, "ghcr.io/x", same.String())
}

func TestCatalogIdentity_RegistryHashBareLiteral(t *testing.T) {
	// A struct literal (no precomputed hash) still computes on demand.
	id := CatalogIdentity{Canonical: "ghcr.io/acme/plugins"}
	assert.Equal(t, wantHash(t, "ghcr.io/acme/plugins"), id.RegistryHash())
}

// fakeRemoteCatalog satisfies registryRepositoryCatalog. It embeds
// catalog.Catalog (nil) so it also satisfies the full interface for slice use;
// only Name/Registry/Repository are exercised.
type fakeRemoteCatalog struct {
	catalog.Catalog
	name, registry, repository string
}

func (c fakeRemoteCatalog) Name() string       { return c.name }
func (c fakeRemoteCatalog) Registry() string   { return c.registry }
func (c fakeRemoteCatalog) Repository() string { return c.repository }

// fakeLocalCatalog satisfies pathCatalog. It embeds catalog.Catalog (nil) so it
// also satisfies the full interface for slice use; only Name/Path are exercised.
type fakeLocalCatalog struct {
	catalog.Catalog
	name, path string
}

func (c fakeLocalCatalog) Name() string { return c.name }
func (c fakeLocalCatalog) Path() string { return c.path }

// fakeChain satisfies catalog.Catalog's Name plus the Catalogs() lister used by
// FromChain. It embeds catalog.Catalog so it satisfies the interface; only
// Name and Catalogs are exercised.
type fakeChain struct {
	catalog.Catalog
	cats []catalog.Catalog
}

func (c fakeChain) Name() string                { return "chain" }
func (c fakeChain) Catalogs() []catalog.Catalog { return c.cats }

func TestFromChain(t *testing.T) {
	chain := fakeChain{cats: []catalog.Catalog{
		fakeRemoteCatalog{name: "Prod", registry: "GHCR.io", repository: "Acme/Plugins"},
		fakeLocalCatalog{name: "local", path: "/Home/User/Catalog"},
	}}
	idx := FromChain(chain)

	// Remote canonical is lowercased; lookups match case-insensitively.
	alias, ok := idx.AliasForRegistry("ghcr.io/acme/plugins")
	assert.True(t, ok)
	assert.Equal(t, "Prod", alias)

	id, ok := idx.IdentityForAlias("prod")
	assert.True(t, ok)
	assert.Equal(t, "ghcr.io/acme/plugins", id.Canonical)
	assert.Equal(t, wantHash(t, "ghcr.io/acme/plugins"), id.RegistryHash())

	// Local path preserves case.
	alias, ok = idx.AliasForFile("/Home/User/Catalog")
	assert.True(t, ok)
	assert.Equal(t, "local", alias)

	// Chain order preserved.
	got := idx.All()
	assert.Equal(t, []string{"Prod", "local"}, []string{got[0].Alias, got[1].Alias})
}

func TestFromChain_SingleCatalog(t *testing.T) {
	// A non-chain catalog (no Catalogs() method) is treated as a single entry.
	idx := FromChain(fakeRemoteCatalog{name: "solo", registry: "ghcr.io", repository: "x"})
	alias, ok := idx.AliasForRegistry("ghcr.io/x")
	assert.True(t, ok)
	assert.Equal(t, "solo", alias)
}

func TestFromChain_Nil(t *testing.T) {
	idx := FromChain(nil)
	assert.NotNil(t, idx)
	assert.Empty(t, idx.All())
}

func TestWithAllowed_Gating(t *testing.T) {
	idx := FromConfig(testConfig()).WithAllowed([]string{"Prod", "staging"})

	assert.True(t, idx.HasAllowlist())
	// Permitted aliases pass, case-insensitively.
	assert.NoError(t, idx.CheckAllowed("prod"))
	assert.NoError(t, idx.CheckAllowed("PROD"))
	assert.NoError(t, idx.CheckAllowed("staging"))

	// A configured-but-not-allowed catalog is rejected.
	err := idx.CheckAllowed("local")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not in the allowed catalogs list")

	// An empty alias is rejected when a gate is set.
	err = idx.CheckAllowed("")
	assert.Error(t, err)
}

func TestWithAllowed_NoGate(t *testing.T) {
	// No allowlist: every catalog (including empty alias) is permitted.
	idx := FromConfig(testConfig())
	assert.False(t, idx.HasAllowlist())
	assert.NoError(t, idx.CheckAllowed("anything"))
	assert.NoError(t, idx.CheckAllowed(""))

	// An empty list leaves the gate open.
	idx = FromConfig(testConfig()).WithAllowed(nil)
	assert.False(t, idx.HasAllowlist())
	assert.NoError(t, idx.CheckAllowed("anything"))

	// A list of only empty strings also leaves the gate open.
	idx = FromConfig(testConfig()).WithAllowed([]string{"", ""})
	assert.False(t, idx.HasAllowlist())
	assert.NoError(t, idx.CheckAllowed("anything"))
}

func TestWithAllowed_CopyOnWrite(t *testing.T) {
	// WithAllowed returns a COPY, never mutating the shared base Index.
	base := FromConfig(testConfig())
	gated := base.WithAllowed([]string{"prod"})

	assert.NotSame(t, base, gated)
	// The base stays ungated so it is safe to share across consumers.
	assert.False(t, base.HasAllowlist())
	assert.NoError(t, base.CheckAllowed("local"))
	// The copy carries the gate.
	assert.True(t, gated.HasAllowlist())
	assert.Error(t, gated.CheckAllowed("local"))

	// Two gated copies from the same base are independent.
	other := base.WithAllowed([]string{"local"})
	assert.NoError(t, other.CheckAllowed("local"))
	assert.Error(t, gated.CheckAllowed("local"))

	// Lookups still work on a gated copy (shared topology).
	alias, ok := gated.AliasForRegistry("ghcr.io/acme/plugins")
	assert.True(t, ok)
	assert.Equal(t, "Prod", alias)
}

func TestNilIndex_Gate(t *testing.T) {
	var idx *Index
	assert.Nil(t, idx.WithAllowed([]string{"prod"}))
	assert.False(t, idx.HasAllowlist())
	assert.NoError(t, idx.CheckAllowed("anything"))
}

func TestCheckPluginAllowed(t *testing.T) {
	policies := map[string]catalog.PluginPolicy{
		"prod":    {Plugins: []string{"aws", "gcp"}},
		"staging": {AllowAll: true},
		// "local" intentionally absent -> deny-all.
	}
	idx := FromConfig(testConfig()).WithPluginPolicies(policies)

	assert.True(t, idx.HasPluginPolicies())

	// Explicit list: listed plugin passes, unlisted is rejected.
	assert.NoError(t, idx.CheckPluginAllowed("prod", "aws"))
	assert.NoError(t, idx.CheckPluginAllowed("prod", "gcp"))
	err := idx.CheckPluginAllowed("prod", "azure")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not in catalog")

	// Alias match is case-insensitive.
	assert.NoError(t, idx.CheckPluginAllowed("Prod", "aws"))

	// AllowAll catalog: any plugin passes.
	assert.NoError(t, idx.CheckPluginAllowed("staging", "anything"))

	// Catalog absent from the policy map is deny-all.
	err = idx.CheckPluginAllowed("local", "aws")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not permitted to serve any plugins")

	// Empty alias is rejected under a gate (unverifiable origin).
	err = idx.CheckPluginAllowed("", "aws")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "origin unknown")
}

func TestCheckPluginAllowed_NoGate(t *testing.T) {
	// No policy map: every plugin is allowed from any catalog, including an
	// empty alias.
	idx := FromConfig(testConfig())
	assert.False(t, idx.HasPluginPolicies())
	assert.NoError(t, idx.CheckPluginAllowed("prod", "anything"))
	assert.NoError(t, idx.CheckPluginAllowed("", "anything"))

	// An empty policy map also leaves the gate open.
	idx = FromConfig(testConfig()).WithPluginPolicies(map[string]catalog.PluginPolicy{})
	assert.False(t, idx.HasPluginPolicies())
	assert.NoError(t, idx.CheckPluginAllowed("prod", "anything"))
}

func TestWithPluginPolicies_CopyOnWrite(t *testing.T) {
	// WithPluginPolicies returns a COPY, never mutating the shared base Index.
	base := FromConfig(testConfig())
	gated := base.WithPluginPolicies(map[string]catalog.PluginPolicy{
		"prod": {Plugins: []string{"aws"}},
	})

	assert.NotSame(t, base, gated)
	// The base stays ungated so it is safe to share across consumers.
	assert.False(t, base.HasPluginPolicies())
	assert.NoError(t, base.CheckPluginAllowed("local", "aws"))
	// The copy carries the gate.
	assert.True(t, gated.HasPluginPolicies())
	assert.Error(t, gated.CheckPluginAllowed("prod", "gcp"))
	assert.Error(t, gated.CheckPluginAllowed("local", "aws")) // absent -> deny

	// The allowlist and plugin-policy gates are independent and composable.
	both := base.WithAllowed([]string{"prod"}).WithPluginPolicies(map[string]catalog.PluginPolicy{
		"prod": {Plugins: []string{"aws"}},
	})
	assert.True(t, both.HasAllowlist())
	assert.True(t, both.HasPluginPolicies())
	assert.NoError(t, both.CheckAllowed("prod"))
	assert.NoError(t, both.CheckPluginAllowed("prod", "aws"))
}

func TestNilIndex_PluginPolicyGate(t *testing.T) {
	var idx *Index
	assert.Nil(t, idx.WithPluginPolicies(map[string]catalog.PluginPolicy{"prod": {AllowAll: true}}))
	assert.False(t, idx.HasPluginPolicies())
	assert.NoError(t, idx.CheckPluginAllowed("prod", "anything"))
}
