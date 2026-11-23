// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package oauth2

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestOAuthServer(t testing.TB) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		grantType := r.FormValue("grant_type")
		switch grantType {
		case "authorization_code":
			code := r.FormValue("code")
			if code == "bad_code" {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]string{"error": "invalid_grant", "error_description": "bad code"})
				return
			}
			writeJSON(w, tokenResponse{AccessToken: "test-access-token", TokenType: "Bearer", ExpiresIn: 3600, RefreshToken: "test-refresh-token", Scope: r.FormValue("scope")})
		case "urn:ietf:params:oauth:grant-type:device_code":
			deviceCode := r.FormValue("device_code")
			if deviceCode == "pending" {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]string{"error": "authorization_pending"})
				return
			}
			if deviceCode == "expired" {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]string{"error": "expired_token"})
				return
			}
			writeJSON(w, tokenResponse{AccessToken: "device-access-token", TokenType: "Bearer", ExpiresIn: 3600, Scope: r.FormValue("scope")})
		case "client_credentials":
			if r.FormValue("client_secret") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				writeJSON(w, map[string]string{"error": "invalid_client"})
				return
			}
			writeJSON(w, tokenResponse{AccessToken: "cc-access-token", TokenType: "Bearer", ExpiresIn: 3600, Scope: r.FormValue("scope")})
		case "refresh_token":
			if r.FormValue("refresh_token") == "bad-refresh" {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]string{"error": "invalid_grant"})
				return
			}
			writeJSON(w, tokenResponse{AccessToken: "refreshed-access-token", TokenType: "Bearer", ExpiresIn: 3600, Scope: r.FormValue("scope")})
		default:
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]string{"error": "unsupported_grant_type"})
		}
	})
	mux.HandleFunc("/device/code", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"device_code": "test-device-code", "user_code": "ABCD-1234", "verification_uri": "https://example.com/activate", "expires_in": 900, "interval": 1})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || authHeader == "Bearer invalid" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		writeJSON(w, map[string]string{"login": "testuser", "email": "testuser@example.com", "name": "Test User"})
	})
	mux.HandleFunc("/exchange", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		writeJSON(w, map[string]any{"token": "exchanged-token", "username": "exchange-user"})
	})
	return httptest.NewServer(mux)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func newTestHandler(t testing.TB, srv *httptest.Server, cfgOverride func(*config.CustomOAuth2Config)) (*Handler, *secrets.MockStore) {
	t.Helper()
	store := secrets.NewMockStore()
	cfg := config.CustomOAuth2Config{Name: "test-provider", DisplayName: "Test Provider", TokenURL: srv.URL + "/token", ClientID: "test-client-id", Scopes: []string{"read", "write"}}
	if cfgOverride != nil {
		cfgOverride(&cfg)
	}
	h, err := New(cfg, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)
	return h, store
}

func TestNew(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, nil)
	assert.Equal(t, "test-provider", h.Name())
	assert.Equal(t, "Test Provider", h.DisplayName())
	assert.NotNil(t, h.tokenCache)
}

func TestNew_DisplayNameFallback(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) { cfg.DisplayName = "" })
	assert.Equal(t, "test-provider", h.DisplayName())
}

func TestHandler_SupportedFlows(t *testing.T) {
	tests := []struct {
		name     string
		override func(*config.CustomOAuth2Config)
		expected []auth.Flow
	}{
		{name: "no flows", expected: nil},
		{name: "interactive", override: func(c *config.CustomOAuth2Config) { c.AuthorizeURL = "https://x.com/auth" }, expected: []auth.Flow{auth.FlowInteractive}},
		{name: "device code", override: func(c *config.CustomOAuth2Config) { c.DeviceAuthURL = "https://x.com/device" }, expected: []auth.Flow{auth.FlowDeviceCode}},
		{name: "workload identity", override: func(c *config.CustomOAuth2Config) { c.FederatedToken = "env://OIDC_TOKEN" }, expected: []auth.Flow{auth.FlowWorkloadIdentity}},
		{name: "client credentials", override: func(c *config.CustomOAuth2Config) { c.ClientSecret = "s" }, expected: []auth.Flow{auth.FlowClientCredentials}},
		{name: "pat", override: func(c *config.CustomOAuth2Config) { c.Token = "secret://pat-tok" }, expected: []auth.Flow{auth.FlowPAT}},
		{name: "all flows", override: func(c *config.CustomOAuth2Config) {
			c.AuthorizeURL = "https://x.com/auth"
			c.DeviceAuthURL = "https://x.com/device"
			c.FederatedTokenFile = "/var/run/secrets/token"
			c.ClientSecret = "s"
			c.Token = "secret://pat-tok"
		}, expected: []auth.Flow{auth.FlowInteractive, auth.FlowDeviceCode, auth.FlowWorkloadIdentity, auth.FlowClientCredentials, auth.FlowPAT}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTestOAuthServer(t)
			defer srv.Close()
			h, _ := newTestHandler(t, srv, tc.override)
			assert.Equal(t, tc.expected, h.SupportedFlows())
		})
	}
}

func TestHandler_Capabilities(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, nil)
	caps := h.Capabilities()
	assert.Contains(t, caps, auth.CapScopesOnLogin)
	assert.Contains(t, caps, auth.CapCallbackPort)
	assert.Contains(t, caps, auth.CapFlowOverride)
}

func TestHandler_Login_ClientCredentials(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, store := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) { cfg.ClientSecret = "test-secret" })
	result, err := h.Login(context.Background(), auth.LoginOptions{Flow: auth.FlowClientCredentials})
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.ExpiresAt.Before(time.Now()))
	assert.NotEmpty(t, store.SetCalls)
}

func TestHandler_Login_ClientCredentials_NoSecret(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, nil)
	_, err := h.Login(context.Background(), auth.LoginOptions{Flow: auth.FlowClientCredentials})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "clientSecret is required")
}

func TestHandler_Login_DeviceCode(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	var capturedUserCode string
	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) { cfg.DeviceAuthURL = srv.URL + "/device/code" })
	result, err := h.Login(context.Background(), auth.LoginOptions{
		Flow:               auth.FlowDeviceCode,
		DeviceCodeCallback: func(userCode, verificationURI, message string) { capturedUserCode = userCode },
	})
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "ABCD-1234", capturedUserCode)
}

func TestHandler_Login_DeviceCode_NoURL(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, nil)
	_, err := h.Login(context.Background(), auth.LoginOptions{Flow: auth.FlowDeviceCode})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deviceAuthURL is required")
}

func TestHandler_Login_UnsupportedFlow(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, nil)
	_, err := h.Login(context.Background(), auth.LoginOptions{Flow: auth.Flow("unsupported")})
	require.Error(t, err)
	assert.ErrorIs(t, err, auth.ErrFlowNotSupported)
}

func TestHandler_Login_WithVerify(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.ClientSecret = "test-secret"
		cfg.VerifyURL = srv.URL + "/userinfo"
		cfg.IdentityFields = &config.IdentityFieldMapping{Username: "login", Email: "email", Name: "name"}
	})
	result, err := h.Login(context.Background(), auth.LoginOptions{Flow: auth.FlowClientCredentials})
	require.NoError(t, err)
	assert.Equal(t, "testuser", result.Claims.Username)
	assert.Equal(t, "testuser@example.com", result.Claims.Email)
	assert.Equal(t, "Test User", result.Claims.Name)
}

func TestHandler_Login_WithTokenExchange(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, store := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.ClientSecret = "test-secret"
		cfg.TokenExchange = &config.TokenExchangeConfig{URL: srv.URL + "/exchange", TokenJSONPath: "token"}
	})
	result, err := h.Login(context.Background(), auth.LoginOptions{Flow: auth.FlowClientCredentials})
	require.NoError(t, err)
	assert.NotNil(t, result)
	_, exists := store.Data[secretKeyPrefix+"test-provider."+derivedTokenSuffix]
	assert.True(t, exists, "derived token should be stored")
}

func TestHandler_Login_NoSecretStore(t *testing.T) {
	h := &Handler{cfg: config.CustomOAuth2Config{Name: "test-no-secrets"}, secretErr: fmt.Errorf("no keyring"), logger: logr.Discard()}
	_, err := h.Login(context.Background(), auth.LoginOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no keyring")
}

func TestHandler_Logout(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, store := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) { cfg.ClientSecret = "test-secret" })
	_, err := h.Login(context.Background(), auth.LoginOptions{Flow: auth.FlowClientCredentials})
	require.NoError(t, err)
	assert.NotEmpty(t, store.Data)
	err = h.Logout(context.Background())
	require.NoError(t, err)
}

func TestHandler_Status_NotAuthenticated(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, nil)
	status, err := h.Status(context.Background())
	require.NoError(t, err)
	assert.False(t, status.Authenticated)
}

func TestHandler_Status_Authenticated(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, store := newTestHandler(t, srv, nil)
	meta := auth.HandlerMetadata{Claims: &auth.Claims{Username: "testuser"}, ExpiresAt: time.Now().Add(1 * time.Hour), Scopes: []string{"read"}}
	metaBytes, _ := json.Marshal(meta)
	_ = store.Set(context.Background(), secretKeyPrefix+"test-provider."+secretKeyMetadataSuffix, metaBytes)
	status, err := h.Status(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Authenticated)
	assert.Equal(t, "testuser", status.Claims.Username)
}

func TestHandler_Status_Expired(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, store := newTestHandler(t, srv, nil)
	meta := auth.HandlerMetadata{Claims: &auth.Claims{Username: "testuser"}, ExpiresAt: time.Now().Add(-1 * time.Hour), Scopes: []string{"read"}}
	metaBytes, _ := json.Marshal(meta)
	_ = store.Set(context.Background(), secretKeyPrefix+"test-provider."+secretKeyMetadataSuffix, metaBytes)
	status, err := h.Status(context.Background())
	require.NoError(t, err)
	assert.False(t, status.Authenticated)
}

func TestHandler_GetToken_FromCache(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) { cfg.ClientSecret = "test-secret" })
	_, err := h.Login(context.Background(), auth.LoginOptions{Flow: auth.FlowClientCredentials})
	require.NoError(t, err)
	token, err := h.GetToken(context.Background(), auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "cc-access-token", token.AccessToken)
}

func TestHandler_GetToken_Refresh(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) { cfg.ClientSecret = "test-secret" })
	require.NoError(t, h.storeRefreshToken(context.Background(), "good-refresh"))
	token, err := h.GetToken(context.Background(), auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "refreshed-access-token", token.AccessToken)
}

func TestHandler_GetToken_NotAuthenticated(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, nil)
	_, err := h.GetToken(context.Background(), auth.TokenOptions{})
	require.Error(t, err)
	assert.ErrorIs(t, err, auth.ErrNotAuthenticated)
}

func TestHandler_GetToken_DerivedToken(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, store := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.ClientSecret = "test-secret"
		cfg.TokenExchange = &config.TokenExchangeConfig{URL: srv.URL + "/exchange", TokenJSONPath: "token"}
	})
	ctx := context.Background()
	metaBytes, err := json.Marshal(auth.HandlerMetadata{
		ExpiresAt:     time.Now().Add(time.Hour),
		LastLoginFlow: auth.FlowClientCredentials,
	})
	require.NoError(t, err)
	require.NoError(t, store.Set(ctx, secretKeyPrefix+"test-provider."+secretKeyMetadataSuffix, metaBytes))
	require.NoError(t, h.storeDerivedToken(ctx, &exchangeResult{Token: "stored-derived-token"}, time.Now().Add(time.Hour), auth.FlowClientCredentials, nil))

	token, err := h.GetToken(ctx, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "stored-derived-token", token.AccessToken)
}

func TestHandler_InjectAuth(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) { cfg.ClientSecret = "test-secret" })
	_, err := h.Login(context.Background(), auth.LoginOptions{Flow: auth.FlowClientCredentials})
	require.NoError(t, err)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.example.com/v1/resource", nil)
	err = h.InjectAuth(context.Background(), req, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "Bearer cc-access-token", req.Header.Get("Authorization"))
}

func TestHandler_resolveDefaultFlow(t *testing.T) {
	tests := []struct {
		name     string
		override func(*config.CustomOAuth2Config)
		expected auth.Flow
	}{
		{name: "explicit interactive", override: func(c *config.CustomOAuth2Config) {
			c.DefaultFlow = "interactive"
			c.AuthorizeURL = "https://x.com/auth"
		}, expected: auth.FlowInteractive},
		{name: "explicit device_code", override: func(c *config.CustomOAuth2Config) { c.DefaultFlow = "device_code"; c.DeviceAuthURL = "https://x.com/d" }, expected: auth.FlowDeviceCode},
		{name: "explicit client_credentials", override: func(c *config.CustomOAuth2Config) { c.DefaultFlow = "client_credentials"; c.ClientSecret = "s" }, expected: auth.FlowClientCredentials},
		{name: "explicit workload_identity", override: func(c *config.CustomOAuth2Config) { c.DefaultFlow = "workload_identity"; c.FederatedToken = "env://T" }, expected: auth.FlowWorkloadIdentity},
		{name: "explicit pat", override: func(c *config.CustomOAuth2Config) { c.DefaultFlow = "pat"; c.Token = "secret://t" }, expected: auth.FlowPAT},
		{name: "auto interactive", override: func(c *config.CustomOAuth2Config) { c.AuthorizeURL = "https://x.com/auth" }, expected: auth.FlowInteractive},
		{name: "auto device", override: func(c *config.CustomOAuth2Config) { c.DeviceAuthURL = "https://x.com/d" }, expected: auth.FlowDeviceCode},
		{name: "auto workload_identity", override: func(c *config.CustomOAuth2Config) { c.FederatedTokenFile = "/tmp/tok" }, expected: auth.FlowWorkloadIdentity},
		{name: "auto client_credentials", override: func(c *config.CustomOAuth2Config) { c.ClientSecret = "s" }, expected: auth.FlowClientCredentials},
		{name: "auto pat", override: func(c *config.CustomOAuth2Config) { c.Token = "secret://t" }, expected: auth.FlowPAT},
		{name: "fallback", expected: auth.FlowInteractive},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTestOAuthServer(t)
			defer srv.Close()
			h, _ := newTestHandler(t, srv, tc.override)
			assert.Equal(t, tc.expected, h.resolveDefaultFlow())
		})
	}
}

