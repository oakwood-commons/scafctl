// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/kubeconfig"
)

func TestStatus_PopulatesIdentityForManaged(t *testing.T) {
	t.Parallel()
	path := writeManagedKubeconfig(t)

	handler := &stubAuth{name: "oidc", token: &auth.Token{AccessToken: "tok"}}
	kc := &stubKube{whoamiRes: kubeconfig.WhoamiResult{Success: true, Username: "alice", Groups: []string{"dev"}}}
	deps := Deps{
		Kubeconfig: kc,
		BinaryName: "scafctl",
		HandlerLookup: func(_ context.Context, name string) (Authenticator, error) {
			assert.Equal(t, "oidc", name, "handler is taken from the managed entry's exec args")
			return handler, nil
		},
	}

	st, err := Status(context.Background(), deps, path)
	require.NoError(t, err)
	assert.True(t, st.Managed)
	assert.Equal(t, "oidc", st.Handler)
	assert.Equal(t, "alice", st.Username)
	assert.Equal(t, []string{"dev"}, st.Groups)
	// The whoami must target the current cluster with the handler's token.
	assert.Equal(t, "tok", kc.whoamiIn.Token)
	assert.Equal(t, "https://api.prod.example.com:6443", kc.whoamiIn.Server)
}

func TestStatus_DegradesWhenWhoamiUnavailable(t *testing.T) {
	t.Parallel()
	path := writeManagedKubeconfig(t)

	handler := &stubAuth{name: "oidc", token: &auth.Token{AccessToken: "tok"}}
	kc := &stubKube{whoamiErr: kubeconfig.ErrProviderUnavailable}
	deps := Deps{
		Kubeconfig: kc,
		BinaryName: "scafctl",
		HandlerLookup: func(_ context.Context, _ string) (Authenticator, error) {
			return handler, nil
		},
	}

	st, err := Status(context.Background(), deps, path)
	require.NoError(t, err)
	// Static data is still reported; identity is simply absent.
	assert.True(t, st.Managed)
	assert.Equal(t, "prod", st.Context)
	assert.Empty(t, st.Username)
}

func TestStatus_DegradesWhenTokenMissing(t *testing.T) {
	t.Parallel()
	path := writeManagedKubeconfig(t)

	// Handler has no token to offer: whoami is never attempted.
	handler := &stubAuth{name: "oidc", tokenErr: assertErr("no token")}
	kc := &stubKube{}
	deps := Deps{
		Kubeconfig: kc,
		BinaryName: "scafctl",
		HandlerLookup: func(_ context.Context, _ string) (Authenticator, error) {
			return handler, nil
		},
	}

	st, err := Status(context.Background(), deps, path)
	require.NoError(t, err)
	assert.True(t, st.Managed)
	assert.Empty(t, st.Username)
	assert.Empty(t, kc.whoamiIn.Token, "whoami must not run without a token")
}

