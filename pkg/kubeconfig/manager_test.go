// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kubeconfig

import (
	"context"
	"errors"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/kube"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
)

// stubProvider is a minimal provider whose capabilities are configurable, used
// to exercise the capability-assertion path.
type stubProvider struct {
	caps []provider.Capability
}

func (s *stubProvider) Descriptor() *provider.Descriptor {
	return &provider.Descriptor{
		Name:         ProviderName,
		APIVersion:   "v1",
		Version:      semver.MustParse("1.0.0"),
		Description:  "stub",
		Capabilities: s.caps,
		OutputSchemas: map[provider.Capability]*jsonschema.Schema{
			provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"value": schemahelper.StringProp("value"),
			}),
		},
	}
}

func (s *stubProvider) Execute(_ context.Context, _ any) (*provider.Output, error) {
	return &provider.Output{Data: map[string]any{"success": true}}, nil
}

func registryWith(t *testing.T, p provider.Provider) *provider.Registry {
	t.Helper()
	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(p))
	return reg
}

func TestManager_WriteKubeconfig(t *testing.T) {
	t.Parallel()
	mock := NewMockProvider()
	m := NewManager("scafctl", WithRegistry(registryWith(t, mock)))

	res, err := m.WriteKubeconfig(context.Background(), WriteInput{
		Server:      "https://api.example.com:6443",
		ClusterName: "prod",
		ContextName: "prod-ctx",
	})
	require.NoError(t, err)
	assert.True(t, res.Success)
	assert.Equal(t, "prod-ctx", res.ContextName)

	call, ok := mock.LastCall()
	require.True(t, ok)
	assert.Equal(t, OperationWrite, call.Operation)
	assert.Equal(t, "scafctl", call.Inputs["exec_command"])
}

func TestManager_WriteKubeconfig_BakesEmbedderBinaryName(t *testing.T) {
	t.Parallel()
	mock := NewMockProvider()
	m := NewManager("mycli", WithRegistry(registryWith(t, mock)))

	_, err := m.WriteKubeconfig(context.Background(), WriteInput{
		Server:      "https://api.example.com:6443",
		ClusterName: "prod",
	})
	require.NoError(t, err)

	call, ok := mock.LastCall()
	require.True(t, ok)
	assert.Equal(t, "mycli", call.Inputs["exec_command"],
		"embedder binary name must be baked into exec_command")
}

func TestManager_WriteKubeconfig_ExplicitExecCommandWins(t *testing.T) {
	t.Parallel()
	mock := NewMockProvider()
	m := NewManager("mycli", WithRegistry(registryWith(t, mock)))

	_, err := m.WriteKubeconfig(context.Background(), WriteInput{
		Server:      "https://api.example.com:6443",
		ClusterName: "prod",
		ExecCommand: "explicit",
	})
	require.NoError(t, err)

	call, _ := mock.LastCall()
	assert.Equal(t, "explicit", call.Inputs["exec_command"])
}

func TestManager_RemoveEntry(t *testing.T) {
	t.Parallel()
	mock := &MockProvider{Remove: &RemoveResult{Removed: true}}
	m := NewManager("scafctl", WithRegistry(registryWith(t, mock)))

	res, err := m.RemoveEntry(context.Background(), RemoveInput{ClusterName: "prod"})
	require.NoError(t, err)
	assert.True(t, res.Success)
	assert.True(t, res.Removed)
	call, _ := mock.LastCall()
	assert.Equal(t, OperationRemove, call.Operation)
}

func TestManager_CurrentServer(t *testing.T) {
	t.Parallel()
	mock := &MockProvider{Server: "https://api.example.com:6443"}
	m := NewManager("scafctl", WithRegistry(registryWith(t, mock)))

	server, err := m.CurrentServer(context.Background(), CurrentServerInput{ContextName: "prod"})
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.com:6443", server)
	call, _ := mock.LastCall()
	assert.Equal(t, OperationCurrentServer, call.Operation)
}

func TestManager_CurrentServer_NotSuccessReturnsEmpty(t *testing.T) {
	t.Parallel()
	mock := &MockProvider{
		ExecuteFunc: func(_ context.Context, _ string, _ map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: map[string]any{
				"success": false,
				"server":  "https://stale.example.com:6443",
			}}, nil
		},
	}
	m := NewManager("scafctl", WithRegistry(registryWith(t, mock)))

	server, err := m.CurrentServer(context.Background(), CurrentServerInput{})
	require.NoError(t, err)
	assert.Empty(t, server, "a non-success lookup must not leak a stale server URL")
}

func TestManager_DetectAuthType(t *testing.T) {
	t.Parallel()
	mock := &MockProvider{Detect: &DetectResult{AuthType: kube.AuthTypeOIDC, OIDCIssuer: "https://issuer"}}
	m := NewManager("scafctl", WithRegistry(registryWith(t, mock)))

	res, err := m.DetectAuthType(context.Background(), DetectInput{Server: "https://api.example.com:6443"})
	require.NoError(t, err)
	assert.Equal(t, kube.AuthTypeOIDC, res.AuthType)
	assert.Equal(t, "https://issuer", res.OIDCIssuer)
}

func TestManager_Reachable(t *testing.T) {
	t.Parallel()
	mock := &MockProvider{Reachable: &ReachableResult{Reachable: false, Status: 503}}
	m := NewManager("scafctl", WithRegistry(registryWith(t, mock)))

	res, err := m.Reachable(context.Background(), ReachableInput{Server: "https://api.example.com:6443"})
	require.NoError(t, err)
	assert.False(t, res.Reachable)
	assert.Equal(t, 503, res.Status)
}

