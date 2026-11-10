// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry/remote/errcode"
)

func TestClassifyCredentialProbe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		presented bool
		err       error
		want      credentialVerdict
	}{
		{name: "presented and accepted", presented: true, want: credentialsAccepted},
		{name: "never presented", want: credentialsUnverified},
		{name: "presented and rejected with 401", presented: true, err: &errcode.ErrorResponse{StatusCode: http.StatusUnauthorized}, want: credentialsRejected},
		{name: "presented and rejected with 403", presented: true, err: &errcode.ErrorResponse{StatusCode: http.StatusForbidden}, want: credentialsRejected},
		{name: "denied without presenting", err: &errcode.ErrorResponse{StatusCode: http.StatusForbidden}, want: credentialsUnverified},
		{name: "server error", presented: true, err: &errcode.ErrorResponse{StatusCode: http.StatusInternalServerError}, want: credentialsUnverified},
		{name: "network error", presented: true, err: &netTimeoutError{}, want: credentialsUnverified},
		{name: "other error", presented: true, err: assert.AnError, want: credentialsUnverified},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, classifyCredentialProbe(tc.presented, tc.err))
		})
	}
}

func TestRemoteCatalog_CredentialsAccepted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		presented bool
		err       error
		want      bool
	}{
		{name: "accepted", presented: true, want: true},
		{name: "rejected", presented: true, err: &errcode.ErrorResponse{StatusCode: http.StatusUnauthorized}},
		{name: "unverified", presented: true, err: &netTimeoutError{}},
	}
	for _, tc := range tests {
		t.Run(tc.name+" is probed on every call", func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			cat := &RemoteCatalog{name: "primary", logger: logr.Discard()}
			cat.probeCredentials = func(context.Context) (bool, error) {
				calls.Add(1)
				return tc.presented, tc.err
			}

			assert.Equal(t, tc.want, cat.credentialsAccepted(t.Context()))
			assert.Equal(t, tc.want, cat.credentialsAccepted(t.Context()))
			assert.Equal(t, int32(2), calls.Load(), "probe invocations")
		})
	}

	// A token refreshed between lookups must be honored on the next one, and
	// one that expires must stop being trusted.
	t.Run("verdict follows the current credentials", func(t *testing.T) {
		t.Parallel()
		outcomes := []error{
			&errcode.ErrorResponse{StatusCode: http.StatusUnauthorized},
			nil,
			&errcode.ErrorResponse{StatusCode: http.StatusUnauthorized},
		}
		var calls atomic.Int32
		cat := &RemoteCatalog{name: "primary", logger: logr.Discard()}
		cat.probeCredentials = func(context.Context) (bool, error) {
			return true, outcomes[calls.Add(1)-1]
		}

		assert.False(t, cat.credentialsAccepted(t.Context()), "expired token")
		assert.True(t, cat.credentialsAccepted(t.Context()), "refreshed token")
		assert.False(t, cat.credentialsAccepted(t.Context()), "expired again")
	})
}

// pingHandler serves only the registry base endpoint (/v2/). With challenge
// set, an unauthenticated request is challenged for Basic credentials and a
// credentialed one is answered with credStatus (200 when unset).
type pingHandler struct {
	challenge  bool
	credStatus int
	paths      []string
	mu         sync.Mutex
}