func TestExtractJSONPath(t *testing.T) {
	tests := []struct {
		name    string
		data    map[string]any
		path    string
		want    string
		wantErr bool
	}{
		{name: "simple", data: map[string]any{"token": "abc123"}, path: "token", want: "abc123"},
		{name: "nested", data: map[string]any{"data": map[string]any{"t": "nested"}}, path: "data.t", want: "nested"},
		{name: "number", data: map[string]any{"n": float64(42)}, path: "n", want: "42"},
		{name: "missing", data: map[string]any{"other": "v"}, path: "token", wantErr: true},
		{name: "not object", data: map[string]any{"d": "str"}, path: "d.t", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractJSONPath(tc.data, tc.path)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestGetStringField(t *testing.T) {
	data := map[string]any{"name": "testuser", "count": 42}
	assert.Equal(t, "testuser", getStringField(data, "name"))
	assert.Equal(t, "", getStringField(data, "count"))
	assert.Equal(t, "", getStringField(data, "missing"))
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.CustomOAuth2Config
		wantErr string
	}{
		{name: "missing name", cfg: config.CustomOAuth2Config{}, wantErr: "name is required"},
		{name: "missing tokenURL", cfg: config.CustomOAuth2Config{Name: "t"}, wantErr: "tokenURL is required"},
		{name: "missing clientID", cfg: config.CustomOAuth2Config{Name: "t", TokenURL: "https://x.com/token"}, wantErr: "clientID is required"},
		{name: "no flow endpoints", cfg: config.CustomOAuth2Config{Name: "t", TokenURL: "https://x.com/token", ClientID: "c"}, wantErr: "at least one of authorizeURL"},
		{name: "device_code no url", cfg: config.CustomOAuth2Config{Name: "t", TokenURL: "https://x.com/token", ClientID: "c", DefaultFlow: "device_code"}, wantErr: "deviceAuthURL is required"},
		{name: "cc no secret", cfg: config.CustomOAuth2Config{Name: "t", TokenURL: "https://x.com/token", ClientID: "c", DefaultFlow: "client_credentials"}, wantErr: "clientSecret is required"},
		{name: "bad flow", cfg: config.CustomOAuth2Config{Name: "t", TokenURL: "https://x.com/token", ClientID: "c", DefaultFlow: "bad"}, wantErr: "unknown defaultFlow"},
		{name: "port low", cfg: config.CustomOAuth2Config{Name: "t", TokenURL: "https://x.com/token", ClientID: "c", AuthorizeURL: "https://x.com/a", CallbackPort: 80}, wantErr: "callbackPort must be 0"},
		{name: "exchange no url", cfg: config.CustomOAuth2Config{Name: "t", TokenURL: "https://x.com/token", ClientID: "c", ClientSecret: "s", DefaultFlow: "client_credentials", TokenExchange: &config.TokenExchangeConfig{TokenJSONPath: "t"}}, wantErr: "tokenExchange.url is required"},
		{name: "exchange no path", cfg: config.CustomOAuth2Config{Name: "t", TokenURL: "https://x.com/token", ClientID: "c", ClientSecret: "s", DefaultFlow: "client_credentials", TokenExchange: &config.TokenExchangeConfig{URL: "https://x.com/e"}}, wantErr: "tokenExchange.tokenJSONPath is required"},
		{name: "exchange bad method", cfg: config.CustomOAuth2Config{Name: "t", TokenURL: "https://x.com/token", ClientID: "c", ClientSecret: "s", DefaultFlow: "client_credentials", TokenExchange: &config.TokenExchangeConfig{URL: "https://x.com/e", TokenJSONPath: "t", Method: "DELETE"}}, wantErr: "tokenExchange.method must be"},
		{name: "valid minimal", cfg: config.CustomOAuth2Config{Name: "t", TokenURL: "https://x.com/token", ClientID: "c", ClientSecret: "s"}},
		{name: "valid full", cfg: config.CustomOAuth2Config{Name: "t", TokenURL: "https://x.com/token", ClientID: "c", AuthorizeURL: "https://x.com/a", DeviceAuthURL: "https://x.com/d", ClientSecret: "s", CallbackPort: 8080, TokenExchange: &config.TokenExchangeConfig{URL: "https://x.com/e", TokenJSONPath: "t", Method: "POST"}}},
		{name: "responseType token valid", cfg: config.CustomOAuth2Config{Name: "t", TokenURL: "https://x.com/token", ClientID: "c", AuthorizeURL: "https://x.com/a", ResponseType: "token"}},
		{name: "responseType token no authorizeURL", cfg: config.CustomOAuth2Config{Name: "t", ClientID: "c", ResponseType: "token"}, wantErr: "authorizeURL is required when responseType is token"},
		{name: "responseType token no tokenURL ok", cfg: config.CustomOAuth2Config{Name: "t", ClientID: "c", AuthorizeURL: "https://x.com/a", ResponseType: "token"}},
		{name: "responseType invalid", cfg: config.CustomOAuth2Config{Name: "t", TokenURL: "https://x.com/token", ClientID: "c", ResponseType: "bad"}, wantErr: "unknown responseType"},
		{name: "responseType token with device_code flow", cfg: config.CustomOAuth2Config{Name: "t", ClientID: "c", AuthorizeURL: "https://x.com/a", ResponseType: "token", DefaultFlow: "device_code"}, wantErr: "implicit grant (responseType=token) only supports interactive flow"},
		{name: "invalid callbackHost", cfg: config.CustomOAuth2Config{Name: "t", TokenURL: "https://x.com/token", ClientID: "c", AuthorizeURL: "https://x.com/a", CallbackHost: "evil.com"}, wantErr: "not allowed"},
		{name: "invalid callbackPath", cfg: config.CustomOAuth2Config{Name: "t", TokenURL: "https://x.com/token", ClientID: "c", AuthorizeURL: "https://x.com/a", CallbackPath: "no-slash"}, wantErr: "must start with /"},
		{name: "valid callbackHost and path", cfg: config.CustomOAuth2Config{Name: "t", TokenURL: "https://x.com/token", ClientID: "c", AuthorizeURL: "https://x.com/a", CallbackHost: "127.0.0.1", CallbackPath: "/auth/callback"}},
		{name: "dcr missing registrationEndpoint", cfg: config.CustomOAuth2Config{Name: "t", TokenURL: "https://x.com/token", ClientID: "c", AuthorizeURL: "https://x.com/a", DynamicClientRegistration: &config.DynamicClientRegistrationConfig{}}, wantErr: "dynamicClientRegistration.registrationEndpoint is required"},
		{name: "dcr-only satisfies clientID", cfg: config.CustomOAuth2Config{Name: "t", TokenURL: "https://x.com/token", DynamicClientRegistration: &config.DynamicClientRegistrationConfig{RegistrationEndpoint: "https://x.com/register"}}},
		{name: "dcr-only satisfies client_credentials secret", cfg: config.CustomOAuth2Config{Name: "t", TokenURL: "https://x.com/token", DefaultFlow: "client_credentials", DynamicClientRegistration: &config.DynamicClientRegistrationConfig{RegistrationEndpoint: "https://x.com/register"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateConfig(tc.cfg)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestHandler_executeTokenExchange(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.ClientSecret = "test-secret"
		cfg.TokenExchange = &config.TokenExchangeConfig{URL: srv.URL + "/exchange", TokenJSONPath: "token", UsernameJSONPath: "username"}
	})
	result, err := h.executeTokenExchange(context.Background(), "test-access-token")
	require.NoError(t, err)
	assert.Equal(t, "exchanged-token", result.Token)
	assert.Equal(t, "exchange-user", result.Username)
}

func TestHandler_executeTokenExchange_WithRequestBody(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.ClientSecret = "test-secret"
		cfg.Registry = "ghcr.io"
		cfg.RegistryUsername = "testuser"
		cfg.TokenExchange = &config.TokenExchangeConfig{URL: srv.URL + "/exchange", TokenJSONPath: "token", RequestBody: `{"hostname":"{{.Hostname}}","username":"{{.Username}}"}`}
	})
	result, err := h.executeTokenExchange(context.Background(), "test-access-token")
	require.NoError(t, err)
	assert.Equal(t, "exchanged-token", result.Token)
}

func TestHandler_verifyToken(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.VerifyURL = srv.URL + "/userinfo"
		cfg.IdentityFields = &config.IdentityFieldMapping{Username: "login", Email: "email", Name: "name"}
	})
	claims, err := h.verifyToken(context.Background(), "test-access-token")
	require.NoError(t, err)
	assert.Equal(t, "testuser", claims.Username)
	assert.Equal(t, "testuser@example.com", claims.Email)
	assert.Equal(t, "Test User", claims.Name)
}

func TestHandler_verifyToken_Unauthorized(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) { cfg.VerifyURL = srv.URL + "/userinfo" })
	_, err := h.verifyToken(context.Background(), "invalid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 401")
}

func TestHandler_RegistryUsername(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.RegistryUsername = "preset-user"
	})
	assert.Equal(t, "preset-user", h.RegistryUsername())
}

func TestTokenEndpointError_Error(t *testing.T) {
	tests := []struct {
		name string
		e    tokenEndpointError
		want string
	}{
		{
			name: "with description",
			e:    tokenEndpointError{ErrorCode: "invalid_grant", Description: "token expired"},
			want: "invalid_grant: token expired",
		},
		{
			name: "code only",
			e:    tokenEndpointError{ErrorCode: "access_denied"},
			want: "access_denied",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.e.Error())
		})
	}
}

func TestHandler_StoreDerivedToken_PersistsUsername(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.ClientSecret = "ts"
		cfg.TokenExchange = &config.TokenExchangeConfig{
			URL:              srv.URL + "/exchange",
			TokenJSONPath:    "token",
			UsernameJSONPath: "username",
		}
	})

	ctx := context.Background()

	// Login to get a token, which triggers token exchange via the /exchange endpoint
	_, err := h.Login(ctx, auth.LoginOptions{Flow: auth.FlowClientCredentials})
	require.NoError(t, err)

	// The exchange server returns username "exchange-user"
	// After a process restart (new Handler with same store), loadDerivedToken should restore it
	assert.Equal(t, "exchange-user", h.RegistryUsername())
}

func TestHandler_GetToken_WithDerivedToken_RestoresUsername(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	store := secrets.NewMockStore()
	cfg := config.CustomOAuth2Config{
		Name:         "test-exchange",
		TokenURL:     srv.URL + "/token",
		ClientID:     "tc",
		ClientSecret: "ts",
		TokenExchange: &config.TokenExchangeConfig{
			URL:              srv.URL + "/exchange",
			TokenJSONPath:    "token",
			UsernameJSONPath: "username",
		},
	}
	h, err := New(cfg, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)

	ctx := context.Background()

	// Login - stores derived token + username
	_, loginErr := h.Login(ctx, auth.LoginOptions{Flow: auth.FlowClientCredentials})
	require.NoError(t, loginErr)
	assert.Equal(t, "exchange-user", h.RegistryUsername())

	// Construct a fresh Handler over the same secretStore to simulate process restart
	hRestarted, err := New(cfg, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)
	assert.Empty(t, hRestarted.RegistryUsername())

	// GetToken should load derived token and restore username
	token, err := hRestarted.GetToken(ctx, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "exchanged-token", token.AccessToken)
	assert.Equal(t, "exchange-user", hRestarted.RegistryUsername())
}

func TestHandler_Login_WithTokenExchange_PersistsUsername(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.ClientSecret = "ts"
		cfg.TokenExchange = &config.TokenExchangeConfig{
			URL:              srv.URL + "/exchange",
			TokenJSONPath:    "token",
			UsernameJSONPath: "username",
		}
	})

	ctx := context.Background()
	_, err := h.Login(ctx, auth.LoginOptions{Flow: auth.FlowClientCredentials})
	require.NoError(t, err)

	// Username extracted from exchange response should be persisted in config
	assert.Equal(t, "exchange-user", h.RegistryUsername())
}

func TestHandler_Logout_ClearsDerivedUsername(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.ClientSecret = "ts"
		cfg.TokenExchange = &config.TokenExchangeConfig{
			URL:              srv.URL + "/exchange",
			TokenJSONPath:    "token",
			UsernameJSONPath: "username",
		}
	})

	ctx := context.Background()
	_, err := h.Login(ctx, auth.LoginOptions{Flow: auth.FlowClientCredentials})
	require.NoError(t, err)

	require.NoError(t, h.Logout(ctx))

	// After logout, loading derived token should fail
	token, loadErr := h.loadDerivedToken(ctx)
	assert.Nil(t, token)
	assert.Error(t, loadErr)
}

func TestHandler_ExchangeAuthCode_PKCEEnabled(t *testing.T) {
	var capturedForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		capturedForm = r.PostForm
		writeJSON(w, tokenResponse{AccessToken: "tok", TokenType: "Bearer", ExpiresIn: 3600})
	}))
	defer srv.Close()

	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.AuthorizeURL = srv.URL + "/authorize"
	})

	_, err := h.exchangeAuthCode(context.Background(), "test-code", "http://localhost:9999", "test-verifier", nil)
	require.NoError(t, err)

	assert.Equal(t, "test-verifier", capturedForm.Get("code_verifier"), "code_verifier should be sent when PKCE is enabled")
}

func TestHandler_ExchangeAuthCode_PKCEDisabled(t *testing.T) {
	var capturedForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		capturedForm = r.PostForm
		writeJSON(w, tokenResponse{AccessToken: "tok", TokenType: "Bearer", ExpiresIn: 3600})
	}))
	defer srv.Close()

	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.AuthorizeURL = srv.URL + "/authorize"
		cfg.DisablePKCE = true
	})

	// When PKCE is disabled, verifier is empty string — exchangeAuthCode should skip code_verifier
	_, err := h.exchangeAuthCode(context.Background(), "test-code", "http://localhost:9999", "", nil)
	require.NoError(t, err)

	assert.Empty(t, capturedForm.Get("code_verifier"), "code_verifier must not be sent when PKCE is disabled")
	_, hasKey := capturedForm["code_verifier"]
	assert.False(t, hasKey, "code_verifier key must be absent from form when PKCE is disabled")
}

func TestNew_ResponseTypeToken_ImpliesDisablePKCE(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.AuthorizeURL = srv.URL + "/authorize"
		cfg.ResponseType = "token"
	})
	assert.True(t, h.cfg.DisablePKCE, "DisablePKCE should be auto-implied when ResponseType is token")
}

func TestNew_ResponseTypeCode_PreservesDisablePKCE(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.AuthorizeURL = srv.URL + "/authorize"
		// ResponseType "" (default) should not force DisablePKCE
	})
	assert.False(t, h.cfg.DisablePKCE)
}

func TestHandler_ImplicitGrantLogin_RequiresAuthorizeURL(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.ResponseType = "token"
		cfg.AuthorizeURL = "" // missing
	})

	_, cb1, _, err := h.authCodeLogin(context.Background(), auth.LoginOptions{}, nil, 0)
	if cb1 != nil {
		_ = cb1.Close()
	}
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authorizeURL is required")
}

func TestHandler_ImplicitGrantLogin_ContextCancelled(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.AuthorizeURL = "https://example.com/authorize"
		cfg.ResponseType = "token"
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, cb2, _, err := h.implicitGrantLogin(ctx, auth.LoginOptions{}, nil, 0)
	if cb2 != nil {
		_ = cb2.Close()
	}
	// Should fail with context cancelled (server can't start or times out)
	assert.Error(t, err)
}

func BenchmarkHandler_ExchangeAuthCode(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, tokenResponse{AccessToken: "tok", TokenType: "Bearer", ExpiresIn: 3600})
	}))
	defer srv.Close()

	store := secrets.NewMockStore()
	cfg := config.CustomOAuth2Config{Name: "bench", TokenURL: srv.URL + "/token", ClientID: "bc", AuthorizeURL: srv.URL + "/auth"}
	h, _ := New(cfg, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := h.exchangeAuthCode(ctx, "code", "http://localhost:9999", "verifier", nil); err != nil {
			b.Fatalf("exchangeAuthCode failed: %v", err)
		}
	}
}

func BenchmarkHandler_ExchangeAuthCode_PKCEDisabled(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, tokenResponse{AccessToken: "tok", TokenType: "Bearer", ExpiresIn: 3600})
	}))
	defer srv.Close()

	store := secrets.NewMockStore()
	cfg := config.CustomOAuth2Config{Name: "bench", TokenURL: srv.URL + "/token", ClientID: "bc", AuthorizeURL: srv.URL + "/auth", DisablePKCE: true}
	h, _ := New(cfg, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := h.exchangeAuthCode(ctx, "code", "http://localhost:9999", "", nil); err != nil {
			b.Fatalf("exchangeAuthCode failed: %v", err)
		}
	}
}

func BenchmarkHandler_ClientCredentialsLogin(b *testing.B) {
	srv := newTestOAuthServer(b)
	defer srv.Close()
	store := secrets.NewMockStore()
	cfg := config.CustomOAuth2Config{Name: "bench", TokenURL: srv.URL + "/token", ClientID: "bc", ClientSecret: "bs"}
	h, _ := New(cfg, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
	ctx := context.Background()
	opts := auth.LoginOptions{Flow: auth.FlowClientCredentials}
	b.ResetTimer()
	for b.Loop() {
		_, _ = h.Login(ctx, opts)
	}
}

func BenchmarkHandler_GetToken_Cached(b *testing.B) {
	srv := newTestOAuthServer(b)
	defer srv.Close()
	store := secrets.NewMockStore()
	cfg := config.CustomOAuth2Config{Name: "bench", TokenURL: srv.URL + "/token", ClientID: "bc", ClientSecret: "bs"}
	h, _ := New(cfg, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
	ctx := context.Background()
	_, _ = h.Login(ctx, auth.LoginOptions{Flow: auth.FlowClientCredentials})
	b.ResetTimer()
	for b.Loop() {
		_, _ = h.GetToken(ctx, auth.TokenOptions{})
	}
}

func BenchmarkValidateConfig(b *testing.B) {
	cfg := config.CustomOAuth2Config{Name: "b", TokenURL: "https://x.com/token", ClientID: "c", ClientSecret: "s", TokenExchange: &config.TokenExchangeConfig{URL: "https://x.com/e", TokenJSONPath: "t", Method: "POST"}}
	b.ResetTimer()
	for b.Loop() {
		_ = ValidateConfig(cfg)
	}
}

func TestHandler_GetToken_LastLoginFlowCacheLookup(t *testing.T) {
	// Scenario: resolveDefaultFlow() returns FlowInteractive (authorizeURL is set),
	// but user logged in with FlowClientCredentials. Without the fix, GetToken
	// would look up the wrong cache key and return ErrNotAuthenticated.
	srv := newTestOAuthServer(t)
	defer srv.Close()

	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.AuthorizeURL = srv.URL + "/authorize" // makes resolveDefaultFlow() return FlowInteractive
		cfg.ClientSecret = "test-secret"          // enables client_credentials
	})

	// Login with client_credentials (different from resolveDefaultFlow)
	_, err := h.Login(context.Background(), auth.LoginOptions{Flow: auth.FlowClientCredentials})
	require.NoError(t, err)

	// GetToken should find the cached token using LastLoginFlow from metadata
	token, err := h.GetToken(context.Background(), auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "cc-access-token", token.AccessToken)
}

