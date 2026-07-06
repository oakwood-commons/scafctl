// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// baseStatusRow builds a minimal authenticated base row like buildStatusResult
// would produce, sufficient for exercising expandInstanceRows.
func baseStatusRow(handler string) map[string]any {
	return map[string]any{
		"handler":        handler,
		"kind":           "auth",
		"type":           "handler",
		"status":         "authenticated",
		"authenticated":  true,
		"user":           "user@example.com",
		"flow":           "device_code",
		"profile":        "built-in",
		"expiresIn":      "",
		"expiresAt":      "",
		"_expiresAtTime": nil,
	}
}

func instanceConfigCtx(t *testing.T, handler string, aliases map[string]string) context.Context {
	t.Helper()
	return config.WithConfig(context.Background(), &config.Config{
		Auth: config.GlobalAuthConfig{
			Handlers: map[string]config.HandlerConfig{
				handler: {
					Hostname: &config.HostnameConfig{Aliases: aliases},
				},
			},
		},
	})
}

func TestExpandInstanceRows_NonInstanceHandler(t *testing.T) {
	t.Parallel()

	h := auth.NewMockHandler("entra")
	h.CapabilitiesValue = []auth.Capability{auth.CapScopesOnLogin}
	status := &auth.Status{Authenticated: true}

	rows := expandInstanceRows(context.Background(), "entra", h, status, baseStatusRow("entra"))
	require.Len(t, rows, 1)
	assert.Equal(t, "built-in", rows[0]["profile"])
}

func TestExpandInstanceRows_NotAuthenticated(t *testing.T) {
	t.Parallel()

	h := auth.NewMockHandler("openshift")
	h.CapabilitiesValue = []auth.Capability{auth.CapInstanceHostname}
	status := &auth.Status{Authenticated: false}

	rows := expandInstanceRows(context.Background(), "openshift", h, status, baseStatusRow("openshift"))
	require.Len(t, rows, 1)
}

func TestExpandInstanceRows_NoHostnameTokens(t *testing.T) {
	t.Parallel()

	h := auth.NewMockHandler("openshift")
	h.CapabilitiesValue = []auth.Capability{auth.CapInstanceHostname}
	h.ListCachedTokensResult = []*auth.CachedTokenInfo{
		{Handler: "openshift", TokenKind: "access_token"}, // no Hostname
	}
	status := &auth.Status{Authenticated: true}

	rows := expandInstanceRows(context.Background(), "openshift", h, status, baseStatusRow("openshift"))
	require.Len(t, rows, 1)
}

func TestExpandInstanceRows_TwoClusters(t *testing.T) {
	t.Parallel()

	now := time.Now()
	h := auth.NewMockHandler("openshift")
	h.CapabilitiesValue = []auth.Capability{auth.CapInstanceHostname}
	h.ListCachedTokensResult = []*auth.CachedTokenInfo{
		{
			Handler:   "openshift",
			Hostname:  "https://api.cluster-b.example.com:6443",
			Flow:      auth.FlowInteractive,
			ExpiresAt: now.Add(time.Hour),
			CachedAt:  now.Add(-2 * time.Hour),
		},
		{
			Handler:   "openshift",
			Hostname:  "https://api.cluster-a.example.com:6443",
			Flow:      auth.FlowInteractive,
			ExpiresAt: now.Add(30 * time.Minute),
			CachedAt:  now, // most recent -> active
		},
	}
	status := &auth.Status{Authenticated: true}

	ctx := instanceConfigCtx(t, "openshift", map[string]string{
		"cluster-a": "https://api.cluster-a.example.com:6443",
	})

	rows := expandInstanceRows(ctx, "openshift", h, status, baseStatusRow("openshift"))
	require.Len(t, rows, 2)

	// Deterministic order: sorted by hostname; cluster-b < cluster-a. For the built-in
	// profile the cluster alias/label alone is shown (no redundant "built-in /").
	assert.Equal(t, "api.cluster-b.example.com", rows[0]["profile"], "unaliased host trimmed")
	assert.Equal(t, "cluster-a (active)", rows[1]["profile"], "aliased host + active marker (most recent CachedAt)")

	// Shared identity fills the user column on every row.
	assert.Equal(t, "user@example.com", rows[0]["user"])
	assert.Equal(t, "user@example.com", rows[1]["user"])

	// Per-cluster status.
	assert.Equal(t, "authenticated", rows[0]["status"])
	assert.Equal(t, "authenticated", rows[1]["status"])
}

