// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/kube"
	"github.com/oakwood-commons/scafctl/pkg/kubeconfig"
)

func TestResolveCluster_ExplicitFlagsOverrideResolver(t *testing.T) {
	t.Parallel()

	resolver := &kube.MockResolver{
		ResolveResult: &kube.ClusterInfo{
			Name:         "prod",
			APIServerURL: "https://resolved:6443",
			OIDCAudience: "resolved-aud",
			AuthType:     kube.AuthTypeOIDC,
		},
	}
	deps := Deps{Resolver: resolver, Kubeconfig: &stubKube{}}

	info, err := resolveCluster(context.Background(), deps, Request{
		Cluster:         "prod",
		Server:          "https://override:6443",
		Audience:        "override-aud",
		InsecureSkipTLS: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "https://override:6443", info.APIServerURL)
	assert.Equal(t, "override-aud", info.OIDCAudience)
	assert.True(t, info.InsecureSkipTLS)
	assert.Equal(t, []string{"prod"}, resolver.ResolveCalls)
}

func TestResolveCluster_NoResolverUsesFlags(t *testing.T) {
	t.Parallel()

	info, err := resolveCluster(context.Background(), Deps{Kubeconfig: &stubKube{}}, Request{
		ClusterName: "ignored",
		Cluster:     "prod",
		Server:      "https://api.example.com:6443",
	})
	require.NoError(t, err)
	assert.Equal(t, "prod", info.Name)
	assert.Equal(t, "https://api.example.com:6443", info.APIServerURL)
}

func TestResolveCluster_ConcreteURLArgumentPassthrough(t *testing.T) {
	t.Parallel()

	// A cluster argument that is already an http(s) URL is used directly as the
	// API server without consulting the resolver, and is not used as the name.
	resolver := &kube.MockResolver{ResolveResult: &kube.ClusterInfo{Name: "should-not-run"}}
	deps := Deps{Resolver: resolver, Kubeconfig: &stubKube{}}

	info, err := resolveCluster(context.Background(), deps, Request{
		Cluster: "https://api.direct.example.com:6443",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://api.direct.example.com:6443", info.APIServerURL)
	assert.Equal(t, "api.direct.example.com", info.Name,
		"a URL argument must derive the name from the host, not use the raw URL")
	assert.Empty(t, resolver.ResolveCalls, "a concrete URL argument must bypass the resolver")
}

func TestResolveCluster_MissingServer(t *testing.T) {
	t.Parallel()

	_, err := resolveCluster(context.Background(), Deps{}, Request{Cluster: "prod"})
	assert.ErrorIs(t, err, ErrNoServer)
}

func TestResolveCluster_AutoDetect(t *testing.T) {
	t.Parallel()

	kc := &stubKube{detectRes: kubeconfig.DetectResult{Success: true, AuthType: kube.AuthTypeOIDC}}
	deps := Deps{Kubeconfig: kc}

	info, err := resolveCluster(context.Background(), deps, Request{
		ClusterName: "prod",
		Server:      "https://api.example.com:6443",
	})
	require.NoError(t, err)
	assert.Equal(t, kube.AuthTypeOIDC, info.AuthType)
	assert.Equal(t, "https://api.example.com:6443", kc.detectIn.Server)
}

func TestResolveCluster_AutoDetectFailureNonFatal(t *testing.T) {
	t.Parallel()

	kc := &stubKube{detectErr: errors.New("probe failed")}
	deps := Deps{Kubeconfig: kc}

	info, err := resolveCluster(context.Background(), deps, Request{
		ClusterName: "prod",
		Server:      "https://api.example.com:6443",
	})
	require.NoError(t, err)
	assert.Equal(t, kube.AuthTypeAuto, info.AuthType)
}

func TestResolveCluster_ResolverNilResult(t *testing.T) {
	t.Parallel()

	resolver := &kube.MockResolver{ResolveResult: nil}
	deps := Deps{Resolver: resolver}

	_, err := resolveCluster(context.Background(), deps, Request{Cluster: "prod"})
	// No server resolved and none supplied.
	assert.ErrorIs(t, err, ErrNoServer)
}

func TestResolveCluster_NameDerivedFromServerHost(t *testing.T) {
	t.Parallel()

	// Direct --server flow with no cluster argument and no --cluster-name:
	// the name must be derived from the host so Validate() passes.
	info, err := resolveCluster(context.Background(), Deps{}, Request{
		Server: "https://api.example.com:6443",
	})
	require.NoError(t, err)
	assert.Equal(t, "api.example.com", info.Name)
}

func TestResolveCluster_NameFallsBackToRawServer(t *testing.T) {
	t.Parallel()

	// A bare host (not a parseable URL with a host component) falls back to the
	// trimmed raw server string so the name is never empty.
	info, err := resolveCluster(context.Background(), Deps{}, Request{
		Server: "api.example.com:6443",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, info.Name)
}

func TestResolveCluster_InsecureSkipTLSClearsResolverCAData(t *testing.T) {
	t.Parallel()

	resolver := &kube.MockResolver{ResolveResult: &kube.ClusterInfo{
		Name:         "prod",
		APIServerURL: "https://api.example.com:6443",
		CAData:       "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----",
	}}
	deps := Deps{Resolver: resolver, Kubeconfig: &stubKube{}}

	info, err := resolveCluster(context.Background(), deps, Request{
		Cluster:         "prod",
		InsecureSkipTLS: true,
	})
	require.NoError(t, err)
	// The writer prefers CAData over InsecureSkipTLS; it must be cleared so the
	// explicit insecure flag is honored.
	assert.True(t, info.InsecureSkipTLS)
	assert.Empty(t, info.CAData)
}

func TestResolveCluster_ServerOverrideMakesResolverMissNonFatal(t *testing.T) {
	t.Parallel()

	// `kube login scratch --server https://... --handler oidc`: the positional
	// is not a known alias, but the explicit --server supplies the connection,
	// so the resolver miss must be non-fatal and the positional is used as the
	// plain cluster name.
	resolver := &kube.MockResolver{ResolveErr: errors.New("cluster not found")}
	deps := Deps{Resolver: resolver, Kubeconfig: &stubKube{}}

	info, err := resolveCluster(context.Background(), deps, Request{
		Cluster: "scratch",
		Server:  "https://api.scratch.example.com:6443",
	})
	require.NoError(t, err)
	assert.Equal(t, "scratch", info.Name)
	assert.Equal(t, "https://api.scratch.example.com:6443", info.APIServerURL)
	assert.Equal(t, []string{"scratch"}, resolver.ResolveCalls)
}

func TestResolveCluster_ResolverMissFatalWithoutServer(t *testing.T) {
	t.Parallel()

	// Without an explicit --server, a resolver miss stays fatal so the user is
	// told the name could not be resolved.
	resolver := &kube.MockResolver{ResolveErr: errors.New("cluster not found")}
	deps := Deps{Resolver: resolver, Kubeconfig: &stubKube{}}

	_, err := resolveCluster(context.Background(), deps, Request{Cluster: "scratch"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve cluster \"scratch\"")
}
