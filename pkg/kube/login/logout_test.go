// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/kube"
	"github.com/oakwood-commons/scafctl/pkg/kubeconfig"
)

func TestLogout_NoCluster(t *testing.T) {
	t.Parallel()

	_, err := Logout(context.Background(), Deps{Kubeconfig: &stubKube{}}, LogoutRequest{})
	assert.ErrorIs(t, err, ErrNoCluster)
}

func TestLogout_NoKubeconfigWriter(t *testing.T) {
	t.Parallel()

	_, err := Logout(context.Background(), Deps{}, LogoutRequest{Cluster: "prod"})
	assert.ErrorIs(t, err, ErrNoKubeconfigWriter)
}

func TestLogout_ProviderSuccess(t *testing.T) {
	t.Parallel()

	handler := &stubAuth{name: "oidc"}
	kc := &stubKube{removeRes: kubeconfig.RemoveResult{Success: true, Removed: true}}
	deps := Deps{Handler: handler, Kubeconfig: kc}

	res, err := Logout(context.Background(), deps, LogoutRequest{Cluster: "prod"})
	require.NoError(t, err)
	assert.True(t, res.Removed)
	assert.False(t, res.UsedFallback)
	assert.Equal(t, 1, handler.logoutCalls)
	assert.Equal(t, "prod", kc.removeIn.ClusterName)
	assert.Equal(t, "prod", kc.removeIn.ContextName)
	assert.Equal(t, "prod", kc.removeIn.UserName)
}

func TestLogout_KeepCredentials(t *testing.T) {
	t.Parallel()

	handler := &stubAuth{name: "oidc"}
	kc := &stubKube{removeRes: kubeconfig.RemoveResult{Removed: true}}
	deps := Deps{Handler: handler, Kubeconfig: kc}

	_, err := Logout(context.Background(), deps, LogoutRequest{Cluster: "prod", KeepCredentials: true})
	require.NoError(t, err)
	assert.Equal(t, 0, handler.logoutCalls, "KeepCredentials must skip handler logout")
}

func TestLogout_HandlerLogoutError(t *testing.T) {
	t.Parallel()

	handler := &stubAuth{name: "oidc", logoutErr: errors.New("revoke failed")}
	kc := &stubKube{removeRes: kubeconfig.RemoveResult{Removed: true}}
	deps := Deps{Handler: handler, Kubeconfig: kc}

	res, err := Logout(context.Background(), deps, LogoutRequest{Cluster: "prod"})
	require.Error(t, err)
	assert.True(t, res.Removed, "kubeconfig removal already happened")
}

func TestLogout_RemoveError(t *testing.T) {
	t.Parallel()

	removeErr := errors.New("io error")
	deps := Deps{Kubeconfig: &stubKube{removeErr: removeErr}}

	_, err := Logout(context.Background(), deps, LogoutRequest{Cluster: "prod"})
	require.Error(t, err)
	assert.ErrorIs(t, err, removeErr)
}

func TestLogout_FallbackOnProviderUnavailable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	_, err := writeStaticKubeconfig(kubeconfig.WriteInput{
		Server:         "https://api.example.com:6443",
		ClusterName:    "prod",
		KubeconfigPath: path,
		ExecCommand:    "mycli",
	})
	require.NoError(t, err)

	kc := &stubKube{removeErr: fmt.Errorf("%w: nope", kubeconfig.ErrProviderUnavailable)}
	deps := Deps{Handler: &stubAuth{name: "oidc"}, Kubeconfig: kc}

	res, err := Logout(context.Background(), deps, LogoutRequest{Cluster: "prod", KubeconfigPath: path})
	require.NoError(t, err)
	assert.True(t, res.UsedFallback)
	assert.True(t, res.Removed)
}