// TestExpandInstanceRows_DedupesTokensPerCluster asserts that several cached
// tokens for the same instance (e.g. a user login plus minted service-account
// tokens) collapse to a single row per cluster, and that the representative row
// reflects the user/login session rather than a machine token.
func TestExpandInstanceRows_DedupesTokensPerCluster(t *testing.T) {
	t.Parallel()

	now := time.Now()
	h := auth.NewMockHandler("openshift")
	h.CapabilitiesValue = []auth.Capability{auth.CapInstanceHostname}
	h.ListCachedTokensResult = []*auth.CachedTokenInfo{
		{
			Handler:  "openshift",
			Hostname: "https://api.cluster-a.example.com:6443",
			Flow:     auth.FlowInteractive, // user login (older)
			CachedAt: now.Add(-time.Hour),
		},
		{
			Handler:  "openshift",
			Hostname: "https://api.cluster-a.example.com:6443",
			Flow:     auth.FlowServicePrincipal, // minted SA token (newer)
			CachedAt: now,
		},
	}
	status := &auth.Status{Authenticated: true}

	rows := expandInstanceRows(context.Background(), "openshift", h, status, baseStatusRow("openshift"))
	require.Len(t, rows, 1, "multiple tokens for one cluster must collapse to a single row")
	// The user/login flow wins over the newer machine flow.
	assert.Equal(t, string(auth.FlowInteractive), rows[0]["flow"])
	assert.Equal(t, "api.cluster-a.example.com (active)", rows[0]["profile"])
}

// TestExpandInstanceRows_NamedProfilePrefix asserts that a non-default (named)
// auth profile is preserved as a prefix on each cluster label, so identity and
// endpoint stay distinguishable when multiple profiles exist.
func TestExpandInstanceRows_NamedProfilePrefix(t *testing.T) {
	t.Parallel()

	now := time.Now()
	h := auth.NewMockHandler("openshift")
	h.CapabilitiesValue = []auth.Capability{auth.CapInstanceHostname}
	h.ListCachedTokensResult = []*auth.CachedTokenInfo{
		{
			Handler:  "openshift",
			Hostname: "https://api.cluster-a.example.com:6443",
			Flow:     auth.FlowInteractive,
			CachedAt: now,
		},
	}
	status := &auth.Status{Authenticated: true}

	base := baseStatusRow("openshift")
	base["profile"] = "work" // a named profile is active
	rows := expandInstanceRows(context.Background(), "openshift", h, status, base)
	require.Len(t, rows, 1)
	assert.Equal(t, "work / api.cluster-a.example.com (active)", rows[0]["profile"])
}

func TestExpandInstanceRows_ExpiredCluster(t *testing.T) {
	t.Parallel()

	now := time.Now()
	h := auth.NewMockHandler("openshift")
	h.CapabilitiesValue = []auth.Capability{auth.CapInstanceHostname}
	h.ListCachedTokensResult = []*auth.CachedTokenInfo{
		{
			Handler:   "openshift",
			Hostname:  "https://api.cluster-a.example.com:6443",
			IsExpired: true,
			CachedAt:  now,
		},
	}
	status := &auth.Status{Authenticated: true}

	rows := expandInstanceRows(context.Background(), "openshift", h, status, baseStatusRow("openshift"))
	require.Len(t, rows, 1)
	assert.Equal(t, "expired", rows[0]["status"])
}

func TestExpandInstanceRows_NoActiveWithoutTimestamps(t *testing.T) {
	t.Parallel()

	h := auth.NewMockHandler("openshift")
	h.CapabilitiesValue = []auth.Capability{auth.CapInstanceHostname}
	h.ListCachedTokensResult = []*auth.CachedTokenInfo{
		{Handler: "openshift", Hostname: "https://a.example.com"},
		{Handler: "openshift", Hostname: "https://b.example.com"},
	}
	status := &auth.Status{Authenticated: true}

	rows := expandInstanceRows(context.Background(), "openshift", h, status, baseStatusRow("openshift"))
	require.Len(t, rows, 2)
	for _, r := range rows {
		assert.NotContains(t, r["profile"], "(active)", "no active marker when no CachedAt timestamps")
	}
}