func TestHandler_CallbackOpts(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()

	tests := []struct {
		name    string
		host    string
		path    string
		wantLen int
	}{
		{"no overrides", "", "", 2},
		{"host only", "127.0.0.1", "", 3},
		{"path only", "", "/auth/callback", 3},
		{"both", "127.0.0.1", "/auth/callback", 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
				cfg.CallbackHost = tt.host
				cfg.CallbackPath = tt.path
			})
			opts := h.callbackOpts(context.Background())
			assert.Len(t, opts, tt.wantLen)
		})
	}
}

func TestWithSecretKeyPrefix(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	store := secrets.NewMockStore()

	// Write metadata under the plugin-style prefix
	pluginPrefix := "scafctl.auth."
	meta := auth.HandlerMetadata{Claims: &auth.Claims{Username: "testuser"}, ExpiresAt: time.Now().Add(1 * time.Hour), Scopes: []string{"read"}}
	metaBytes, err := json.Marshal(meta)
	require.NoError(t, err)
	require.NoError(t, store.Set(context.Background(), pluginPrefix+"test-provider.metadata", metaBytes))

	// Default prefix should NOT find the metadata
	cfg := config.CustomOAuth2Config{Name: "test-provider", TokenURL: srv.URL + "/token", ClientID: "cid"}
	hDefault, err := New(cfg, WithSecretStore(store))
	require.NoError(t, err)
	status, err := hDefault.Status(context.Background())
	require.NoError(t, err)
	assert.False(t, status.Authenticated)

	// Custom prefix should find it
	hCustom, err := New(cfg, WithSecretStore(store), WithSecretKeyPrefix(pluginPrefix))
	require.NoError(t, err)
	status, err = hCustom.Status(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Authenticated)
	assert.Equal(t, "testuser", status.Claims.Username)
}

func TestHandler_Status_Reason_NoSecretStore(t *testing.T) {
	cfg := config.CustomOAuth2Config{Name: "test-provider", TokenURL: "http://example.com/token", ClientID: "cid"}
	h := &Handler{cfg: cfg, secretKeyPrefix: secretKeyPrefix}
	status, err := h.Status(context.Background())
	require.NoError(t, err)
	assert.False(t, status.Authenticated)
	assert.Equal(t, "secrets store unavailable", status.Reason)
}

func TestHandler_Status_Reason_NotLoggedIn(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, nil)
	status, err := h.Status(context.Background())
	require.NoError(t, err)
	assert.False(t, status.Authenticated)
	assert.Equal(t, "not logged in", status.Reason)
}

func TestHandler_Status_Reason_CorruptMetadata(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, store := newTestHandler(t, srv, nil)
	require.NoError(t, store.Set(context.Background(), secretKeyPrefix+"test-provider.metadata", []byte("not json")))
	status, err := h.Status(context.Background())
	require.NoError(t, err)
	assert.False(t, status.Authenticated)
	assert.Equal(t, "metadata corrupt or unreadable", status.Reason)
}

func TestHandler_Status_Reason_Expired(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, store := newTestHandler(t, srv, nil)
	meta := auth.HandlerMetadata{Claims: &auth.Claims{Username: "testuser"}, ExpiresAt: time.Now().Add(-1 * time.Hour)}
	metaBytes, _ := json.Marshal(meta)
	_ = store.Set(context.Background(), secretKeyPrefix+"test-provider.metadata", metaBytes)
	status, err := h.Status(context.Background())
	require.NoError(t, err)
	assert.False(t, status.Authenticated)
	assert.Equal(t, "session expired", status.Reason)
}

func TestHandler_ListCachedTokens_Empty(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, nil)

	tokens, err := h.ListCachedTokens(context.Background())
	require.NoError(t, err)
	assert.Empty(t, tokens)
}

func TestHandler_ListCachedTokens_WithTokens(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, nil)

	// Store a token in the cache
	token := &auth.Token{
		AccessToken: "test-access-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scope:       "read",
		Flow:        auth.FlowInteractive,
		SessionID:   "sess-123",
		CachedAt:    time.Now(),
	}
	err := h.tokenCache.Set(context.Background(), auth.FlowInteractive, "fp1", "read", token)
	require.NoError(t, err)

	tokens, err := h.ListCachedTokens(context.Background())
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	assert.Equal(t, "test-provider", tokens[0].Handler)
	assert.Equal(t, "access", tokens[0].TokenKind)
	assert.Equal(t, "read", tokens[0].Scope)
	assert.Equal(t, "Bearer", tokens[0].TokenType)
	assert.Equal(t, auth.FlowInteractive, tokens[0].Flow)
	assert.False(t, tokens[0].IsExpired)
}

func TestHandler_ListCachedTokens_NilCache(t *testing.T) {
	h := &Handler{cfg: config.CustomOAuth2Config{Name: "no-cache"}, secretErr: fmt.Errorf("no keyring"), logger: logr.Discard()}

	tokens, err := h.ListCachedTokens(context.Background())
	require.NoError(t, err)
	assert.Nil(t, tokens)
}

func TestHandler_PurgeExpiredTokens_Empty(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, nil)

	n, err := h.PurgeExpiredTokens(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestHandler_PurgeExpiredTokens_RemovesExpired(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()
	h, _ := newTestHandler(t, srv, nil)

	// Store an expired token
	expired := &auth.Token{
		AccessToken: "expired-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(-time.Hour),
		Scope:       "read",
		Flow:        auth.FlowInteractive,
		CachedAt:    time.Now().Add(-2 * time.Hour),
	}
	err := h.tokenCache.Set(context.Background(), auth.FlowInteractive, "fp1", "read", expired)
	require.NoError(t, err)

	// Store a valid token
	valid := &auth.Token{
		AccessToken: "valid-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scope:       "write",
		Flow:        auth.FlowInteractive,
		CachedAt:    time.Now(),
	}
	err = h.tokenCache.Set(context.Background(), auth.FlowInteractive, "fp2", "write", valid)
	require.NoError(t, err)

	n, err := h.PurgeExpiredTokens(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// Verify valid token is still there
	tokens, err := h.ListCachedTokens(context.Background())
	require.NoError(t, err)
	assert.Len(t, tokens, 1)
	assert.Equal(t, "write", tokens[0].Scope)
}

func TestHandler_PurgeExpiredTokens_NilCache(t *testing.T) {
	h := &Handler{cfg: config.CustomOAuth2Config{Name: "no-cache"}, secretErr: fmt.Errorf("no keyring"), logger: logr.Discard()}

	n, err := h.PurgeExpiredTokens(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestHandler_GetToken_ResolvesActiveProfile(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()

	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.Name = "github"
		cfg.ClientSecret = "test-secret"
	})

	// Store a refresh token under the "work" profile key.
	require.NoError(t, h.storeRefreshToken(auth.WithProfile(context.Background(), "work"), "good-refresh"))

	// Build a context with config-level activeProfile but NO explicit WithProfile.
	ctx := config.WithConfig(context.Background(), &config.Config{
		Auth: config.GlobalAuthConfig{
			GitHub: &config.GitHubAuthConfig{ActiveProfile: "work"},
		},
	})

	// GetToken should resolve the active profile from config and find the token.
	token, err := h.GetToken(ctx, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "refreshed-access-token", token.AccessToken)
}

func TestHandler_GetToken_ExplicitProfileTakesPrecedence(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()

	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.Name = "github"
		cfg.ClientSecret = "test-secret"
	})

	// Store refresh token ONLY under the "explicit" profile.
	// The "config" profile has no token — if GetToken mistakenly uses it, the test fails.
	require.NoError(t, h.storeRefreshToken(auth.WithProfile(context.Background(), "explicit"), "good-refresh"))

	// Context has config-level activeProfile="config" but explicit WithProfile="explicit".
	ctx := config.WithConfig(context.Background(), &config.Config{
		Auth: config.GlobalAuthConfig{
			GitHub: &config.GitHubAuthConfig{ActiveProfile: "config"},
		},
	})
	ctx = auth.WithProfile(ctx, "explicit")

	// Explicit profile should win — only the "explicit" profile has a valid token.
	token, err := h.GetToken(ctx, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "refreshed-access-token", token.AccessToken)
}

func TestHandler_GetToken_GlobalFlagProfileResolved(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()

	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.Name = "github"
		cfg.ClientSecret = "test-secret"
	})

	// Store refresh token under the "flag" profile.
	require.NoError(t, h.storeRefreshToken(auth.WithProfile(context.Background(), "flag"), "good-refresh"))

	// Context has global --auth-profile flag set but no explicit WithProfile.
	ctx := auth.WithGlobalProfile(context.Background(), "flag")

	token, err := h.GetToken(ctx, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "refreshed-access-token", token.AccessToken)
}

func TestHandler_GetToken_NoProfileFallsBackToDefault(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()

	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.ClientSecret = "test-secret"
	})

	// Store refresh token under default (no profile) key.
	require.NoError(t, h.storeRefreshToken(context.Background(), "good-refresh"))

	// No profile anywhere — should use default key as before.
	token, err := h.GetToken(context.Background(), auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "refreshed-access-token", token.AccessToken)
}

func TestHandler_GetToken_ProfileResolvedSkipsReResolution(t *testing.T) {
	srv := newTestOAuthServer(t)
	defer srv.Close()

	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.Name = "github"
		cfg.ClientSecret = "test-secret"
	})

	// Store refresh token ONLY under the default (built-in) profile key.
	require.NoError(t, h.storeRefreshToken(context.Background(), "good-refresh"))

	// Config has activeProfile="work", but the caller marked profile as resolved
	// (simulating "auth token --profile built-in" which normalizes to default).
	ctx := config.WithConfig(context.Background(), &config.Config{
		Auth: config.GlobalAuthConfig{
			GitHub: &config.GitHubAuthConfig{ActiveProfile: "work"},
		},
	})
	ctx = auth.WithProfileResolved(ctx)

	// GetToken should use the built-in (default) profile, NOT re-resolve to "work".
	token, err := h.GetToken(ctx, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "refreshed-access-token", token.AccessToken)
}

func TestHandler_Login_DynamicClientRegistration(t *testing.T) {
	var registrationRequests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/register":
			registrationRequests++
			var reqBody map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))
			assert.Equal(t, "scafctl", reqBody["client_name"])
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, map[string]string{"client_id": "dynamic-client-id", "client_secret": "dynamic-client-secret"})
		case "/token":
			require.NoError(t, r.ParseForm())
			assert.Equal(t, "dynamic-client-id", r.FormValue("client_id"))
			writeJSON(w, tokenResponse{AccessToken: "dcr-access-token", TokenType: "Bearer", ExpiresIn: 3600})
		default:
			t.Fatalf("unexpected request to %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	h, store := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.ClientSecret = "static-secret" // This will be overridden by DCR
		cfg.DynamicClientRegistration = &config.DynamicClientRegistrationConfig{
			RegistrationEndpoint: srv.URL + "/register",
			ClientMetadata:       map[string]any{"client_name": "scafctl"},
		}
	})

	// First login should perform registration.
	_, err := h.Login(context.Background(), auth.LoginOptions{Flow: auth.FlowClientCredentials})
	require.NoError(t, err)
	assert.Equal(t, 1, registrationRequests)

	// Check that the dynamic client is stored.
	clientBytes, err := store.Get(context.Background(), h.profileSecretKey(context.Background(), dynamicClientSecretKey))
	require.NoError(t, err)
	var client dynamicClient
	require.NoError(t, json.Unmarshal(clientBytes, &client))
	assert.Equal(t, "dynamic-client-id", client.ClientID)
	assert.Equal(t, "dynamic-client-secret", client.ClientSecret)

	// Second login should use the cached client.
	newHandler, err := New(h.cfg, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)

	_, err = newHandler.Login(context.Background(), auth.LoginOptions{Flow: auth.FlowClientCredentials})
	require.NoError(t, err)
	assert.Equal(t, 1, registrationRequests, "registration should not be called a second time")
}

func TestHandler_Login_DynamicClientRegistration_ExpiredSecretReregisters(t *testing.T) {
	var registrationRequests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/register":
			registrationRequests++
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, map[string]any{
				"client_id":                fmt.Sprintf("dynamic-client-%d", registrationRequests),
				"client_secret":            "dynamic-secret",
				"client_secret_expires_at": time.Now().Add(time.Hour).Unix(),
			})
		case "/token":
			require.NoError(t, r.ParseForm())
			writeJSON(w, tokenResponse{AccessToken: "dcr-access-token", TokenType: "Bearer", ExpiresIn: 3600})
		default:
			t.Fatalf("unexpected request to %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	h, store := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.DynamicClientRegistration = &config.DynamicClientRegistrationConfig{
			RegistrationEndpoint: srv.URL + "/register",
			ClientMetadata:       map[string]any{"client_name": "scafctl"},
		}
	})

	_, err := h.Login(context.Background(), auth.LoginOptions{Flow: auth.FlowClientCredentials})
	require.NoError(t, err)
	assert.Equal(t, 1, registrationRequests)

	// Manually expire the cached dynamic client's secret.
	key := h.profileSecretKey(context.Background(), dynamicClientSecretKey)
	expired := dynamicClient{ClientID: "dynamic-client-1", ClientSecret: "dynamic-secret", ClientSecretExpiresAt: time.Now().Add(-time.Hour).Unix()}
	data, err := json.Marshal(expired)
	require.NoError(t, err)
	require.NoError(t, store.Set(context.Background(), key, data))

	// Login again should detect the expired secret and re-register.
	_, err = h.Login(context.Background(), auth.LoginOptions{Flow: auth.FlowClientCredentials})
	require.NoError(t, err)
	assert.Equal(t, 2, registrationRequests, "expired dynamic client should trigger re-registration")
}

func TestHandler_Login_DynamicClientRegistration_FingerprintMismatchReregisters(t *testing.T) {
	var registrationRequests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/register":
			registrationRequests++
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, map[string]any{
				"client_id":     fmt.Sprintf("dynamic-client-%d", registrationRequests),
				"client_secret": "dynamic-secret",
			})
		case "/token":
			require.NoError(t, r.ParseForm())
			writeJSON(w, tokenResponse{AccessToken: "dcr-access-token", TokenType: "Bearer", ExpiresIn: 3600})
		default:
			t.Fatalf("unexpected request to %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	h, store := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.DynamicClientRegistration = &config.DynamicClientRegistrationConfig{
			RegistrationEndpoint: srv.URL + "/register",
			ClientMetadata:       map[string]any{"client_name": "scafctl"},
		}
	})

	_, err := h.Login(context.Background(), auth.LoginOptions{Flow: auth.FlowClientCredentials})
	require.NoError(t, err)
	assert.Equal(t, 1, registrationRequests)

	// Simulate a stale cache entry fingerprinted against a different
	// registration target (e.g. the handler's tokenURL/metadata changed
	// since the credentials were cached).
	key := h.profileSecretKey(context.Background(), dynamicClientSecretKey)
	stale := dynamicClient{ClientID: "dynamic-client-1", ClientSecret: "dynamic-secret", Fingerprint: "stale-fingerprint"}
	data, err := json.Marshal(stale)
	require.NoError(t, err)
	require.NoError(t, store.Set(context.Background(), key, data))

	// Login again should detect the fingerprint mismatch and re-register
	// rather than silently reusing credentials issued for a different target.
	_, err = h.Login(context.Background(), auth.LoginOptions{Flow: auth.FlowClientCredentials})
	require.NoError(t, err)
	assert.Equal(t, 2, registrationRequests, "fingerprint mismatch should trigger re-registration")
}

