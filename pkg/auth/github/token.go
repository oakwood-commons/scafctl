// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// TokenMetadata stores information about the stored credentials.
type TokenMetadata struct {
	Claims                *auth.Claims `json:"claims"`
	RefreshTokenExpiresAt time.Time    `json:"refreshTokenExpiresAt,omitempty"`
	LastRefresh           time.Time    `json:"lastRefresh"`
	Hostname              string       `json:"hostname"`
	ClientID              string       `json:"clientId,omitempty"`
	Scopes                []string     `json:"scopes,omitempty"`
	IdentityType          string       `json:"identityType,omitempty"`

	// SessionID is a stable identifier for the authentication session.
	// Generated once at login time and preserved across refresh-token rotations.
	SessionID string `json:"sessionId,omitempty"`
}

// TokenResponse represents the response from the GitHub OAuth token endpoint.
type TokenResponse struct {
	AccessToken           string `json:"access_token"`            //nolint:gosec // Not a hardcoded credential
	RefreshToken          string `json:"refresh_token,omitempty"` //nolint:gosec // Not a hardcoded credential
	TokenType             string `json:"token_type"`
	Scope                 string `json:"scope"`
	ExpiresIn             int    `json:"expires_in,omitempty"`               // Present when token expiration is enabled
	RefreshTokenExpiresIn int    `json:"refresh_token_expires_in,omitempty"` // Present when token expiration is enabled
}

// TokenErrorResponse represents an error from the GitHub OAuth token endpoint.
type TokenErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
	ErrorURI         string `json:"error_uri,omitempty"`
}

// mintToken creates a new access token using the refresh token.
func (h *Handler) mintToken(ctx context.Context, scope string) (*auth.Token, error) {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("minting access token", "scope", scope)

	// Load refresh token
	refreshToken, err := h.loadRefreshToken(ctx)
	if err != nil {
		return nil, auth.ErrNotAuthenticated
	}

	// Load metadata for client info
	metadata, err := h.loadMetadata(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load metadata: %w", err)
	}

	// Use the client ID that was used during login
	if metadata.ClientID == "" {
		return nil, fmt.Errorf("stored credentials are missing client ID, please re-authenticate with '%s auth login github': %w", settings.BinaryNameFromContext(ctx), auth.ErrNotAuthenticated)
	}

	// Refresh the token
	token, err := h.refreshAccessToken(ctx, refreshToken, metadata.ClientID)
	if err != nil {
		return nil, err
	}

	return token, nil
}

// refreshAccessToken refreshes an access token using the refresh token.
func (h *Handler) refreshAccessToken(ctx context.Context, refreshToken, clientID string) (*auth.Token, error) {
	lgr := logger.FromContext(ctx)
	endpoint := fmt.Sprintf("%s/login/oauth/access_token", h.config.GetOAuthBaseURL())

	data := makeFormData(map[string]string{
		"client_id":     clientID,
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
	})

	resp, err := h.httpClient.PostForm(ctx, endpoint, data)
	if err != nil {
		return nil, fmt.Errorf("token refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	// GitHub returns errors in the JSON body even with 200 status
	if tokenResp.AccessToken == "" {
		lgr.V(1).Info("token refresh returned empty access token, possible error")
		_ = h.Logout(ctx)
		return nil, fmt.Errorf("refresh token expired or revoked: %w", auth.ErrTokenExpired)
	}

	// If we got a new refresh token, update stored credentials
	if tokenResp.RefreshToken != "" && tokenResp.RefreshToken != refreshToken {
		lgr.V(1).Info("refresh token rotated, storing new token")
		metadata, _ := h.loadMetadata(ctx)
		var scopes []string
		var sessionID string
		if metadata != nil {
			scopes = metadata.Scopes
			sessionID = metadata.SessionID
		}
		if _, err := h.storeCredentials(ctx, &tokenResp, scopes, sessionID); err != nil {
			lgr.V(1).Info("warning: failed to update refresh token", "error", err)
		}
	}

	expiresAt := time.Now().Add(8 * time.Hour) // Default if no expiry info
	if tokenResp.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	return &auth.Token{
		AccessToken: tokenResp.AccessToken,
		TokenType:   tokenResp.TokenType,
		ExpiresAt:   expiresAt,
		Scope:       tokenResp.Scope,
		Flow:        auth.FlowDeviceCode,
	}, nil
}

// storeCredentials securely stores the refresh token (or access token) and metadata.
func (h *Handler) storeCredentials(ctx context.Context, tokenResp *TokenResponse, scopes []string, sessionID string) (*auth.Claims, error) {
	// Store refresh token if present (OAuth App with token expiration enabled)
	if tokenResp.RefreshToken != "" {
		if err := h.secretStore.Set(ctx, SecretKeyRefreshToken, []byte(tokenResp.RefreshToken)); err != nil {
			return nil, fmt.Errorf("failed to store refresh token: %w", err)
		}
	}

	// Store access token for PAT-like usage or when no refresh token
	if tokenResp.AccessToken != "" {
		if err := h.secretStore.Set(ctx, SecretKeyAccessToken, []byte(tokenResp.AccessToken)); err != nil {
			return nil, fmt.Errorf("failed to store access token: %w", err)
		}
	}

	// Fetch claims from the GitHub API
	claims, err := h.fetchUserClaims(ctx, tokenResp.AccessToken)
	if err != nil {
		// Use minimal claims if extraction fails
		claims = &auth.Claims{
			Issuer: h.config.Hostname,
		}
	}

	// Generate a new session ID on initial login; preserve across rotations.
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	// Determine refresh token expiry
	var refreshTokenExpiresAt time.Time
	if tokenResp.RefreshTokenExpiresIn > 0 {
		refreshTokenExpiresAt = time.Now().Add(time.Duration(tokenResp.RefreshTokenExpiresIn) * time.Second)
	}

	metadata := &TokenMetadata{
		Claims:                claims,
		RefreshTokenExpiresAt: refreshTokenExpiresAt,
		LastRefresh:           time.Now(),
		Hostname:              h.config.Hostname,
		ClientID:              h.config.ClientID,
		Scopes:                scopes,
		IdentityType:          string(auth.IdentityTypeUser),
		SessionID:             sessionID,
	}

	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := h.secretStore.Set(ctx, SecretKeyMetadata, metadataBytes); err != nil {
		return nil, fmt.Errorf("failed to store metadata: %w", err)
	}

	return claims, nil
}

// loadRefreshToken loads the stored refresh token.
func (h *Handler) loadRefreshToken(ctx context.Context) (string, error) {
	data, err := h.secretStore.Get(ctx, SecretKeyRefreshToken)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// loadAccessToken loads the stored access token.
func (h *Handler) loadAccessToken(ctx context.Context) (string, error) {
	data, err := h.secretStore.Get(ctx, SecretKeyAccessToken)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// loadMetadata loads the stored token metadata.
func (h *Handler) loadMetadata(ctx context.Context) (*TokenMetadata, error) {
	data, err := h.secretStore.Get(ctx, SecretKeyMetadata)
	if err != nil {
		return nil, err
	}

	var metadata TokenMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return &metadata, nil
}

// makeFormData is a helper to create url.Values from a string map.
func makeFormData(params map[string]string) map[string][]string {
	data := make(map[string][]string, len(params))
	for k, v := range params {
		data[k] = []string{v}
	}
	return data
}
