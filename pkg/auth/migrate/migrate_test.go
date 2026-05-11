// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package migrate

import (
	"context"
	"errors"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	authofficial "github.com/oakwood-commons/scafctl/pkg/auth/official"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandlers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		officialNames  []authofficial.AuthHandler
		authHandlers   map[string]*auth.MockHandler
		fetchResults   []plugin.FetchResult
		fetchErr       error
		expectStatuses map[string]Status
		expectTokenMsg map[string]string
		expectSource   map[string]string
	}{
		{
			name: "all handlers fetched successfully with cached tokens",
			officialNames: []authofficial.AuthHandler{
				{Name: "github", CatalogRef: "github", DefaultVersion: "latest"},
				{Name: "entra", CatalogRef: "entra", DefaultVersion: "latest"},
			},
			authHandlers: map[string]*auth.MockHandler{
				"github": func() *auth.MockHandler {
					m := auth.NewMockHandler("github")
					m.ListCachedTokensResult = []*auth.CachedTokenInfo{
						{IsExpired: false},
						{IsExpired: false},
					}
					return m
				}(),
				"entra": func() *auth.MockHandler {
					m := auth.NewMockHandler("entra")
					m.ListCachedTokensResult = []*auth.CachedTokenInfo{
						{IsExpired: false},
					}
					return m
				}(),
			},
			fetchResults: []plugin.FetchResult{
				{Name: "github", Version: "0.1.2", FromCache: true},
				{Name: "entra", Version: "0.1.0", FromCache: false},
			},
			expectStatuses: map[string]Status{
				"github": StatusReady,
				"entra":  StatusReady,
			},
			expectTokenMsg: map[string]string{
				"github": "2 cached token(s) validated successfully",
				"entra":  "1 cached token(s) validated successfully",
			},
			expectSource: map[string]string{
				"github": "cached (0.1.2)",
				"entra":  "installed (0.1.0)",
			},
		},
		{
			name: "handler not in auth registry - no cached tokens",
			officialNames: []authofficial.AuthHandler{
				{Name: "gcp", CatalogRef: "gcp", DefaultVersion: "latest"},
			},
			authHandlers: map[string]*auth.MockHandler{},
			fetchResults: []plugin.FetchResult{
				{Name: "gcp", Version: "0.1.1", FromCache: false},
			},
			expectStatuses: map[string]Status{
				"gcp": StatusReady,
			},
			expectTokenMsg: map[string]string{
				"gcp": "no cached tokens (login required after migration)",
			},
			expectSource: map[string]string{
				"gcp": "installed (0.1.1)",
			},
		},
		{
			name: "fetch error - all handlers fail",
			officialNames: []authofficial.AuthHandler{
				{Name: "github", CatalogRef: "github", DefaultVersion: "latest"},
			},
			authHandlers: map[string]*auth.MockHandler{},
			fetchResults: nil,
			fetchErr:     errors.New("network timeout"),
			expectStatuses: map[string]Status{
				"github": StatusFailed,
			},
			expectTokenMsg: map[string]string{
				"github": "skipped (plugin not available)",
			},
		},
		{
			name: "handler with expired tokens",
			officialNames: []authofficial.AuthHandler{
				{Name: "entra", CatalogRef: "entra", DefaultVersion: "latest"},
			},
			authHandlers: map[string]*auth.MockHandler{
				"entra": func() *auth.MockHandler {
					m := auth.NewMockHandler("entra")
					m.ListCachedTokensResult = []*auth.CachedTokenInfo{
						{IsExpired: false},
						{IsExpired: true},
					}
					return m
				}(),
			},
			fetchResults: []plugin.FetchResult{
				{Name: "entra", Version: "0.1.0", FromCache: true},
			},
			expectStatuses: map[string]Status{
				"entra": StatusReady,
			},
			expectTokenMsg: map[string]string{
				"entra": "2 cached token(s), 1 expired",
			},
		},
		{
			name: "handler with token list error",
			officialNames: []authofficial.AuthHandler{
				{Name: "github", CatalogRef: "github", DefaultVersion: "latest"},
			},
			authHandlers: map[string]*auth.MockHandler{
				"github": func() *auth.MockHandler {
					m := auth.NewMockHandler("github")
					m.ListCachedTokensErr = errors.New("keyring unavailable")
					return m
				}(),
			},
			fetchResults: []plugin.FetchResult{
				{Name: "github", Version: "0.1.2", FromCache: true},
			},
			expectStatuses: map[string]Status{
				"github": StatusFailed,
			},
			expectTokenMsg: map[string]string{
				"github": "could not read cached tokens: keyring unavailable",
			},
		},
		{
			name: "handler not found in catalog",
			officialNames: []authofficial.AuthHandler{
				{Name: "github", CatalogRef: "github", DefaultVersion: "latest"},
			},
			authHandlers: map[string]*auth.MockHandler{},
			fetchResults: []plugin.FetchResult{},
			expectStatuses: map[string]Status{
				"github": StatusFailed,
			},
			expectTokenMsg: map[string]string{
				"github": "skipped (plugin not available)",
			},
		},
		{
			name: "embedder handler where Name differs from CatalogRef",
			officialNames: []authofficial.AuthHandler{
				{Name: "my-auth", CatalogRef: "my-auth-oci-ref", DefaultVersion: "latest"},
			},
			authHandlers: map[string]*auth.MockHandler{
				"my-auth": func() *auth.MockHandler {
					m := auth.NewMockHandler("my-auth")
					m.ListCachedTokensResult = []*auth.CachedTokenInfo{
						{IsExpired: false},
					}
					return m
				}(),
			},
			fetchResults: []plugin.FetchResult{
				{Name: "my-auth-oci-ref", Version: "1.0.0", FromCache: false},
			},
			expectStatuses: map[string]Status{
				"my-auth": StatusReady,
			},
			expectTokenMsg: map[string]string{
				"my-auth": "1 cached token(s) validated successfully",
			},
			expectSource: map[string]string{
				"my-auth": "installed (1.0.0)",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			officialReg := authofficial.NewRegistryFrom(tc.officialNames)

			authReg := auth.NewRegistry()
			for _, mock := range tc.authHandlers {
				require.NoError(t, authReg.Register(mock))
			}

			fetchFn := func(_ context.Context, _ []solution.PluginDependency) ([]plugin.FetchResult, error) {
				return tc.fetchResults, tc.fetchErr
			}

			results := Handlers(context.Background(), officialReg, authReg, fetchFn)

			for _, r := range results {
				if expectedStatus, ok := tc.expectStatuses[r.Name]; ok {
					assert.Equal(t, expectedStatus, r.Status, "handler %s status mismatch", r.Name)
				}
				if expectedMsg, ok := tc.expectTokenMsg[r.Name]; ok {
					assert.Equal(t, expectedMsg, r.TokenMessage, "handler %s token message mismatch", r.Name)
				}
				if expectedSrc, ok := tc.expectSource[r.Name]; ok {
					assert.Equal(t, expectedSrc, r.PluginSource, "handler %s source mismatch", r.Name)
				}
			}
		})
	}
}

