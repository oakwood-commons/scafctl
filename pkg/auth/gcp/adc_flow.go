// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package gcp provides Google Cloud Platform authentication for scafctl.
// This file implements the ADC (Application Default Credentials) browser OAuth flow
// using authorization code + PKCE with a local redirect server.
package gcp

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

const (
	// Google OAuth 2.0 endpoints.
	authorizationEndpoint = "https://accounts.google.com/o/oauth2/v2/auth"
	revokeEndpoint        = "https://oauth2.googleapis.com/revoke"
)

// adcLogin performs the ADC browser OAuth flow (authorization code + PKCE).
func (h *Handler) adcLogin(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("starting ADC browser OAuth flow", "handler", HandlerName)

	// Use configured client ID, or fall back to Google's well-known ADC client credentials
	clientID := h.config.ClientID
	clientSecret := h.config.ClientSecret
	if clientID == "" {
		lgr.V(1).Info("no client ID configured, using Google's default ADC client credentials")
		clientID = DefaultADCClientID
		clientSecret = DefaultADCClientSecret
	}

	// Determine scopes
	scopes := opts.Scopes
	if len(scopes) == 0 {
		scopes = h.config.DefaultScopes
	}
	scopeStr := strings.Join(scopes, " ")

	// Generate PKCE code verifier and challenge
	codeVerifier, err := oauth.GenerateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("generating PKCE code verifier: %w", err)
	}
	codeChallenge := oauth.GenerateCodeChallenge(codeVerifier)

	// Start local callback server for OAuth redirect
	callbackServer, err := oauth.StartCallbackServer(ctx, opts.CallbackPort, "")
	if err != nil {
		return nil, fmt.Errorf("starting callback server: %w", err)
	}
	defer callbackServer.Close()
	redirectURI := callbackServer.RedirectURI

	// Build authorization URL
	authURL := fmt.Sprintf("%s?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&code_challenge=%s&code_challenge_method=S256&access_type=offline&prompt=consent",
		authorizationEndpoint,
		url.QueryEscape(clientID),
		url.QueryEscape(redirectURI),
		url.QueryEscape(scopeStr),
		url.QueryEscape(codeChallenge),
	)

	// Open browser
	lgr.V(1).Info("opening browser for authentication", "url", authURL)
	if err := oauth.OpenBrowser(ctx, authURL); err != nil {
		lgr.V(0).Info("failed to open browser, please open this URL manually", "url", authURL)
	}

	// Wait for authorization code or timeout
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	var authCode string
	select {
	case result := <-callbackServer.ResultChan():
		if result.Err != nil {
			return nil, result.Err
		}
		authCode = result.Code
		lgr.V(1).Info("received authorization code")
	case <-time.After(timeout):
		return nil, fmt.Errorf("authentication timed out: no response received from browser: %w", auth.ErrTimeout)
	case <-ctx.Done():
		return nil, fmt.Errorf("authentication cancelled: %w", auth.ErrUserCancelled)
	}

	// Exchange code for tokens
	data := url.Values{}
	data.Set("code", authCode)
	data.Set("client_id", clientID)
	if clientSecret != "" {
		data.Set("client_secret", clientSecret)
	}
	data.Set("redirect_uri", redirectURI)
	data.Set("grant_type", "authorization_code")
	data.Set("code_verifier", codeVerifier)

	resp, err := h.httpClient.PostForm(ctx, tokenEndpoint, data)
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		var errResp TokenErrorResponse
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		return nil, fmt.Errorf("token exchange failed: %s - %s", errResp.Error, errResp.ErrorDescription)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	// Store credentials
	if err := h.storeCredentials(ctx, &tokenResp, auth.FlowInteractive, scopes, ""); err != nil {
		return nil, fmt.Errorf("failed to store credentials: %w", err)
	}

	// Cache the access token
	if tokenResp.AccessToken != "" {
		expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		_ = h.tokenCache.Set(ctx, auth.FlowInteractive, auth.FingerprintHash(h.config.ClientID), scopeStr, &auth.Token{
			AccessToken: tokenResp.AccessToken,
			TokenType:   tokenResp.TokenType,
			ExpiresAt:   expiresAt,
			Scope:       scopeStr,
		})
	}

	// Extract claims
	claims, err := extractClaims(&tokenResp)
	if err != nil {
		// Try userinfo endpoint as fallback
		if tokenResp.AccessToken != "" {
			claims, err = h.fetchUserinfoClaims(ctx, tokenResp.AccessToken)
			if err != nil {
				claims = &auth.Claims{Issuer: "https://accounts.google.com"}
			}
		} else {
			claims = &auth.Claims{Issuer: "https://accounts.google.com"}
		}
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	lgr.V(1).Info("ADC browser OAuth flow completed successfully",
		"email", claims.Email,
		"expiresIn", tokenResp.ExpiresIn,
	)

	return &auth.Result{
		Claims:    claims,
		ExpiresAt: expiresAt,
	}, nil
}