func TestDcrFingerprint(t *testing.T) {
	base := &config.DynamicClientRegistrationConfig{
		RegistrationEndpoint: "https://as.example.com/register",
		ClientMetadata:       map[string]any{"client_name": "scafctl", "grant_types": []any{"client_credentials"}},
	}

	// Same inputs (even with metadata keys in a different map iteration
	// order) must produce the same fingerprint.
	reordered := &config.DynamicClientRegistrationConfig{
		RegistrationEndpoint: base.RegistrationEndpoint,
		ClientMetadata:       map[string]any{"grant_types": []any{"client_credentials"}, "client_name": "scafctl"},
	}
	assert.Equal(t, dcrFingerprint(base, "https://as.example.com/token"), dcrFingerprint(reordered, "https://as.example.com/token"))

	// A different token URL must change the fingerprint.
	assert.NotEqual(t, dcrFingerprint(base, "https://as.example.com/token"), dcrFingerprint(base, "https://other.example.com/token"))

	// A different registration endpoint must change the fingerprint.
	other := &config.DynamicClientRegistrationConfig{RegistrationEndpoint: "https://other.example.com/register", ClientMetadata: base.ClientMetadata}
	assert.NotEqual(t, dcrFingerprint(base, "https://as.example.com/token"), dcrFingerprint(other, "https://as.example.com/token"))

	// Different client metadata must change the fingerprint.
	diffMeta := &config.DynamicClientRegistrationConfig{RegistrationEndpoint: base.RegistrationEndpoint, ClientMetadata: map[string]any{"client_name": "other"}}
	assert.NotEqual(t, dcrFingerprint(base, "https://as.example.com/token"), dcrFingerprint(diffMeta, "https://as.example.com/token"))
}

func TestValidateConfig_PipelineFlowsAndSecretRef(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		cfg       config.CustomOAuth2Config
		binName   string
		wantError string
	}{
		{
			name: "valid RFC 7523 workload_identity with federatedToken file",
			cfg: config.CustomOAuth2Config{
				Name:           "wif-k8s",
				TokenURL:       "https://idp.example.com/oauth/token",
				ClientID:       "wif-client",
				FederatedToken: "file:///var/run/secrets/tokens/token",
				DefaultFlow:    "workload_identity",
			},
		},
		{
			name: "valid RFC 8693 token_exchange without clientID",
			cfg: config.CustomOAuth2Config{
				Name:                      "wif-sts",
				TokenURL:                  "https://sts.example.com/v1/token",
				FederatedToken:            "env://ACTIONS_ID_TOKEN",
				WorkloadIdentityGrantType: config.WorkloadIdentityGrantTokenExchange,
				DefaultFlow:               "workload_identity",
			},
		},
		{
			name: "RFC 7523 workload_identity requires clientID",
			cfg: config.CustomOAuth2Config{
				Name:           "wif-missing-cid",
				TokenURL:       "https://idp.example.com/oauth/token",
				FederatedToken: "env://OIDC_JWT",
				DefaultFlow:    "workload_identity",
			},
			wantError: "clientID is required",
		},
		{
			name: "workload_identity requires federatedToken or federatedTokenFile",
			cfg: config.CustomOAuth2Config{
				Name:        "wif-no-tok",
				TokenURL:    "https://idp.example.com/oauth/token",
				ClientID:    "cid",
				DefaultFlow: "workload_identity",
			},
			wantError: "federatedToken or federatedTokenFile is required",
		},
		{
			name: "federatedToken and federatedTokenFile mutually exclusive",
			cfg: config.CustomOAuth2Config{
				Name:               "wif-both",
				TokenURL:           "https://idp.example.com/oauth/token",
				ClientID:           "cid",
				FederatedToken:     "env://OIDC_JWT",
				FederatedTokenFile: "/var/run/secrets/token",
			},
			wantError: "mutually exclusive",
		},
		{
			name: "invalid workloadIdentityGrantType",
			cfg: config.CustomOAuth2Config{
				Name:                      "wif-bad-grant",
				TokenURL:                  "https://idp.example.com/oauth/token",
				ClientID:                  "cid",
				FederatedToken:            "env://OIDC_JWT",
				WorkloadIdentityGrantType: "implicit",
			},
			wantError: "unknown workloadIdentityGrantType",
		},
		{
			name: "pure pat handler requires neither tokenURL nor clientID",
			cfg: config.CustomOAuth2Config{
				Name:        "quay-robot",
				Token:       "secret://quay-robot-token",
				Username:    "myorg+ci_robot",
				DefaultFlow: "pat",
				Registry:    "quay.io",
			},
		},
		{
			name: "implicit pat-only handler without defaultFlow requires neither tokenURL nor clientID",
			cfg: config.CustomOAuth2Config{
				Name:     "quay-robot-auto",
				Token:    "env://QUAY_ROBOT_TOKEN",
				Username: "myorg+ci_robot",
			},
		},
		{
			name: "pat defaultFlow requires token",
			cfg: config.CustomOAuth2Config{
				Name:        "pat-missing-tok",
				DefaultFlow: "pat",
			},
			wantError: "token is required when defaultFlow is pat",
		},
		{
			name: "rejects internal secret prefix for default scafctl binary",
			cfg: config.CustomOAuth2Config{
				Name:        "pat-internal",
				Token:       "secret://scafctl.auth.oauth2.other.pat_token",
				DefaultFlow: "pat",
			},
			wantError: "reserved internal secret prefix",
		},
		{
			name: "rejects internal secret prefix for custom embedder binary",
			cfg: config.CustomOAuth2Config{
				Name:        "pat-embedder-internal",
				Token:       "secret://mycli.auth.token",
				DefaultFlow: "pat",
			},
			binName:   "mycli",
			wantError: "reserved internal secret prefix",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var err error
			if tc.binName != "" {
				err = ValidateConfigFor(tc.cfg, tc.binName)
			} else {
				err = ValidateConfig(tc.cfg)
			}
			if tc.wantError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantError)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestHandler_WorkloadIdentity_RFC7523_And_RotatedFile(t *testing.T) {
	t.Parallel()

	var observedAssertions []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			require.NoError(t, r.ParseForm())
			assert.Equal(t, "client_credentials", r.FormValue("grant_type"))
			assert.Equal(t, "k8s-client-id", r.FormValue("client_id"))
			assert.Equal(t, "urn:ietf:params:oauth:client-assertion-type:jwt-bearer", r.FormValue("client_assertion_type"))
			assert.Equal(t, "api://target-service", r.FormValue("audience"))
			assert.Equal(t, "repo:read", r.FormValue("scope"))

			assertion := r.FormValue("client_assertion")
			mu.Lock()
			observedAssertions = append(observedAssertions, assertion)
			callNum := len(observedAssertions)
			mu.Unlock()

			writeJSON(w, tokenResponse{
				AccessToken: fmt.Sprintf("minted-token-%d-for-%s", callNum, assertion),
				TokenType:   "Bearer",
				ExpiresIn:   3600,
				Scope:       "repo:read",
			})
		case "/exchange":
			assert.Contains(t, r.Header.Get("Authorization"), "Bearer minted-token-")
			writeJSON(w, map[string]any{"token": "derived-" + r.Header.Get("Authorization")[7:], "username": "wif-bot"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	jwtPath := filepath.Join(dir, "projected-sa-token")
	require.NoError(t, os.WriteFile(jwtPath, []byte("  jwt-generation-1\n\n"), 0o600))

	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.ClientID = "k8s-client-id"
		cfg.FederatedToken = config.SecretRef("file://" + jwtPath)
		cfg.Audience = "api://target-service"
		cfg.Scopes = []string{"repo:read"}
		cfg.TokenExchange = &config.TokenExchangeConfig{
			URL:              srv.URL + "/exchange",
			TokenJSONPath:    "token",
			UsernameJSONPath: "username",
		}
	})

	ctx := context.Background()

	// Zero-ceremony Status succeeds before any Login call.
	st, err := h.Status(ctx)
	require.NoError(t, err)
	assert.True(t, st.Authenticated)
	assert.Equal(t, auth.FlowWorkloadIdentity, st.Flow)
	assert.Equal(t, auth.IdentityTypeWorkloadIdentity, st.IdentityType)
	assert.Equal(t, jwtPath, st.TokenFile)

	// First GetToken mints via RFC 7523 + runs post-grant TokenExchange.
	tok1, err := h.GetToken(ctx, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "derived-minted-token-1-for-jwt-generation-1", tok1.AccessToken)
	assert.Equal(t, "wif-bot", h.RegistryUsername())

	// Second GetToken hits cache without re-posting to tokenURL.
	tokCached, err := h.GetToken(ctx, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, tok1.AccessToken, tokCached.AccessToken)
	mu.Lock()
	assert.Len(t, observedAssertions, 1)
	mu.Unlock()

	// Kubelet rotates projected volume token file; ForceRefresh (or expiry) re-reads it.
	require.NoError(t, os.WriteFile(jwtPath, []byte("\tjwt-generation-2\r\n"), 0o600))
	tok2, err := h.GetToken(ctx, auth.TokenOptions{ForceRefresh: true})
	require.NoError(t, err)
	assert.Equal(t, "derived-minted-token-2-for-jwt-generation-2", tok2.AccessToken)

	mu.Lock()
	assert.Equal(t, []string{"jwt-generation-1", "jwt-generation-2"}, observedAssertions)
	mu.Unlock()
}

func TestHandler_WorkloadIdentity_RFC8693_TokenExchange(t *testing.T) {
	t.Parallel()

	t.Run("public STS exchange without clientID and custom subjectTokenType", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, r.ParseForm())
			assert.Equal(t, "urn:ietf:params:oauth:grant-type:token-exchange", r.FormValue("grant_type"))
			assert.Equal(t, "oidc-jwt-value", r.FormValue("subject_token"))
			assert.Equal(t, "urn:ietf:params:oauth:token-type:id_token", r.FormValue("subject_token_type"))
			assert.Equal(t, "sts://vault", r.FormValue("audience"))
			assert.Empty(t, r.FormValue("client_id"))
			_, _, hasBasic := r.BasicAuth()
			assert.False(t, hasBasic)
			writeJSON(w, tokenResponse{AccessToken: "sts-access-token", TokenType: "Bearer", ExpiresIn: 1800})
		}))
		defer srv.Close()

		store := secrets.NewMockStore()
		require.NoError(t, store.Set(context.Background(), "ci-id-token", []byte("oidc-jwt-value\n")))

		cfg := config.CustomOAuth2Config{
			Name:                      "vault-sts",
			TokenURL:                  srv.URL + "/token",
			FederatedToken:            "secret://ci-id-token",
			WorkloadIdentityGrantType: config.WorkloadIdentityGrantTokenExchange,
			SubjectTokenType:          "urn:ietf:params:oauth:token-type:id_token",
			Audience:                  "sts://vault",
		}
		require.NoError(t, ValidateConfig(cfg))

		h, err := New(cfg, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
		require.NoError(t, err)

		tok, err := h.GetToken(context.Background(), auth.TokenOptions{})
		require.NoError(t, err)
		assert.Equal(t, "sts-access-token", tok.AccessToken)
		assert.Equal(t, auth.FlowWorkloadIdentity, tok.Flow)
	})

	t.Run("confidential STS exchange sends HTTP Basic auth when clientID and clientSecret are set", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, r.ParseForm())
			user, pass, ok := r.BasicAuth()
			assert.True(t, ok, "expected HTTP Basic auth header")
			assert.Equal(t, "sts-client", user)
			assert.Equal(t, "resolved-sts-secret", pass)
			assert.Empty(t, r.FormValue("client_id"), "client_id must not be duplicated in form when Basic auth is used")
			assert.Equal(t, config.DefaultSubjectTokenType, r.FormValue("subject_token_type"))
			writeJSON(w, tokenResponse{AccessToken: "sts-basic-token", TokenType: "Bearer", ExpiresIn: 1800})
		}))
		defer srv.Close()

		dir := t.TempDir()
		jwtFile := filepath.Join(dir, "sa-jwt")
		require.NoError(t, os.WriteFile(jwtFile, []byte("k8s-jwt"), 0o600))

		store := secrets.NewMockStore()
		require.NoError(t, store.Set(context.Background(), "sts-client-secret", []byte("resolved-sts-secret")))

		h, err := New(config.CustomOAuth2Config{
			Name:                      "sts-basic",
			TokenURL:                  srv.URL + "/token",
			ClientID:                  "sts-client",
			ClientSecret:              "secret://sts-client-secret",
			FederatedTokenFile:        jwtFile,
			WorkloadIdentityGrantType: config.WorkloadIdentityGrantTokenExchange,
		}, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
		require.NoError(t, err)

		res, err := h.Login(context.Background(), auth.LoginOptions{})
		require.NoError(t, err)
		assert.False(t, res.ExpiresAt.IsZero())
		assert.Equal(t, auth.FlowWorkloadIdentity, h.ActiveFlow(context.Background()))
	})
}

func TestHandler_ZeroCeremony_KeylessNilSecretStore(t *testing.T) {
	t.Parallel()

	var tokenCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenCalls.Add(1)
		require.NoError(t, r.ParseForm())
		writeJSON(w, tokenResponse{AccessToken: "keyless-wif-token", TokenType: "Bearer", ExpiresIn: 3600})
	}))
	defer srv.Close()

	dir := t.TempDir()
	jwtFile := filepath.Join(dir, "token")
	require.NoError(t, os.WriteFile(jwtFile, []byte("ambient-ci-jwt"), 0o600))

	// Construct handler via New + WithSecretStoreError simulating a keyless CI
	// container where requireSecureKeyring prevented secret store initialization.
	h, err := New(config.CustomOAuth2Config{
		Name:               "keyless-ci",
		TokenURL:           srv.URL + "/token",
		ClientID:           "ci-client",
		FederatedTokenFile: jwtFile,
		Scopes:             []string{"api:read"},
	}, WithSecretStoreError(fmt.Errorf("keyring unavailable in container")), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)
	assert.Nil(t, h.secretStore)

	ctx := context.Background()

	st, err := h.Status(ctx)
	require.NoError(t, err)
	assert.True(t, st.Authenticated)
	assert.Equal(t, auth.FlowWorkloadIdentity, st.Flow)

	// Repeated GetToken calls use in-memory cache rather than hammering tokenURL.
	tok1, err := h.GetToken(ctx, auth.TokenOptions{MinValidFor: time.Minute})
	require.NoError(t, err)
	assert.Equal(t, "keyless-wif-token", tok1.AccessToken)

	tok2, err := h.GetToken(ctx, auth.TokenOptions{MinValidFor: time.Minute})
	require.NoError(t, err)
	assert.Equal(t, "keyless-wif-token", tok2.AccessToken)
	assert.Equal(t, int32(1), tokenCalls.Load(), "in-memory cache must serve second GetToken without hitting tokenURL")
}

