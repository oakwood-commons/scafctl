// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hostname

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/config"
)

func TestDefaultFetch_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		assert.Equal(t, "custom-value", r.Header.Get("X-Custom"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer srv.Close()

	src := config.HostnameResolverSource{
		URL:     srv.URL,
		Headers: map[string]string{"X-Custom": "custom-value"},
	}

	body, err := defaultFetch(context.Background(), src, "secret")
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok": true}`, string(body))
}

func TestDefaultFetch_NoBearer(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	_, err := defaultFetch(context.Background(), config.HostnameResolverSource{URL: srv.URL}, "")
	require.NoError(t, err)
}

func TestDefaultFetch_NonOKStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := defaultFetch(context.Background(), config.HostnameResolverSource{URL: srv.URL}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 404")
}

func TestDefaultFetch_InvalidURL(t *testing.T) {
	t.Parallel()

	tests := []string{
		"",
		"not-a-url",
		"ftp://example.com/inventory",
		"file:///etc/passwd",
	}
	for _, u := range tests {
		t.Run(u, func(t *testing.T) {
			t.Parallel()
			_, err := defaultFetch(context.Background(), config.HostnameResolverSource{URL: u}, "")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid inventory source URL")
		})
	}
}

func TestDefaultFetch_RejectsBearerOverPlaintextHTTP(t *testing.T) {
	t.Parallel()

	// A bearer token must never be sent to a non-HTTPS, non-loopback host.
	src := config.HostnameResolverSource{URL: "http://inventory.example.com/clusters"}
	_, err := defaultFetch(context.Background(), src, "secret")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-HTTPS")
}

func TestDefaultFetch_AllowsBearerOverLoopbackHTTP(t *testing.T) {
	t.Parallel()

	// Loopback is exempt: the token never leaves the machine. httptest serves
	// on 127.0.0.1, so a bearer over http here must be accepted.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	_, err := defaultFetch(context.Background(), config.HostnameResolverSource{URL: srv.URL}, "secret")
	require.NoError(t, err)
}
