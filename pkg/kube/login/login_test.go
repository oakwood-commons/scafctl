// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/kube"
	"github.com/oakwood-commons/scafctl/pkg/kubeconfig"
)

// stubAuth is a test Authenticator that records inputs and returns configured
// outputs.
type stubAuth struct {
	name         string
	caps         []auth.Capability
	loginErr     error
	loginOpts    auth.LoginOptions
	loginProfile string
	token        *auth.Token
	tokenErr     error
	tokenOpts    auth.TokenOptions
	tokenProfile string
	logoutErr    error
	logoutCalls  int
}

func (s *stubAuth) Name() string        { return s.name }
func (s *stubAuth) DisplayName() string { return s.name }

func (s *stubAuth) Login(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
	s.loginOpts = opts
	s.loginProfile = auth.ProfileFromContext(ctx)
	if s.loginErr != nil {
		return nil, s.loginErr
	}
	return &auth.Result{}, nil
}

func (s *stubAuth) GetToken(ctx context.Context, opts auth.TokenOptions) (*auth.Token, error) {
	s.tokenOpts = opts
	s.tokenProfile = auth.ProfileFromContext(ctx)
	return s.token, s.tokenErr
}

func (s *stubAuth) Logout(_ context.Context) error {
	s.logoutCalls++
	return s.logoutErr
}

func (s *stubAuth) Capabilities() []auth.Capability { return s.caps }

// stubKube is a test KubeconfigWriter that records inputs and returns configured
// outputs.
type stubKube struct {
	writeIn   kubeconfig.WriteInput
	writeRes  kubeconfig.WriteResult
	writeErr  error
	removeIn  kubeconfig.RemoveInput
	removeRes kubeconfig.RemoveResult
	removeErr error
	detectIn  kubeconfig.DetectInput
	detectRes kubeconfig.DetectResult
	detectErr error
	whoamiIn  kubeconfig.WhoamiInput
	whoamiRes kubeconfig.WhoamiResult
	whoamiErr error
}

func (s *stubKube) WriteKubeconfig(_ context.Context, in kubeconfig.WriteInput) (kubeconfig.WriteResult, error) {
	s.writeIn = in
	return s.writeRes, s.writeErr
}

func (s *stubKube) RemoveEntry(_ context.Context, in kubeconfig.RemoveInput) (kubeconfig.RemoveResult, error) {
	s.removeIn = in
	return s.removeRes, s.removeErr
}

func (s *stubKube) DetectAuthType(_ context.Context, in kubeconfig.DetectInput) (kubeconfig.DetectResult, error) {
	s.detectIn = in
	return s.detectRes, s.detectErr
}

func (s *stubKube) Whoami(_ context.Context, in kubeconfig.WhoamiInput) (kubeconfig.WhoamiResult, error) {
	s.whoamiIn = in
	return s.whoamiRes, s.whoamiErr
}

func TestLogin_NoHandler(t *testing.T) {
	t.Parallel()

	_, err := Login(context.Background(), Deps{}, Request{})
	assert.ErrorIs(t, err, ErrNoHandler)
}

func TestLogin_NoServer(t *testing.T) {
	t.Parallel()

	deps := Deps{
		Handler:    &stubAuth{name: "oidc"},
		Kubeconfig: &stubKube{},
	}
	_, err := Login(context.Background(), deps, Request{Cluster: "prod"})
	assert.ErrorIs(t, err, ErrNoServer)
}

func TestLogin_NoKubeconfigWriter(t *testing.T) {
	t.Parallel()

	// A handler is available but no kubeconfig writer: Login must report the
	// missing dependency instead of panicking on a nil writer.
	deps := Deps{Handler: &stubAuth{name: "oidc"}}
	_, err := Login(context.Background(), deps, Request{
		Server:      "https://api.example.com:6443",
		ClusterName: "prod",
	})
	assert.ErrorIs(t, err, ErrNoKubeconfigWriter)
}

func TestLogin_ProfileAppliedToContext(t *testing.T) {
	t.Parallel()

	handler := &stubAuth{
		name:  "oidc",
		token: &auth.Token{AccessToken: "tok"},
	}
	kc := &stubKube{
		writeRes:  kubeconfig.WriteResult{Success: true, ContextName: "prod"},
		whoamiRes: kubeconfig.WhoamiResult{Success: true, Username: "alice"},
	}
	deps := Deps{Handler: handler, Kubeconfig: kc}

	_, err := Login(context.Background(), deps, Request{
		Server:      "https://api.example.com:6443",
		ClusterName: "prod",
		Profile:     "work",
	})
	require.NoError(t, err)
	// The requested profile must reach both the initial login and the token
	// request so credentials are stored and read under the same profile that
	// the kubeconfig exec block passes via --profile.
	assert.Equal(t, "work", handler.loginProfile)
	assert.Equal(t, "work", handler.tokenProfile)
}