// mintToken creates a new access token using the stored refresh token.
func (h *Handler) mintToken(ctx context.Context, scope string) (*auth.Token, error) {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("minting access token", "scope", scope)

	refreshToken, err := h.loadRefreshToken(ctx)
	if err != nil {
		return nil, auth.ErrNotAuthenticated
	}

	metadata, err := h.loadMetadata(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load metadata: %w", err)
	}

	// For ADC flow, use the stored client ID, then configured, then default
	clientID := h.config.ClientID
	clientSecret := h.config.ClientSecret
	if metadata.ClientID != "" {
		clientID = metadata.ClientID
	}
	if clientID == "" {
		clientID = DefaultADCClientID
		clientSecret = DefaultADCClientSecret
	}

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", clientID)
	if clientSecret != "" {
		data.Set("client_secret", clientSecret)
	}
	data.Set("refresh_token", refreshToken)

	resp, err := h.httpClient.PostForm(ctx, tokenEndpoint, data)
	if err != nil {
		return nil, fmt.Errorf("token refresh failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		var errResp TokenErrorResponse
		_ = json.NewDecoder(resp.Body).Decode(&errResp)

		if errResp.Error == "invalid_grant" {
			_ = h.Logout(ctx)
			return nil, fmt.Errorf(
				"GCP credentials have expired or been revoked (%s). "+
					"Run: scafctl auth login gcp: %w",
				errResp.ErrorDescription, auth.ErrTokenExpired)
		}
		return nil, fmt.Errorf(
			"token refresh failed: %s - %s. Run: scafctl auth login gcp to re-authenticate",
			errResp.Error, errResp.ErrorDescription)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	// Handle refresh token rotation
	if tokenResp.RefreshToken != "" && tokenResp.RefreshToken != refreshToken {
		lgr.V(1).Info("refresh token rotated, storing new token")
		if err := h.storeCredentials(ctx, &tokenResp, auth.FlowInteractive, metadata.Scopes, metadata.SessionID); err != nil {
			lgr.V(1).Info("warning: failed to update refresh token", "error", err)
		}
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	return &auth.Token{
		AccessToken: tokenResp.AccessToken,
		TokenType:   tokenResp.TokenType,
		ExpiresAt:   expiresAt,
		Scope:       scope,
		Flow:        auth.FlowInteractive,
		SessionID:   metadata.SessionID,
	}, nil
}

// revokeRefreshToken revokes the stored refresh token with Google.
func (h *Handler) revokeRefreshToken(ctx context.Context) error {
	refreshToken, err := h.loadRefreshToken(ctx)
	if err != nil {
		return nil // No refresh token to revoke
	}

	data := url.Values{}
	data.Set("token", refreshToken)

	resp, err := h.httpClient.PostForm(ctx, revokeEndpoint, data)
	if err != nil {
		return fmt.Errorf("token revocation failed: %w", err)
	}
	defer resp.Body.Close()

	// Google returns 200 on success, but we don't fail on revocation errors
	// because the token might already be revoked
	return nil
}

// NOTE: PKCE and browser utilities have been moved to the shared
// pkg/auth/oauth package (GenerateCodeVerifier, GenerateCodeChallenge, OpenBrowser).
