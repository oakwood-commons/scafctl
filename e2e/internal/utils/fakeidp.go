// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/onsi/gomega"
)

// FakeIDP is a programmable, in-process OAuth2 identity provider for e2e auth
// specs. It stands in for a real IdP (Entra/GCP/GitHub/custom OAuth2) so the
// suite can exercise scafctl's own auth wiring -- CLI orchestration, token
// caching, storage, and error surfacing -- deterministically and without any
// network access or credentials.
//
// It serves the two endpoints the generic OAuth2 handler talks to:
//   - POST /token    (RFC 6749 token endpoint; client_credentials + refresh_token)
//   - GET  /userinfo (identity endpoint used as the handler's verifyURL)
//
// Tests program responses via the setter methods (safe for concurrent use) and
// inspect the recorded request log to assert what the CLI actually sent.
type FakeIDP struct {
	server *httptest.Server

	mu           sync.Mutex
	clientCreds  grantResponse
	refresh      grantResponse
	userinfo     userinfoResponse
	tokenCalls   int
	refreshCalls int
	calls        []FakeIDPRequest

	// scopedTokens, when enabled, makes each successful client_credentials
	// grant mint a distinct access token derived from the requested scope, so
	// two different scopes yield two different tokens. This lets scope-isolation
	// specs observe that a token minted for scope A is never substituted for
	// scope B.
	scopedTokens bool

	// issued maps each minted access token to the scope it was granted for, so
	// a ProtectedResource fixture can enforce scope without the handler leaking
	// any internal state.
	issued map[string]string
}

// grantResponse programs a single /token grant outcome. When Status is >= 400
// or ErrorCode is non-empty, the endpoint returns an OAuth2 error object;
// otherwise it returns a success token payload.
type grantResponse struct {
	Status       int
	AccessToken  string
	TokenType    string
	ExpiresIn    int
	RefreshToken string
	Scope        string
	ErrorCode    string
	ErrorDesc    string
}

type userinfoResponse struct {
	Status int
	Body   map[string]any
}

// FakeIDPRequest is a recorded inbound request to the fake IdP.
type FakeIDPRequest struct {
	Path      string
	GrantType string
	Scope     string
	Form      url.Values
	AuthHdr   string
}

// NewFakeIDP starts a fake IdP with sensible happy-path defaults: the
// client_credentials grant issues a bearer access token valid for one hour, and
// /userinfo returns a stable identity. Callers override behavior per spec via
// the setter methods. The server is automatically shut down at spec cleanup.
func NewFakeIDP() *FakeIDP {
	idp := &FakeIDP{
		clientCreds: grantResponse{
			AccessToken: "fake-access-token",
			TokenType:   "Bearer",
			ExpiresIn:   3600,
		},
		refresh: grantResponse{
			AccessToken: "fake-refreshed-token",
			TokenType:   "Bearer",
			ExpiresIn:   3600,
		},
		userinfo: userinfoResponse{
			Status: http.StatusOK,
			Body: map[string]any{
				"sub":                "e2e-user",
				"preferred_username": "e2e-user",
				"email":              "e2e-user@example.com",
				"name":               "E2E User",
			},
		},
		issued: make(map[string]string),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/token", idp.handleToken)
	mux.HandleFunc("/userinfo", idp.handleUserinfo)
	idp.server = httptest.NewServer(mux)

	return idp
}

// URL returns the base URL of the fake IdP.
func (f *FakeIDP) URL() string { return f.server.URL }

// TokenURL returns the token endpoint URL for use as customOAuth2 tokenURL.
func (f *FakeIDP) TokenURL() string { return f.server.URL + "/token" }

// UserinfoURL returns the identity endpoint URL for use as customOAuth2 verifyURL.
func (f *FakeIDP) UserinfoURL() string { return f.server.URL + "/userinfo" }

// Close shuts down the underlying server. Safe to call more than once.
func (f *FakeIDP) Close() {
	if f.server != nil {
		f.server.Close()
	}
}

// SetClientCredentials programs the next (and subsequent) successful
// client_credentials responses with the given access/refresh tokens and expiry.
func (f *FakeIDP) SetClientCredentials(accessToken, refreshToken string, expiresIn int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clientCreds = grantResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    expiresIn,
		RefreshToken: refreshToken,
	}
}

// FailToken makes the client_credentials grant return an OAuth2 error (HTTP 401),
// simulating invalid credentials or a rejected grant.
func (f *FakeIDP) FailToken(errorCode, description string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clientCreds = grantResponse{
		Status:    http.StatusUnauthorized,
		ErrorCode: errorCode,
		ErrorDesc: description,
	}
}

// SetRefreshResponse programs the next successful refresh_token grant response.
func (f *FakeIDP) SetRefreshResponse(accessToken string, expiresIn int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refresh = grantResponse{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresIn:   expiresIn,
	}
}

