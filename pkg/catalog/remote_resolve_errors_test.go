// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
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
//
// missingRepo models a repository that does not exist yet: the configured
// failures apply only until the first write (push) arrives, after which the
// repository is served normally.
type manifestStatusHandler struct {
	inner       http.Handler
	status      atomic.Int32
	credStatus  atomic.Int32
	credsValid  atomic.Bool
	sawCreds    atomic.Bool
	missingRepo atomic.Bool
	created     atomic.Bool
	// credLookups counts credentialed manifest and tag-list requests.
	credLookups atomic.Int32
	// authProbes counts credentialed /v2/ login probes.
	authProbes atomic.Int32
}

func (h *manifestStatusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.missingRepo.Load() && r.Method != http.MethodGet && r.Method != http.MethodHead {
		h.created.Store(true)
	}
	if r.URL.Path == "/v2/" {
		if r.Header.Get("Authorization") != "" {
			h.authProbes.Add(1)
		}
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
	if h.created.Load() {
		h.inner.ServeHTTP(w, r)
		return
	}
	if cs := h.credStatus.Load(); cs != 0 {
		if r.Header.Get("Authorization") != "" {
			h.sawCreds.Store(true)
			h.credLookups.Add(1)
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
		"ListPlatforms": func(ctx context.Context, c *pluginSourceChain) (string, error) {
			platforms, err := c.ListPlatforms(ctx, testRef("p", "1.0.0"))
			if len(platforms) > 0 {
				return platforms[0], err
			}
			return "primary", err
		},
		"ListPlatforms latest": func(ctx context.Context, c *pluginSourceChain) (string, error) {
			platforms, err := c.ListPlatforms(ctx, testRef("p", ""))
			if len(platforms) > 0 {
				return platforms[0], err
			}
			return "primary", err
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
					ctx := WithCredentialRejections(t.Context())

					_, err := op(ctx, newPluginSourceChain(logr.Discard(), nil, remote, fallback))
					require.Error(t, err)
					assert.NotErrorIs(t, err, ErrArtifactNotFound)
					assert.Zero(t, fallback.calls, "a registry failure must not fall through to a lower-priority source")
					if tc.rejected {
						assert.ErrorIs(t, err, errCredentialsRejected)
						assert.NotNil(t, CredentialRejection(ctx), "the rejection is recorded on the request context")
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
					ctx := WithCredentialRejections(t.Context())

					got, err := op(ctx, newPluginSourceChain(logr.Discard(), nil, remote, fallback))
					require.NoError(t, err)
					assert.Equal(t, "secondary", got)
					assert.Nil(t, CredentialRejection(ctx), "an anonymous catalog has no credentials to reject")
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
				ctx := WithCredentialRejections(t.Context())

				got, err := op(ctx, newPluginSourceChain(logr.Discard(), nil, remote, fallback))
				require.NoError(t, err)
				assert.Equal(t, "secondary", got)
				assert.Nil(t, CredentialRejection(ctx), "valid credentials must not be reported as rejected")
			})

			t.Run("credentialed 403 then anonymous success serves primary", func(t *testing.T) {
				t.Parallel()
				remote, h := newManifestStatusRemote(t)
				withCredentials(remote)
				h.credStatus.Store(http.StatusForbidden)
				fallback := okSource("secondary")
				ctx := WithCredentialRejections(t.Context())

				got, err := op(ctx, newPluginSourceChain(logr.Discard(), nil, remote, fallback))
				require.NoError(t, err)
				assert.Equal(t, "primary", got)
				assert.Zero(t, fallback.calls)
				assert.NotNil(t, CredentialRejection(ctx), "an anonymous result is reported as degraded")
				assert.Nil(t, CredentialRejection(t.Context()), "the record does not outlive the request context")
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

	newCat := func(credentialed bool, probe *probeResult, probes *atomic.Int32) *RemoteCatalog {
		cat := &RemoteCatalog{name: "primary", registry: "registry.example", client: &auth.Client{}, logger: logr.Discard()}
		if credentialed {
			withCredentials(cat)
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
		probe        *probeResult
		err          error
		wantNotFound bool
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
		{name: "anonymous other", err: assert.AnError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var probes atomic.Int32
			cat := newCat(tc.credentialed, tc.probe, &probes)
			err := cat.lookupError(t.Context(), cat.client, "resolving", ref, tc.err)
			require.Error(t, err)
			assert.Equal(t, tc.wantNotFound, IsNotFound(err))
			assert.NotErrorIs(t, err, errCredentialsRejected, "only retryAnonymously reports rejected credentials")
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
			ctx := WithCredentialRejections(t.Context())
			_, err := retryAnonymously(ctx, cat, "op", testRef("p", "1.0.0"), func(*auth.Client) (struct{}, error) {
				runs++
				return struct{}{}, tc.err
			})
			assert.Equal(t, tc.err, err)
			assert.Equal(t, tc.wantCalls, calls.Load(), "credential callback invocations")
			assert.Equal(t, 1, runs, "an anonymous catalog is not retried")
			assert.Nil(t, CredentialRejection(ctx))
		})
	}

	t.Run("credentialed auth failure retries with anonymous client without mutating catalog client", func(t *testing.T) {
		t.Parallel()
		baseClient := &auth.Client{Credential: auth.StaticCredential("r", auth.Credential{Username: "u", Password: "p"})}
		cat := &RemoteCatalog{name: "primary", registry: "r", logger: logr.Discard(), client: baseClient, authHandlerUsed: "github"}

		var runs int
		var first, second *auth.Client
		ctx := WithCredentialRejections(t.Context())
		_, err := retryAnonymously(ctx, cat, "op", testRef("p", "1.0.0"), func(client *auth.Client) (struct{}, error) {
			runs++
			if runs == 1 {
				first = client
				return struct{}{}, &errcode.ErrorResponse{StatusCode: http.StatusForbidden}
			}
			second = client
			return struct{}{}, nil
		})
		require.NoError(t, err)
		assert.Equal(t, 2, runs)
		assert.Same(t, baseClient, first, "first attempt must use the catalog client")
		assert.NotSame(t, baseClient, second, "retry must use a request-scoped anonymous client")
		assert.Same(t, baseClient, cat.client, "catalog client must remain unchanged")
		assert.Equal(t, &AuthDegradedError{Registry: "r", Handler: "github"}, CredentialRejection(ctx))
	})
}

// TestRemoteCatalog_LatestFetch_SingleIdentityPerAttempt verifies that a
// fetch without a version resolves the latest version with the same client
// as the rest of the attempt: once the credentialed attempt is rejected, the
// anonymous retry must not send credentials again.
func TestRemoteCatalog_LatestFetch_SingleIdentityPerAttempt(t *testing.T) {
	t.Parallel()

	ops := map[string]func(ctx context.Context, cat *RemoteCatalog) (ArtifactInfo, error){
		"Fetch": func(ctx context.Context, cat *RemoteCatalog) (ArtifactInfo, error) {
			_, info, err := cat.Fetch(ctx, testRef("p", ""))
			return info, err
		},
		"FetchWithBundle": func(ctx context.Context, cat *RemoteCatalog) (ArtifactInfo, error) {
			_, _, info, err := cat.FetchWithBundle(ctx, testRef("p", ""))
			return info, err
		},
		"FetchByPlatform": func(ctx context.Context, cat *RemoteCatalog) (ArtifactInfo, error) {
			_, info, err := cat.FetchByPlatform(ctx, testRef("p", ""), "linux/amd64")
			return info, err
		},
	}
	for name, op := range ops {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cat, h := newManifestStatusRemote(t)
			withCredentials(cat)
			h.credStatus.Store(http.StatusForbidden)

			info, err := op(t.Context(), cat)
			require.NoError(t, err)
			assert.Equal(t, "1.0.0", info.Reference.Version.String())
			assert.Equal(t, int32(1), h.credLookups.Load(),
				"only the first lookup of the credentialed attempt may send credentials")
		})
	}
}

// TestRemoteCatalog_VersionedFetch_RejectedCredentials verifies that fetches
// with an explicit version classify a manifest auth failure as an auth error,
// so the anonymous retry runs, and that a failed retry halts instead of being
// reported as not-found.
func TestRemoteCatalog_VersionedFetch_RejectedCredentials(t *testing.T) {
	t.Parallel()

	ref := testRef("p", "1.0.0")
	ops := map[string]func(ctx context.Context, cat *RemoteCatalog) (ArtifactInfo, error){
		"Fetch": func(ctx context.Context, cat *RemoteCatalog) (ArtifactInfo, error) {
			_, info, err := cat.Fetch(ctx, ref)
			return info, err
		},
		"FetchWithBundle": func(ctx context.Context, cat *RemoteCatalog) (ArtifactInfo, error) {
			_, _, info, err := cat.FetchWithBundle(ctx, ref)
			return info, err
		},
		"FetchWithLayer": func(ctx context.Context, cat *RemoteCatalog) (ArtifactInfo, error) {
			_, _, info, err := cat.FetchWithLayer(ctx, ref)
			return info, err
		},
	}
	for name, op := range ops {
		t.Run(name+" anonymous retry succeeds", func(t *testing.T) {
			t.Parallel()
			cat, h := newManifestStatusRemote(t)
			withCredentials(cat)
			h.credStatus.Store(http.StatusForbidden)

			info, err := op(t.Context(), cat)
			require.NoError(t, err)
			assert.Equal(t, "1.0.0", info.Reference.Version.String())
			assert.Equal(t, int32(1), h.credLookups.Load())
		})

		t.Run(name+" anonymous 404 halts", func(t *testing.T) {
			t.Parallel()
			cat, h := newManifestStatusRemote(t)
			withCredentials(cat)
			h.credStatus.Store(http.StatusForbidden)
			h.status.Store(http.StatusNotFound)

			_, err := op(t.Context(), cat)
			require.Error(t, err)
			require.ErrorIs(t, err, errCredentialsRejected)
			assert.NotErrorIs(t, err, ErrArtifactNotFound)
		})

		t.Run(name+" anonymous not found", func(t *testing.T) {
			t.Parallel()
			cat, h := newManifestStatusRemote(t)
			h.status.Store(http.StatusNotFound)

			_, err := op(t.Context(), cat)
			require.ErrorIs(t, err, ErrArtifactNotFound)
		})
	}
}

// TestRemoteCatalog_RejectedCredentials_NotReportedAsAbsent verifies that the
// paths which infer absence from a resolve error do not turn a rejected
// credential into a false negative: the artifact may exist but be hidden from
// anonymous access, so a strict source chain must halt instead of falling
// through to a lower-priority catalog.
func TestRemoteCatalog_RejectedCredentials_NotReportedAsAbsent(t *testing.T) {
	t.Parallel()

	ops := map[string]func(ctx context.Context, cat *RemoteCatalog) error{
		"Exists": func(ctx context.Context, cat *RemoteCatalog) error {
			exists, err := cat.Exists(ctx, testRef("p", "1.0.0"))
			if err == nil && !exists {
				return fmt.Errorf("reported absent: %w", ErrArtifactNotFound)
			}
			return err
		},
		"resolveByTag": func(ctx context.Context, cat *RemoteCatalog) error {
			_, err := cat.resolveByTag(ctx, testRef("p", "1.0.0"), "1.0.0")
			return err
		},
	}
	for name, op := range ops {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cat, h := newManifestStatusRemote(t)
			withCredentials(cat)
			h.credStatus.Store(http.StatusForbidden)
			h.status.Store(http.StatusNotFound)

			err := op(t.Context(), cat)
			require.ErrorIs(t, err, errCredentialsRejected)
			assert.NotErrorIs(t, err, ErrArtifactNotFound, "a hidden artifact must not be reported as absent")
		})
	}
}

// TestRemoteCatalog_CopyToLocal_RejectedCredentials verifies that copying makes
// the same anonymous fallback as resolving: pull resolves the reference first,
// so by the time CopyToLocal runs the configured client is intact and its
// manifest request is rejected. Both the verbatim copy and the --as rebuild
// must retry anonymously for the copy itself.
func TestRemoteCatalog_CopyToLocal_RejectedCredentials(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		target string
	}{
		{name: "verbatim"},
		{name: "renamed", target: "renamed@2.0.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cat, h := newManifestStatusRemote(t)
			withCredentials(cat)
			h.credStatus.Store(http.StatusForbidden)

			// Resolve first, as catalog pull does, exercising the anonymous
			// fallback without changing the catalog's configured client.
			info, err := cat.Resolve(t.Context(), testRef("p", "1.0.0"))
			require.NoError(t, err)

			local := newTestCatalog(t)
			_, err = cat.CopyToLocal(t.Context(), info.Reference, local, CopyOptions{TargetName: tc.target})
			require.NoError(t, err)
		})
	}
}