func TestHandler_FlowPAT_LifecycleAndVerification(t *testing.T) {
	t.Parallel()

	var exchangeCalled atomic.Bool
	var verifyStatus atomic.Int32
	verifyStatus.Store(http.StatusOK)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/exchange":
			exchangeCalled.Store(true)
			w.WriteHeader(http.StatusUnauthorized)
		case "/verify":
			code := int(verifyStatus.Load())
			if code != http.StatusOK {
				w.WriteHeader(code)
				_, _ = w.Write([]byte(`{"error":"invalid token"}`))
				return
			}
			assert.Equal(t, "Bearer quay-robot-secret-value", r.Header.Get("Authorization"))
			writeJSON(w, map[string]any{"username": "verified-robot"})
		default:
			t.Fatalf("unexpected request to %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	store := secrets.NewMockStore()
	ctx := context.Background()
	require.NoError(t, store.Set(ctx, "quay-robot-token", []byte("quay-robot-secret-value\n")))
	require.NoError(t, store.Set(ctx, "quay-robot-user", []byte("myorg+ci_robot")))

	cfg := config.CustomOAuth2Config{
		Name:             "quay",
		AuthorizeURL:     "https://quay.io/oauth/authorize",
		ResponseType:     "token",
		ClientID:         "quay-oauth-app",
		Token:            "secret://quay-robot-token",
		Username:         "secret://quay-robot-user",
		Registry:         "quay.io",
		RegistryUsername: "$oauthtoken",
		VerifyURL:        srv.URL + "/verify",
		IdentityFields:   &config.IdentityFieldMapping{Username: "username"},
		TokenExchange: &config.TokenExchangeConfig{
			URL:           srv.URL + "/exchange",
			TokenJSONPath: "token",
		},
	}
	require.NoError(t, ValidateConfig(cfg))

	h, err := New(cfg, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)

	// Before PAT is activated, interactive default keeps $oauthtoken.
	assert.Equal(t, "$oauthtoken", h.RegistryUsername())

	// Login with FlowPAT fails when VerifyURL returns 401 or 403, leaving Status unauthenticated.
	verifyStatus.Store(http.StatusUnauthorized)
	_, err = h.Login(ctx, auth.LoginOptions{Flow: auth.FlowPAT})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
	stAfterFail, err := h.Status(ctx)
	require.NoError(t, err)
	assert.False(t, stAfterFail.Authenticated)

	verifyStatus.Store(http.StatusForbidden)
	_, err = h.Login(ctx, auth.LoginOptions{Flow: auth.FlowPAT})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")

	// Valid PAT Login succeeds, skips TokenExchange, overrides RegistryUsername,
	// and stores pat_token outside TokenCache so PurgeExpiredTokens leaves it intact.
	verifyStatus.Store(http.StatusOK)
	res, err := h.Login(ctx, auth.LoginOptions{Flow: auth.FlowPAT})
	require.NoError(t, err)
	assert.True(t, res.ExpiresAt.IsZero())
	assert.False(t, exchangeCalled.Load(), "TokenExchange must be skipped on FlowPAT")
	assert.Equal(t, "myorg+ci_robot", h.RegistryUsername(), "FlowPAT must override $oauthtoken with resolved Username")

	purged, err := h.PurgeExpiredTokens(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, purged)
	cachedList, err := h.ListCachedTokens(ctx)
	require.NoError(t, err)
	assert.Empty(t, cachedList, "zero-expiry PAT must not be stored in TokenCache")

	st, err := h.Status(ctx)
	require.NoError(t, err)
	assert.True(t, st.Authenticated, "stored PAT session with zero ExpiresAt must not report expired")
	assert.Equal(t, auth.FlowPAT, st.Flow)

	tok, err := h.GetToken(ctx, auth.TokenOptions{MinValidFor: 5 * time.Minute})
	require.NoError(t, err)
	assert.Equal(t, "quay-robot-secret-value", tok.AccessToken)
	assert.Equal(t, auth.FlowPAT, tok.Flow)
	assert.False(t, exchangeCalled.Load(), "GetToken on FlowPAT must skip TokenExchange")

	// Logout clears pat_token and restores default RegistryUsername.
	require.NoError(t, h.Logout(ctx))
	_, patGetErr := store.Get(ctx, h.profileSecretKey(ctx, secretKeyPATTokenSuffix))
	assert.ErrorIs(t, patGetErr, secrets.ErrNotFound)
	assert.Equal(t, "$oauthtoken", h.RegistryUsername())
}

func TestHandler_PriorityOrder_And_FlowDetector(t *testing.T) {
	t.Setenv("TEST_OAUTH2_PAT_ENV", "pat-fallback-token")
	t.Setenv("TEST_OAUTH2_CC_SECRET_ENV", "cc-secret")

	srv := newTestOAuthServer(t)
	defer srv.Close()

	dir := t.TempDir()
	missingWIF := filepath.Join(dir, "missing-jwt")

	// WIF is configured via missing file -> falls through to client_credentials -> PAT.
	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.FederatedTokenFile = missingWIF
		cfg.ClientSecret = "env://TEST_OAUTH2_CC_SECRET_ENV"
		cfg.Token = "env://TEST_OAUTH2_PAT_ENV"
	})

	ctx := context.Background()
	flows, err := h.DetectAvailableFlows(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(flows), 3)
	assert.Equal(t, auth.FlowWorkloadIdentity, flows[0].Flow)
	assert.False(t, flows[0].Available)
	assert.Equal(t, auth.FlowClientCredentials, flows[1].Flow)
	assert.True(t, flows[1].Available)
	assert.Equal(t, auth.FlowPAT, flows[2].Flow)
	assert.True(t, flows[2].Available)

	tok, err := h.GetToken(ctx, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "cc-access-token", tok.AccessToken)
	assert.Equal(t, auth.FlowClientCredentials, tok.Flow)

	// When defaultFlow explicitly selects pat, pat takes priority over client_credentials.
	hPATPreferred, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.ClientSecret = "env://TEST_OAUTH2_CC_SECRET_ENV"
		cfg.Token = "env://TEST_OAUTH2_PAT_ENV"
		cfg.DefaultFlow = "pat"
	})
	tokPAT, err := hPATPreferred.GetToken(ctx, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "pat-fallback-token", tokPAT.AccessToken)
	assert.Equal(t, auth.FlowPAT, tokPAT.Flow)
}

func TestHandler_DCR_InitialAccessTokenSecretRef_And_UntrustedClientSecretLiteral(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sensitiveFile := filepath.Join(dir, "do-not-read")
	require.NoError(t, os.WriteFile(sensitiveFile, []byte("TOP_SECRET_LOCAL_FILE"), 0o600))
	maliciousSecretPayload := "file://" + sensitiveFile

	var receivedClientSecret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/register":
			assert.Equal(t, "Bearer dcr-secret-store-iat", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, map[string]any{
				"client_id":     "dcr-cid",
				"client_secret": maliciousSecretPayload,
			})
		case "/token":
			require.NoError(t, r.ParseForm())
			receivedClientSecret = r.FormValue("client_secret")
			writeJSON(w, tokenResponse{AccessToken: "dcr-tok", TokenType: "Bearer", ExpiresIn: 3600})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	store := secrets.NewMockStore()
	require.NoError(t, store.Set(context.Background(), "dcr-iat", []byte("dcr-secret-store-iat")))

	h, err := New(config.CustomOAuth2Config{
		Name:        "dcr-safe",
		TokenURL:    srv.URL + "/token",
		DefaultFlow: "client_credentials",
		DynamicClientRegistration: &config.DynamicClientRegistrationConfig{
			RegistrationEndpoint: srv.URL + "/register",
			InitialAccessToken:   "secret://dcr-iat",
		},
	}, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)

	tok, err := h.GetToken(context.Background(), auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "dcr-tok", tok.AccessToken)
	// DCR-returned client_secret must be sent verbatim, NEVER resolved as a local file:// SecretRef!
	assert.Equal(t, maliciousSecretPayload, receivedClientSecret)
}

func TestHandler_ConcurrentGetToken_Singleflight(t *testing.T) {
	t.Parallel()

	var tokenEndpointHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenEndpointHits.Add(1)
		// Small delay so all goroutines overlap on the in-flight request.
		time.Sleep(25 * time.Millisecond)
		writeJSON(w, tokenResponse{
			AccessToken: "coalesced-wif-token",
			TokenType:   "Bearer",
			ExpiresIn:   3600,
			Scope:       "read write",
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	jwtFile := filepath.Join(dir, "token")
	require.NoError(t, os.WriteFile(jwtFile, []byte("concurrent-jwt"), 0o600))

	h, _ := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) {
		cfg.FederatedTokenFile = jwtFile
	})

	const workers = 25
	var wg sync.WaitGroup
	wg.Add(workers)
	errs := make([]error, workers)
	tokens := make([]string, workers)

	start := make(chan struct{})
	for i := range workers {
		go func(idx int) {
			defer wg.Done()
			<-start
			ctx := context.Background()
			_, _ = h.Status(ctx)
			_ = h.RegistryUsername()
			tok, err := h.GetToken(ctx, auth.TokenOptions{})
			errs[idx] = err
			if tok != nil {
				tokens[idx] = tok.AccessToken
			}
		}(i)
	}
	close(start)
	wg.Wait()

	for i := range workers {
		require.NoError(t, errs[i])
		assert.Equal(t, "coalesced-wif-token", tokens[i])
	}
	assert.Equal(t, int32(1), tokenEndpointHits.Load(), "singleflight must coalesce concurrent cold-cache GetToken calls into 1 HTTP request")
}

func TestHandler_HeadlessStatus_ActiveFlow_And_KeylessTokenExchange(t *testing.T) {
	t.Parallel()

	var exchangeHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			// Return ExpiresIn: 0 to exercise fallback expiry calculation.
			writeJSON(w, tokenResponse{AccessToken: "opaque-primary-tok", TokenType: "Bearer", ExpiresIn: 0})
		case "/exchange":
			exchangeHits.Add(1)
			writeJSON(w, map[string]any{"token": "keyless-derived-tok", "username": "derived-bot"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	// 1. Keyless handler (secretStore == nil) with client_credentials + TokenExchange.
	hKeyless, err := New(config.CustomOAuth2Config{
		Name:         "keyless-cc",
		TokenURL:     srv.URL + "/token",
		ClientID:     "sp-client",
		ClientSecret: "sp-secret",
		Scopes:       []string{"repo:read"},
		TokenExchange: &config.TokenExchangeConfig{
			URL:              srv.URL + "/exchange",
			TokenJSONPath:    "token",
			UsernameJSONPath: "username",
		},
	}, WithSecretStoreError(fmt.Errorf("no keyring")), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)

	ctx := context.Background()
	assert.Equal(t, auth.FlowClientCredentials, hKeyless.ActiveFlow(ctx))

	stCC, err := hKeyless.Status(ctx)
	require.NoError(t, err)
	assert.True(t, stCC.Authenticated)
	assert.Equal(t, auth.FlowClientCredentials, stCC.Flow)
	assert.Equal(t, auth.IdentityTypeServicePrincipal, stCC.IdentityType)

	tok1, err := hKeyless.GetToken(ctx, auth.TokenOptions{MinValidFor: time.Minute})
	require.NoError(t, err)
	assert.Equal(t, "keyless-derived-tok", tok1.AccessToken)
	assert.False(t, tok1.ExpiresAt.IsZero())

	// Second call hits memDerived cache without calling /exchange again.
	tok2, err := hKeyless.GetToken(ctx, auth.TokenOptions{MinValidFor: time.Minute})
	require.NoError(t, err)
	assert.Equal(t, "keyless-derived-tok", tok2.AccessToken)
	assert.Equal(t, int32(1), exchangeHits.Load())

	// 2. Headless PAT Status & ForceRefresh on stored PAT session.
	store := secrets.NewMockStore()
	require.NoError(t, store.Set(ctx, "robot-tok", []byte("initial-pat")))
	hPAT, err := New(config.CustomOAuth2Config{
		Name:        "pat-headless",
		Token:       "secret://robot-tok",
		Username:    "org+bot",
		DefaultFlow: "pat",
	}, WithSecretStore(store), WithLogger(logr.Discard()))
	require.NoError(t, err)

	assert.Equal(t, "org+bot", hPAT.RegistryUsername())
	stPAT, err := hPAT.Status(ctx)
	require.NoError(t, err)
	assert.True(t, stPAT.Authenticated)
	assert.Equal(t, auth.FlowPAT, stPAT.Flow)
	assert.Equal(t, "org+bot", stPAT.Claims.Username)

	// Login then rotate underlying secret and ForceRefresh GetToken.
	_, err = hPAT.Login(ctx, auth.LoginOptions{Flow: auth.FlowPAT})
	require.NoError(t, err)
	require.NoError(t, store.Set(ctx, "robot-tok", []byte("rotated-pat")))
	refreshedPAT, err := hPAT.GetToken(ctx, auth.TokenOptions{ForceRefresh: true})
	require.NoError(t, err)
	assert.Equal(t, "rotated-pat", refreshedPAT.AccessToken)
}

func TestHandler_DCR_PersistsAcrossProcessRestart_And_SessionLifecycle(t *testing.T) {
	t.Parallel()

	var regCalls, tokenCalls, refreshCalls atomic.Int32
	var failRefresh, failExchange atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/register":
			regCalls.Add(1)
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, map[string]any{
				"client_id":     "registered-dcr-id",
				"client_secret": "registered-dcr-secret",
			})
		case "/token":
			require.NoError(t, r.ParseForm())
			switch r.FormValue("grant_type") {
			case "client_credentials":
				tokenCalls.Add(1)
				assert.Equal(t, "registered-dcr-id", r.FormValue("client_id"))
				assert.Equal(t, "registered-dcr-secret", r.FormValue("client_secret"))
				writeJSON(w, tokenResponse{
					AccessToken:  "dcr-access-1",
					TokenType:    "Bearer",
					ExpiresIn:    3600,
					RefreshToken: "dcr-refresh-1",
				})
			case "refresh_token":
				refreshCalls.Add(1)
				if failRefresh.Load() {
					w.WriteHeader(http.StatusBadRequest)
					writeJSON(w, tokenEndpointError{ErrorCode: "invalid_grant", Description: "revoked"})
					return
				}
				assert.Equal(t, "registered-dcr-id", r.FormValue("client_id"))
				assert.Equal(t, "registered-dcr-secret", r.FormValue("client_secret"))
				writeJSON(w, tokenResponse{
					AccessToken:  "dcr-access-refreshed",
					TokenType:    "Bearer",
					ExpiresIn:    3600,
					RefreshToken: "dcr-refresh-2",
				})
			}
		case "/exchange":
			if failExchange.Load() {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte("upstream down"))
				return
			}
			writeJSON(w, map[string]any{"token": "derived-" + r.Header.Get("Authorization")})
		}
	}))
	defer srv.Close()

	store := secrets.NewMockStore()
	ctx := context.Background()
	cfg := config.CustomOAuth2Config{
		Name:        "dcr-svc",
		TokenURL:    srv.URL + "/token",
		DefaultFlow: "client_credentials",
		DynamicClientRegistration: &config.DynamicClientRegistrationConfig{
			RegistrationEndpoint: srv.URL + "/register",
		},
	}

	// Process 1: Login registers DCR client and caches access + refresh token.
	h1, err := New(cfg, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)
	_, err = h1.Login(ctx, auth.LoginOptions{})
	require.NoError(t, err)
	assert.Equal(t, int32(1), regCalls.Load())
	assert.Equal(t, int32(1), tokenCalls.Load())

	// Process 2: Fresh Handler sharing secretStore hits cached token without re-registering or re-minting.
	h2, err := New(cfg, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)
	tokCached, err := h2.GetToken(ctx, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "dcr-access-1", tokCached.AccessToken)
	assert.Equal(t, int32(1), tokenCalls.Load(), "fresh process must hit cached token using persisted DCR client_id")

	// Process 3: Demand MinValidFor > 1h so fresh Handler exercises refresh_token using persisted DCR credentials.
	h3, err := New(cfg, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)
	tokRefreshed, err := h3.GetToken(ctx, auth.TokenOptions{MinValidFor: 2 * time.Hour})
	require.NoError(t, err)
	assert.Equal(t, "dcr-access-refreshed", tokRefreshed.AccessToken)
	assert.Equal(t, int32(1), refreshCalls.Load())

	// Revoked refresh token fails closed instead of falling back to headless minting.
	failRefresh.Store(true)
	_, err = h3.GetToken(ctx, auth.TokenOptions{MinValidFor: 2 * time.Hour})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_grant")

	// Logout disables headless fallback across subprocess restarts until next Login.
	require.NoError(t, h3.Logout(ctx))
	h4, err := New(cfg, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)
	stAfterLogout, err := h4.Status(ctx)
	require.NoError(t, err)
	assert.False(t, stAfterLogout.Authenticated)
	_, err = h4.GetToken(ctx, auth.TokenOptions{})
	require.ErrorIs(t, err, auth.ErrNotAuthenticated)

	// Hybrid PAT + OAuth handler still validates tokenURL even when defaultFlow is pat.
	hybridBad := config.CustomOAuth2Config{
		Name:         "hybrid-missing-token-url",
		AuthorizeURL: "https://idp.example.com/authorize",
		ClientID:     "cid",
		Token:        "secret://pat-tok",
		DefaultFlow:  "pat",
	}
	require.ErrorContains(t, ValidateConfig(hybridBad), "tokenURL is required")

	// TokenExchange fails closed on refresh failure (never leaks raw primary OAuth token).
	failRefresh.Store(false)
	failExchange.Store(true)
	cfgEx := cfg
	cfgEx.Name = "dcr-exchange"
	cfgEx.TokenExchange = &config.TokenExchangeConfig{URL: srv.URL + "/exchange", TokenJSONPath: "token"}
	hEx, err := New(cfgEx, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)
	_ = store.Set(ctx, secretKeyPrefix+"dcr-exchange."+dynamicClientSecretKey, store.Data[secretKeyPrefix+"dcr-svc."+dynamicClientSecretKey])
	require.NoError(t, hEx.storeRefreshToken(ctx, "dcr-refresh-1"))
	_, err = hEx.GetToken(ctx, auth.TokenOptions{ForceRefresh: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token_exchange")
}

func TestHandler_Status_ZeroExpiryGuards(t *testing.T) {
	t.Parallel()
	store := secrets.NewMockStore()
	ctx := context.Background()

	// Non-PAT metadata with zero ExpiresAt must be treated as expired, not non-expiring.
	metaNonPAT, _ := json.Marshal(auth.HandlerMetadata{
		Claims:        &auth.Claims{Username: "u"},
		ExpiresAt:     time.Time{},
		LastLoginFlow: auth.FlowInteractive,
	})
	require.NoError(t, store.Set(ctx, secretKeyPrefix+"zero-nonpat."+secretKeyMetadataSuffix, metaNonPAT))
	hNonPAT, err := New(config.CustomOAuth2Config{
		Name:         "zero-nonpat",
		AuthorizeURL: "https://idp.example.com/authorize",
		TokenURL:     "https://idp.example.com/token",
		ClientID:     "cid",
	}, WithSecretStore(store), WithLogger(logr.Discard()))
	require.NoError(t, err)
	st, err := hNonPAT.Status(ctx)
	require.NoError(t, err)
	assert.False(t, st.Authenticated)
	assert.Equal(t, "session expired", st.Reason)

	// Stored PAT metadata with missing pat_token secret must not report Authenticated when no fallback exists.
	metaPAT, _ := json.Marshal(auth.HandlerMetadata{
		Claims:        &auth.Claims{Username: "bot"},
		ExpiresAt:     time.Time{},
		LastLoginFlow: auth.FlowPAT,
	})
	require.NoError(t, store.Set(ctx, secretKeyPrefix+"missing-pat."+secretKeyMetadataSuffix, metaPAT))
	hMissingPAT, err := New(config.CustomOAuth2Config{
		Name:        "missing-pat",
		Token:       "env://UNSET_PAT_VAR_FOR_TEST_230",
		DefaultFlow: "pat",
	}, WithSecretStore(store), WithLogger(logr.Discard()))
	require.NoError(t, err)
	stPAT, err := hMissingPAT.Status(ctx)
	require.NoError(t, err)
	assert.False(t, stPAT.Authenticated)
}

func BenchmarkHandler_WorkloadIdentity_GetToken_Cached(b *testing.B) {
	dir := b.TempDir()
	jwtFile := filepath.Join(dir, "token")
	_ = os.WriteFile(jwtFile, []byte("bench-jwt"), 0o600)

	srvWIF := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, tokenResponse{AccessToken: "wif-tok", TokenType: "Bearer", ExpiresIn: 3600})
	}))
	defer srvWIF.Close()

	store := secrets.NewMockStore()
	cfg := config.CustomOAuth2Config{
		Name:               "bench-wif",
		TokenURL:           srvWIF.URL + "/token",
		ClientID:           "bc",
		FederatedTokenFile: jwtFile,
	}
	h, _ := New(cfg, WithSecretStore(store), WithHTTPClient(srvWIF.Client()), WithLogger(logr.Discard()))
	ctx := context.Background()
	_, _ = h.GetToken(ctx, auth.TokenOptions{})

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = h.GetToken(ctx, auth.TokenOptions{})
	}
}

