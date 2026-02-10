// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package entra

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/logger"
)

// TokenMetadata stores information about the stored credentials.
type TokenMetadata struct {
	Claims                *auth.Claims `json:"claims"`
	RefreshTokenExpiresAt time.Time    `json:"refreshTokenExpiresAt"`
	LastRefresh           time.Time    `json:"lastRefresh"`
	TenantID              string       `json:"tenantId"`
}

// mintToken creates a new access token for the specified scope.
func (h *Handler) mintToken(ctx context.Context, scope string) (*auth.Token, error) {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("minting access token", "scope", scope)

	// Load refresh token
	refreshToken, err := h.loadRefreshToken(ctx)
	if err != nil {
		return nil, auth.ErrNotAuthenticated
	}

	// Load metadata for tenant info
	metadata, err := h.loadMetadata(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load metadata: %w", err)
	}

	// Request new access token using refresh token
	endpoint := fmt.Sprintf("%s/%s/oauth2/v2.0/token", h.config.GetAuthority(), metadata.TenantID)

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", h.config.ClientID)
	data.Set("refresh_token", refreshToken)
	data.Set("scope", scope)

	resp, err := h.httpClient.PostForm(ctx, endpoint, data)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp TokenErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			return nil, fmt.Errorf("token request failed with status %d", resp.StatusCode)
		}

		// Check if refresh token is expired
		if errResp.Error == "invalid_grant" {
			// Clear stored credentials since they're no longer valid
			_ = h.Logout(ctx)
			return nil, auth.ErrTokenExpired
		}

		return nil, fmt.Errorf("token request failed: %s - %s", errResp.Error, errResp.ErrorDescription)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	// If we got a new refresh token, store it (token rotation)
	if tokenResp.RefreshToken != "" && tokenResp.RefreshToken != refreshToken {
		lgr.V(1).Info("refresh token rotated, storing new token")
		if err := h.storeCredentials(ctx, metadata.TenantID, &tokenResp); err != nil {
			lgr.V(1).Info("warning: failed to update refresh token", "error", err)
		}
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	lgr.V(1).Info("access token minted successfully",
		"expiresIn", tokenResp.ExpiresIn,
		"expiresAt", expiresAt,
		"scope", scope,
	)

	return &auth.Token{
		AccessToken: tokenResp.AccessToken,
		TokenType:   tokenResp.TokenType,
		ExpiresAt:   expiresAt,
		Scope:       scope,
	}, nil
}

// storeCredentials securely stores the refresh token and metadata.
func (h *Handler) storeCredentials(ctx context.Context, tenantID string, tokenResp *TokenResponse) error {
	// Store refresh token
	if err := h.secretStore.Set(ctx, SecretKeyRefreshToken, []byte(tokenResp.RefreshToken)); err != nil {
		return fmt.Errorf("failed to store refresh token: %w", err)
	}

	// Extract claims and store metadata
	claims, err := h.extractClaims(tokenResp)
	if err != nil {
		// Use minimal claims if extraction fails
		claims = &auth.Claims{
			TenantID: tenantID,
		}
	}

	metadata := &TokenMetadata{
		Claims:                claims,
		RefreshTokenExpiresAt: time.Now().Add(DefaultRefreshTokenLifetime),
		LastRefresh:           time.Now(),
		TenantID:              tenantID,
	}

	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := h.secretStore.Set(ctx, SecretKeyMetadata, metadataBytes); err != nil {
		return fmt.Errorf("failed to store metadata: %w", err)
	}

	return nil
}

// loadRefreshToken loads the stored refresh token.
func (h *Handler) loadRefreshToken(ctx context.Context) (string, error) {
	data, err := h.secretStore.Get(ctx, SecretKeyRefreshToken)
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

// extractClaims extracts normalized claims from the token response.
func (h *Handler) extractClaims(tokenResp *TokenResponse) (*auth.Claims, error) {
	if tokenResp.IDToken == "" {
		return &auth.Claims{}, nil
	}

	// Parse ID token (JWT format: header.payload.signature)
	parts := splitJWT(tokenResp.IDToken)
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid ID token format")
	}

	// Decode payload (base64url)
	payload, err := base64URLDecode(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode ID token payload: %w", err)
	}

	var idTokenClaims struct {
		Issuer            string `json:"iss"`
		Subject           string `json:"sub"`
		Audience          string `json:"aud"`
		TenantID          string `json:"tid"`
		ObjectID          string `json:"oid"`
		Email             string `json:"email"`
		PreferredUsername string `json:"preferred_username"`
		Name              string `json:"name"`
		IssuedAt          int64  `json:"iat"`
		ExpiresAt         int64  `json:"exp"`
	}

	if err := json.Unmarshal(payload, &idTokenClaims); err != nil {
		return nil, fmt.Errorf("failed to parse ID token claims: %w", err)
	}

	email := idTokenClaims.Email
	if email == "" {
		email = idTokenClaims.PreferredUsername
	}

	return &auth.Claims{
		Issuer:    idTokenClaims.Issuer,
		Subject:   idTokenClaims.Subject,
		TenantID:  idTokenClaims.TenantID,
		ObjectID:  idTokenClaims.ObjectID,
		ClientID:  idTokenClaims.Audience,
		Email:     email,
		Name:      idTokenClaims.Name,
		Username:  idTokenClaims.PreferredUsername,
		IssuedAt:  time.Unix(idTokenClaims.IssuedAt, 0),
		ExpiresAt: time.Unix(idTokenClaims.ExpiresAt, 0),
	}, nil
}

// splitJWT splits a JWT into its parts without using strings.Split
// to avoid issues with tokens that might contain periods in their values.
func splitJWT(token string) []string {
	parts := make([]string, 0, 3)
	start := 0
	for i := 0; i < len(token); i++ {
		if token[i] == '.' {
			parts = append(parts, token[start:i])
			start = i + 1
		}
	}
	parts = append(parts, token[start:])
	return parts
}

// base64URLDecode decodes a base64url encoded string.
func base64URLDecode(s string) ([]byte, error) {
	// Add padding if necessary
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.URLEncoding.DecodeString(s)
}