// TestRemoteCatalog_ListPlatforms_RejectedCredentials verifies that a versioned
// platform listing classifies its manifest denial as an auth failure, so the
// anonymous retry runs instead of the listing failing as not-found.
func TestRemoteCatalog_ListPlatforms_RejectedCredentials(t *testing.T) {
	t.Parallel()

	cat, h := newManifestStatusRemote(t)
	withCredentials(cat)
	h.credStatus.Store(http.StatusForbidden)

	_, err := cat.ListPlatforms(t.Context(), testRef("p", "1.0.0"))
	require.NoError(t, err)
	assert.Equal(t, int32(1), h.credLookups.Load(),
		"the credentialed attempt is rejected and retried anonymously")
}

// TestRemoteCatalog_RejectedCredentials_AnonymousRetrySkipsCredentialProbe
// verifies that when the anonymous retry itself is denied, classification does
// not fall back to the configured client: no extra credentialed /v2/ login
// probe and no extra credentialed manifest or tag-list request may be made.
func TestRemoteCatalog_RejectedCredentials_AnonymousRetrySkipsCredentialProbe(t *testing.T) {
	t.Parallel()

	ops := map[string]func(ctx context.Context, cat *RemoteCatalog) error{
		"ListPlatforms": func(ctx context.Context, cat *RemoteCatalog) error {
			_, err := cat.ListPlatforms(ctx, testRef("p", "1.0.0"))
			return err
		},
		"List": func(ctx context.Context, cat *RemoteCatalog) error {
			_, err := cat.List(ctx, ArtifactKindProvider, "p")
			return err
		},
	}
	for name, op := range ops {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cat, h := newManifestStatusRemote(t)
			withCredentials(cat)
			// Credentials are rejected, and so is anonymous access, so the
			// anonymous retry fails and must report rejected credentials.
			h.credStatus.Store(http.StatusForbidden)
			h.status.Store(http.StatusForbidden)

			err := op(t.Context(), cat)
			require.ErrorIs(t, err, errCredentialsRejected)
			assert.Equal(t, int32(1), h.authProbes.Load(),
				"only the credentialed attempt may probe the registry's credentials")
			assert.Equal(t, int32(1), h.credLookups.Load(),
				"only the credentialed attempt may send credentials to a manifest or tag list")
		})
	}
}