type failingSessionStateStore struct {
	*secrets.MockStore
	failGetKey string
	failSetKey string
	getErr     error
	setErr     error
}

func (s *failingSessionStateStore) Get(ctx context.Context, name string) ([]byte, error) {
	if s.failGetKey != "" && name == s.failGetKey {
		return nil, s.getErr
	}
	return s.MockStore.Get(ctx, name)
}

func (s *failingSessionStateStore) Set(ctx context.Context, name string, value []byte) error {
	if s.failSetKey != "" && name == s.failSetKey {
		return s.setErr
	}
	return s.MockStore.Set(ctx, name, value)
}

func TestHandler_Logout_PreventsInFlightMintAndRefreshFromRepopulatingCache(t *testing.T) {
	t.Parallel()

	t.Run("blocked headless mint completing after Logout neither publishes nor returns credentials", func(t *testing.T) {
		t.Parallel()
		entered := make(chan struct{})
		release := make(chan struct{})

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, r.ParseForm())
			close(entered)
			<-release
			writeJSON(w, tokenResponse{
				AccessToken: "late-minted-token",
				TokenType:   "Bearer",
				ExpiresIn:   3600,
			})
		}))
		defer srv.Close()

		dir := t.TempDir()
		jwtFile := filepath.Join(dir, "token")
		require.NoError(t, os.WriteFile(jwtFile, []byte("wif-jwt"), 0o600))

		store := secrets.NewMockStore()
		h, err := New(config.CustomOAuth2Config{
			Name:               "race-mint",
			TokenURL:           srv.URL + "/token",
			ClientID:           "cid",
			FederatedTokenFile: jwtFile,
		}, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
		require.NoError(t, err)

		ctx := context.Background()
		errCh := make(chan error, 1)
		go func() {
			_, gErr := h.GetToken(ctx, auth.TokenOptions{})
			errCh <- gErr
		}()

		<-entered
		require.NoError(t, h.Logout(ctx))
		close(release)

		mintErr := <-errCh
		require.ErrorIs(t, mintErr, auth.ErrNotAuthenticated)

		cachedList, err := h.ListCachedTokens(ctx)
		require.NoError(t, err)
		assert.Empty(t, cachedList, "pre-logout mint must not repopulate token cache")

		_, nextErr := h.GetToken(ctx, auth.TokenOptions{})
		require.ErrorIs(t, nextErr, auth.ErrNotAuthenticated)

		st, stErr := h.Status(ctx)
		require.NoError(t, stErr)
		assert.False(t, st.Authenticated)
	})

	t.Run("blocked refresh completing after Logout neither publishes nor returns credentials", func(t *testing.T) {
		t.Parallel()
		entered := make(chan struct{})
		release := make(chan struct{})

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, r.ParseForm())
			close(entered)
			<-release
			writeJSON(w, tokenResponse{
				AccessToken:  "late-refreshed-token",
				TokenType:    "Bearer",
				ExpiresIn:    3600,
				RefreshToken: "late-rotated-refresh",
			})
		}))
		defer srv.Close()

		store := secrets.NewMockStore()
		ctx := context.Background()

		h, err := New(config.CustomOAuth2Config{
			Name:         "race-refresh",
			TokenURL:     srv.URL + "/token",
			ClientID:     "cid",
			ClientSecret: "csec",
		}, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
		require.NoError(t, err)
		require.NoError(t, h.storeRefreshToken(ctx, "initial-refresh"))

		errCh := make(chan error, 1)
		go func() {
			_, gErr := h.GetToken(ctx, auth.TokenOptions{})
			errCh <- gErr
		}()

		<-entered
		require.NoError(t, h.Logout(ctx))
		close(release)

		refErr := <-errCh
		require.ErrorIs(t, refErr, auth.ErrNotAuthenticated)

		_, storedRefreshErr := store.Get(ctx, secretKeyPrefix+"race-refresh."+secretKeyRefreshSuffix)
		assert.ErrorIs(t, storedRefreshErr, secrets.ErrNotFound, "pre-logout refresh must not re-persist refresh_token")

		cachedList, err := h.ListCachedTokens(ctx)
		require.NoError(t, err)
		assert.Empty(t, cachedList)

		// Even if a stale cached token existed alongside logged_out session_state, GetToken rejects it.
		_ = h.tokenCache.Set(ctx, auth.FlowClientCredentials, h.cacheFingerprint(ctx, auth.FlowClientCredentials), "", &auth.Token{
			AccessToken: "stale-cached",
			TokenType:   "Bearer",
			ExpiresAt:   time.Now().Add(time.Hour),
		})
		_, nextErr := h.GetToken(ctx, auth.TokenOptions{})
		require.ErrorIs(t, nextErr, auth.ErrNotAuthenticated)
	})
}

func TestHandler_SessionState_FailClosedOnReadError_And_PreservesLocalLogout(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	baseStore := secrets.NewMockStore()
	require.NoError(t, baseStore.Set(ctx, "robot-tok", []byte("ambient-pat")))

	sessionStateKey := secretKeyPrefix + "sess-guard." + secretKeySessionStateSuffix
	wrapStore := &failingSessionStateStore{
		MockStore:  baseStore,
		failGetKey: sessionStateKey,
		getErr:     errors.New("permission denied reading secret file"),
	}

	h, err := New(config.CustomOAuth2Config{
		Name:        "sess-guard",
		Token:       "secret://robot-tok",
		DefaultFlow: "pat",
	}, WithSecretStore(wrapStore), WithLogger(logr.Discard()))
	require.NoError(t, err)

	// Non-ErrNotFound session_state read failure must fail closed in Status and GetToken.
	st, err := h.Status(ctx)
	require.NoError(t, err)
	assert.False(t, st.Authenticated)
	assert.Contains(t, st.Reason, "session state unreadable")

	_, tokErr := h.GetToken(ctx, auth.TokenOptions{})
	require.Error(t, tokErr)
	assert.Contains(t, tokErr.Error(), "session_state")

	// Clear read error, simulate Set failure on session_state during Logout, and verify
	// in-memory logout state still blocks ambient credentials when store lacks session_state.
	wrapStore.failGetKey = ""
	wrapStore.failSetKey = sessionStateKey
	wrapStore.setErr = errors.New("disk full")

	logoutErr := h.Logout(ctx)
	require.Error(t, logoutErr)

	stAfterLocalLogout, err := h.Status(ctx)
	require.NoError(t, err)
	assert.False(t, stAfterLocalLogout.Authenticated)

	_, tokAfterLocalLogoutErr := h.GetToken(ctx, auth.TokenOptions{})
	require.ErrorIs(t, tokAfterLocalLogoutErr, auth.ErrNotAuthenticated)
}

func TestHandler_WorkloadIdentity_And_DerivedToken_ConfigFingerprints(t *testing.T) {
	t.Parallel()

	var srv1Hits, srv2Hits atomic.Int32
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			srv1Hits.Add(1)
			require.NoError(t, r.ParseForm())
			writeJSON(w, tokenResponse{
				AccessToken: "idp1-tok-" + r.FormValue("subject_token_type"),
				TokenType:   "Bearer",
				ExpiresIn:   3600,
			})
		case "/exchange":
			writeJSON(w, map[string]any{"token": "derived-idp1", "username": "user-idp1"})
		}
	}))
	defer srv1.Close()

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			srv2Hits.Add(1)
			require.NoError(t, r.ParseForm())
			writeJSON(w, tokenResponse{
				AccessToken: "idp2-tok-" + r.FormValue("subject_token_type"),
				TokenType:   "Bearer",
				ExpiresIn:   3600,
			})
		case "/exchange":
			writeJSON(w, map[string]any{"token": "derived-idp2", "username": "user-idp2"})
		}
	}))
	defer srv2.Close()

	dir := t.TempDir()
	jwtFile := filepath.Join(dir, "sa-jwt")
	require.NoError(t, os.WriteFile(jwtFile, []byte("jwt-payload"), 0o600))

	store := secrets.NewMockStore()
	ctx := context.Background()

	// 1. Primary WIF cache must miss when tokenURL or subjectTokenType changes.
	baseCfg := config.CustomOAuth2Config{
		Name:                      "wif-fp",
		TokenURL:                  srv1.URL + "/token",
		ClientID:                  "shared-cid",
		FederatedTokenFile:        jwtFile,
		WorkloadIdentityGrantType: config.WorkloadIdentityGrantTokenExchange,
		SubjectTokenType:          "urn:ietf:params:oauth:token-type:jwt",
		Audience:                  "api://svc",
	}
	h1, err := New(baseCfg, WithSecretStore(store), WithHTTPClient(srv1.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)
	tok1, err := h1.GetToken(ctx, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Contains(t, tok1.AccessToken, "idp1-tok-")
	assert.Equal(t, int32(1), srv1Hits.Load())

	// Changing SubjectTokenType invalidates cache.
	cfgNewSubjType := baseCfg
	cfgNewSubjType.SubjectTokenType = "urn:ietf:params:oauth:token-type:id_token"
	hSubj, err := New(cfgNewSubjType, WithSecretStore(store), WithHTTPClient(srv1.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)
	tokSubj, err := hSubj.GetToken(ctx, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "idp1-tok-urn:ietf:params:oauth:token-type:id_token", tokSubj.AccessToken)
	assert.Equal(t, int32(2), srv1Hits.Load())

	// Repointing TokenURL to srv2 invalidates cache and contacts srv2.
	cfgNewIssuer := cfgNewSubjType
	cfgNewIssuer.TokenURL = srv2.URL + "/token"
	hIssuer, err := New(cfgNewIssuer, WithSecretStore(store), WithHTTPClient(srv2.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)
	tokIssuer, err := hIssuer.GetToken(ctx, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "idp2-tok-urn:ietf:params:oauth:token-type:id_token", tokIssuer.AccessToken)
	assert.Equal(t, int32(1), srv2Hits.Load())

	// 2. Derived tokens and derived usernames must also be bound to the config fingerprint.
	storeEx := secrets.NewMockStore()
	cfgEx1 := baseCfg
	cfgEx1.Name = "wif-derived-fp"
	cfgEx1.TokenExchange = &config.TokenExchangeConfig{
		URL:              srv1.URL + "/exchange",
		TokenJSONPath:    "token",
		UsernameJSONPath: "username",
	}
	hEx1, err := New(cfgEx1, WithSecretStore(storeEx), WithHTTPClient(srv1.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)
	_, err = hEx1.Login(ctx, auth.LoginOptions{Flow: auth.FlowWorkloadIdentity})
	require.NoError(t, err)
	assert.Equal(t, "user-idp1", hEx1.RegistryUsername())

	// Restarting with a different Audience must not reuse derived-idp1 or user-idp1.
	cfgEx2 := cfgEx1
	cfgEx2.Audience = "api://different-audience"
	cfgEx2.TokenURL = srv2.URL + "/token"
	cfgEx2.TokenExchange = &config.TokenExchangeConfig{
		URL:              srv2.URL + "/exchange",
		TokenJSONPath:    "token",
		UsernameJSONPath: "username",
	}
	hEx2, err := New(cfgEx2, WithSecretStore(storeEx), WithHTTPClient(srv2.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)
	assert.Empty(t, hEx2.RegistryUsername(), "mismatched config fingerprint must not restore stale derived username")

	tokEx2, err := hEx2.GetToken(ctx, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "derived-idp2", tokEx2.AccessToken)
	assert.Equal(t, "user-idp2", hEx2.RegistryUsername())
}

func TestHandler_ProfileIsolatedRegistryUsername(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			writeJSON(w, tokenResponse{AccessToken: "oauth-tok-" + r.FormValue("scope"), TokenType: "Bearer", ExpiresIn: 3600})
		case "/exchange":
			writeJSON(w, map[string]any{"token": "exchanged-dev-tok", "username": "dev-exchange-user"})
		}
	}))
	defer srv.Close()

	store := secrets.NewMockStore()
	ctx := context.Background()
	require.NoError(t, store.Set(ctx, "prod-robot-token", []byte("prod-pat-secret")))

	h, err := New(config.CustomOAuth2Config{
		Name:             "quay-multi",
		TokenURL:         srv.URL + "/token",
		ClientID:         "cid",
		ClientSecret:     "csec",
		Registry:         "quay.io",
		RegistryUsername: "$oauthtoken",
		Token:            "secret://prod-robot-token",
		Username:         "acme+prod_robot",
		TokenExchange: &config.TokenExchangeConfig{
			URL:              srv.URL + "/exchange",
			TokenJSONPath:    "token",
			UsernameJSONPath: "username",
		},
	}, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)

	prodCtx := auth.WithProfile(ctx, "prod")
	devCtx := auth.WithProfile(ctx, "dev")

	_, err = h.Login(prodCtx, auth.LoginOptions{Flow: auth.FlowPAT})
	require.NoError(t, err)
	_, err = h.Login(devCtx, auth.LoginOptions{Flow: auth.FlowClientCredentials})
	require.NoError(t, err)

	// Interleaving profile requests must never pair prod's PAT with dev's derived username.
	uProd, pProd, err := catalog.BridgeAuthToRegistry(prodCtx, h, "quay.io", "")
	require.NoError(t, err)
	assert.Equal(t, "acme+prod_robot", uProd)
	assert.Equal(t, "prod-pat-secret", pProd)

	uDev, pDev, err := catalog.BridgeAuthToRegistry(devCtx, h, "quay.io", "")
	require.NoError(t, err)
	assert.Equal(t, "dev-exchange-user", uDev)
	assert.Equal(t, "exchanged-dev-tok", pDev)

	// Re-checking prod after dev's request still returns prod's robot username.
	assert.Equal(t, "acme+prod_robot", h.RegistryUsernameForContext(prodCtx))
}

func TestHandler_DCR_StatusAndAvailabilitySkipsStaticSecretAndBootstrapToken(t *testing.T) {
	var regCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/register":
			regCalls.Add(1)
			assert.Equal(t, "Bearer bootstrap-iat", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, map[string]any{
				"client_id":     "dcr-persisted-id",
				"client_secret": "dcr-persisted-secret",
			})
		case "/token":
			require.NoError(t, r.ParseForm())
			assert.Equal(t, "dcr-persisted-id", r.FormValue("client_id"))
			assert.Equal(t, "dcr-persisted-secret", r.FormValue("client_secret"))
			writeJSON(w, tokenResponse{AccessToken: "fresh-dcr-access", TokenType: "Bearer", ExpiresIn: 3600})
		}
	}))
	defer srv.Close()

	t.Setenv("TEST_DCR_BOOTSTRAP_IAT", "bootstrap-iat")
	t.Setenv("TEST_UNUSED_STATIC_SECRET", "")

	store := secrets.NewMockStore()
	ctx := context.Background()
	cfg := config.CustomOAuth2Config{
		Name:         "dcr-reuse",
		TokenURL:     srv.URL + "/token",
		ClientSecret: "env://TEST_UNUSED_STATIC_SECRET",
		DefaultFlow:  "client_credentials",
		DynamicClientRegistration: &config.DynamicClientRegistrationConfig{
			RegistrationEndpoint: srv.URL + "/register",
			InitialAccessToken:   "env://TEST_DCR_BOOTSTRAP_IAT",
		},
	}

	h1, err := New(cfg, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)

	// Initial Status and GetToken succeed despite TEST_UNUSED_STATIC_SECRET being empty.
	st1, err := h1.Status(ctx)
	require.NoError(t, err)
	assert.True(t, st1.Authenticated)

	tok1, err := h1.GetToken(ctx, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "fresh-dcr-access", tok1.AccessToken)
	assert.Equal(t, int32(1), regCalls.Load())

	// Remove bootstrap initialAccessToken and expire cached access token; persisted DCR client must still work.
	t.Setenv("TEST_DCR_BOOTSTRAP_IAT", "")
	require.NoError(t, h1.tokenCache.Clear(ctx))

	h2, err := New(cfg, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)

	st2, err := h2.Status(ctx)
	require.NoError(t, err)
	assert.True(t, st2.Authenticated, "persisted unexpired DCR client must not require bootstrap token")

	tok2, err := h2.GetToken(ctx, auth.TokenOptions{ForceRefresh: true})
	require.NoError(t, err)
	assert.Equal(t, "fresh-dcr-access", tok2.AccessToken)
	assert.Equal(t, int32(1), regCalls.Load(), "persisted DCR client must be reused without re-registering")
}

