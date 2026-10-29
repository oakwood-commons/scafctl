// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package load

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
)

// newTestClient returns a non-caching, non-retrying client. When allowLoopback
// is true it may reach httptest servers, which always bind loopback.
func newTestClient(t testing.TB, allowLoopback bool) *httpc.Client {
	t.Helper()
	cfg := httpc.DefaultConfig()
	cfg.EnableCache = false
	cfg.RetryMax = 0
	if allowLoopback {
		policy, err := httpc.PolicyFromAppConfig(&config.HTTPClientConfig{
			AllowedPrivateCIDRs: config.PrivateCIDRList("127.0.0.0/8", "::1/128"),
		})
		require.NoError(t, err)
		cfg.IPPolicy = policy
	}
	return httpc.NewClient(cfg)
}

func serve(t testing.TB, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestURLLoader_Load(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		status        int
		body          string
		ref           string // overrides the server URL when set
		blockLoopback bool
		opts          []URLOption
		wantResolvers []string
		errMsg        string
	}{
		{
			name:   "valid solution",
			status: http.StatusOK,
			body:   validSolutionYAML,
		},
		{
			name:   "compose is parsed but not applied",
			status: http.StatusOK,
			body:   composeRootYAML,
		},
		{
			name:   "strict validation failure",
			status: http.StatusOK,
			body:   undefinedDepJSON,
			errMsg: "loading solution from URL",
		},
		{
			name:          "lenient skips spec validation",
			status:        http.StatusOK,
			body:          undefinedDepJSON,
			opts:          []URLOption{WithLenient()},
			wantResolvers: []string{"app"},
		},
		{
			name:   "parse error",
			status: http.StatusOK,
			body:   "not valid yaml: [",
			errMsg: "loading solution from URL",
		},
		{
			name:   "non-200 response",
			status: http.StatusNotFound,
			errMsg: "non-200 response",
		},
		{
			name:   "invalid URL",
			ref:    "not-a-url",
			errMsg: "not a valid URL",
		},
		{
			name:          "blocked by policy",
			status:        http.StatusOK,
			body:          validSolutionYAML,
			blockLoopback: true,
			errMsg:        "fetching solution from URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ref := tt.ref
			if ref == "" {
				ref = serve(t, tt.status, tt.body).URL
			}
			l := NewURLLoader(newTestClient(t, !tt.blockLoopback), tt.opts...)
			c := source.Classification{Kind: source.KindURL, Ref: ref}

			got, err := l.Load(t.Context(), c)

			if tt.errMsg != "" {
				require.Error(t, err)
				assert.Nil(t, got)
				assert.Contains(t, err.Error(), tt.errMsg)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, got)
			require.NotNil(t, got.Solution)
			assert.Equal(t, ref, got.Solution.GetPath())
			for _, name := range tt.wantResolvers {
				assert.Contains(t, got.Solution.Spec.Resolvers, name)
			}
			assert.Equal(t, c, got.Source)
			assert.Nil(t, got.Bundle)
			assert.Nil(t, got.Lock)
		})
	}
}

func TestURLLoader_Load_UnsupportedKinds(t *testing.T) {
	t.Parallel()

	for _, kind := range allKinds {
		if kind == source.KindURL {
			continue
		}
		t.Run(kind.String(), func(t *testing.T) {
			t.Parallel()
			got, err := NewURLLoader(nil).Load(t.Context(), source.Classification{Kind: kind, Ref: "ref"})
			require.ErrorIs(t, err, ErrUnsupportedKind)
			assert.Nil(t, got)
			assert.Contains(t, err.Error(), kind.String())
		})
	}
}

func TestNewURLLoader(t *testing.T) {
	t.Parallel()

	t.Run("nil client uses secure default", func(t *testing.T) {
		t.Parallel()
		l := NewURLLoader(nil)
		assert.NotNil(t, l.client)
		assert.False(t, l.lenient)
	})

	t.Run("custom client and options", func(t *testing.T) {
		t.Parallel()
		client := newTestClient(t, false)
		l := NewURLLoader(client, WithLenient())
		assert.Same(t, client, l.client)
		assert.True(t, l.lenient)
	})
}

func BenchmarkURLLoader_Load(b *testing.B) {
	srv := serve(b, http.StatusOK, validSolutionYAML)
	l := NewURLLoader(newTestClient(b, true))
	c := source.Classification{Kind: source.KindURL, Ref: srv.URL}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := l.Load(b.Context(), c); err != nil {
			b.Fatal(err)
		}
	}
}