// TestRemoteCatalog_DeniedRepository_ValidCredentials verifies that a
// repository denial to credentials the registry accepts -- GHCR answers a
// logged-in request for a package that does not exist with 403 DENIED -- is
// classified as not-found by the attempt that hit it. It must not be mistaken
// for rejected credentials, even when anonymous access is denied too.
func TestRemoteCatalog_DeniedRepository_ValidCredentials(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T) (*RemoteCatalog, *manifestStatusHandler) {
		t.Helper()
		cat, h := newManifestStatusRemote(t)
		withCredentials(cat)
		h.credStatus.Store(http.StatusForbidden)
		h.credsValid.Store(true)
		h.status.Store(http.StatusForbidden)
		return cat, h
	}

	t.Run("Exists reports absent", func(t *testing.T) {
		t.Parallel()
		cat, h := setup(t)
		ctx := WithCredentialRejections(t.Context())

		exists, err := cat.Exists(ctx, testRef("q", "1.0.0"))
		require.NoError(t, err)
		assert.False(t, exists)
		assert.Nil(t, CredentialRejection(ctx), "valid credentials must not be reported as rejected")
		assert.Equal(t, int32(1), h.authProbes.Load(), "the denial is checked against the login once")
		assert.Equal(t, int32(1), h.credLookups.Load(), "no anonymous retry follows a repository denial")
	})

	t.Run("resolveByTag reports not found", func(t *testing.T) {
		t.Parallel()
		cat, h := setup(t)
		ctx := WithCredentialRejections(t.Context())

		_, err := cat.resolveByTag(ctx, testRef("q", "1.0.0"), "1.0.0")
		require.ErrorIs(t, err, ErrArtifactNotFound)
		assert.NotErrorIs(t, err, errCredentialsRejected)
		assert.Nil(t, CredentialRejection(ctx), "valid credentials must not be reported as rejected")
		assert.Equal(t, int32(1), h.credLookups.Load(), "no anonymous retry follows a repository denial")
	})

	t.Run("FetchIndex reports not found", func(t *testing.T) {
		t.Parallel()
		cat, h := setup(t)
		ctx := WithCredentialRejections(t.Context())

		_, err := cat.FetchIndex(ctx)
		require.ErrorIs(t, err, ErrArtifactNotFound)
		assert.NotErrorIs(t, err, errCredentialsRejected)
		assert.Nil(t, CredentialRejection(ctx), "valid credentials must not be reported as rejected")
		assert.Equal(t, int32(1), h.authProbes.Load(), "the denial is checked against the login once")
		assert.Equal(t, int32(1), h.credLookups.Load(), "no anonymous retry follows a repository denial")
	})

	t.Run("Store creates a missing package", func(t *testing.T) {
		t.Parallel()
		cat, h := setup(t)
		h.missingRepo.Store(true)
		ctx := WithCredentialRejections(t.Context())

		ref := testRef("q", "1.0.0")
		_, err := cat.Store(ctx, ref, []byte("binary"), nil, nil, false)
		require.NoError(t, err, "a denied existence check with valid credentials must not abort the push")
		assert.Nil(t, CredentialRejection(ctx))

		exists, err := cat.Exists(ctx, ref)
		require.NoError(t, err)
		assert.True(t, exists)
	})
}