func TestStatus_NoIdentityForUnmanagedContext(t *testing.T) {
	t.Parallel()
	// current-context points at a foreign (non-scafctl) user.
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	content := `apiVersion: v1
kind: Config
current-context: foreign
clusters:
  - name: foreign
    cluster:
      server: https://api.foreign.example.com:6443
contexts:
  - name: foreign
    context:
      cluster: foreign
      user: foreign
users:
  - name: foreign
    user:
      token: static
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	handler := &stubAuth{name: "oidc", token: &auth.Token{AccessToken: "tok"}}
	kc := &stubKube{whoamiRes: kubeconfig.WhoamiResult{Success: true, Username: "alice"}}
	deps := Deps{
		Kubeconfig: kc,
		BinaryName: "scafctl",
		HandlerLookup: func(_ context.Context, _ string) (Authenticator, error) {
			return handler, nil
		},
	}

	st, err := Status(context.Background(), deps, path)
	require.NoError(t, err)
	assert.False(t, st.Managed)
	assert.Empty(t, st.Username, "unmanaged contexts get no whoami")
	assert.Empty(t, kc.whoamiIn.Token)
}

func TestCurrentContext_ExtractsHandlerProfileAndCAData(t *testing.T) {
	t.Parallel()
	caPEM := "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
	caB64 := base64.StdEncoding.EncodeToString([]byte(caPEM))

	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	content := `apiVersion: v1
kind: Config
current-context: prod
clusters:
  - name: prod
    cluster:
      server: https://api.prod.example.com:6443
      certificate-authority-data: ` + caB64 + `
contexts:
  - name: prod
    context:
      cluster: prod
      user: prod
users:
  - name: prod
    user:
      exec:
        command: scafctl
        args:
          - auth
          - token
          - oidc
          - --exec-credential
          - --scope
          - api://cluster-aud
          - --profile
          - work
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	st, err := CurrentContext("scafctl", path)
	require.NoError(t, err)
	assert.True(t, st.Managed)
	assert.Equal(t, "oidc", st.Handler)
	assert.Equal(t, "work", st.Profile)
	assert.Equal(t, "api://cluster-aud", st.Scope)
	assert.Equal(t, caPEM, st.CAData)
}

// assertErr is a tiny error helper for token-failure fixtures.
type assertErr string

func (e assertErr) Error() string { return string(e) }

func TestStatus_PassesBakedScopeToTokenRequest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	content := `apiVersion: v1
kind: Config
current-context: prod
clusters:
  - name: prod
    cluster:
      server: https://api.prod.example.com:6443
contexts:
  - name: prod
    context:
      cluster: prod
      user: prod
users:
  - name: prod
    user:
      exec:
        command: scafctl
        args: [auth, token, oidc, --exec-credential, --scope, "api://aud"]
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	// A handler that accepts per-request scopes must receive the baked --scope
	// so the whoami mints/looks up the same token kubectl would use.
	handler := &stubAuth{
		name:  "oidc",
		caps:  []auth.Capability{auth.CapScopesOnTokenRequest},
		token: &auth.Token{AccessToken: "tok"},
	}
	kc := &stubKube{whoamiRes: kubeconfig.WhoamiResult{Success: true, Username: "alice"}}
	deps := Deps{
		Kubeconfig: kc,
		BinaryName: "scafctl",
		HandlerLookup: func(_ context.Context, _ string) (Authenticator, error) {
			return handler, nil
		},
	}

	st, err := Status(context.Background(), deps, path)
	require.NoError(t, err)
	assert.Equal(t, "api://aud", st.Scope)
	assert.Equal(t, "api://aud", handler.tokenOpts.Scope,
		"the baked --scope must be passed to GetToken for scoped handlers")
}

func TestStatus_OmitsScopeForUnscopedHandler(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	content := `apiVersion: v1
kind: Config
current-context: prod
clusters:
  - name: prod
    cluster:
      server: https://api.prod.example.com:6443
contexts:
  - name: prod
    context:
      cluster: prod
      user: prod
users:
  - name: prod
    user:
      exec:
        command: scafctl
        args: [auth, token, oidc, --exec-credential, --scope, "api://aud"]
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	// No CapScopesOnTokenRequest: the scope is captured for display but must not
	// be forwarded to GetToken.
	handler := &stubAuth{name: "oidc", token: &auth.Token{AccessToken: "tok"}}
	kc := &stubKube{whoamiRes: kubeconfig.WhoamiResult{Success: true, Username: "alice"}}
	deps := Deps{
		Kubeconfig: kc,
		BinaryName: "scafctl",
		HandlerLookup: func(_ context.Context, _ string) (Authenticator, error) {
			return handler, nil
		},
	}

	st, err := Status(context.Background(), deps, path)
	require.NoError(t, err)
	assert.Equal(t, "api://aud", st.Scope)
	assert.Empty(t, handler.tokenOpts.Scope,
		"handlers without CapScopesOnTokenRequest must not receive a scope")
}
