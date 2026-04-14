// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package oauth2 implements a generic configurable OAuth2 auth handler.
// Each CustomOAuth2Config registers as its own named auth.Handler supporting
// authorization code + PKCE, device code (RFC 8628), and client credentials
// (RFC 6749 §4.4).
package oauth2

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/auth/oauth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
)

const (
	secretKeyPrefix         = "scafctl.auth.oauth2." //nolint:gosec // key prefix, not credential
	secretKeyRefreshSuffix  = "refresh_token"        //nolint:gosec // key suffix, not credential
	secretKeyMetadataSuffix = "metadata"             //nolint:gosec // key suffix, not credential
	tokenCacheSuffix        = "token."               //nolint:gosec // key suffix, not credential
	derivedTokenSuffix      = "derived_token"        //nolint:gosec // key suffix, not credential
	derivedUsernameSuffix   = "derived_username"     //nolint:gosec // key suffix, not credential
	defaultTimeout          = 5 * time.Minute
	defaultPollInterval     = 5
	maxResponseBody         = 1 << 20
	defaultTokenType        = "Bearer"
)

// Handler implements auth.Handler for generic configurable OAuth2 services.
type Handler struct {
	cfg         config.CustomOAuth2Config
	secretStore secrets.Store
	secretErr   error
	tokenCache  *auth.TokenCache
	httpClient  *http.Client
	logger      logr.Logger
}

// Option configures the Handler.
type Option func(*Handler)

// WithSecretStore sets a custom secrets store.
func WithSecretStore(store secrets.Store) Option {
	return func(h *Handler) { h.secretStore = store }
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(h *Handler) { h.httpClient = client }
}

// WithLogger sets the logger for the handler.
func WithLogger(lgr logr.Logger) Option {
	return func(h *Handler) { h.logger = lgr }
}