func TestLogin_ResolverError(t *testing.T) {
	t.Parallel()

	resolveErr := errors.New("boom")
	deps := Deps{
		Handler:    &stubAuth{name: "oidc"},
		Kubeconfig: &stubKube{},
		Resolver:   &kube.MockResolver{ResolveErr: resolveErr},
	}
	_, err := Login(context.Background(), deps, Request{Cluster: "prod"})
	require.Error(t, err)
	assert.ErrorIs(t, err, resolveErr)
}

func TestLogin_LoginError(t *testing.T) {
	t.Parallel()

	loginErr := errors.New("auth failed")
	deps := Deps{
		Handler:    &stubAuth{name: "oidc", loginErr: loginErr},
		Kubeconfig: &stubKube{},
	}
	_, err := Login(context.Background(), deps, Request{Server: "https://api.example.com:6443", ClusterName: "prod"})
	require.Error(t, err)
	assert.ErrorIs(t, err, loginErr)
}

func TestLogin_LoginRunnerUsed(t *testing.T) {
	t.Parallel()

	handler := &stubAuth{name: "oidc", token: &auth.Token{AccessToken: "tok"}}
	kc := &stubKube{writeRes: kubeconfig.WriteResult{Success: true, ContextName: "prod"}}

	var (
		runnerCalls   int
		runnerHandler Authenticator
		runnerOpts    auth.LoginOptions
	)
	deps := Deps{
		Handler:    handler,
		Kubeconfig: kc,
		LoginRunner: func(_ context.Context, h Authenticator, opts auth.LoginOptions) (*auth.Result, error) {
			runnerCalls++
			runnerHandler = h
			runnerOpts = opts
			return &auth.Result{}, nil
		},
	}

	_, err := Login(context.Background(), deps, Request{
		Server:      "https://api.example.com:6443",
		ClusterName: "prod",
		Timeout:     42 * time.Second,
	})
	require.NoError(t, err)

	// The runner is invoked in place of handler.Login and receives the resolved
	// handler plus the built login options.
	assert.Equal(t, 1, runnerCalls)
	assert.Same(t, handler, runnerHandler)
	assert.Equal(t, 42*time.Second, runnerOpts.Timeout)
	// handler.Login must not be called directly when a runner is set.
	assert.Equal(t, auth.LoginOptions{}, handler.loginOpts)
}

func TestLogin_LoginRunnerError(t *testing.T) {
	t.Parallel()

	runnerErr := errors.New("runner boom")
	deps := Deps{
		Handler:    &stubAuth{name: "oidc"},
		Kubeconfig: &stubKube{},
		LoginRunner: func(_ context.Context, _ Authenticator, _ auth.LoginOptions) (*auth.Result, error) {
			return nil, runnerErr
		},
	}
	_, err := Login(context.Background(), deps, Request{Server: "https://api.example.com:6443", ClusterName: "prod"})
	require.Error(t, err)
	assert.ErrorIs(t, err, runnerErr)
}

func TestLogin_ProviderSuccess(t *testing.T) {
	t.Parallel()

	handler := &stubAuth{
		name:  "oidc",
		caps:  []auth.Capability{auth.CapScopesOnTokenRequest},
		token: &auth.Token{AccessToken: "tok", ExpiresAt: time.Date(2026, 6, 24, 15, 4, 5, 0, time.UTC)},
	}
	kc := &stubKube{
		writeRes:  kubeconfig.WriteResult{Success: true, ContextName: "prod", KubeconfigPath: "/tmp/cfg"},
		whoamiRes: kubeconfig.WhoamiResult{Success: true, Username: "alice", Groups: []string{"dev"}},
	}
	deps := Deps{Handler: handler, Kubeconfig: kc, BinaryName: "mycli"}

	res, err := Login(context.Background(), deps, Request{
		Cluster:           "prod",
		Server:            "https://api.example.com:6443",
		Audience:          "my-client-id",
		Profile:           "work",
		SetCurrentContext: true,
	})
	require.NoError(t, err)

	assert.False(t, res.UsedFallback)
	assert.Equal(t, "prod", res.ContextName)
	assert.Equal(t, "/tmp/cfg", res.KubeconfigPath)
	assert.Equal(t, "https://api.example.com:6443", res.Server)
	assert.Equal(t, "alice", res.Username)
	assert.Equal(t, []string{"dev"}, res.Groups)
	assert.Equal(t, handler.token.ExpiresAt, res.ExpiresAt)

	// Embedder binary name is baked into the exec command.
	assert.Equal(t, "mycli", kc.writeIn.ExecCommand)
	assert.Equal(t, []string{"auth", "token", "oidc", "--exec-credential", "--scope", "my-client-id", "--profile", "work"}, kc.writeIn.ExecArgs)
	assert.True(t, kc.writeIn.SetCurrentContext)
	// Scope-on-token handler passes the audience as the token request scope.
	assert.Equal(t, "my-client-id", handler.tokenOpts.Scope)
	assert.Equal(t, "tok", kc.whoamiIn.Token)
}