func TestLogout_RevokesResolverDefaultHandler(t *testing.T) {
	t.Parallel()

	// No explicit Deps.Handler: logout resolves the cluster's DefaultHandler and
	// revokes the same handler that login would have used.
	handler := &stubAuth{name: "entra"}
	var lookupName string
	kc := &stubKube{removeRes: kubeconfig.RemoveResult{Removed: true}}
	deps := Deps{
		Kubeconfig: kc,
		Resolver:   &kube.MockResolver{ResolveResult: &kube.ClusterInfo{Name: "prod", DefaultHandler: "entra"}},
		HandlerLookup: func(_ context.Context, name string) (Authenticator, error) {
			lookupName = name
			return handler, nil
		},
	}

	res, err := Logout(context.Background(), deps, LogoutRequest{Cluster: "prod"})
	require.NoError(t, err)
	assert.True(t, res.Removed)
	assert.Equal(t, "entra", lookupName, "resolver DefaultHandler must drive the revoke")
	assert.Equal(t, 1, handler.logoutCalls)
}

func TestLogout_DefaultHandlerKeepCredentials(t *testing.T) {
	t.Parallel()

	handler := &stubAuth{name: "entra"}
	kc := &stubKube{removeRes: kubeconfig.RemoveResult{Removed: true}}
	deps := Deps{
		Kubeconfig: kc,
		Resolver:   &kube.MockResolver{ResolveResult: &kube.ClusterInfo{Name: "prod", DefaultHandler: "entra"}},
		HandlerLookup: func(_ context.Context, name string) (Authenticator, error) {
			return handler, nil
		},
	}

	_, err := Logout(context.Background(), deps, LogoutRequest{Cluster: "prod", KeepCredentials: true})
	require.NoError(t, err)
	assert.Equal(t, 0, handler.logoutCalls, "KeepCredentials must skip the default-handler revoke")
}

func TestLogout_NoDefaultHandlerSkipsRevoke(t *testing.T) {
	t.Parallel()

	// Resolver supplies no DefaultHandler and no explicit handler is given: the
	// kubeconfig entry is still removed and credential revocation is skipped.
	lookupCalled := false
	kc := &stubKube{removeRes: kubeconfig.RemoveResult{Removed: true}}
	deps := Deps{
		Kubeconfig: kc,
		Resolver:   &kube.MockResolver{ResolveResult: &kube.ClusterInfo{Name: "prod"}},
		HandlerLookup: func(_ context.Context, _ string) (Authenticator, error) {
			lookupCalled = true
			return &stubAuth{name: "x"}, nil
		},
	}

	res, err := Logout(context.Background(), deps, LogoutRequest{Cluster: "prod"})
	require.NoError(t, err)
	assert.True(t, res.Removed)
	assert.False(t, lookupCalled, "no DefaultHandler means no handler lookup")
}

func TestLogout_RevokesAuthTypeFallbackHandler(t *testing.T) {
	t.Parallel()

	// The resolved cluster carries an AuthType but no DefaultHandler (e.g. an
	// inventory that stamps authType only). Logout applies the same
	// AuthTypeHandlers fallback that login used, revoking the auto-routed handler.
	handler := &stubAuth{name: "openshift"}
	var lookupName string
	kc := &stubKube{removeRes: kubeconfig.RemoveResult{Removed: true}}
	deps := Deps{
		Kubeconfig:       kc,
		Resolver:         &kube.MockResolver{ResolveResult: &kube.ClusterInfo{Name: "prod", AuthType: kube.AuthTypeOAuth}},
		AuthTypeHandlers: DefaultAuthTypeHandlers(),
		HandlerLookup: func(_ context.Context, name string) (Authenticator, error) {
			lookupName = name
			return handler, nil
		},
	}

	res, err := Logout(context.Background(), deps, LogoutRequest{Cluster: "prod"})
	require.NoError(t, err)
	assert.True(t, res.Removed)
	assert.Equal(t, "openshift", lookupName, "AuthType fallback must drive the revoke when DefaultHandler is empty")
	assert.Equal(t, 1, handler.logoutCalls)
}