// New creates a new generic OAuth2 auth handler from a CustomOAuth2Config.
func New(cfg config.CustomOAuth2Config, opts ...Option) (*Handler, error) {
	// Implicit grant flow does not use PKCE.
	if cfg.ResponseType == "token" {
		cfg.DisablePKCE = true
	}

	h := &Handler{
		cfg:    cfg,
		logger: logr.Discard(),
	}
	for _, opt := range opts {
		opt(h)
	}

	if h.secretStore == nil {
		store, err := secrets.New()
		if err != nil {
			h.secretErr = fmt.Errorf("failed to initialize secrets store: %w", err)
		} else {
			h.secretStore = store
		}
	}
	if h.httpClient == nil {
		h.httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	if h.secretStore != nil {
		prefix := secretKeyPrefix + h.cfg.Name + "." + tokenCacheSuffix
		h.tokenCache = auth.NewTokenCache(h.secretStore, prefix)
	}
	return h, nil
}

func (h *Handler) Name() string { return h.cfg.Name }

// DisplayName returns the human-readable name.
func (h *Handler) DisplayName() string {
	if h.cfg.DisplayName != "" {
		return h.cfg.DisplayName
	}
	return h.cfg.Name
}

// RegistryUsername returns the configured OCI registry username convention for
// this handler (e.g. "$oauthtoken" for Quay.io). An empty string means the
// caller should use the default convention. This implements the optional
// catalog.RegistryUsernameProvider interface consumed by BridgeAuthToRegistry.
func (h *Handler) RegistryUsername() string { return h.cfg.RegistryUsername }

// SupportedFlows returns the flows this handler supports based on config.
func (h *Handler) SupportedFlows() []auth.Flow {
	var flows []auth.Flow
	if h.cfg.AuthorizeURL != "" {
		flows = append(flows, auth.FlowInteractive)
	}
	if h.cfg.DeviceAuthURL != "" {
		flows = append(flows, auth.FlowDeviceCode)
	}
	if h.cfg.ClientSecret != "" {
		flows = append(flows, auth.FlowClientCredentials)
	}
	return flows
}

// Capabilities returns the handler's capabilities.
func (h *Handler) Capabilities() []auth.Capability {
	return []auth.Capability{
		auth.CapScopesOnLogin,
		auth.CapCallbackPort,
		auth.CapFlowOverride,
	}
}

// Login performs authentication using the configured OAuth2 flow.
func (h *Handler) Login(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
	if err := h.ensureSecrets(); err != nil {
		return nil, err
	}

	flow := opts.Flow
	if flow == "" {
		flow = h.resolveDefaultFlow()
	}

	scopes := opts.Scopes
	if len(scopes) == 0 {
		scopes = h.cfg.Scopes
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var (
		tokenResp *tokenResponse
		err       error
	)

	switch flow { //nolint:exhaustive // only generic OAuth2 flows are supported
	case auth.FlowInteractive:
		callbackPort := opts.CallbackPort
		if callbackPort == 0 {
			callbackPort = h.cfg.CallbackPort
		}
		tokenResp, err = h.authCodeLogin(ctx, opts, scopes, callbackPort)
	case auth.FlowDeviceCode:
		tokenResp, err = h.deviceCodeLogin(ctx, opts, scopes)
	case auth.FlowClientCredentials:
		tokenResp, err = h.clientCredentialsLogin(ctx, scopes)
	default:
		return nil, auth.NewError(h.cfg.Name, "login",
			fmt.Errorf("%w: %s (supported: %v)", auth.ErrFlowNotSupported, flow, h.SupportedFlows()))
	}
	if err != nil {
		return nil, err
	}

	// Token exchange (optional post-flow pipeline)
	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	if h.cfg.TokenExchange != nil {
		derived, exchangeErr := h.executeTokenExchange(ctx, tokenResp.AccessToken)
		if exchangeErr != nil {
			return nil, auth.NewError(h.cfg.Name, "token_exchange", exchangeErr)
		}
		// Use the primary token's expiry for the derived token since the exchange
		// response does not carry its own expiry.
		if storeErr := h.storeDerivedToken(ctx, derived, expiresAt, flow, scopes); storeErr != nil {
			h.logger.V(1).Info("failed to cache derived token", "error", storeErr)
		}
	}

	// Verify token identity (optional)
	var claims *auth.Claims
	if h.cfg.VerifyURL != "" {
		claims, err = h.verifyToken(ctx, tokenResp.AccessToken)
		if err != nil {
			h.logger.V(1).Info("token verification failed, continuing without identity", "error", err)
		}
	}
	if claims == nil {
		claims = &auth.Claims{}
	}

	if storeErr := h.storeTokens(ctx, tokenResp, claims, expiresAt, flow, scopes); storeErr != nil {
		h.logger.V(1).Info("failed to store tokens", "error", storeErr)
	}

	return &auth.Result{Claims: claims, ExpiresAt: expiresAt}, nil
}

// Logout clears all stored tokens for this handler.
func (h *Handler) Logout(ctx context.Context) error {
	if err := h.ensureSecrets(); err != nil {
		return err
	}
	var errs []error
	if h.tokenCache != nil {
		if err := h.tokenCache.Clear(ctx); err != nil {
			errs = append(errs, fmt.Errorf("clear token cache: %w", err))
		}
	}
	for _, suffix := range []string{secretKeyRefreshSuffix, secretKeyMetadataSuffix, derivedTokenSuffix, derivedUsernameSuffix} {
		if err := h.secretStore.Delete(ctx, h.secretKey(suffix)); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("logout %s: %w", h.cfg.Name, errs[0])
	}
	return nil
}

// Status returns the current authentication state.
func (h *Handler) Status(ctx context.Context) (*auth.Status, error) {
	if err := h.ensureSecrets(); err != nil {
		return &auth.Status{Authenticated: false}, nil //nolint:nilerr // graceful degradation
	}

	meta, err := h.loadMetadata(ctx)
	if err != nil || meta == nil {
		return &auth.Status{Authenticated: false}, nil //nolint:nilerr // metadata absence is not an error for status
	}
	if meta.ExpiresAt.Before(time.Now()) {
		return &auth.Status{Authenticated: false, Reason: "session expired", Claims: meta.Claims}, nil
	}
	return &auth.Status{
		Authenticated: true,
		Claims:        meta.Claims,
		ExpiresAt:     meta.ExpiresAt,
		Scopes:        meta.Scopes,
	}, nil
}

// GetToken returns a valid access token, refreshing if necessary.
// If tokenExchange is configured, returns the derived token.
func (h *Handler) GetToken(ctx context.Context, opts auth.TokenOptions) (*auth.Token, error) {
	if err := h.ensureSecrets(); err != nil {
		return nil, err
	}

	// Derived token takes precedence when exchange is configured
	if h.cfg.TokenExchange != nil {
		derived, err := h.loadDerivedToken(ctx)
		if err == nil && derived != nil && derived.IsValidFor(opts.MinValidFor) && !opts.ForceRefresh {
			return derived, nil
		}
	}

	fingerprint := auth.FingerprintHash(h.cfg.ClientID)
	scope := opts.Scope
	if scope == "" {
		scope = strings.Join(h.cfg.Scopes, " ")
	}
	flow := h.resolveDefaultFlow()

	// Try cached token
	if !opts.ForceRefresh && h.tokenCache != nil {
		cached, err := h.tokenCache.Get(ctx, flow, fingerprint, scope)
		if err == nil && cached != nil && cached.IsValidFor(opts.MinValidFor) {
			return cached, nil
		}
	}

	// Try refresh
	refreshData, err := h.secretStore.Get(ctx, h.secretKey(secretKeyRefreshSuffix))
	if err == nil && len(refreshData) > 0 {
		tokenResp, refreshErr := h.refreshAccessToken(ctx, string(refreshData), scope)
		if refreshErr == nil {
			token := h.tokenRespToToken(tokenResp, flow, scope)
			if h.tokenCache != nil {
				_ = h.tokenCache.Set(ctx, flow, fingerprint, scope, token)
			}
			if h.cfg.TokenExchange != nil {
				if derived, exchangeErr := h.executeTokenExchange(ctx, tokenResp.AccessToken); exchangeErr == nil {
					_ = h.storeDerivedToken(ctx, derived, token.ExpiresAt, flow, strings.Split(scope, " "))
				}
			}
			return token, nil
		}
		h.logger.V(1).Info("refresh token failed", "error", refreshErr)
	}

	return nil, auth.NewError(h.cfg.Name, "get_token", auth.ErrNotAuthenticated)
}

// InjectAuth adds an Authorization header to the HTTP request.
func (h *Handler) InjectAuth(ctx context.Context, req *http.Request, opts auth.TokenOptions) error {
	token, err := h.GetToken(ctx, opts)
	if err != nil {
		return err
	}
	tokenType := token.TokenType
	if tokenType == "" {
		tokenType = defaultTokenType
	}
	req.Header.Set("Authorization", fmt.Sprintf("%s %s", tokenType, token.AccessToken))
	return nil
}

// ---------- internal types ----------

type tokenResponse struct {
	AccessToken  string `json:"access_token"` //nolint:gosec // JSON field
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"` //nolint:gosec // JSON field
	Scope        string `json:"scope"`
}

type handlerMetadata struct {
	Claims    *auth.Claims `json:"claims"`
	ExpiresAt time.Time    `json:"expiresAt"`
	Scopes    []string     `json:"scopes"`
}

type exchangeResult struct {
	Token    string `json:"token"`
	Username string `json:"username,omitempty"`
}

type tokenEndpointError struct {
	ErrorCode   string `json:"error"`
	Description string `json:"error_description"`
}

func (e *tokenEndpointError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("%s: %s", e.ErrorCode, e.Description)
	}
	return e.ErrorCode
}

// ---------- flow: authorization code + PKCE ----------

func (h *Handler) authCodeLogin(ctx context.Context, opts auth.LoginOptions, scopes []string, callbackPort int) (*tokenResponse, error) {
	if h.cfg.AuthorizeURL == "" {
		return nil, fmt.Errorf("authorizeURL is required for interactive flow")
	}

	// Implicit grant flow (response_type=token) uses a different callback mechanism.
	if h.cfg.ResponseType == "token" {
		return h.implicitGrantLogin(ctx, opts, scopes, callbackPort)
	}

	var verifier string
	if !h.cfg.DisablePKCE {
		var err error
		verifier, err = oauth.GenerateCodeVerifier()
		if err != nil {
			return nil, fmt.Errorf("generate PKCE verifier: %w", err)
		}
	}

	state, err := oauth.GenerateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("generate state: %w", err)
	}

	callbackServer, err := oauth.StartCallbackServer(ctx, callbackPort, state)
	if err != nil {
		return nil, fmt.Errorf("start callback server: %w", err)
	}
	defer callbackServer.Close()

	authURL, err := url.Parse(h.cfg.AuthorizeURL)
	if err != nil {
		return nil, fmt.Errorf("parse authorizeURL: %w", err)
	}
	q := authURL.Query()
	q.Set("response_type", "code")
	q.Set("client_id", h.cfg.ClientID)
	q.Set("redirect_uri", callbackServer.RedirectURI)
	q.Set("state", state)
	if !h.cfg.DisablePKCE {
		challenge := oauth.GenerateCodeChallenge(verifier)
		q.Set("code_challenge", challenge)
		q.Set("code_challenge_method", "S256")
	}
	if len(scopes) > 0 {
		q.Set("scope", strings.Join(scopes, " "))
	}
	authURL.RawQuery = q.Encode()

	if opts.BrowserAuthCallback != nil {
		opts.BrowserAuthCallback(authURL.String())
	}
	if openErr := oauth.OpenBrowser(ctx, authURL.String()); openErr != nil {
		h.logger.V(1).Info("failed to open browser", "url", authURL.String(), "error", openErr)
	}

	select {
	case result := <-callbackServer.ResultChan():
		if result.Err != nil {
			return nil, fmt.Errorf("callback error: %w", result.Err)
		}
		return h.exchangeAuthCode(ctx, result.Code, callbackServer.RedirectURI, verifier, scopes)
	case <-ctx.Done():
		return nil, auth.ErrTimeout
	}
}