// TestQueryHandlerStatuses_PerClusterExpansion exercises the full status query
// path (queryHandlerStatuses -> expandInstanceRows -> flatten) so the wiring
// that turns one instance handler into multiple rows is covered end-to-end.
func TestQueryHandlerStatuses_PerClusterExpansion(t *testing.T) {
	ctx, _ := newTestContext(t)
	ctx = config.WithConfig(ctx, &config.Config{
		Auth: config.GlobalAuthConfig{
			Handlers: map[string]config.HandlerConfig{
				"openshift": {
					Hostname: &config.HostnameConfig{Aliases: map[string]string{
						"prod": "https://api.prod.example.com:6443",
					}},
				},
			},
		},
	})

	now := time.Now()
	mock := auth.NewMockHandler("openshift")
	mock.CapabilitiesValue = []auth.Capability{auth.CapInstanceHostname}
	mock.StatusResult = &auth.Status{
		Authenticated: true,
		Claims:        &auth.Claims{Email: "user@example.com"},
	}
	mock.ListCachedTokensResult = []*auth.CachedTokenInfo{
		{
			Handler:  "openshift",
			Hostname: "https://api.prod.example.com:6443",
			Flow:     auth.FlowInteractive,
			CachedAt: now, // most recent -> active
		},
		{
			Handler:  "openshift",
			Hostname: "https://api.stg.example.com:6443",
			Flow:     auth.FlowInteractive,
			CachedAt: now.Add(-time.Hour),
		},
	}

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mock))
	ctx = auth.WithRegistry(ctx, registry)

	results, warnings := queryHandlerStatuses(ctx, settings.NewCliParams(), []string{"openshift"}, false)
	assert.Empty(t, warnings)
	require.Len(t, results, 2, "one instance handler with two cluster sessions should yield two rows")

	// Sorted by hostname: prod < stg. For the built-in profile the cluster label
	// alone is shown.
	assert.Equal(t, "prod (active)", results[0]["profile"])
	assert.Equal(t, "api.stg.example.com", results[1]["profile"])
	for _, r := range results {
		assert.Equal(t, "openshift", r["handler"])
		assert.Equal(t, "user@example.com", r["user"])
		assert.Equal(t, "authenticated", r["status"])
	}
}

// TestAuthStatusSchema_PropertySetUnchanged guards the display shape: the set of
// declared properties in both the inline and embedded schemas must stay in sync
// with this expected list. Adding/removing a property (a shape change) fails the
// test intentionally; only the profile maxLength was widened for cluster labels.
func TestAuthStatusSchema_PropertySetUnchanged(t *testing.T) {
	t.Parallel()

	expected := []string{
		"handler", "kind", "status", "flow", "user", "expiresIn", "profile",
		"type", "displayName", "email", "username", "authenticated",
		"identityType", "clientId", "tokenFile", "name", "tenantId",
		"expiresAt", "lastRefresh", "scopes", "cachedTokens", "hint",
		"_expiresAtTime",
	}
	sort.Strings(expected)

	inline := schemaPropertyKeys(t, authStatusSchema)
	assert.Equal(t, expected, inline, "inline authStatusSchema property set drifted")

	// The embedded display schema mirrors the inline schema minus the two
	// inline-only internal fields ("type" and "_expiresAtTime").
	embedExpected := make([]string, 0, len(expected))
	for _, k := range expected {
		if k == "_expiresAtTime" || k == "type" {
			continue
		}
		embedExpected = append(embedExpected, k)
	}
	embed := schemaPropertyKeys(t, authStatusDisplaySchema)
	assert.Equal(t, embedExpected, embed, "embedded auth_status_schema.json property set drifted")

	// The profile column must be wide enough for the "<identity> / <cluster>"
	// label used by per-cluster rows.
	assert.Equal(t, float64(60), schemaPropertyMaxLength(t, authStatusSchema, "profile"))
	assert.Equal(t, float64(60), schemaPropertyMaxLength(t, authStatusDisplaySchema, "profile"))
}

func schemaPropertyKeys(t *testing.T, raw []byte) []string {
	t.Helper()
	props := schemaProperties(t, raw)
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func schemaPropertyMaxLength(t *testing.T, raw []byte, prop string) float64 {
	t.Helper()
	props := schemaProperties(t, raw)
	entry, ok := props[prop].(map[string]any)
	require.True(t, ok, "property %q not found", prop)
	ml, ok := entry["maxLength"].(float64)
	require.True(t, ok, "property %q has no maxLength", prop)
	return ml
}

func schemaProperties(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var doc struct {
		Items struct {
			Properties map[string]any `json:"properties"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))
	return doc.Items.Properties
}