func (h *pingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.paths = append(h.paths, r.URL.Path)
	h.mu.Unlock()

	if h.challenge {
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
			writeRegistryError(w, http.StatusUnauthorized)
			return
		}
		if h.credStatus != 0 {
			writeRegistryError(w, h.credStatus)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}

func TestRemoteCatalog_PingWithCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		handler       *pingHandler
		wantPresented bool
		wantStatus    int
	}{
		{name: "valid credentials", handler: &pingHandler{challenge: true}, wantPresented: true},
		{name: "rejected credentials", handler: &pingHandler{challenge: true, credStatus: http.StatusForbidden}, wantPresented: true, wantStatus: http.StatusForbidden},
		{name: "open endpoint never asks for credentials", handler: &pingHandler{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ts := httptest.NewTLSServer(tc.handler)
			t.Cleanup(ts.Close)

			cat, err := NewRemoteCatalog(RemoteCatalogConfig{
				Name:     "primary",
				Registry: strings.TrimPrefix(ts.URL, "https://"),
				Insecure: true,
				Logger:   logr.Discard(),
			})
			require.NoError(t, err)
			cat.client.Client = ts.Client()
			withCredentials(cat)

			presented, err := cat.pingWithCredentials(t.Context())
			assert.Equal(t, tc.wantPresented, presented)
			if tc.wantStatus == 0 {
				require.NoError(t, err)
			} else {
				var resp *errcode.ErrorResponse
				require.ErrorAs(t, err, &resp)
				assert.Equal(t, tc.wantStatus, resp.StatusCode)
			}
			assert.Contains(t, tc.handler.paths, "/v2/")
		})
	}
}

// ghcrTokenHandler mimics GHCR's token auth: the /v2/ challenge hints a
// repository scope, and the token endpoint denies any scoped request (the
// repository does not exist) while issuing an unscoped token only to valid
// Basic credentials.
type ghcrTokenHandler struct {
	password    string
	tokenScopes []string
	mu          sync.Mutex
}

const ghcrTestToken = "ghcr-token"

func (h *ghcrTokenHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/token":
		h.mu.Lock()
		h.tokenScopes = append(h.tokenScopes, r.URL.Query()["scope"]...)
		h.mu.Unlock()
		if r.URL.Query().Has("scope") {
			writeRegistryError(w, http.StatusForbidden)
			return
		}
		if user, pass, ok := r.BasicAuth(); !ok || user != "user" || pass != h.password {
			writeRegistryError(w, http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"` + ghcrTestToken + `"}`))
	case "/v2/":
		if r.Header.Get("Authorization") == "Bearer "+ghcrTestToken {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("WWW-Authenticate",
			`Bearer realm="https://`+r.Host+`/token",service="ghcr.io",scope="repository:user/image:pull"`)
		writeRegistryError(w, http.StatusUnauthorized)
	default:
		writeRegistryError(w, http.StatusNotFound)
	}
}

func TestRemoteCatalog_PingWithCredentials_ScopedChallenge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		password     string
		wantAccepted bool
	}{
		{name: "valid credentials are accepted", password: "secret", wantAccepted: true},
		{name: "invalid credentials are rejected", password: "rotated"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := &ghcrTokenHandler{password: tc.password}
			ts := httptest.NewTLSServer(h)
			t.Cleanup(ts.Close)

			cat, err := NewRemoteCatalog(RemoteCatalogConfig{
				Name:     "primary",
				Registry: strings.TrimPrefix(ts.URL, "https://"),
				Insecure: true,
				Logger:   logr.Discard(),
			})
			require.NoError(t, err)
			cat.client.Client = ts.Client()
			withCredentials(cat)

			presented, err := cat.pingWithCredentials(t.Context())
			assert.True(t, presented)
			if tc.wantAccepted {
				require.NoError(t, err)
			} else {
				var resp *errcode.ErrorResponse
				require.ErrorAs(t, err, &resp)
				assert.Equal(t, http.StatusForbidden, resp.StatusCode)
			}
			assert.Equal(t, tc.wantAccepted, cat.credentialsAccepted(t.Context()))
			assert.Empty(t, h.tokenScopes, "token requests must not carry a scope")
		})
	}
}

func TestStripChallengeScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		challenge string
		want      string
	}{
		{name: "scope last", challenge: `Bearer realm="https://r/token",service="s",scope="repository:a/b:pull"`, want: `Bearer realm="https://r/token",service="s"`},
		{name: "scope first", challenge: `Bearer scope="repository:a/b:pull",realm="https://r/token",service="s"`, want: `Bearer realm="https://r/token",service="s"`},
		{name: "scope in the middle", challenge: `Bearer realm="https://r/token",scope="repository:a/b:pull",service="s"`, want: `Bearer realm="https://r/token",service="s"`},
		{name: "spaced separators", challenge: `Bearer realm="https://r/token", scope="repository:a/b:pull", service="s"`, want: `Bearer realm="https://r/token", service="s"`},
		{name: "multiple scopes in one value", challenge: `Bearer realm="https://r/token",scope="repository:a/b:pull repository:c/d:pull"`, want: `Bearer realm="https://r/token"`},
		{name: "case-insensitive name", challenge: `Bearer realm="https://r/token",Scope="repository:a/b:pull"`, want: `Bearer realm="https://r/token"`},
		{name: "no scope", challenge: `Bearer realm="https://r/token",service="s"`, want: `Bearer realm="https://r/token",service="s"`},
		{name: "basic challenge", challenge: `Basic realm="test"`, want: `Basic realm="test"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, stripChallengeScope(tc.challenge))
		})
	}
}

