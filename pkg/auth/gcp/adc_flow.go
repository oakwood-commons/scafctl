// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package gcp provides Google Cloud Platform authentication for scafctl.
// This file implements the ADC (Application Default Credentials) browser OAuth flow
// using authorization code + PKCE with a local redirect server.
package gcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
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

	if h.config.ClientID == "" {
		// Try gcloud ADC fallback
		lgr.V(1).Info("no client ID configured, trying gcloud ADC fallback")
		return h.gcloudADCLogin(ctx, opts)
	}

	// Determine scopes
	scopes := opts.Scopes
	if len(scopes) == 0 {
		scopes = h.config.DefaultScopes
	}
	scopeStr := strings.Join(scopes, " ")

	// Generate PKCE code verifier and challenge
	codeVerifier, err := generateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("generating PKCE code verifier: %w", err)
	}
	codeChallenge := generateCodeChallenge(codeVerifier)

	// Start local HTTP server for redirect
	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", "localhost:0")
	if err != nil {
		return nil, fmt.Errorf("starting local redirect server: %w", err)
	}
	defer listener.Close()

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return nil, fmt.Errorf("unexpected listener address type: %T", listener.Addr())
	}
	port := tcpAddr.Port
	redirectURI := fmt.Sprintf("http://localhost:%d", port)

	// Channel to receive the authorization code
	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)

	// Set up the redirect handler
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			errMsg := r.URL.Query().Get("error")
			if errMsg == "" {
				errMsg = "no authorization code received"
			}
			errChan <- fmt.Errorf("OAuth error: %s", errMsg)
			fmt.Fprintf(w, "<html><body><h1>Authentication Failed</h1><p>%s</p><p>You can close this window.</p></body></html>", html.EscapeString(errMsg)) //nolint:gosec // G705: input is escaped via html.EscapeString
			return
		}
		codeChan <- code
		fmt.Fprint(w, "<html><body><h1>Authentication Successful</h1><p>You can close this window and return to the terminal.</p></body></html>")
	})

	server := &http.Server{Handler: mux, ReadHeaderTimeout: 30 * time.Second}
	go func() {
		if sErr := server.Serve(listener); sErr != nil && sErr != http.ErrServerClosed {
			errChan <- fmt.Errorf("redirect server error: %w", sErr)
		}
	}()
	defer server.Close()

	// Build authorization URL
	authURL := fmt.Sprintf("%s?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&code_challenge=%s&code_challenge_method=S256&access_type=offline&prompt=consent",
		authorizationEndpoint,
		url.QueryEscape(h.config.ClientID),
		url.QueryEscape(redirectURI),
		url.QueryEscape(scopeStr),
		url.QueryEscape(codeChallenge),
	)

	// Open browser
	lgr.V(1).Info("opening browser for authentication", "url", authURL)
	if err := openBrowser(ctx, authURL); err != nil {
		lgr.V(0).Info("failed to open browser, please open this URL manually", "url", authURL)
	}

	// Wait for authorization code or timeout
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	var authCode string
	select {
	case authCode = <-codeChan:
		lgr.V(1).Info("received authorization code")
	case err := <-errChan:
		return nil, err
	case <-time.After(timeout):
		return nil, fmt.Errorf("authentication timed out: no response received from browser: %w", auth.ErrTimeout)
	case <-ctx.Done():
		return nil, fmt.Errorf("authentication cancelled: %w", auth.ErrUserCancelled)
	}

	// Exchange code for tokens
	data := url.Values{}
	data.Set("code", authCode)
	data.Set("client_id", h.config.ClientID)
	if h.config.ClientSecret != "" {
		data.Set("client_secret", h.config.ClientSecret)
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
	if err := h.storeCredentials(ctx, &tokenResp, auth.FlowInteractive, scopes); err != nil {
		return nil, fmt.Errorf("failed to store credentials: %w", err)
	}

	// Cache the access token
	if tokenResp.AccessToken != "" {
		expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		_ = h.tokenCache.Set(ctx, scopeStr, &auth.Token{
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

	// For ADC flow, use the stored client ID
	clientID := h.config.ClientID
	if metadata.ClientID != "" {
		clientID = metadata.ClientID
	}

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", clientID)
	if h.config.ClientSecret != "" {
		data.Set("client_secret", h.config.ClientSecret)
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
			return nil, fmt.Errorf("%s: %w", errResp.ErrorDescription, auth.ErrTokenExpired)
		}
		return nil, fmt.Errorf("token refresh failed: %s - %s", errResp.Error, errResp.ErrorDescription)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	// Handle refresh token rotation
	if tokenResp.RefreshToken != "" && tokenResp.RefreshToken != refreshToken {
		lgr.V(1).Info("refresh token rotated, storing new token")
		if err := h.storeCredentials(ctx, &tokenResp, auth.FlowInteractive, metadata.Scopes); err != nil {
			lgr.V(1).Info("warning: failed to update refresh token", "error", err)
		}
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	return &auth.Token{
		AccessToken: tokenResp.AccessToken,
		TokenType:   tokenResp.TokenType,
		ExpiresAt:   expiresAt,
		Scope:       scope,
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

// generateCodeVerifier generates a PKCE code verifier (43-128 character random string).
func generateCodeVerifier() (string, error) {
	buf := make([]byte, 32) // 32 bytes → 43 base64url chars
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// generateCodeChallenge creates a PKCE code challenge from a code verifier.
func generateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// openBrowser opens a URL in the default system browser.
func openBrowser(ctx context.Context, url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "linux":
		cmd = "xdg-open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return exec.CommandContext(ctx, cmd, args...).Start() //nolint:gosec // URL is from trusted internal config
}