func (h *Handler) exchangeAuthCode(ctx context.Context, code, redirectURI, codeVerifier string, scopes []string) (*tokenResponse, error) {
	data := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {redirectURI},
		"client_id":    {h.cfg.ClientID},
	}
	// codeVerifier is empty when h.cfg.DisablePKCE is true (set by authCodeLogin).
	if codeVerifier != "" {
		data.Set("code_verifier", codeVerifier)
	}
	if h.cfg.ClientSecret != "" {
		data.Set("client_secret", h.cfg.ClientSecret)
	}
	if len(scopes) > 0 {
		data.Set("scope", strings.Join(scopes, " "))
	}
	return h.postTokenEndpoint(ctx, data)
}

// ---------- flow: implicit grant (response_type=token) ----------

func (h *Handler) implicitGrantLogin(ctx context.Context, opts auth.LoginOptions, scopes []string, callbackPort int) (*tokenResponse, error) {
	state, err := oauth.GenerateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("generate state: %w", err)
	}

	callbackServer, err := oauth.StartImplicitCallbackServer(ctx, callbackPort, state)
	if err != nil {
		return nil, fmt.Errorf("start callback server: %w", err)
	}
	defer callbackServer.Close()

	authURL, err := url.Parse(h.cfg.AuthorizeURL)
	if err != nil {
		return nil, fmt.Errorf("parse authorizeURL: %w", err)
	}
	q := authURL.Query()
	q.Set("response_type", "token")
	q.Set("client_id", h.cfg.ClientID)
	q.Set("redirect_uri", callbackServer.RedirectURI)
	q.Set("state", state)
	if len(scopes) > 0 {
		q.Set("scope", strings.Join(scopes, " "))
	}
	authURL.RawQuery = q.Encode()

	if opts.BrowserAuthCallback != nil {
		opts.BrowserAuthCallback(authURL.String())
	}
	if openErr := oauth.OpenBrowser(ctx, authURL.String()); openErr != nil {
		h.logger.V(1).Info("failed to open browser", "url", authURL.String(), "error", openErr)
	}

	select {
	case result := <-callbackServer.ResultChan():
		if result.Err != nil {
			return nil, fmt.Errorf("callback error: %w", result.Err)
		}
		resp := &tokenResponse{
			AccessToken: result.AccessToken,
			TokenType:   result.TokenType,
		}
		if resp.TokenType == "" {
			resp.TokenType = defaultTokenType
		}
		if result.ExpiresIn != "" {
			var expiresIn int
			if _, scanErr := fmt.Sscanf(result.ExpiresIn, "%d", &expiresIn); scanErr == nil {
				resp.ExpiresIn = expiresIn
			} else {
				h.logger.V(1).Info("failed to parse expires_in from implicit grant, defaulting to 3600", "value", result.ExpiresIn, "error", scanErr)
				resp.ExpiresIn = 3600
			}
		} else {
			h.logger.V(1).Info("expires_in not provided by implicit grant server, defaulting to 3600")
			resp.ExpiresIn = 3600
		}
		return resp, nil
	case <-ctx.Done():
		return nil, auth.ErrTimeout
	}
}