func TestLogin_DefaultBinaryName(t *testing.T) {
	t.Parallel()

	kc := &stubKube{writeRes: kubeconfig.WriteResult{Success: true, ContextName: "prod"}}
	deps := Deps{Handler: &stubAuth{name: "oidc"}, Kubeconfig: kc}

	_, err := Login(context.Background(), deps, Request{Server: "https://api.example.com:6443", ClusterName: "prod"})
	require.NoError(t, err)
	assert.NotEmpty(t, kc.writeIn.ExecCommand, "default binary name must be applied")
}

func TestLogin_ScopesOnLogin(t *testing.T) {
	t.Parallel()

	handler := &stubAuth{name: "oidc", caps: []auth.Capability{auth.CapScopesOnLogin}}
	kc := &stubKube{writeRes: kubeconfig.WriteResult{Success: true}}
	deps := Deps{Handler: handler, Kubeconfig: kc}

	_, err := Login(context.Background(), deps, Request{
		Server:      "https://api.example.com:6443",
		ClusterName: "prod",
		Audience:    "aud-1",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"aud-1"}, handler.loginOpts.Scopes)
	// Without CapScopesOnTokenRequest, the audience is not a token scope nor an exec --scope arg.
	assert.Empty(t, handler.tokenOpts.Scope)
	assert.NotContains(t, kc.writeIn.ExecArgs, "--scope")
}