func TestManager_Whoami(t *testing.T) {
	t.Parallel()
	mock := &MockProvider{Whoami: &WhoamiResult{Username: "alice", Groups: []string{"dev"}, UID: "u1"}}
	m := NewManager("scafctl", WithRegistry(registryWith(t, mock)))

	res, err := m.Whoami(context.Background(), WhoamiInput{
		Server: "https://api.example.com:6443",
		Token:  "secret-token",
	})
	require.NoError(t, err)
	assert.Equal(t, "alice", res.Username)
	assert.Equal(t, []string{"dev"}, res.Groups)
	assert.Equal(t, "u1", res.UID)

	call, _ := mock.LastCall()
	assert.Equal(t, "secret-token", call.Inputs["token"])
}

func TestManager_Ensure_ProviderUnavailableWithoutOfficialRegistry(t *testing.T) {
	t.Parallel()
	m := NewManager("scafctl") // empty internal registry, no official registry in ctx

	_, err := m.Reachable(context.Background(), ReachableInput{Server: "https://api.example.com:6443"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProviderUnavailable)
}

func TestManager_Ensure_NotAnOfficialProvider(t *testing.T) {
	t.Parallel()
	m := NewManager("scafctl") // empty internal registry forces the fetch path

	// Official registry present in context but without a kubeconfig entry.
	officialReg := official.NewRegistryFrom([]official.Provider{
		{Name: "env", CatalogRef: "env", DefaultVersion: "latest"},
	})
	ctx := official.WithRegistry(context.Background(), officialReg)

	_, err := m.Reachable(ctx, ReachableInput{Server: "https://api.example.com:6443"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProviderUnavailable)
	assert.Contains(t, err.Error(), "not an official provider")
}

func TestManager_Ensure_ProviderLacksCapability(t *testing.T) {
	t.Parallel()
	stub := &stubProvider{caps: []provider.Capability{provider.CapabilityFrom}}
	m := NewManager("scafctl", WithRegistry(registryWith(t, stub)))

	_, err := m.Reachable(context.Background(), ReachableInput{Server: "https://api.example.com:6443"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProviderUnavailable)
}

func TestManager_Ensure_CachesResolvedProvider(t *testing.T) {
	t.Parallel()
	mock := NewMockProvider()
	m := NewManager("scafctl", WithRegistry(registryWith(t, mock)))

	_, err := m.Reachable(context.Background(), ReachableInput{Server: "https://a"})
	require.NoError(t, err)
	require.NotNil(t, m.resolved)

	_, err = m.Reachable(context.Background(), ReachableInput{Server: "https://b"})
	require.NoError(t, err)
	assert.Len(t, mock.Calls, 2)
}

func TestManager_ExecuteError(t *testing.T) {
	t.Parallel()
	mock := &MockProvider{Err: errors.New("boom")}
	m := NewManager("scafctl", WithRegistry(registryWith(t, mock)))

	_, err := m.Reachable(context.Background(), ReachableInput{Server: "https://a"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

func TestManager_Close_NoClients(t *testing.T) {
	t.Parallel()
	m := NewManager("scafctl", WithRegistry(provider.NewRegistry()))
	assert.NoError(t, m.Close())
	// Idempotent.
	assert.NoError(t, m.Close())
}

func TestManager_Close_UnregistersRegisteredProviders(t *testing.T) {
	t.Parallel()
	// A caller-owned registry that outlives the manager must not be left holding
	// providers this manager registered during ensure.
	reg := registryWith(t, NewMockProvider())
	m := NewManager("scafctl", WithRegistry(reg))
	m.registered = []string{ProviderName}

	require.NoError(t, m.Close())

	_, ok := reg.Get(ProviderName)
	assert.False(t, ok, "Close must unregister providers it registered")
	assert.Nil(t, m.registered)

	// Idempotent: a second Close does not panic and the provider stays gone.
	require.NoError(t, m.Close())
	_, ok = reg.Get(ProviderName)
	assert.False(t, ok)
}

func TestManager_WriteKubeconfig_DefaultsContextToClusterName(t *testing.T) {
	t.Parallel()
	mock := NewMockProvider()
	m := NewManager("scafctl", WithRegistry(registryWith(t, mock)))

	res, err := m.WriteKubeconfig(context.Background(), WriteInput{
		Server:      "https://api.example.com:6443",
		ClusterName: "prod",
	})
	require.NoError(t, err)
	assert.True(t, res.Success)
	assert.Equal(t, "prod", res.ContextName,
		"empty context name must default to the cluster name")
}

func TestNewManager_DefaultBinaryName(t *testing.T) {
	t.Parallel()
	m := NewManager("")
	assert.Equal(t, "scafctl", m.binaryName)
	assert.NotNil(t, m.registry)
}

func BenchmarkManager_Reachable(b *testing.B) {
	mock := NewMockProvider()
	reg := provider.NewRegistry()
	_ = reg.Register(mock)
	m := NewManager("scafctl", WithRegistry(reg))
	ctx := context.Background()
	in := ReachableInput{Server: "https://api.example.com:6443"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Reachable(ctx, in); err != nil {
			b.Fatal(err)
		}
	}
}
