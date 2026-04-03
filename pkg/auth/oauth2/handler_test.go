// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package oauth2

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
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
		{name: "client credentials", override: func(c *config.CustomOAuth2Config) { c.ClientSecret = "s" }, expected: []auth.Flow{auth.FlowClientCredentials}},
		{name: "all flows", override: func(c *config.CustomOAuth2Config) {
			c.AuthorizeURL = "https://x.com/auth"
			c.DeviceAuthURL = "https://x.com/device"
			c.ClientSecret = "s"
		}, expected: []auth.Flow{auth.FlowInteractive, auth.FlowDeviceCode, auth.FlowClientCredentials}},
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
	meta := handlerMetadata{Claims: &auth.Claims{Username: "testuser"}, ExpiresAt: time.Now().Add(1 * time.Hour), Scopes: []string{"read"}}
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
	meta := handlerMetadata{Claims: &auth.Claims{Username: "testuser"}, ExpiresAt: time.Now().Add(-1 * time.Hour), Scopes: []string{"read"}}
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
	h, store := newTestHandler(t, srv, func(cfg *config.CustomOAuth2Config) { cfg.ClientSecret = "test-secret" })
	_ = store.Set(context.Background(), secretKeyPrefix+"test-provider."+secretKeyRefreshSuffix, []byte("good-refresh"))
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
	derived := &auth.Token{AccessToken: "stored-derived-token", TokenType: "Bearer", ExpiresAt: time.Now().Add(1 * time.Hour), Flow: auth.FlowClientCredentials}
	derivedBytes, _ := json.Marshal(derived)
	_ = store.Set(context.Background(), secretKeyPrefix+"test-provider."+derivedTokenSuffix, derivedBytes)
	token, err := h.GetToken(context.Background(), auth.TokenOptions{})
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
		{name: "auto interactive", override: func(c *config.CustomOAuth2Config) { c.AuthorizeURL = "https://x.com/auth" }, expected: auth.FlowInteractive},
		{name: "auto device", override: func(c *config.CustomOAuth2Config) { c.DeviceAuthURL = "https://x.com/d" }, expected: auth.FlowDeviceCode},
		{name: "auto client_credentials", override: func(c *config.CustomOAuth2Config) { c.ClientSecret = "s" }, expected: auth.FlowClientCredentials},
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

	// Reset in-memory username to simulate process restart
	h.cfg.RegistryUsername = ""
	assert.Empty(t, h.RegistryUsername())

	// GetToken should load derived token and restore username
	token, err := h.GetToken(ctx, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "exchanged-token", token.AccessToken)
	assert.Equal(t, "exchange-user", h.RegistryUsername())
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