func TestLogin_HostnameForwardedWhenCapable(t *testing.T) {
	t.Parallel()

	handler := &stubAuth{name: "openshift", caps: []auth.Capability{auth.CapHostname}}
	kc := &stubKube{writeRes: kubeconfig.WriteResult{Success: true}}
	deps := Deps{Handler: handler, Kubeconfig: kc}

	_, err := Login(context.Background(), deps, Request{
		Server:      "https://api.example.com:6443",
		ClusterName: "prod",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.com:6443", handler.loginOpts.Hostname,
		"resolved API server URL must be forwarded to CapHostname handlers")
}

func TestLogin_HostnameNotForwardedWithoutCapability(t *testing.T) {
	t.Parallel()

	handler := &stubAuth{name: "plain"}
	kc := &stubKube{writeRes: kubeconfig.WriteResult{Success: true}}
	deps := Deps{Handler: handler, Kubeconfig: kc}

	_, err := Login(context.Background(), deps, Request{
		Server:      "https://api.example.com:6443",
		ClusterName: "prod",
	})
	require.NoError(t, err)
	assert.Empty(t, handler.loginOpts.Hostname,
		"handlers without CapHostname must not receive the API server URL")
}

func TestLogin_ProvideClusterInfoAlwaysSet(t *testing.T) {
	t.Parallel()

	handler := &stubAuth{name: "openshift"}
	kc := &stubKube{writeRes: kubeconfig.WriteResult{Success: true}}
	deps := Deps{Handler: handler, Kubeconfig: kc}

	_, err := Login(context.Background(), deps, Request{
		Server:      "https://api.example.com:6443",
		ClusterName: "prod",
	})
	require.NoError(t, err)
	assert.True(t, kc.writeIn.ProvideClusterInfo,
		"kube login must set provideClusterInfo so kubectl passes cluster details via KUBERNETES_EXEC_INFO")
}

func TestLogin_DirectURLDerivesEntryNames(t *testing.T) {
	t.Parallel()

	// `kube login https://api.example.com:6443` (positional is a concrete URL):
	// the kubeconfig entries must be named with the derived host, not the raw
	// URL, which is not a usable entry name and can exceed cluster_name limits.
	handler := &stubAuth{name: "oidc"}
	kc := &stubKube{writeRes: kubeconfig.WriteResult{Success: true}}
	deps := Deps{Handler: handler, Kubeconfig: kc}

	_, err := Login(context.Background(), deps, Request{
		Cluster: "https://api.example.com:6443",
	})
	require.NoError(t, err)
	assert.Equal(t, "api.example.com", kc.writeIn.ClusterName)
	assert.Equal(t, "api.example.com", kc.writeIn.ContextName)
	assert.Equal(t, "api.example.com", kc.writeIn.UserName)
	assert.Equal(t, "https://api.example.com:6443", kc.writeIn.Server)
}

func TestLogin_ClusterNameFlagOverridesDerivedURLName(t *testing.T) {
	t.Parallel()

	// An explicit --cluster-name still wins over the host-derived name for the
	// direct URL form.
	handler := &stubAuth{name: "oidc"}
	kc := &stubKube{writeRes: kubeconfig.WriteResult{Success: true}}
	deps := Deps{Handler: handler, Kubeconfig: kc}

	_, err := Login(context.Background(), deps, Request{
		Cluster:     "https://api.example.com:6443",
		ClusterName: "prod",
	})
	require.NoError(t, err)
	assert.Equal(t, "prod", kc.writeIn.ClusterName)
}

func TestLogin_SupportsPerClusterTokens(t *testing.T) {
	t.Parallel()

	t.Run("handler with CapTokenHostname", func(t *testing.T) {
		t.Parallel()
		handler := &stubAuth{name: "openshift", caps: []auth.Capability{auth.CapTokenHostname}}
		kc := &stubKube{writeRes: kubeconfig.WriteResult{Success: true}}
		res, err := Login(context.Background(), Deps{Handler: handler, Kubeconfig: kc}, Request{
			Server:      "https://api.example.com:6443",
			ClusterName: "prod",
		})
		require.NoError(t, err)
		assert.True(t, res.SupportsPerClusterTokens)
	})

	t.Run("handler without CapTokenHostname", func(t *testing.T) {
		t.Parallel()
		handler := &stubAuth{name: "plain", caps: []auth.Capability{auth.CapHostname}}
		kc := &stubKube{writeRes: kubeconfig.WriteResult{Success: true}}
		res, err := Login(context.Background(), Deps{Handler: handler, Kubeconfig: kc}, Request{
			Server:      "https://api.example.com:6443",
			ClusterName: "prod",
		})
		require.NoError(t, err)
		assert.False(t, res.SupportsPerClusterTokens,
			"a handler that advertises only CapHostname (login) must not be reported as per-cluster token capable")
	})
}

func TestLogin_WhoamiTokenRoutedByHostname(t *testing.T) {
	t.Parallel()

	t.Run("CapTokenHostname handler gets the cluster hostname", func(t *testing.T) {
		t.Parallel()
		handler := &stubAuth{
			name:  "openshift",
			caps:  []auth.Capability{auth.CapTokenHostname},
			token: &auth.Token{AccessToken: "tok"},
		}
		kc := &stubKube{
			writeRes:  kubeconfig.WriteResult{Success: true},
			whoamiRes: kubeconfig.WhoamiResult{Success: true},
		}
		_, err := Login(context.Background(), Deps{Handler: handler, Kubeconfig: kc}, Request{
			Server:      "https://api.example.com:6443",
			ClusterName: "prod",
		})
		require.NoError(t, err)
		assert.Equal(t, "https://api.example.com:6443", handler.tokenOpts.Hostname,
			"post-login whoami must route to the just-authenticated cluster")
	})

	t.Run("non-capable handler gets no hostname", func(t *testing.T) {
		t.Parallel()
		handler := &stubAuth{
			name:  "plain",
			token: &auth.Token{AccessToken: "tok"},
		}
		kc := &stubKube{
			writeRes:  kubeconfig.WriteResult{Success: true},
			whoamiRes: kubeconfig.WhoamiResult{Success: true},
		}
		_, err := Login(context.Background(), Deps{Handler: handler, Kubeconfig: kc}, Request{
			Server:      "https://api.example.com:6443",
			ClusterName: "prod",
		})
		require.NoError(t, err)
		assert.Empty(t, handler.tokenOpts.Hostname)
	})
}

func TestLogin_WriteError(t *testing.T) {
	t.Parallel()

	writeErr := errors.New("disk full")
	deps := Deps{
		Handler:    &stubAuth{name: "oidc"},
		Kubeconfig: &stubKube{writeErr: writeErr},
	}
	_, err := Login(context.Background(), deps, Request{Server: "https://api.example.com:6443", ClusterName: "prod"})
	require.Error(t, err)
	assert.ErrorIs(t, err, writeErr)
}

func TestLogin_FallbackOnProviderUnavailable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config")
	kc := &stubKube{
		writeErr: fmt.Errorf("%w: not registered", kubeconfig.ErrProviderUnavailable),
	}
	deps := Deps{Handler: &stubAuth{name: "oidc"}, Kubeconfig: kc, BinaryName: "mycli"}

	res, err := Login(context.Background(), deps, Request{
		Server:            "https://api.example.com:6443",
		ClusterName:       "prod",
		KubeconfigPath:    cfgPath,
		SetCurrentContext: true,
	})
	require.NoError(t, err)
	assert.True(t, res.UsedFallback)
	assert.Equal(t, "prod", res.ContextName)
	assert.Equal(t, cfgPath, res.KubeconfigPath)
	assert.FileExists(t, cfgPath)
}

