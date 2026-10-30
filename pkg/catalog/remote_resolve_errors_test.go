// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/errcode"
)

// manifestStatusHandler serves a fake registry but can make every manifest
// and tag-list request fail.
//
// status fails anonymous lookups. credStatus, when set, models a registry that
// requires credentials and rejects them: unauthenticated lookups are
// challenged (401 + Basic) until a credentialed one arrives, which fails with
// credStatus; every unauthenticated lookup after that (the anonymous retry)
// gets status, or is served normally when status is unset.
//
// With credStatus set, the registry base endpoint (/v2/) also challenges and
// rejects credentials with credStatus, unless credsValid is set: that models a
// registry that accepts the login but denies the repository, as GHCR does for
// a package that does not exist.
type manifestStatusHandler struct {
	inner      http.Handler
	status     atomic.Int32
	credStatus atomic.Int32
	credsValid atomic.Bool
	sawCreds   atomic.Bool
}

func (h *manifestStatusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v2/" {
		if cs := h.credStatus.Load(); cs != 0 {
			if r.Header.Get("Authorization") == "" {
				w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
				writeRegistryError(w, http.StatusUnauthorized)
				return
			}
			if !h.credsValid.Load() {
				writeRegistryError(w, int(cs))
				return
			}
		}
		h.inner.ServeHTTP(w, r)
		return
	}
	if !strings.Contains(r.URL.Path, "/manifests/") && !strings.HasSuffix(r.URL.Path, "/tags/list") {
		h.inner.ServeHTTP(w, r)
		return
	}
	if cs := h.credStatus.Load(); cs != 0 {
		if r.Header.Get("Authorization") != "" {
			h.sawCreds.Store(true)
			writeRegistryError(w, int(cs))
			return
		}
		if !h.sawCreds.Load() {
			w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
			writeRegistryError(w, http.StatusUnauthorized)
			return
		}
	}
	if s := h.status.Load(); s != 0 {
		writeRegistryError(w, int(s))
		return
	}
	h.inner.ServeHTTP(w, r)
}