// TestRemoteCatalog_MalformedRepository verifies that a repository path that
// cannot be constructed is reported as a configuration error, never as a
// missing artifact, and that no registry request is made.
func TestRemoteCatalog_MalformedRepository(t *testing.T) {
	t.Parallel()

	ops := map[string]func(ctx context.Context, cat *RemoteCatalog) error{
		"Exists": func(ctx context.Context, cat *RemoteCatalog) error {
			exists, err := cat.Exists(ctx, testRef("p", "1.0.0"))
			if err == nil && !exists {
				return fmt.Errorf("reported absent: %w", ErrArtifactNotFound)
			}
			return err
		},
		"resolveByTag": func(ctx context.Context, cat *RemoteCatalog) error {
			_, err := cat.resolveByTag(ctx, testRef("p", "1.0.0"), "1.0.0")
			return err
		},
	}
	for name, op := range ops {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var requests atomic.Int32
			ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusNotFound)
			}))
			t.Cleanup(ts.Close)

			cat := &RemoteCatalog{
				name:       "primary",
				registry:   strings.TrimPrefix(ts.URL, "https://"),
				repository: "INVALID//Repo",
				client:     &auth.Client{Client: ts.Client()},
				logger:     logr.Discard(),
			}

			err := op(t.Context(), cat)
			require.Error(t, err)
			require.ErrorIs(t, err, errdef.ErrInvalidReference)
			assert.NotErrorIs(t, err, ErrArtifactNotFound, "a malformed repository must not be reported as a missing artifact")
			assert.Contains(t, err.Error(), "failed to create repository")
			assert.Zero(t, requests.Load(), "no registry request is made for a malformed repository")
		})
	}
}

func TestRemoteCatalog_ManifestResolveError(t *testing.T) {
	t.Parallel()
	cat := &RemoteCatalog{name: "primary"}
	ref := testRef("p", "1.0.0")

	_, unreachable := IsCatalogUnreachable(cat.manifestResolveError(t.Context(), cat.client, ref, &netTimeoutError{}))
	assert.True(t, unreachable)

	err := cat.manifestResolveError(t.Context(), cat.client, ref, assert.AnError)
	require.ErrorIs(t, err, assert.AnError)
	assert.NotErrorIs(t, err, ErrArtifactNotFound)
	assert.Contains(t, err.Error(), `catalog "primary"`)
}

// netTimeoutError is a net.Error reporting a timeout.
type netTimeoutError struct{}

func (*netTimeoutError) Error() string   { return "timeout" }
func (*netTimeoutError) Timeout() bool   { return true }
func (*netTimeoutError) Temporary() bool { return true }
