// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package oauth2

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler_GitUsernameAndFingerprints(t *testing.T) {
	t.Run("configured GitUsername returned by default and when logged out", func(t *testing.T) {
		store := secrets.NewMockStore()
		h, err := New(config.CustomOAuth2Config{
			Name:        "gitlab",
			TokenURL:    "https://git.example.com/oauth/token",
			ClientID:    "app",
			Token:       "env://DUMMY_TOK",
			DefaultFlow: "pat",
			GitHost:     "git.example.com",
			GitUsername: "oauth2",
		}, WithSecretStore(store))
		require.NoError(t, err)
		assert.Equal(t, "oauth2", h.GitUsername())
		assert.NoError(t, h.Logout(context.Background()))
		assert.Equal(t, "oauth2", h.GitUsernameForContext(context.Background()))
	})

	t.Run("PAT flow resolves configured Username ahead of GitUsername", func(t *testing.T) {
		store := secrets.NewMockStore()
		t.Setenv("TEST_GIT_PAT_VAL", "pat_secret_123")

		h, err := New(config.CustomOAuth2Config{
			Name:        "gitea-pat",
			DefaultFlow: "pat",
			Token:       "env://TEST_GIT_PAT_VAL",
			Username:    "ci-bot",
			GitHost:     "git.example.com",
			GitUsername: "oauth2",
		}, WithSecretStore(store))
		require.NoError(t, err)

		ctx := auth.WithProfile(context.Background(), "ci")
		ctx = auth.WithProfileResolved(ctx)
		_, err = h.Login(ctx, auth.LoginOptions{Flow: auth.FlowPAT})
		require.NoError(t, err)

		assert.Equal(t, "ci-bot", h.GitUsernameForContext(ctx))

		otherCtx := auth.WithProfile(context.Background(), "other")
		otherCtx = auth.WithProfileResolved(otherCtx)
		require.NoError(t, h.Logout(otherCtx))
		assert.Equal(t, "oauth2", h.GitUsernameForContext(otherCtx))
	})

	t.Run("TokenExchange populatesHostnameAndUsernameFromGitFields", func(t *testing.T) {
		var gotBody map[string]string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/oauth/token":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token": "primary-token",
					"token_type":   "Bearer",
					"expires_in":   3600,
				})
			case "/exchange":
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"token": "derived-git-token",
					"user":  "derived-git-user",
				})
			default:
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()

		store := secrets.NewMockStore()
		cfgA := config.CustomOAuth2Config{
			Name:         "exchange-git",
			TokenURL:     srv.URL + "/oauth/token",
			ClientID:     "client-a",
			ClientSecret: "static-client-secret",
			DefaultFlow:  "client_credentials",
			GitHost:      "git.example.com",
			GitUsername:  "git-bot",
			TokenExchange: &config.TokenExchangeConfig{
				URL:              srv.URL + "/exchange",
				RequestBody:      `{"host":"{{.Hostname}}","user":"{{.Username}}"}`,
				TokenJSONPath:    "token",
				UsernameJSONPath: "user",
			},
		}
		hA, err := New(cfgA, WithSecretStore(store), WithHTTPClient(srv.Client()))
		require.NoError(t, err)

		tok, err := hA.GetToken(context.Background(), auth.TokenOptions{})
		require.NoError(t, err)
		assert.Equal(t, "derived-git-token", tok.AccessToken)
		assert.Equal(t, "derived-git-user", hA.GitUsernameForContext(context.Background()))
		assert.Equal(t, "git.example.com", gotBody["host"])
		assert.Equal(t, "git-bot", gotBody["user"])

		// Changing GitHost alters both patFingerprint and derivedFingerprint.
		cfgB := cfgA
		cfgB.GitHost = "other-git.example.com"
		hB, err := New(cfgB, WithSecretStore(store))
		require.NoError(t, err)
		assert.NotEqual(t, hA.patFingerprint(), hB.patFingerprint())
		assert.NotEqual(t,
			hA.derivedFingerprint(context.Background(), auth.FlowClientCredentials),
			hB.derivedFingerprint(context.Background(), auth.FlowClientCredentials),
		)
	})
}