func TestLogin_WhoamiFailureNonFatal(t *testing.T) {
	t.Parallel()

	handler := &stubAuth{
		name:  "oidc",
		token: &auth.Token{AccessToken: "tok"},
	}
	kc := &stubKube{
		writeRes:  kubeconfig.WriteResult{Success: true, ContextName: "prod"},
		whoamiErr: errors.New("whoami down"),
	}
	deps := Deps{Handler: handler, Kubeconfig: kc}

	res, err := Login(context.Background(), deps, Request{Server: "https://api.example.com:6443", ClusterName: "prod"})
	require.NoError(t, err)
	assert.Empty(t, res.Username)
}

func TestLogin_ResolverCADataReachesWriteInput(t *testing.T) {
	t.Parallel()

	const caBundle = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----"
	resolver := &kube.MockResolver{ResolveResult: &kube.ClusterInfo{
		Name:         "prod",
		APIServerURL: "https://api.example.com:6443",
		CAData:       caBundle,
	}}
	kc := &stubKube{writeRes: kubeconfig.WriteResult{Success: true, ContextName: "prod"}}
	deps := Deps{Handler: &stubAuth{name: "oidc"}, Kubeconfig: kc, Resolver: resolver}

	_, err := Login(context.Background(), deps, Request{Cluster: "prod"})
	require.NoError(t, err)
	assert.Equal(t, caBundle, kc.writeIn.CAData, "resolver-supplied CA bundle must reach the kubeconfig write")
}

func TestLogin_VerifyUsesCADataForWhoami(t *testing.T) {
	t.Parallel()

	const caBundle = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----"
	resolver := &kube.MockResolver{ResolveResult: &kube.ClusterInfo{
		Name:         "prod",
		APIServerURL: "https://api.example.com:6443",
		CAData:       caBundle,
	}}
	handler := &stubAuth{name: "oidc", token: &auth.Token{AccessToken: "tok"}}
	kc := &stubKube{
		writeRes:  kubeconfig.WriteResult{Success: true, ContextName: "prod"},
		whoamiRes: kubeconfig.WhoamiResult{Success: true, Username: "alice"},
	}
	deps := Deps{Handler: handler, Kubeconfig: kc, Resolver: resolver}

	res, err := Login(context.Background(), deps, Request{Cluster: "prod", Verify: true})
	require.NoError(t, err)
	assert.Equal(t, "alice", res.Username)
	// The CA bundle must be passed to the verification whoami so --verify does
	// not fail TLS against private-CA clusters that work via the kubeconfig.
	assert.Equal(t, caBundle, kc.whoamiIn.CAData)
}

func TestLogin_NoHandlerAndNoLookup(t *testing.T) {
	t.Parallel()

	// Neither an explicit handler nor a lookup hook: cannot authenticate.
	_, err := Login(context.Background(), Deps{Kubeconfig: &stubKube{}}, Request{
		Server:      "https://api.example.com:6443",
		ClusterName: "prod",
	})
	assert.ErrorIs(t, err, ErrNoHandler)
}