func TestHandler_AmbientHeadlessFlowOrder_WithCachedDerivedToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			writeJSON(w, tokenResponse{AccessToken: "wif-primary", TokenType: "Bearer", ExpiresIn: 3600})
		case "/exchange":
			writeJSON(w, map[string]any{"token": "wif-derived", "username": "wif-user"})
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	jwtFile := filepath.Join(dir, "sa-token")
	require.NoError(t, os.WriteFile(jwtFile, []byte("ambient-jwt"), 0o600))

	t.Setenv("TEST_PREFERRED_PAT_LATE", "")

	store := secrets.NewMockStore()
	h, err := New(config.CustomOAuth2Config{
		Name:               "ambient-order",
		TokenURL:           srv.URL + "/token",
		ClientID:           "cid",
		FederatedTokenFile: jwtFile,
		Token:              "env://TEST_PREFERRED_PAT_LATE",
		Username:           "acme+pat_robot",
		DefaultFlow:        "pat",
		TokenExchange: &config.TokenExchangeConfig{
			URL:              srv.URL + "/exchange",
			TokenJSONPath:    "token",
			UsernameJSONPath: "username",
		},
	}, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)

	ctx := context.Background()

	// Preferred PAT is initially unset, so GetToken falls back to WIF and caches wif-derived.
	tokFallback, err := h.GetToken(ctx, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "wif-derived", tokFallback.AccessToken)
	assert.Equal(t, auth.FlowWorkloadIdentity, tokFallback.Flow)

	// Once preferred PAT becomes available, GetToken must honor defaultFlow: pat
	// instead of shortcutting to the cached ambient WIF derived token.
	t.Setenv("TEST_PREFERRED_PAT_LATE", "preferred-pat-secret")
	tokPreferred, err := h.GetToken(ctx, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "preferred-pat-secret", tokPreferred.AccessToken)
	assert.Equal(t, auth.FlowPAT, tokPreferred.Flow)
	assert.Equal(t, "acme+pat_robot", h.RegistryUsername())
}

func TestHandler_RefreshToken_RotationPersistence_And_ConcurrentSingleUseSerialization(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	currentValidRefresh := "refresh-gen-1"
	rotationSeq := 1

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "refresh_token", r.FormValue("grant_type"))

		// Small delay so concurrent refreshes with different scopes overlap.
		time.Sleep(15 * time.Millisecond)

		submitted := r.FormValue("refresh_token")
		scope := r.FormValue("scope")

		mu.Lock()
		if submitted != currentValidRefresh {
			mu.Unlock()
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, tokenEndpointError{
				ErrorCode:   "invalid_grant",
				Description: fmt.Sprintf("single-use refresh token %q already consumed", submitted),
			})
			return
		}
		rotationSeq++
		nextRefresh := fmt.Sprintf("refresh-gen-%d", rotationSeq)
		currentValidRefresh = nextRefresh
		seq := rotationSeq
		mu.Unlock()

		writeJSON(w, tokenResponse{
			AccessToken:  fmt.Sprintf("access-%s-gen-%d", scope, seq),
			TokenType:    "Bearer",
			ExpiresIn:    3600,
			RefreshToken: nextRefresh,
			Scope:        scope,
		})
	}))
	defer srv.Close()

	store := secrets.NewMockStore()
	ctx := context.Background()

	h, err := New(config.CustomOAuth2Config{
		Name:         "rot-idp",
		TokenURL:     srv.URL + "/token",
		ClientID:     "cid",
		ClientSecret: "csec",
	}, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)
	require.NoError(t, h.storeRefreshToken(ctx, "refresh-gen-1"))

	// 1. Two consecutive refreshes with ForceRefresh must persist and submit rotated tokens.
	tok1, err := h.GetToken(ctx, auth.TokenOptions{Scope: "read", ForceRefresh: true})
	require.NoError(t, err)
	assert.Equal(t, "access-read-gen-2", tok1.AccessToken)

	tok2, err := h.GetToken(ctx, auth.TokenOptions{Scope: "read", ForceRefresh: true})
	require.NoError(t, err)
	assert.Equal(t, "access-read-gen-3", tok2.AccessToken)

	// 2. Concurrent refreshes with distinct scopes share one single-use refresh token
	// without invalid_grant races and retain separate scope-specific access-token caches.
	var wg sync.WaitGroup
	scopes := []string{"scope-alpha", "scope-beta", "scope-gamma"}
	results := make([]*auth.Token, len(scopes))
	errs := make([]error, len(scopes))
	start := make(chan struct{})

	for i, sc := range scopes {
		wg.Add(1)
		go func(idx int, scopeVal string) {
			defer wg.Done()
			<-start
			results[idx], errs[idx] = h.GetToken(ctx, auth.TokenOptions{Scope: scopeVal})
		}(i, sc)
	}
	close(start)
	wg.Wait()

	for i, sc := range scopes {
		require.NoError(t, errs[i], "concurrent refresh for %s must succeed", sc)
		require.NotNil(t, results[i])
		assert.Contains(t, results[i].AccessToken, "access-"+sc+"-")
	}

	// Subsequent calls for each scope hit their respective scope-specific cache entries.
	mu.Lock()
	seqBeforeCacheCheck := rotationSeq
	mu.Unlock()
	for i, sc := range scopes {
		cachedTok, cErr := h.GetToken(ctx, auth.TokenOptions{Scope: sc})
		require.NoError(t, cErr)
		assert.Equal(t, results[i].AccessToken, cachedTok.AccessToken)
	}
	mu.Lock()
	assert.Equal(t, seqBeforeCacheCheck, rotationSeq, "scope-specific cached tokens must avoid further refreshes")
	mu.Unlock()
}

func TestHandler_DCR_ConcurrentColdRegistrationCoalescedAcrossScopesAndLogin(t *testing.T) {
	t.Parallel()

	var regSeq atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/register":
			n := regSeq.Add(1)
			time.Sleep(25 * time.Millisecond)
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, map[string]any{
				"client_id":     fmt.Sprintf("coalesced-client-%d", n),
				"client_secret": fmt.Sprintf("coalesced-secret-%d", n),
			})
		case "/token":
			require.NoError(t, r.ParseForm())
			cid := r.FormValue("client_id")
			sc := r.FormValue("scope")
			writeJSON(w, tokenResponse{
				AccessToken: fmt.Sprintf("tok-%s-%s", cid, sc),
				TokenType:   "Bearer",
				ExpiresIn:   3600,
				Scope:       sc,
			})
		}
	}))
	defer srv.Close()

	store := secrets.NewMockStore()
	h, err := New(config.CustomOAuth2Config{
		Name:        "dcr-coalesce",
		TokenURL:    srv.URL + "/token",
		DefaultFlow: "client_credentials",
		DynamicClientRegistration: &config.DynamicClientRegistrationConfig{
			RegistrationEndpoint: srv.URL + "/register",
		},
	}, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)

	ctx := context.Background()
	var wg sync.WaitGroup
	start := make(chan struct{})

	var tokA, tokB *auth.Token
	var errA, errB, errLogin error

	wg.Add(3)
	go func() {
		defer wg.Done()
		<-start
		tokA, errA = h.GetToken(ctx, auth.TokenOptions{Scope: "scope-a"})
	}()
	go func() {
		defer wg.Done()
		<-start
		tokB, errB = h.GetToken(ctx, auth.TokenOptions{Scope: "scope-b"})
	}()
	go func() {
		defer wg.Done()
		<-start
		_, errLogin = h.Login(ctx, auth.LoginOptions{Scopes: []string{"scope-login"}})
	}()
	close(start)
	wg.Wait()

	require.NoError(t, errA)
	require.NoError(t, errB)
	require.NoError(t, errLogin)
	assert.Equal(t, int32(1), regSeq.Load(), "concurrent cold GetToken and Login calls must coalesce into one registration")
	assert.Equal(t, "tok-coalesced-client-1-scope-a", tokA.AccessToken)
	assert.Equal(t, "tok-coalesced-client-1-scope-b", tokB.AccessToken)

	// Cached entries must be indexed under coalesced-client-1's fingerprint.
	expectedFP := h.cacheFingerprint(ctx, auth.FlowClientCredentials)
	entries, err := h.ListCachedTokens(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	for _, entry := range entries {
		assert.Equal(t, expectedFP, entry.Fingerprint)
	}
}

func TestHandler_ConfidentialTokenExchange_URLEncodesBasicAuthCredentials(t *testing.T) {
	t.Parallel()

	rawClientID := "my+client:id%1 with space"
	rawClientSecret := "sec:ret+val%2 with space"

	var receivedAuthHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthHeader = r.Header.Get("Authorization")
		writeJSON(w, tokenResponse{AccessToken: "rfc6749-basic-tok", TokenType: "Bearer", ExpiresIn: 1800})
	}))
	defer srv.Close()

	dir := t.TempDir()
	jwtFile := filepath.Join(dir, "jwt")
	require.NoError(t, os.WriteFile(jwtFile, []byte("subject-jwt"), 0o600))

	h, err := New(config.CustomOAuth2Config{
		Name:                      "rfc6749-basic",
		TokenURL:                  srv.URL + "/token",
		ClientID:                  rawClientID,
		ClientSecret:              rawClientSecret,
		FederatedTokenFile:        jwtFile,
		WorkloadIdentityGrantType: config.WorkloadIdentityGrantTokenExchange,
	}, WithSecretStore(secrets.NewMockStore()), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)

	tok, err := h.GetToken(context.Background(), auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "rfc6749-basic-tok", tok.AccessToken)

	encodedPayload, ok := strings.CutPrefix(receivedAuthHeader, "Basic ")
	require.True(t, ok, "expected Basic Authorization header")
	decodedBytes, err := base64.StdEncoding.DecodeString(encodedPayload)
	require.NoError(t, err)

	expectedPair := url.QueryEscape(rawClientID) + ":" + url.QueryEscape(rawClientSecret)
	assert.Equal(t, expectedPair, string(decodedBytes))
}

type deleteOnCorruptStore struct {
	*secrets.MockStore
	corruptKey       string
	corruptErr       error
	failSet          bool
	cancelAfterGetFn context.CancelFunc
	checkCtxOnGetSet bool
}

func (s *deleteOnCorruptStore) Get(ctx context.Context, name string) ([]byte, error) {
	if s.checkCtxOnGetSet {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	if s.corruptKey != "" && name == s.corruptKey {
		s.corruptKey = ""
		_ = s.Delete(context.WithoutCancel(ctx), name)
		if s.cancelAfterGetFn != nil {
			s.cancelAfterGetFn()
		}
		return nil, s.corruptErr
	}
	return s.MockStore.Get(ctx, name)
}

func (s *deleteOnCorruptStore) Set(ctx context.Context, name string, value []byte) error {
	if s.checkCtxOnGetSet {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if s.failSet {
		return errors.New("disk read-only")
	}
	return s.MockStore.Set(ctx, name, value)
}

func TestHandler_BlockedLogin_CannotUndoCompletedLogout(t *testing.T) {
	t.Parallel()

	t.Run("OAuth Login blocked at token endpoint does not overwrite Logout", func(t *testing.T) {
		t.Parallel()
		entered := make(chan struct{})
		release := make(chan struct{})

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, r.ParseForm())
			close(entered)
			<-release
			writeJSON(w, tokenResponse{
				AccessToken:  "late-login-token",
				TokenType:    "Bearer",
				ExpiresIn:    3600,
				RefreshToken: "late-login-refresh",
			})
		}))
		defer srv.Close()

		store := secrets.NewMockStore()
		h, err := New(config.CustomOAuth2Config{
			Name:         "race-oauth-login",
			TokenURL:     srv.URL + "/token",
			ClientID:     "cid",
			ClientSecret: "csec",
			DefaultFlow:  "client_credentials",
		}, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
		require.NoError(t, err)

		ctx := context.Background()
		errCh := make(chan error, 1)
		go func() {
			_, lErr := h.Login(ctx, auth.LoginOptions{Flow: auth.FlowClientCredentials})
			errCh <- lErr
		}()

		<-entered
		require.NoError(t, h.Logout(ctx))
		close(release)

		loginErr := <-errCh
		require.ErrorIs(t, loginErr, auth.ErrNotAuthenticated)

		st, stErr := h.Status(ctx)
		require.NoError(t, stErr)
		assert.False(t, st.Authenticated, "blocked OAuth Login must not undo Logout")

		_, tokErr := h.GetToken(ctx, auth.TokenOptions{})
		require.ErrorIs(t, tokErr, auth.ErrNotAuthenticated)
	})

	t.Run("patLogin blocked at VerifyURL does not overwrite Logout", func(t *testing.T) {
		t.Parallel()
		entered := make(chan struct{})
		release := make(chan struct{})

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			close(entered)
			<-release
			writeJSON(w, map[string]any{"username": "robot-user"})
		}))
		defer srv.Close()

		store := secrets.NewMockStore()
		ctx := context.Background()
		require.NoError(t, store.Set(ctx, "robot-pat", []byte("pat-secret-value")))

		h, err := New(config.CustomOAuth2Config{
			Name:           "race-pat-login",
			Token:          "secret://robot-pat",
			Username:       "acme+robot",
			VerifyURL:      srv.URL + "/verify",
			IdentityFields: &config.IdentityFieldMapping{Username: "username"},
			DefaultFlow:    "pat",
		}, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
		require.NoError(t, err)

		errCh := make(chan error, 1)
		go func() {
			_, lErr := h.Login(ctx, auth.LoginOptions{Flow: auth.FlowPAT})
			errCh <- lErr
		}()

		<-entered
		require.NoError(t, h.Logout(ctx))
		close(release)

		loginErr := <-errCh
		require.ErrorIs(t, loginErr, auth.ErrNotAuthenticated)

		st, stErr := h.Status(ctx)
		require.NoError(t, stErr)
		assert.False(t, st.Authenticated, "blocked PAT Login must not undo Logout")

		_, tokErr := h.GetToken(ctx, auth.TokenOptions{})
		require.ErrorIs(t, tokErr, auth.ErrNotAuthenticated)
	})
}