func TestValidateTokenMigration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		handler   *auth.MockHandler
		queryName string
		expectMsg string
		expectOK  bool
	}{
		{
			name:      "nil registry",
			handler:   nil,
			queryName: "github",
			expectMsg: "no cached tokens (login required after migration)",
			expectOK:  true,
		},
		{
			name:      "handler not registered",
			handler:   auth.NewMockHandler("entra"),
			queryName: "github",
			expectMsg: "no cached tokens (login required after migration)",
			expectOK:  true,
		},
		{
			name: "handler with valid tokens",
			handler: func() *auth.MockHandler {
				m := auth.NewMockHandler("github")
				m.ListCachedTokensResult = []*auth.CachedTokenInfo{
					{IsExpired: false},
					{IsExpired: false},
				}
				return m
			}(),
			queryName: "github",
			expectMsg: "2 cached token(s) validated successfully",
			expectOK:  true,
		},
		{
			name: "handler with mixed tokens",
			handler: func() *auth.MockHandler {
				m := auth.NewMockHandler("gcp")
				m.ListCachedTokensResult = []*auth.CachedTokenInfo{
					{IsExpired: false},
					{IsExpired: true},
					{IsExpired: true},
				}
				return m
			}(),
			queryName: "gcp",
			expectMsg: "3 cached token(s), 2 expired",
			expectOK:  true,
		},
		{
			name: "handler with no tokens",
			handler: func() *auth.MockHandler {
				m := auth.NewMockHandler("github")
				m.ListCachedTokensResult = []*auth.CachedTokenInfo{}
				return m
			}(),
			queryName: "github",
			expectMsg: "no cached tokens (login required after migration)",
			expectOK:  true,
		},
		{
			name: "handler with token list error",
			handler: func() *auth.MockHandler {
				m := auth.NewMockHandler("github")
				m.ListCachedTokensErr = errors.New("keyring unavailable")
				return m
			}(),
			queryName: "github",
			expectMsg: "could not read cached tokens: keyring unavailable",
			expectOK:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var authReg *auth.Registry
			if tc.handler != nil {
				authReg = auth.NewRegistry()
				require.NoError(t, authReg.Register(tc.handler))
			}

			msg, ok := validateTokenMigration(context.Background(), authReg, tc.queryName)
			assert.Equal(t, tc.expectMsg, msg)
			assert.Equal(t, tc.expectOK, ok)
		})
	}
}

func TestResult_Methods(t *testing.T) {
	t.Parallel()

	t.Run("ready result", func(t *testing.T) {
		t.Parallel()
		r := &Result{Status: StatusReady}
		assert.True(t, r.IsReady())
		assert.Equal(t, "READY", r.StatusString())
	})

	t.Run("failed result", func(t *testing.T) {
		t.Parallel()
		r := &Result{Status: StatusFailed}
		assert.False(t, r.IsReady())
		assert.Equal(t, "FAILED", r.StatusString())
	})
}