// ---------- flow: device code (RFC 8628) ----------

func (h *Handler) deviceCodeLogin(ctx context.Context, opts auth.LoginOptions, scopes []string) (*tokenResponse, error) {
	if h.cfg.DeviceAuthURL == "" {
		return nil, fmt.Errorf("deviceAuthURL is required for device_code flow")
	}

	data := url.Values{"client_id": {h.cfg.ClientID}}
	if len(scopes) > 0 {
		data.Set("scope", strings.Join(scopes, " "))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.cfg.DeviceAuthURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create device auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := h.httpClient.Do(req) //nolint:gosec // URL from trusted admin config
	if err != nil {
		return nil, fmt.Errorf("device auth request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("read device auth response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device auth failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var deviceResp struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	if err := json.Unmarshal(body, &deviceResp); err != nil {
		return nil, fmt.Errorf("parse device auth response: %w", err)
	}

	if opts.DeviceCodeCallback != nil {
		opts.DeviceCodeCallback(deviceResp.UserCode, deviceResp.VerificationURI, "")
	}

	interval := deviceResp.Interval
	if interval <= 0 {
		interval = defaultPollInterval
	}
	if h.cfg.DeviceCodePollInterval > 0 && deviceResp.Interval <= 0 {
		interval = h.cfg.DeviceCodePollInterval
	}

	return h.pollDeviceCode(ctx, deviceResp.DeviceCode, interval, scopes)
}

func (h *Handler) pollDeviceCode(ctx context.Context, deviceCode string, interval int, scopes []string) (*tokenResponse, error) {
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, auth.ErrTimeout
		case <-ticker.C:
			data := url.Values{
				"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
				"device_code": {deviceCode},
				"client_id":   {h.cfg.ClientID},
			}
			if len(scopes) > 0 {
				data.Set("scope", strings.Join(scopes, " "))
			}

			tokenResp, err := h.postTokenEndpoint(ctx, data)
			if err == nil {
				return tokenResp, nil
			}

			var errMsg string
			if e, ok := err.(*tokenEndpointError); ok { //nolint:errorlint // concrete type assertion
				errMsg = e.ErrorCode
			}
			switch errMsg {
			case "authorization_pending":
				continue
			case "slow_down":
				interval++
				ticker.Reset(time.Duration(interval) * time.Second)
				continue
			case "expired_token":
				return nil, fmt.Errorf("device code expired — please try again")
			case "access_denied":
				return nil, fmt.Errorf("access denied by user")
			default:
				return nil, err
			}
		}
	}
}

// ---------- flow: client credentials ----------

func (h *Handler) clientCredentialsLogin(ctx context.Context, scopes []string) (*tokenResponse, error) {
	if h.cfg.ClientSecret == "" {
		return nil, fmt.Errorf("clientSecret is required for client_credentials flow")
	}
	data := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {h.cfg.ClientID},
		"client_secret": {h.cfg.ClientSecret},
	}
	if len(scopes) > 0 {
		data.Set("scope", strings.Join(scopes, " "))
	}
	return h.postTokenEndpoint(ctx, data)
}

