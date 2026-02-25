// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package github

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

// authCodeLogin performs the OAuth 2.0 authorization code + PKCE flow.
// This opens the user's default browser to the GitHub authorize endpoint and
// listens on a local ephemeral port for the redirect containing the auth code.
func (h *Handler) authCodeLogin(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("starting GitHub authorization code + PKCE flow")

	// Determine scopes
	scopes := opts.Scopes
	if len(scopes) == 0 {
		scopes = h.config.DefaultScopes
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
	state, err := oauth.GenerateCodeVerifier() // reuse verifier generator for random state
	if err != nil {
		return nil, auth.NewError(HandlerName, "state_generate", fmt.Errorf("generating state parameter: %w", err))
	}

	// Start local callback server for OAuth redirect
	callbackServer, err := oauth.StartCallbackServer(ctx, opts.CallbackPort)
	if err != nil {
		return nil, auth.NewError(HandlerName, "callback_server", fmt.Errorf("starting callback server: %w", err))
	}
	defer callbackServer.Close()

	redirectURI := callbackServer.RedirectURI

	// Build authorization URL
	// GitHub OAuth: https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps
	scopeStr := strings.Join(scopes, " ")
	params := url.Values{}
	params.Set("client_id", h.config.ClientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("scope", scopeStr)
	params.Set("state", state)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")

	authURL := fmt.Sprintf("%s/login/oauth/authorize?%s", h.config.GetOAuthBaseURL(), params.Encode())

	// Open browser
	lgr.V(1).Info("opening browser for authentication", "url", authURL)
	if err := BrowserOpener(ctx, authURL); err != nil {
		lgr.V(0).Info("failed to open browser, please open this URL manually", "url", authURL)
		// Notify via callback if available so the CLI can display the URL
		if opts.DeviceCodeCallback != nil {
			opts.DeviceCodeCallback("", authURL, fmt.Sprintf("Open this URL in your browser to authenticate:\n%s", authURL))
		}
	}

	// Wait for authorization code or timeout
	var authCode string
	select {
	case result := <-callbackServer.ResultChan():
		if result.Err != nil {
			return nil, auth.NewError(HandlerName, "auth_code", result.Err)
		}
		authCode = result.Code
		lgr.V(1).Info("received authorization code")
	case <-time.After(timeout):
		return nil, auth.NewError(HandlerName, "auth_code", fmt.Errorf(
			"no response received from browser within %s; "+
				"if running over SSH or in a headless environment, use '--flow device-code' instead: %w",
			timeout, auth.ErrTimeout))
	case <-ctx.Done():
		return nil, auth.NewError(HandlerName, "auth_code", fmt.Errorf("authentication cancelled: %w", auth.ErrUserCancelled))
	}

	// Exchange authorization code for tokens
	tokenResp, err := h.exchangeAuthCode(ctx, authCode, redirectURI, codeVerifier)
	if err != nil {
		return nil, auth.NewError(HandlerName, "token_exchange", err)
	}

	// Store credentials and fetch claims
	claims, err := h.storeCredentials(ctx, tokenResp, scopes, "")
	if err != nil {
		return nil, auth.NewError(HandlerName, "store_credentials", err)
	}

	lgr.V(1).Info("authorization code flow completed successfully",
		"subject", claims.Subject,
		"name", claims.Name,
	)

	// Determine expiry
	expiresAt := time.Now().Add(8 * time.Hour) // Default for non-expiring tokens
	if tokenResp.RefreshTokenExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(tokenResp.RefreshTokenExpiresIn) * time.Second)
	}

	return &auth.Result{
		Claims:    claims,
		ExpiresAt: expiresAt,
	}, nil
}

// exchangeAuthCode exchanges an authorization code for tokens at the GitHub
// token endpoint. Includes client_secret when configured (confidential OAuth
// App). For public clients with PKCE enabled on the OAuth App, client_secret
// can be omitted; however most GitHub OAuth Apps require it.
func (h *Handler) exchangeAuthCode(ctx context.Context, code, redirectURI, codeVerifier string) (*TokenResponse, error) {
	endpoint := fmt.Sprintf("%s/login/oauth/access_token", h.config.GetOAuthBaseURL())

	params := map[string]string{
		"client_id":     h.config.ClientID,
		"code":          code,
		"redirect_uri":  redirectURI,
		"code_verifier": codeVerifier,
	}
	// Include client_secret when configured. GitHub OAuth Apps typically require
	// it unless the app has explicit PKCE-without-secret support enabled.
	if h.config.ClientSecret != "" {
		params["client_secret"] = h.config.ClientSecret
	}

	data := makeFormData(params)

	resp, err := h.httpClient.PostForm(ctx, endpoint, data)
	if err != nil {
		return nil, fmt.Errorf("token exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	// GitHub returns 200 even for errors, with error details in the JSON body
	body := struct {
		TokenResponse
		TokenErrorResponse
	}{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	if body.AccessToken == "" {
		if body.Error != "" {
			return nil, fmt.Errorf("token exchange failed: %s - %s", body.Error, body.ErrorDescription)
		}
		return nil, fmt.Errorf("token exchange returned empty access token")
	}

	return &TokenResponse{
		AccessToken:           body.AccessToken,
		RefreshToken:          body.RefreshToken,
		TokenType:             body.TokenType,
		Scope:                 body.Scope,
		ExpiresIn:             body.ExpiresIn,
		RefreshTokenExpiresIn: body.RefreshTokenExpiresIn,
	}, nil
}
