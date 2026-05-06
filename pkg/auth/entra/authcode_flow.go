// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package entra

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/auth/oauth"
	"github.com/oakwood-commons/scafctl/pkg/logger"
)

// BrowserOpener is a function that opens a URL in the system browser.
// It is a package-level variable so tests can override it.
var BrowserOpener = oauth.OpenBrowser

// authCodeLogin performs the authorization code + PKCE authentication flow.
// This opens the user's default browser to the Entra authorize endpoint and
// listens on a local ephemeral port for the redirect containing the auth code.
func (h *Handler) authCodeLogin(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("starting authorization code + PKCE flow")

	// Determine tenant
	tenantID := opts.TenantID
	if tenantID == "" {
		tenantID = h.config.TenantID
	}

	// Determine scopes
	scopes := opts.Scopes
	if len(scopes) == 0 {
		scopes = h.config.DefaultScopes
	}

	// Ensure offline_access is included for refresh token
	hasOfflineAccess := false
	for _, s := range scopes {
		if s == "offline_access" {
			hasOfflineAccess = true
			break
		}
	}
	if !hasOfflineAccess {
		scopes = append(scopes, "offline_access")
	}

	// Determine timeout
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	// Generate PKCE code verifier and challenge
	codeVerifier, err := oauth.GenerateCodeVerifier()
	if err != nil {
		return nil, auth.NewError(HandlerName, "pkce_generate", fmt.Errorf("generating PKCE code verifier: %w", err))
	}
	codeChallenge := oauth.GenerateCodeChallenge(codeVerifier)

	// Generate random state for CSRF protection
	state, err := oauth.GenerateCodeVerifier()
	if err != nil {
		return nil, auth.NewError(HandlerName, "state_generate", fmt.Errorf("generating state parameter: %w", err))
	}

	// Start local callback server for OAuth redirect
	callbackServer, err := oauth.StartCallbackServer(ctx, opts.CallbackPort, state)
	if err != nil {
		return nil, auth.NewError(HandlerName, "callback_server", fmt.Errorf("starting callback server: %w", err))
	}
	defer callbackServer.Close()

	redirectURI := callbackServer.RedirectURI

	// Build authorization URL
	scopeStr := strings.Join(scopes, " ")
	authURL := fmt.Sprintf("%s/%s/oauth2/v2.0/authorize?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&code_challenge=%s&code_challenge_method=S256&state=%s",
		h.config.GetAuthority(),
		tenantID,
		url.QueryEscape(h.config.ClientID),
		url.QueryEscape(redirectURI),
		url.QueryEscape(scopeStr),
		url.QueryEscape(codeChallenge),
		url.QueryEscape(state),
	)

	// Append claims parameter if provided (e.g. from a claims challenge)
	if claims := claimsChallengeFromContext(ctx); claims != "" {
		authURL += "&claims=" + url.QueryEscape(claims)
	}

	// Open browser
	lgr.V(1).Info("opening browser for authentication", "url", authURL)
	browserOpenErr := BrowserOpener(ctx, authURL)
	if browserOpenErr != nil {
		lgr.V(0).Info("failed to open browser, please open this URL manually", "url", authURL)
	}
	// Always notify the CLI callback with the auth URL so the TUI can show
	// a "Re-open in browser" action. Empty userCode signals a browser flow.
	if opts.DeviceCodeCallback != nil {
		opts.DeviceCodeCallback("", authURL, "Open this URL in your browser to authenticate")
	}

	// Wait for authorization code or timeout
	var authCode string
	select {
	case result := <-callbackServer.ResultChan():
		if result.Err != nil {
			errMsg := result.Err.Error()
			if strings.Contains(errMsg, "AADSTS") {
				if hint := aadstsHint(errMsg); hint != "" {
					return nil, auth.NewError(HandlerName, "auth_code", fmt.Errorf("%w\nHint: %s", result.Err, hint))
				}
			}
			return nil, auth.NewError(HandlerName, "auth_code", result.Err)
		}
		authCode = result.Code
		lgr.V(1).Info("received authorization code")
	case <-time.After(timeout):
		return nil, auth.NewError(HandlerName, "auth_code", fmt.Errorf(
			"no response received from browser within %s; if using a custom --client-id, "+
				"ensure http://localhost is registered as a redirect URI in the app registration "+
				"(App registrations \u2192 Authentication \u2192 Mobile and desktop applications), "+
				"or use '--flow device-code': %w", timeout, auth.ErrTimeout))
	case <-ctx.Done():
		return nil, auth.NewError(HandlerName, "auth_code", fmt.Errorf("authentication cancelled: %w", auth.ErrUserCancelled))
	}

	// Exchange authorization code for tokens
	tokenResp, err := h.exchangeAuthCode(ctx, tenantID, authCode, redirectURI, codeVerifier)
	if err != nil {
		return nil, auth.NewError(HandlerName, "token_exchange", err)
	}

	// Store refresh token and metadata
	if err := h.storeCredentials(ctx, tenantID, tokenResp, h.config.ClientID, scopes, auth.FlowInteractive, ""); err != nil {
		return nil, auth.NewError(HandlerName, "store_credentials", err)
	}

	// Extract and return claims
	claims, err := h.extractClaims(tokenResp)
	if err != nil {
		return nil, auth.NewError(HandlerName, "extract_claims", err)
	}

	lgr.V(1).Info("authorization code flow completed successfully",
		"subject", claims.Subject,
		"tenantId", claims.TenantID,
	)

	return &auth.Result{
		Claims:    claims,
		ExpiresAt: time.Now().Add(DefaultRefreshTokenLifetime),
	}, nil
}

// exchangeAuthCode exchanges an authorization code for tokens at the Entra
// token endpoint. This is a public client flow (PKCE) so no client_secret
// is sent.
func (h *Handler) exchangeAuthCode(ctx context.Context, tenantID, code, redirectURI, codeVerifier string) (*TokenResponse, error) {
	endpoint := fmt.Sprintf("%s/%s/oauth2/v2.0/token", h.config.GetAuthority(), tenantID)

	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", h.config.ClientID)
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)
	data.Set("code_verifier", codeVerifier)

	resp, err := h.httpClient.PostForm(ctx, endpoint, data)
	if err != nil {
		return nil, fmt.Errorf("token exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		var errResp TokenErrorResponse
		if decErr := json.NewDecoder(resp.Body).Decode(&errResp); decErr != nil {
			return nil, fmt.Errorf("token exchange failed with status %d", resp.StatusCode)
		}
		if strings.Contains(errResp.ErrorDescription, "AADSTS") {
			return nil, formatAADSTSError("token exchange failed", errResp)
		}
		return nil, fmt.Errorf("token exchange failed: %s - %s", errResp.Error, errResp.ErrorDescription)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	return &tokenResp, nil
}