// FailRefresh makes the refresh_token grant return an OAuth2 error (HTTP 400),
// simulating a revoked or invalid refresh token.
func (f *FakeIDP) FailRefresh(errorCode, description string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refresh = grantResponse{
		Status:    http.StatusBadRequest,
		ErrorCode: errorCode,
		ErrorDesc: description,
	}
}

// TokenCalls returns the number of client_credentials grant requests received.
func (f *FakeIDP) TokenCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokenCalls
}

// RefreshCalls returns the number of refresh_token grant requests received.
func (f *FakeIDP) RefreshCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refreshCalls
}

// Requests returns a copy of the recorded request log.
func (f *FakeIDP) Requests() []FakeIDPRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]FakeIDPRequest, len(f.calls))
	copy(out, f.calls)
	return out
}

// EnableScopedTokens makes each successful client_credentials grant mint a
// distinct access token derived from the requested scope. Use it in scope
// isolation specs so tokens for different scopes are observably different.
func (f *FakeIDP) EnableScopedTokens() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scopedTokens = true
}

// GrantedScope reports the scope an access token was issued for, and whether
// the token was issued by this IdP at all.
func (f *FakeIDP) GrantedScope(accessToken string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	scope, ok := f.issued[accessToken]
	return scope, ok
}

func (f *FakeIDP) handleToken(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	form, _ := url.ParseQuery(string(body))
	grantType := form.Get("grant_type")

	f.mu.Lock()
	f.calls = append(f.calls, FakeIDPRequest{
		Path:      "/token",
		GrantType: grantType,
		Scope:     form.Get("scope"),
		Form:      form,
		AuthHdr:   r.Header.Get("Authorization"),
	})
	var resp grantResponse
	switch grantType {
	case "refresh_token":
		f.refreshCalls++
		resp = f.refresh
	default:
		f.tokenCalls++
		resp = f.clientCreds
	}
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if resp.Status >= http.StatusBadRequest || resp.ErrorCode != "" {
		status := resp.Status
		if status == 0 {
			status = http.StatusBadRequest
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":             resp.ErrorCode,
			"error_description": resp.ErrorDesc,
		})
		return
	}

	tokenType := resp.TokenType
	if tokenType == "" {
		tokenType = "Bearer"
	}

	accessToken := resp.AccessToken
	requestedScope := form.Get("scope")
	// In scoped mode, mint a distinct token per requested scope so specs can
	// prove a token issued for scope A is never reused for scope B. Record the
	// grant so a ProtectedResource fixture can enforce it.
	f.mu.Lock()
	if f.scopedTokens && grantType != "refresh_token" {
		accessToken = "access-token::" + requestedScope
	}
	if accessToken != "" {
		f.issued[accessToken] = requestedScope
	}
	f.mu.Unlock()

	payload := map[string]any{
		"access_token": accessToken,
		"token_type":   tokenType,
		"expires_in":   resp.ExpiresIn,
	}
	if resp.RefreshToken != "" {
		payload["refresh_token"] = resp.RefreshToken
	}
	// Echo the granted scope so the handler records what was actually issued.
	scope := resp.Scope
	if scope == "" {
		scope = requestedScope
	}
	if scope != "" {
		payload["scope"] = scope
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(payload)
}

func (f *FakeIDP) handleUserinfo(w http.ResponseWriter, r *http.Request) {
	authorization := r.Header.Get("Authorization")
	f.mu.Lock()
	f.calls = append(f.calls, FakeIDPRequest{Path: "/userinfo", AuthHdr: authorization})
	resp := f.userinfo
	f.mu.Unlock()

	if authorization == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return
	}

	bearerToken := strings.TrimPrefix(authorization, "Bearer ")
	if bearerToken == authorization {
		bearerToken = ""
	}
	f.mu.Lock()
	_, ok := f.issued[bearerToken]
	f.mu.Unlock()
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_token"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	status := resp.Status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp.Body)
}

// ProtectedResource is a fake, scope-enforcing API server backed by a FakeIdP.
// It authorizes a request by looking up the bearer token's granted scope in the
// IdP and comparing it against a required scope, returning:
//
//   - 401 Unauthorized when no bearer token is present or the token was not
//     issued by the backing IdP,
//   - 403 Forbidden when the token's granted scope does not include the
//     required scope,
//   - 200 OK otherwise.
//
// It lets scope-isolation specs assert an observable authorization outcome
// rather than only inspecting cache keys. The required scope is supplied per
// request via the "require" query parameter, so one server can stand in for
// several protected resources.
type ProtectedResource struct {
	server *httptest.Server
	idp    *FakeIDP

	mu             sync.Mutex
	lastAuthHeader string
}