func TestLogin_HandlerFromResolverDefault(t *testing.T) {
	t.Parallel()

	// No explicit --handler; the handler name comes from the cluster's
	// DefaultHandler and is resolved via HandlerLookup.
	handler := &stubAuth{name: "entra"}
	var lookupName string
	resolver := &kube.MockResolver{ResolveResult: &kube.ClusterInfo{
		Name:           "prod",
		APIServerURL:   "https://api.example.com:6443",
		DefaultHandler: "entra",
	}}
	kc := &stubKube{writeRes: kubeconfig.WriteResult{Success: true, ContextName: "prod"}}
	deps := Deps{
		Kubeconfig: kc,
		Resolver:   resolver,
		HandlerLookup: func(_ context.Context, name string) (Authenticator, error) {
			lookupName = name
			return handler, nil
		},
	}

	res, err := Login(context.Background(), deps, Request{Cluster: "prod"})
	require.NoError(t, err)
	assert.Equal(t, "entra", lookupName, "resolver DefaultHandler must drive the lookup")
	assert.Equal(t, "entra", res.Handler)
}

func TestLogin_RequestHandlerOverridesDefault(t *testing.T) {
	t.Parallel()

	// An explicit request handler wins over the cluster's DefaultHandler.
	var lookupName string
	resolver := &kube.MockResolver{ResolveResult: &kube.ClusterInfo{
		Name:           "prod",
		APIServerURL:   "https://api.example.com:6443",
		DefaultHandler: "entra",
	}}
	kc := &stubKube{writeRes: kubeconfig.WriteResult{Success: true, ContextName: "prod"}}
	deps := Deps{
		Kubeconfig: kc,
		Resolver:   resolver,
		HandlerLookup: func(_ context.Context, name string) (Authenticator, error) {
			lookupName = name
			return &stubAuth{name: name}, nil
		},
	}

	_, err := Login(context.Background(), deps, Request{Cluster: "prod", Handler: "github"})
	require.NoError(t, err)
	assert.Equal(t, "github", lookupName, "explicit request handler must override the default")
}

func TestLogin_HandlerLookupError(t *testing.T) {
	t.Parallel()

	lookupErr := errors.New("unknown handler")
	deps := Deps{
		Kubeconfig: &stubKube{},
		HandlerLookup: func(_ context.Context, _ string) (Authenticator, error) {
			return nil, lookupErr
		},
	}

	_, err := Login(context.Background(), deps, Request{
		Server:      "https://api.example.com:6443",
		ClusterName: "prod",
		Handler:     "nope",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, lookupErr)
}

func TestLogin_LookupModeNoHandlerName(t *testing.T) {
	t.Parallel()

	// HandlerLookup is configured but no handler name is available (no flag and
	// the resolved cluster has no DefaultHandler): expect ErrNoHandler.
	deps := Deps{
		Kubeconfig: &stubKube{},
		HandlerLookup: func(_ context.Context, _ string) (Authenticator, error) {
			return &stubAuth{name: "x"}, nil
		},
	}
	_, err := Login(context.Background(), deps, Request{
		Server:      "https://api.example.com:6443",
		ClusterName: "prod",
	})
	assert.ErrorIs(t, err, ErrNoHandler)
}

func TestLogin_VerifySuccess(t *testing.T) {
	t.Parallel()

	handler := &stubAuth{name: "oidc", token: &auth.Token{AccessToken: "tok"}}
	kc := &stubKube{
		writeRes:  kubeconfig.WriteResult{Success: true, ContextName: "prod"},
		whoamiRes: kubeconfig.WhoamiResult{Success: true, Username: "alice"},
	}
	deps := Deps{Handler: handler, Kubeconfig: kc}

	res, err := Login(context.Background(), deps, Request{
		Server:      "https://api.example.com:6443",
		ClusterName: "prod",
		Verify:      true,
	})
	require.NoError(t, err)
	assert.Equal(t, "alice", res.Username)
}

func TestLogin_VerifyFailure(t *testing.T) {
	t.Parallel()

	handler := &stubAuth{name: "oidc", token: &auth.Token{AccessToken: "tok"}}
	kc := &stubKube{
		writeRes:  kubeconfig.WriteResult{Success: true, ContextName: "prod"},
		whoamiRes: kubeconfig.WhoamiResult{Success: false},
	}
	deps := Deps{Handler: handler, Kubeconfig: kc}

	res, err := Login(context.Background(), deps, Request{
		Server:      "https://api.example.com:6443",
		ClusterName: "prod",
		Verify:      true,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrVerificationFailed)
	// The kubeconfig entry is still written even though verification failed.
	require.NotNil(t, res)
	assert.Equal(t, "prod", res.ContextName)
}