// staticRoundTripper answers every request with a copy of its response.
type staticRoundTripper struct {
	status int
	header http.Header
	err    error
}

func (s staticRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &http.Response{StatusCode: s.status, Header: s.header.Clone(), Body: http.NoBody}, nil
}

func TestUnscopedChallenge_RoundTrip(t *testing.T) {
	t.Parallel()

	const scoped = `Bearer realm="https://r/token",scope="repository:a/b:pull"`
	const unscoped = `Bearer realm="https://r/token"`
	tests := []struct {
		name   string
		next   staticRoundTripper
		want   []string
		wantOK bool
	}{
		{name: "401 challenge is stripped", next: staticRoundTripper{status: http.StatusUnauthorized, header: http.Header{"Www-Authenticate": {scoped}}}, want: []string{unscoped}, wantOK: true},
		{name: "every challenge is stripped", next: staticRoundTripper{status: http.StatusUnauthorized, header: http.Header{"Www-Authenticate": {scoped, `Basic realm="test"`}}}, want: []string{unscoped, `Basic realm="test"`}, wantOK: true},
		{name: "401 without challenge", next: staticRoundTripper{status: http.StatusUnauthorized, header: http.Header{}}, wantOK: true},
		{name: "non-401 is untouched", next: staticRoundTripper{status: http.StatusForbidden, header: http.Header{"Www-Authenticate": {scoped}}}, want: []string{scoped}, wantOK: true},
		{name: "transport error is returned", next: staticRoundTripper{err: assert.AnError}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://r/v2/", http.NoBody)
			resp, err := unscopedChallenge{next: tc.next}.RoundTrip(req)
			if !tc.wantOK {
				require.ErrorIs(t, err, assert.AnError)
				return
			}
			require.NoError(t, err)
			t.Cleanup(func() { _ = resp.Body.Close() })
			assert.Equal(t, tc.want, resp.Header.Values("WWW-Authenticate"))
		})
	}
}

func TestUnscopedHTTPClient(t *testing.T) {
	t.Parallel()

	t.Run("nil base wraps the default transport", func(t *testing.T) {
		t.Parallel()
		got := unscopedHTTPClient(nil)
		require.IsType(t, unscopedChallenge{}, got.Transport)
		assert.Equal(t, http.DefaultTransport, got.Transport.(unscopedChallenge).next)
		assert.Nil(t, http.DefaultClient.Transport, "default client must not be modified")
	})

	t.Run("base transport is kept and base is not modified", func(t *testing.T) {
		t.Parallel()
		inner := staticRoundTripper{status: http.StatusOK}
		base := &http.Client{Transport: inner}
		got := unscopedHTTPClient(base)
		require.IsType(t, unscopedChallenge{}, got.Transport)
		assert.Equal(t, inner, got.Transport.(unscopedChallenge).next)
		assert.Equal(t, inner, base.Transport)
	})
}