// NewProtectedResource starts a scope-enforcing resource server backed by this
// IdP. The server is automatically shut down at spec cleanup.
func (f *FakeIDP) NewProtectedResource() *ProtectedResource {
	pr := &ProtectedResource{idp: f}
	pr.server = httptest.NewServer(http.HandlerFunc(pr.handle))
	return pr
}

// URL returns the resource URL that requires the given scope.
func (p *ProtectedResource) URL(requireScope string) string {
	return p.server.URL + "/resource?require=" + url.QueryEscape(requireScope)
}

// Close shuts down the underlying server. Safe to call more than once.
func (p *ProtectedResource) Close() {
	if p.server != nil {
		p.server.Close()
	}
}

// Get presents the given access token as a bearer credential to the resource
// requiring requireScope, returning the resulting HTTP status code. It models a
// downstream consumer without depending on any specific scafctl consumer path.
func (p *ProtectedResource) Get(accessToken, requireScope string) int {
	gomega.Expect(p.server).NotTo(gomega.BeNil())

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, p.URL(requireScope), nil)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}

	resp, err := http.DefaultClient.Do(req)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// LastAuthHeader returns the most recent Authorization header the resource
// received, held in memory only (never logged) so redaction specs can assert
// the token reached the consumer without it leaking to output.
func (p *ProtectedResource) LastAuthHeader() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastAuthHeader
}

func (p *ProtectedResource) handle(w http.ResponseWriter, r *http.Request) {
	authHdr := r.Header.Get("Authorization")
	p.mu.Lock()
	p.lastAuthHeader = authHdr
	p.mu.Unlock()

	token := strings.TrimPrefix(authHdr, "Bearer ")
	if token == "" || token == authHdr {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	granted, ok := p.idp.GrantedScope(token)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	required := r.URL.Query().Get("require")
	if required != "" && !scopeContains(granted, required) {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// scopeContains reports whether the space-separated granted scope set includes
// the required scope.
func scopeContains(granted, required string) bool {
	for _, s := range strings.Fields(granted) {
		if s == required {
			return true
		}
	}
	return false
}

// OAuth2HandlerSpec describes one custom OAuth2 handler to register in an
// isolated config, bound to a FakeIdP. Scopes are the handler's fixed scopes,
// which the client_credentials flow sends to the token endpoint at login.
type OAuth2HandlerSpec struct {
	Name   string
	Scopes []string
}

// WriteAuthConfig writes a scafctl config.yaml into the isolated environment's
// config dir that registers a single custom OAuth2 auth handler bound to the
// given fake IdP, using the fully non-interactive client_credentials flow with
// the fixed "api:read" scope.
//
// The config also disables the official remote catalog (matching
// NewIsolatedEnv) so specs never reach the network.
func (i Isolation) WriteAuthConfig(handlerName string, idp *FakeIDP) {
	i.WriteAuthConfigHandlers(idp, OAuth2HandlerSpec{Name: handlerName, Scopes: []string{"api:read"}})
}

// WriteAuthConfigHandlers writes a scafctl config.yaml registering one or more
// custom OAuth2 handlers bound to the given fake IdP. Each handler uses the
// non-interactive client_credentials flow with its own fixed scopes, which lets
// scope-isolation specs give distinct handlers distinct resource scopes against
// a single IdP. The config also disables the official remote catalog so specs
// never reach the network.
func (i Isolation) WriteAuthConfigHandlers(idp *FakeIDP, handlers ...OAuth2HandlerSpec) {
	gomega.Expect(handlers).NotTo(gomega.BeEmpty())

	var b strings.Builder
	b.WriteString(`# e2e auth test config
catalogs:
  - name: local
    type: filesystem
settings:
  disableOfficialCatalog: true
auth:
  customOAuth2:
`)
	for _, h := range handlers {
		gomega.Expect(h.Name).NotTo(gomega.BeEmpty())
		scopes := h.Scopes
		if len(scopes) == 0 {
			scopes = []string{"api:read"}
		}
		fmt.Fprintf(&b, `    - name: %s
      displayName: "E2E OAuth2 %s"
      tokenURL: %q
      verifyURL: %q
      clientID: e2e-client
      clientSecret: e2e-secret
      defaultFlow: client_credentials
      scopes:
`, h.Name, h.Name, idp.TokenURL(), idp.UserinfoURL())
		for _, s := range scopes {
			fmt.Fprintf(&b, "        - %s\n", s)
		}
		b.WriteString(`      identityFields:
        username: preferred_username
        email: email
        name: name
`)
	}

	configPath := filepath.Join(i.Root, "scafctl", "config.yaml")
	gomega.Expect(os.MkdirAll(filepath.Dir(configPath), 0o755)).To(gomega.Succeed())
	gomega.Expect(os.WriteFile(configPath, []byte(b.String()), 0o600)).To(gomega.Succeed())
}