func TestHandler_ConfigFingerprintIsolation_PAT_And_Refresh(t *testing.T) {
	t.Parallel()

	t.Run("PAT session is invalidated when token username or registry changes", func(t *testing.T) {
		t.Parallel()
		store := secrets.NewMockStore()
		ctx := context.Background()
		require.NoError(t, store.Set(ctx, "old-pat", []byte("old-registry-token")))
		require.NoError(t, store.Set(ctx, "new-pat", []byte("new-registry-token")))

		hOld, err := New(config.CustomOAuth2Config{
			Name:        "reg-pat",
			Token:       "secret://old-pat",
			Username:    "oldorg+robot",
			Registry:    "old-registry.example.com",
			DefaultFlow: "pat",
		}, WithSecretStore(store), WithLogger(logr.Discard()))
		require.NoError(t, err)
		_, err = hOld.Login(ctx, auth.LoginOptions{Flow: auth.FlowPAT})
		require.NoError(t, err)

		// Restart with updated PAT SecretRef, username, and registry under the same handler name.
		hNew, err := New(config.CustomOAuth2Config{
			Name:        "reg-pat",
			Token:       "secret://new-pat",
			Username:    "neworg+robot",
			Registry:    "new-registry.example.com",
			DefaultFlow: "pat",
		}, WithSecretStore(store), WithLogger(logr.Discard()))
		require.NoError(t, err)

		tok, err := hNew.GetToken(ctx, auth.TokenOptions{})
		require.NoError(t, err)
		assert.Equal(t, "new-registry-token", tok.AccessToken)
		assert.Equal(t, "neworg+robot", hNew.RegistryUsernameForContext(ctx))

		st, err := hNew.Status(ctx)
		require.NoError(t, err)
		assert.True(t, st.Authenticated)
		assert.Equal(t, "neworg+robot", st.Claims.Username)
	})

	t.Run("Refresh token is not submitted to a different issuer or replaced DCR client", func(t *testing.T) {
		t.Parallel()
		var newIdPRefreshSubmitted atomic.Bool
		var newIdPCCCalls atomic.Int32

		srvOld := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, tokenResponse{
				AccessToken:  "old-idp-access",
				TokenType:    "Bearer",
				ExpiresIn:    1,
				RefreshToken: "old-idp-refresh-secret",
			})
		}))
		defer srvOld.Close()

		srvNew := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, r.ParseForm())
			if r.FormValue("grant_type") == "refresh_token" {
				newIdPRefreshSubmitted.Store(true)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			newIdPCCCalls.Add(1)
			writeJSON(w, tokenResponse{
				AccessToken: "new-idp-access",
				TokenType:   "Bearer",
				ExpiresIn:   3600,
			})
		}))
		defer srvNew.Close()

		store := secrets.NewMockStore()
		ctx := context.Background()

		hOld, err := New(config.CustomOAuth2Config{
			Name:         "repointed-oauth",
			TokenURL:     srvOld.URL + "/token",
			ClientID:     "cid-old",
			ClientSecret: "csec-old",
			DefaultFlow:  "client_credentials",
		}, WithSecretStore(store), WithHTTPClient(srvOld.Client()), WithLogger(logr.Discard()))
		require.NoError(t, err)
		_, err = hOld.Login(ctx, auth.LoginOptions{})
		require.NoError(t, err)

		// Repoint same handler name at a new TokenURL: old refresh_token must NOT be sent to srvNew.
		hNew, err := New(config.CustomOAuth2Config{
			Name:         "repointed-oauth",
			TokenURL:     srvNew.URL + "/token",
			ClientID:     "cid-old",
			ClientSecret: "csec-new",
			DefaultFlow:  "client_credentials",
		}, WithSecretStore(store), WithHTTPClient(srvNew.Client()), WithLogger(logr.Discard()))
		require.NoError(t, err)

		tok, err := hNew.GetToken(ctx, auth.TokenOptions{MinValidFor: 10 * time.Minute})
		require.NoError(t, err)
		assert.Equal(t, "new-idp-access", tok.AccessToken)
		assert.False(t, newIdPRefreshSubmitted.Load(), "previous issuer refresh token must never be submitted to new TokenURL")
		assert.Equal(t, int32(1), newIdPCCCalls.Load())
	})

	t.Run("DCR client replacement invalidates previous client refresh token", func(t *testing.T) {
		t.Parallel()
		var regSeq atomic.Int32
		var staleRefreshSubmitted atomic.Bool

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/register":
				n := regSeq.Add(1)
				w.WriteHeader(http.StatusCreated)
				exp := time.Now().Add(time.Hour).Unix()
				if n == 1 {
					// Already-expired client secret after login forces DCR replacement on next run.
					exp = time.Now().Add(-10 * time.Second).Unix()
				}
				writeJSON(w, map[string]any{
					"client_id":                fmt.Sprintf("dcr-client-%d", n),
					"client_secret":            fmt.Sprintf("dcr-secret-%d", n),
					"client_secret_expires_at": exp,
				})
			case "/token":
				require.NoError(t, r.ParseForm())
				if r.FormValue("grant_type") == "refresh_token" {
					staleRefreshSubmitted.Store(true)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				writeJSON(w, tokenResponse{
					AccessToken:  "access-for-" + r.FormValue("client_id"),
					TokenType:    "Bearer",
					ExpiresIn:    1,
					RefreshToken: "refresh-for-" + r.FormValue("client_id"),
				})
			}
		}))
		defer srv.Close()

		store := secrets.NewMockStore()
		ctx := context.Background()
		cfg := config.CustomOAuth2Config{
			Name:        "dcr-rotate",
			TokenURL:    srv.URL + "/token",
			DefaultFlow: "client_credentials",
			DynamicClientRegistration: &config.DynamicClientRegistrationConfig{
				RegistrationEndpoint: srv.URL + "/register",
			},
		}

		h1, err := New(cfg, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
		require.NoError(t, err)
		_, err = h1.Login(ctx, auth.LoginOptions{})
		require.NoError(t, err)

		// Next process sees dcr-client-1 is expired, must reject dcr-client-1's refresh token,
		// re-register dcr-client-2, and mint cleanly under dcr-client-2.
		h2, err := New(cfg, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
		require.NoError(t, err)
		tok, err := h2.GetToken(ctx, auth.TokenOptions{MinValidFor: 5 * time.Minute})
		require.NoError(t, err)
		assert.Equal(t, "access-for-dcr-client-2", tok.AccessToken)
		assert.False(t, staleRefreshSubmitted.Load(), "refresh token bound to expired/replaced DCR client must not be submitted")
	})
}

func TestHandler_CorruptedLogoutMarker_RemainsFailClosedAfterStoreAutoDelete(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	baseStore := secrets.NewMockStore()
	require.NoError(t, baseStore.Set(ctx, "ambient-pat", []byte("ambient-pat-token")))

	cfg := config.CustomOAuth2Config{
		Name:        "corrupt-logout",
		Token:       "secret://ambient-pat",
		DefaultFlow: "pat",
	}

	stateKey := secretKeyPrefix + "corrupt-logout." + secretKeySessionStateSuffix
	require.NoError(t, baseStore.Set(ctx, stateKey, []byte(sessionStateLoggedOut)))

	// Simulate secrets.Store.Get auto-deleting an undecryptable entry before returning CorruptedSecretError.
	delStore := &deleteOnCorruptStore{
		MockStore:  baseStore,
		corruptKey: stateKey,
		corruptErr: secrets.NewCorruptedSecretError(stateKey, "decryption failed", errors.New("cipher: message authentication failed")),
	}

	h1, err := New(cfg, WithSecretStore(delStore), WithLogger(logr.Discard()))
	require.NoError(t, err)

	// 1st read triggers corruption + auto-delete in store; fails closed and persists corrupt tombstone.
	_, err = h1.GetToken(ctx, auth.TokenOptions{})
	require.Error(t, err)

	// 2nd read on same handler still fails closed even though the original entry was deleted.
	_, err = h1.GetToken(ctx, auth.TokenOptions{})
	require.Error(t, err)
	st1, stErr := h1.Status(ctx)
	require.NoError(t, stErr)
	assert.False(t, st1.Authenticated)

	// 3rd read on a fresh Handler (new CLI process) also stays fail-closed via persisted corrupt tombstone.
	h2, err := New(cfg, WithSecretStore(delStore), WithLogger(logr.Discard()))
	require.NoError(t, err)
	_, err = h2.GetToken(ctx, auth.TokenOptions{})
	require.Error(t, err)
	st2, stErr := h2.Status(ctx)
	require.NoError(t, stErr)
	assert.False(t, st2.Authenticated)

	// Even when Set fails (read-only disk), consecutive reads on the same Handler stay fail-closed locally.
	require.NoError(t, baseStore.Set(ctx, stateKey, []byte(sessionStateLoggedOut)))
	roStore := &deleteOnCorruptStore{
		MockStore:  baseStore,
		corruptKey: stateKey,
		corruptErr: errors.New("cipher: message authentication failed"),
		failSet:    true,
	}
	hRO, err := New(cfg, WithSecretStore(roStore), WithLogger(logr.Discard()))
	require.NoError(t, err)
	_, err = hRO.GetToken(ctx, auth.TokenOptions{})
	require.Error(t, err)
	_, err = hRO.GetToken(ctx, auth.TokenOptions{})
	require.Error(t, err, "local corrupt state must remain fail-closed when Set fails after auto-delete")

	// Context cancellation on Get must NOT poison a healthy handler with sessionStateCorrupt.
	healthyBase := secrets.NewMockStore()
	require.NoError(t, healthyBase.Set(ctx, "ambient-pat", []byte("ambient-pat-token")))
	ctxStore := &deleteOnCorruptStore{MockStore: healthyBase, checkCtxOnGetSet: true}
	hCtx, err := New(cfg, WithSecretStore(ctxStore), WithLogger(logr.Discard()))
	require.NoError(t, err)
	canceledCtx, cancelNow := context.WithCancel(ctx)
	cancelNow()
	stCanceled, stCancelErr := hCtx.Status(canceledCtx)
	require.NoError(t, stCancelErr)
	assert.False(t, stCanceled.Authenticated)
	tokHealthy, err := hCtx.GetToken(ctx, auth.TokenOptions{})
	require.NoError(t, err, "prior canceled ctx must not poison sessionStates as corrupt")
	assert.Equal(t, "ambient-pat-token", tokHealthy.AccessToken)

	// Tombstone write succeeds even if caller ctx expires immediately after Store.Get auto-deletes corrupt entry.
	expBase := secrets.NewMockStore()
	require.NoError(t, expBase.Set(ctx, "ambient-pat", []byte("ambient-pat-token")))
	require.NoError(t, expBase.Set(ctx, stateKey, []byte(sessionStateLoggedOut)))
	callCtx, cancelAfterGet := context.WithCancel(ctx)
	expStore := &deleteOnCorruptStore{
		MockStore:        expBase,
		corruptKey:       stateKey,
		corruptErr:       secrets.NewCorruptedSecretError(stateKey, "decryption failed", errors.New("bad mac")),
		cancelAfterGetFn: cancelAfterGet,
		checkCtxOnGetSet: true,
	}
	hExp1, err := New(cfg, WithSecretStore(expStore), WithLogger(logr.Discard()))
	require.NoError(t, err)
	_, err = hExp1.GetToken(callCtx, auth.TokenOptions{})
	require.Error(t, err)
	hExp2, err := New(cfg, WithSecretStore(expStore), WithLogger(logr.Discard()))
	require.NoError(t, err)
	_, err = hExp2.GetToken(ctx, auth.TokenOptions{})
	require.Error(t, err, "WithoutCancel tombstone write must persist across processes even when caller ctx expired")
}

func TestHandler_CrossFlowReLogin_ClearsStaleRefreshAndPATArtifacts(t *testing.T) {
	t.Parallel()

	var refreshSubmitted atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		if r.FormValue("grant_type") == "refresh_token" {
			refreshSubmitted.Store(true)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		writeJSON(w, tokenResponse{
			AccessToken: "cc-short-lived-token",
			TokenType:   "Bearer",
			ExpiresIn:   1,
		})
	}))
	defer srv.Close()

	store := secrets.NewMockStore()
	ctx := context.Background()
	userFile := filepath.Join(t.TempDir(), "pat-user")
	require.NoError(t, os.WriteFile(userFile, []byte("acme+stored_robot"), 0o600))
	require.NoError(t, store.Set(ctx, "hybrid-pat", []byte("pat-token-val")))

	cfg := config.CustomOAuth2Config{
		Name:             "hybrid-switch",
		AuthorizeURL:     srv.URL + "/authorize",
		TokenURL:         srv.URL + "/token",
		ClientID:         "cid",
		ClientSecret:     "csec",
		Token:            "secret://hybrid-pat",
		Username:         "file://" + userFile,
		RegistryUsername: "$oauthtoken",
	}

	h1, err := New(cfg, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)

	// 1. Login with PAT, then check standalone RegistryUsernameForContext in a fresh process after file is removed.
	_, err = h1.Login(ctx, auth.LoginOptions{Flow: auth.FlowPAT})
	require.NoError(t, err)
	require.NoError(t, os.Remove(userFile))

	hFreshPAT, err := New(cfg, WithSecretStore(store), WithHTTPClient(srv.Client()), WithLogger(logr.Discard()))
	require.NoError(t, err)
	assert.Equal(t, "acme+stored_robot", hFreshPAT.RegistryUsernameForContext(ctx))

	// 2. Seed a valid refresh token as if from an earlier interactive login, then re-login with client_credentials (no refresh_token).
	require.NoError(t, h1.storeRefreshToken(ctx, "interactive-user-refresh"))
	_, err = h1.Login(ctx, auth.LoginOptions{Flow: auth.FlowClientCredentials})
	require.NoError(t, err)

	// Previous PAT artifacts and interactive refresh_token must be cleared so subsequent GetToken mints client_credentials cleanly.
	_, patErr := store.Get(ctx, h1.profileSecretKey(ctx, secretKeyPATTokenSuffix))
	assert.ErrorIs(t, patErr, secrets.ErrNotFound)

	tok, err := h1.GetToken(ctx, auth.TokenOptions{MinValidFor: 5 * time.Minute})
	require.NoError(t, err)
	assert.Equal(t, "cc-short-lived-token", tok.AccessToken)
	assert.False(t, refreshSubmitted.Load(), "re-login with client_credentials must clear stale refresh_token from earlier flow")
}

func BenchmarkHandler_PAT_GetToken(b *testing.B) {
	store := secrets.NewMockStore()
	ctx := context.Background()
	_ = store.Set(ctx, "bench-pat", []byte("static-pat-token"))
	cfg := config.CustomOAuth2Config{
		Name:        "bench-pat",
		Token:       "secret://bench-pat",
		Username:    "myorg+robot",
		DefaultFlow: "pat",
	}
	h, _ := New(cfg, WithSecretStore(store), WithLogger(logr.Discard()))

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = h.GetToken(ctx, auth.TokenOptions{})
	}
}