// ---------- token exchange ----------

func (h *Handler) executeTokenExchange(ctx context.Context, primaryToken string) (*exchangeResult, error) {
	exc := h.cfg.TokenExchange
	method := exc.Method
	if method == "" {
		method = http.MethodPost
	}

	var bodyReader io.Reader
	if exc.RequestBody != "" {
		result, tmplErr := gotmpl.Execute(ctx, gotmpl.TemplateOptions{
			Name:    "exchange-request-body",
			Content: string(exc.RequestBody),
			Data: map[string]string{
				"Hostname": h.cfg.Registry,
				"Username": h.cfg.RegistryUsername,
			},
		})
		if tmplErr != nil {
			return nil, fmt.Errorf("render exchange request body: %w", tmplErr)
		}
		bodyReader = bytes.NewBufferString(result.Output)
	}

	req, err := http.NewRequestWithContext(ctx, method, exc.URL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create exchange request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+primaryToken)
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := h.httpClient.Do(req) //nolint:gosec // URL from trusted admin config
	if err != nil {
		return nil, fmt.Errorf("exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("read exchange response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("token exchange returned 401 — primary token may be expired, try re-authenticating")
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("token exchange failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var respData map[string]any
	if err := json.Unmarshal(body, &respData); err != nil {
		return nil, fmt.Errorf("parse exchange response: %w", err)
	}

	token, err := extractJSONPath(respData, exc.TokenJSONPath)
	if err != nil {
		return nil, fmt.Errorf("extract token from exchange response: %w", err)
	}

	result := &exchangeResult{Token: token}
	if exc.UsernameJSONPath != "" {
		if username, extractErr := extractJSONPath(respData, exc.UsernameJSONPath); extractErr == nil && username != "" {
			result.Username = username
			// Persist the extracted username so BridgeAuthToRegistry uses it for registry auth.
			h.cfg.RegistryUsername = username
		}
	}
	return result, nil
}

// ---------- token verification ----------

func (h *Handler) verifyToken(ctx context.Context, accessToken string) (*auth.Claims, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.cfg.VerifyURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create verify request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := h.httpClient.Do(req) //nolint:gosec // URL from trusted admin config
	if err != nil {
		return nil, fmt.Errorf("verify request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("read verify response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("verify returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("parse verify response: %w", err)
	}

	claims := &auth.Claims{}
	if h.cfg.IdentityFields != nil {
		if h.cfg.IdentityFields.Username != "" {
			claims.Username = getStringField(data, h.cfg.IdentityFields.Username)
		}
		if h.cfg.IdentityFields.Email != "" {
			claims.Email = getStringField(data, h.cfg.IdentityFields.Email)
		}
		if h.cfg.IdentityFields.Name != "" {
			claims.Name = getStringField(data, h.cfg.IdentityFields.Name)
		}
	}
	return claims, nil
}

// ---------- storage ----------

func (h *Handler) storeTokens(ctx context.Context, resp *tokenResponse, claims *auth.Claims, expiresAt time.Time, flow auth.Flow, scopes []string) error {
	if resp.RefreshToken != "" {
		if err := h.secretStore.Set(ctx, h.secretKey(secretKeyRefreshSuffix), []byte(resp.RefreshToken)); err != nil {
			return fmt.Errorf("store refresh token: %w", err)
		}
	}
	meta := &handlerMetadata{Claims: claims, ExpiresAt: expiresAt, Scopes: scopes}
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	if err := h.secretStore.Set(ctx, h.secretKey(secretKeyMetadataSuffix), metaBytes); err != nil {
		return fmt.Errorf("store metadata: %w", err)
	}
	if h.tokenCache != nil {
		fingerprint := auth.FingerprintHash(h.cfg.ClientID)
		scope := strings.Join(scopes, " ")
		return h.tokenCache.Set(ctx, flow, fingerprint, scope, h.tokenRespToToken(resp, flow, scope))
	}
	return nil
}

func (h *Handler) storeDerivedToken(ctx context.Context, result *exchangeResult, expiresAt time.Time, flow auth.Flow, scopes []string) error {
	token := &auth.Token{
		AccessToken: result.Token,
		TokenType:   defaultTokenType,
		ExpiresAt:   expiresAt,
		Scope:       strings.Join(scopes, " "),
		Flow:        flow,
	}
	data, err := json.Marshal(token) //nolint:gosec // runtime token
	if err != nil {
		return fmt.Errorf("marshal derived token: %w", err)
	}
	if setErr := h.secretStore.Set(ctx, h.secretKey(derivedTokenSuffix), data); setErr != nil {
		return setErr
	}
	// Persist the extracted username so it survives process restarts.
	if result.Username != "" {
		if setErr := h.secretStore.Set(ctx, h.secretKey(derivedUsernameSuffix), []byte(result.Username)); setErr != nil {
			h.logger.V(1).Info("failed to persist derived username", "error", setErr)
		}
	}
	return nil
}

func (h *Handler) loadDerivedToken(ctx context.Context) (*auth.Token, error) {
	data, err := h.secretStore.Get(ctx, h.secretKey(derivedTokenSuffix))
	if err != nil {
		return nil, err
	}
	var token auth.Token
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, err
	}
	// Restore the derived username so BridgeAuthToRegistry uses the correct
	// username even when the token was loaded from cache across process restarts.
	if usernameData, usernameErr := h.secretStore.Get(ctx, h.secretKey(derivedUsernameSuffix)); usernameErr == nil && len(usernameData) > 0 {
		h.cfg.RegistryUsername = string(usernameData)
	}
	return &token, nil
}

func (h *Handler) loadMetadata(ctx context.Context) (*handlerMetadata, error) {
	data, err := h.secretStore.Get(ctx, h.secretKey(secretKeyMetadataSuffix))
	if err != nil {
		return nil, err
	}
	var meta handlerMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// ---------- refresh ----------

func (h *Handler) refreshAccessToken(ctx context.Context, refreshToken, scope string) (*tokenResponse, error) {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {h.cfg.ClientID},
	}
	if h.cfg.ClientSecret != "" {
		data.Set("client_secret", h.cfg.ClientSecret)
	}
	if scope != "" {
		data.Set("scope", scope)
	}
	return h.postTokenEndpoint(ctx, data)
}

// ---------- HTTP ----------

func (h *Handler) postTokenEndpoint(ctx context.Context, data url.Values) (*tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.cfg.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := h.httpClient.Do(req) //nolint:gosec // URL from trusted admin config
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp tokenEndpointError
		if jsonErr := json.Unmarshal(body, &errResp); jsonErr == nil && errResp.ErrorCode != "" {
			return nil, &errResp
		}
		return nil, fmt.Errorf("token endpoint returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp tokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}
	return &tokenResp, nil
}

// ---------- utilities ----------

func (h *Handler) secretKey(suffix string) string {
	return secretKeyPrefix + h.cfg.Name + "." + suffix
}

func (h *Handler) ensureSecrets() error {
	if h.secretStore == nil {
		if h.secretErr != nil {
			return h.secretErr
		}
		return fmt.Errorf("secret store not initialized")
	}
	return nil
}

func (h *Handler) resolveDefaultFlow() auth.Flow {
	switch h.cfg.DefaultFlow {
	case "interactive":
		return auth.FlowInteractive
	case "device_code":
		return auth.FlowDeviceCode
	case "client_credentials":
		return auth.FlowClientCredentials
	default:
		if h.cfg.AuthorizeURL != "" {
			return auth.FlowInteractive
		}
		if h.cfg.DeviceAuthURL != "" {
			return auth.FlowDeviceCode
		}
		if h.cfg.ClientSecret != "" {
			return auth.FlowClientCredentials
		}
		return auth.FlowInteractive
	}
}

func (h *Handler) tokenRespToToken(resp *tokenResponse, flow auth.Flow, scope string) *auth.Token {
	tokenType := resp.TokenType
	if tokenType == "" {
		tokenType = defaultTokenType
	}
	return &auth.Token{
		AccessToken: resp.AccessToken,
		TokenType:   tokenType,
		ExpiresAt:   time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second),
		Scope:       scope,
		Flow:        flow,
	}
}

// extractJSONPath extracts a value from a nested map using dot-notation.
func extractJSONPath(data map[string]any, path string) (string, error) {
	parts := strings.Split(path, ".")
	var current any = data
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return "", fmt.Errorf("expected object at %q, got %T", part, current)
		}
		current, ok = m[part]
		if !ok {
			return "", fmt.Errorf("field %q not found", part)
		}
	}
	switch v := current.(type) {
	case string:
		return v, nil
	case float64:
		return fmt.Sprintf("%g", v), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

func getStringField(data map[string]any, field string) string {
	if val, ok := data[field]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}

// ValidateConfig validates a CustomOAuth2Config for required fields and constraints.
func ValidateConfig(cfg config.CustomOAuth2Config) error {
	if cfg.Name == "" {
		return fmt.Errorf("custom OAuth2 handler: name is required")
	}

	// Validate responseType early so unknown values are rejected before
	// field-presence checks that depend on the response type.
	switch cfg.ResponseType {
	case "", "code":
		// default authorization code flow
	case "token":
		if cfg.AuthorizeURL == "" {
			return fmt.Errorf("custom OAuth2 handler %q: authorizeURL is required when responseType is token", cfg.Name)
		}
		if cfg.DefaultFlow != "" && cfg.DefaultFlow != "interactive" {
			return fmt.Errorf("custom OAuth2 handler %q: implicit grant (responseType=token) only supports interactive flow, got defaultFlow=%q", cfg.Name, cfg.DefaultFlow)
		}
	default:
		return fmt.Errorf("custom OAuth2 handler %q: unknown responseType %q (valid: code, token)", cfg.Name, cfg.ResponseType)
	}

	if cfg.TokenURL == "" && cfg.ResponseType != "token" {
		return fmt.Errorf("custom OAuth2 handler %q: tokenURL is required", cfg.Name)
	}
	if cfg.ClientID == "" {
		return fmt.Errorf("custom OAuth2 handler %q: clientID is required", cfg.Name)
	}

	switch cfg.DefaultFlow {
	case "", "interactive":
		if cfg.AuthorizeURL == "" && cfg.DeviceAuthURL == "" && cfg.ClientSecret == "" {
			return fmt.Errorf("custom OAuth2 handler %q: at least one of authorizeURL, deviceAuthURL, or clientSecret must be set", cfg.Name)
		}
	case "device_code":
		if cfg.DeviceAuthURL == "" {
			return fmt.Errorf("custom OAuth2 handler %q: deviceAuthURL is required when defaultFlow is device_code", cfg.Name)
		}
	case "client_credentials":
		if cfg.ClientSecret == "" {
			return fmt.Errorf("custom OAuth2 handler %q: clientSecret is required when defaultFlow is client_credentials", cfg.Name)
		}
	default:
		return fmt.Errorf("custom OAuth2 handler %q: unknown defaultFlow %q (valid: interactive, device_code, client_credentials)", cfg.Name, cfg.DefaultFlow)
	}

	if cfg.CallbackPort != 0 && (cfg.CallbackPort < 1024 || cfg.CallbackPort > 65535) {
		return fmt.Errorf("custom OAuth2 handler %q: callbackPort must be 0 (random) or 1024-65535, got %d", cfg.Name, cfg.CallbackPort)
	}

	if cfg.TokenExchange != nil {
		if cfg.TokenExchange.URL == "" {
			return fmt.Errorf("custom OAuth2 handler %q: tokenExchange.url is required", cfg.Name)
		}
		if cfg.TokenExchange.TokenJSONPath == "" {
			return fmt.Errorf("custom OAuth2 handler %q: tokenExchange.tokenJSONPath is required", cfg.Name)
		}
		if cfg.TokenExchange.Method != "" {
			switch strings.ToUpper(cfg.TokenExchange.Method) {
			case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch:
			default:
				return fmt.Errorf("custom OAuth2 handler %q: tokenExchange.method must be GET, POST, PUT, or PATCH", cfg.Name)
			}
		}
	}

	return nil
}