func TestLogout_DefaultHandlerWinsOverAuthTypeFallback(t *testing.T) {
	t.Parallel()

	// When both are present, the explicit DefaultHandler wins over the AuthType
	// fallback map.
	var lookupName string
	kc := &stubKube{removeRes: kubeconfig.RemoveResult{Removed: true}}
	deps := Deps{
		Kubeconfig:       kc,
		Resolver:         &kube.MockResolver{ResolveResult: &kube.ClusterInfo{Name: "prod", DefaultHandler: "github", AuthType: kube.AuthTypeOAuth}},
		AuthTypeHandlers: DefaultAuthTypeHandlers(),
		HandlerLookup: func(_ context.Context, name string) (Authenticator, error) {
			lookupName = name
			return &stubAuth{name: name}, nil
		},
	}

	_, err := Logout(context.Background(), deps, LogoutRequest{Cluster: "prod"})
	require.NoError(t, err)
	assert.Equal(t, "github", lookupName, "DefaultHandler must win over the AuthType fallback")
}

func TestLogout_AuthTypeFallbackUndetectedSkipsRevoke(t *testing.T) {
	t.Parallel()

	// AuthType is unset (auto/undetected) with no DefaultHandler: the fallback
	// map has no entry for auto, so no handler is revoked (the entry is still
	// removed).
	lookupCalled := false
	kc := &stubKube{removeRes: kubeconfig.RemoveResult{Removed: true}}
	deps := Deps{
		Kubeconfig:       kc,
		Resolver:         &kube.MockResolver{ResolveResult: &kube.ClusterInfo{Name: "prod"}},
		AuthTypeHandlers: DefaultAuthTypeHandlers(),
		HandlerLookup: func(_ context.Context, _ string) (Authenticator, error) {
			lookupCalled = true
			return &stubAuth{name: "x"}, nil
		},
	}

	res, err := Logout(context.Background(), deps, LogoutRequest{Cluster: "prod"})
	require.NoError(t, err)
	assert.True(t, res.Removed)
	assert.False(t, lookupCalled, "undetected AuthType has no fallback mapping")
}

func TestLogout_DefaultHandlerLookupErrorIsNonFatal(t *testing.T) {
	t.Parallel()

	// A failed handler lookup must not fail logout: the kubeconfig entry removal
	// is the primary action.
	kc := &stubKube{removeRes: kubeconfig.RemoveResult{Removed: true}}
	deps := Deps{
		Kubeconfig: kc,
		Resolver:   &kube.MockResolver{ResolveResult: &kube.ClusterInfo{Name: "prod", DefaultHandler: "entra"}},
		HandlerLookup: func(_ context.Context, _ string) (Authenticator, error) {
			return nil, errors.New("handler not registered")
		},
	}

	res, err := Logout(context.Background(), deps, LogoutRequest{Cluster: "prod"})
	require.NoError(t, err)
	assert.True(t, res.Removed)
}

func TestLogout_ExplicitHandlerWinsOverDefault(t *testing.T) {
	t.Parallel()

	// An explicit Deps.Handler is revoked directly; the resolver is not consulted
	// for the default handler.
	explicit := &stubAuth{name: "github"}
	lookupCalled := false
	kc := &stubKube{removeRes: kubeconfig.RemoveResult{Removed: true}}
	deps := Deps{
		Handler:    explicit,
		Kubeconfig: kc,
		Resolver:   &kube.MockResolver{ResolveResult: &kube.ClusterInfo{Name: "prod", DefaultHandler: "entra"}},
		HandlerLookup: func(_ context.Context, _ string) (Authenticator, error) {
			lookupCalled = true
			return &stubAuth{name: "entra"}, nil
		},
	}

	_, err := Logout(context.Background(), deps, LogoutRequest{Cluster: "prod"})
	require.NoError(t, err)
	assert.Equal(t, 1, explicit.logoutCalls)
	assert.False(t, lookupCalled, "explicit handler must bypass the resolver default")
}