// writeRegistryError writes a distribution-spec error body for status.
func writeRegistryError(w http.ResponseWriter, status int) {
	code := "UNKNOWN"
	switch status {
	case http.StatusUnauthorized:
		code = "UNAUTHORIZED"
	case http.StatusForbidden:
		code = "DENIED"
	case http.StatusNotFound:
		code = "NAME_UNKNOWN"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"errors":[{"code":%q,"message":"test"}]}`, code)
}

// withCredentials makes cat send static credentials to its registry.
func withCredentials(cat *RemoteCatalog) {
	cat.client.Credential = auth.StaticCredential(cat.registry, auth.Credential{Username: "user", Password: "secret"})
}

// newManifestStatusRemote returns a remote catalog holding provider p@1.0.0
// whose manifest requests can be made to fail with a chosen status.
func newManifestStatusRemote(t *testing.T) (*RemoteCatalog, *manifestStatusHandler) {
	t.Helper()
	h := &manifestStatusHandler{inner: newFakeOCIRegistry().handler()}
	ts := httptest.NewTLSServer(h)
	t.Cleanup(ts.Close)

	cat, err := NewRemoteCatalog(RemoteCatalogConfig{
		Name:       "primary",
		Registry:   strings.TrimPrefix(ts.URL, "https://"),
		Repository: "myorg/artifacts",
		Insecure:   true,
		Logger:     logr.Discard(),
	})
	require.NoError(t, err)
	cat.client.Client = ts.Client()

	_, err = cat.Store(t.Context(), testRef("p", "1.0.0"), []byte("binary"), nil, nil, false)
	require.NoError(t, err)
	return cat, h
}

func TestRemoteCatalog_ManifestErrors_StrictChain(t *testing.T) {
	t.Parallel()

	ops := map[string]func(ctx context.Context, c *pluginSourceChain) (string, error){
		"Find": func(ctx context.Context, c *pluginSourceChain) (string, error) {
			info, err := c.Find(ctx, ArtifactQuery{Name: "p", Kind: ArtifactKindProvider})
			return info.Catalog, err
		},
		"FetchByPlatform": func(ctx context.Context, c *pluginSourceChain) (string, error) {
			_, info, err := c.FetchByPlatform(ctx, testRef("p", "1.0.0"), "linux/amd64")
			return info.Catalog, err
		},
		"FetchByPlatform latest": func(ctx context.Context, c *pluginSourceChain) (string, error) {
			_, info, err := c.FetchByPlatform(ctx, testRef("p", ""), "linux/amd64")
			return info.Catalog, err
		},
		"ResolveContentDigest": func(ctx context.Context, c *pluginSourceChain) (string, error) {
			info, err := c.ResolveContentDigest(ctx, testRef("p", "1.0.0"), "linux/amd64", MediaTypeProviderBinary)
			return info.Catalog, err
		},
	}

	for opName, op := range ops {
		t.Run(opName, func(t *testing.T) {
			t.Parallel()

			halts := []struct {
				name         string
				credentialed bool
				credStatus   int32
				status       int32
				rejected     bool
			}{
				{name: "server error", status: http.StatusInternalServerError},
				{name: "credentialed 401", credentialed: true, credStatus: http.StatusUnauthorized, status: http.StatusUnauthorized, rejected: true},
				{name: "credentialed 403", credentialed: true, credStatus: http.StatusForbidden, status: http.StatusForbidden, rejected: true},
				{name: "credentialed 403 then anonymous 404", credentialed: true, credStatus: http.StatusForbidden, status: http.StatusNotFound, rejected: true},
			}
			for _, tc := range halts {
				t.Run(tc.name+" halts", func(t *testing.T) {
					t.Parallel()
					remote, h := newManifestStatusRemote(t)
					if tc.credentialed {
						withCredentials(remote)
					}
					h.credStatus.Store(tc.credStatus)
					h.status.Store(tc.status)
					fallback := okSource("secondary")

					_, err := op(t.Context(), newPluginSourceChain(logr.Discard(), nil, remote, fallback))
					require.Error(t, err)
					assert.NotErrorIs(t, err, ErrArtifactNotFound)
					assert.Zero(t, fallback.calls, "a registry failure must not fall through to a lower-priority source")
					if tc.rejected {
						assert.ErrorIs(t, err, errCredentialsRejected)
						assert.True(t, remote.HasStaleCredentials())
					}
				})
			}

			fallsThrough := []struct {
				name   string
				status int32
			}{
				{name: "not found", status: http.StatusNotFound},
				{name: "anonymous 401", status: http.StatusUnauthorized},
				{name: "anonymous 403 DENIED", status: http.StatusForbidden},
			}
			for _, tc := range fallsThrough {
				t.Run(tc.name+" falls through", func(t *testing.T) {
					t.Parallel()
					remote, h := newManifestStatusRemote(t)
					h.status.Store(tc.status)
					fallback := okSource("secondary")

					got, err := op(t.Context(), newPluginSourceChain(logr.Discard(), nil, remote, fallback))
					require.NoError(t, err)
					assert.Equal(t, "secondary", got)
					assert.False(t, remote.HasStaleCredentials(), "an anonymous catalog has no credentials to reject")
				})
			}

			t.Run("denied with valid credentials falls through", func(t *testing.T) {
				t.Parallel()
				remote, h := newManifestStatusRemote(t)
				withCredentials(remote)
				h.credStatus.Store(http.StatusForbidden)
				h.credsValid.Store(true)
				h.status.Store(http.StatusForbidden)
				fallback := okSource("secondary")

				got, err := op(t.Context(), newPluginSourceChain(logr.Discard(), nil, remote, fallback))
				require.NoError(t, err)
				assert.Equal(t, "secondary", got)
				assert.False(t, remote.HasStaleCredentials(), "valid credentials must not degrade the catalog to anonymous")
			})

			t.Run("credentialed 403 then anonymous success serves primary", func(t *testing.T) {
				t.Parallel()
				remote, h := newManifestStatusRemote(t)
				withCredentials(remote)
				h.credStatus.Store(http.StatusForbidden)
				fallback := okSource("secondary")

				got, err := op(t.Context(), newPluginSourceChain(logr.Discard(), nil, remote, fallback))
				require.NoError(t, err)
				assert.Equal(t, "primary", got)
				assert.Zero(t, fallback.calls)
				assert.True(t, remote.HasStaleCredentials())
			})
		})
	}
}

func TestRemoteCatalog_LookupError(t *testing.T) {
	t.Parallel()
	ref := testRef("p", "1.0.0")
	denied := &errcode.ErrorResponse{StatusCode: http.StatusForbidden}
	missing := &errcode.ErrorResponse{StatusCode: http.StatusNotFound}

	unauthorized := &errcode.ErrorResponse{StatusCode: http.StatusUnauthorized}
	probeRejected := &errcode.ErrorResponse{StatusCode: http.StatusUnauthorized}

	type probeResult struct {
		presented bool
		err       error
	}
	accepted := &probeResult{presented: true}
	rejected := &probeResult{presented: true, err: probeRejected}
	unverified := &probeResult{}

	newCat := func(credentialed, stale bool, probe *probeResult, probes *atomic.Int32) *RemoteCatalog {
		cat := &RemoteCatalog{name: "primary", registry: "registry.example", client: &auth.Client{}, logger: logr.Discard()}
		if credentialed {
			withCredentials(cat)
		}
		if stale {
			cat.SetStaleForTesting()
		}
		cat.probeCredentials = func(context.Context) (bool, error) {
			probes.Add(1)
			if probe == nil {
				return false, assert.AnError
			}
			return probe.presented, probe.err
		}
		return cat
	}

	tests := []struct {
		name         string
		credentialed bool
		stale        bool
		probe        *probeResult
		err          error
		wantNotFound bool
		wantRejected bool
		wantProbes   int32
	}{
		{name: "anonymous 404", err: missing, wantNotFound: true},
		{name: "anonymous errdef not found", err: errdef.ErrNotFound, wantNotFound: true},
		{name: "anonymous 403", err: denied, wantNotFound: true},
		{name: "credentialed 404", credentialed: true, err: missing, wantNotFound: true},
		{name: "credentialed 403 with accepted credentials", credentialed: true, probe: accepted, err: denied, wantNotFound: true, wantProbes: 1},
		{name: "credentialed 401 with accepted credentials", credentialed: true, probe: accepted, err: unauthorized, wantNotFound: true, wantProbes: 1},
		{name: "credentialed 403 with rejected credentials", credentialed: true, probe: rejected, err: denied, wantProbes: 1},
		{name: "credentialed 403 with unverified credentials", credentialed: true, probe: unverified, err: denied, wantProbes: 1},
		{name: "credentialed 403 when the probe fails", credentialed: true, err: denied, wantProbes: 1},
		{name: "stale 404", credentialed: true, stale: true, err: missing, wantRejected: true},
		{name: "stale 403", credentialed: true, stale: true, probe: accepted, err: denied, wantRejected: true},
		{name: "anonymous other", err: assert.AnError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var probes atomic.Int32
			err := newCat(tc.credentialed, tc.stale, tc.probe, &probes).lookupError(t.Context(), "resolving", ref, tc.err)
			require.Error(t, err)
			assert.Equal(t, tc.wantNotFound, IsNotFound(err))
			assert.Equal(t, tc.wantRejected, errors.Is(err, errCredentialsRejected))
			assert.Equal(t, tc.wantProbes, probes.Load(), "credential probes")
			if !tc.wantNotFound {
				assert.ErrorIs(t, err, tc.err)
				assert.Contains(t, err.Error(), `catalog "primary"`)
			}
		})
	}
}

func TestRemoteCatalog_SendsCredentials(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	assert.False(t, (&RemoteCatalog{}).sendsCredentials(ctx), "nil client")
	assert.False(t, (&RemoteCatalog{client: &auth.Client{}}).sendsCredentials(ctx), "no credential func")

	empty := &RemoteCatalog{registry: "r", client: &auth.Client{
		Credential: func(context.Context, string) (auth.Credential, error) { return auth.EmptyCredential, nil },
	}}
	assert.False(t, empty.sendsCredentials(ctx), "credential source with nothing for the host")

	failing := &RemoteCatalog{registry: "r", client: &auth.Client{
		Credential: func(context.Context, string) (auth.Credential, error) { return auth.EmptyCredential, assert.AnError },
	}}
	assert.True(t, failing.sendsCredentials(ctx), "a failing lookup fails closed")

	cred := &RemoteCatalog{registry: "r", client: &auth.Client{}}
	withCredentials(cred)
	assert.True(t, cred.sendsCredentials(ctx))
	cred.SetStaleForTesting()
	assert.False(t, cred.sendsCredentials(ctx), "a degraded catalog is anonymous")
}

func TestRetryAnonymously_CredentialLookupOnlyOnAuthFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		err       error
		wantCalls int32
	}{
		{name: "success", wantCalls: 0},
		{name: "not found", err: &errcode.ErrorResponse{StatusCode: http.StatusNotFound}, wantCalls: 0},
		{name: "auth failure", err: &errcode.ErrorResponse{StatusCode: http.StatusForbidden}, wantCalls: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			cat := &RemoteCatalog{name: "primary", registry: "r", logger: logr.Discard(), client: &auth.Client{
				Credential: func(context.Context, string) (auth.Credential, error) {
					calls.Add(1)
					return auth.EmptyCredential, nil
				},
			}}

			var runs int
			_, err := retryAnonymously(t.Context(), cat, "op", testRef("p", "1.0.0"), func() (struct{}, error) {
				runs++
				return struct{}{}, tc.err
			})
			assert.Equal(t, tc.err, err)
			assert.Equal(t, tc.wantCalls, calls.Load(), "credential callback invocations")
			assert.Equal(t, 1, runs, "an anonymous catalog is not retried")
			assert.False(t, cat.HasStaleCredentials())
		})
	}

	t.Run("already degraded catalog retries without a lookup", func(t *testing.T) {
		t.Parallel()
		var calls atomic.Int32
		cat := &RemoteCatalog{name: "primary", registry: "r", logger: logr.Discard(), client: &auth.Client{
			Credential: func(context.Context, string) (auth.Credential, error) {
				calls.Add(1)
				return auth.Credential{Username: "u", Password: "p"}, nil
			},
		}}
		cat.switchToAnonymous()

		var runs int
		_, err := retryAnonymously(t.Context(), cat, "op", testRef("p", "1.0.0"), func() (struct{}, error) {
			runs++
			if runs == 1 {
				return struct{}{}, &errcode.ErrorResponse{StatusCode: http.StatusUnauthorized}
			}
			return struct{}{}, nil
		})
		require.NoError(t, err)
		assert.Equal(t, 2, runs)
		assert.Zero(t, calls.Load())
	})
}

func TestRemoteCatalog_ManifestResolveError(t *testing.T) {
	t.Parallel()
	cat := &RemoteCatalog{name: "primary"}
	ref := testRef("p", "1.0.0")

	_, unreachable := IsCatalogUnreachable(cat.manifestResolveError(t.Context(), ref, &netTimeoutError{}))
	assert.True(t, unreachable)

	err := cat.manifestResolveError(t.Context(), ref, assert.AnError)
	require.ErrorIs(t, err, assert.AnError)
	assert.NotErrorIs(t, err, ErrArtifactNotFound)
	assert.Contains(t, err.Error(), `catalog "primary"`)
}

// netTimeoutError is a net.Error reporting a timeout.
type netTimeoutError struct{}

func (*netTimeoutError) Error() string   { return "timeout" }
func (*netTimeoutError) Timeout() bool   { return true }
func (*netTimeoutError) Temporary() bool { return true }
