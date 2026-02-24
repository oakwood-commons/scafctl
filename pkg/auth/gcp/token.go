// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/logger"
)

// TokenMetadata stores information about the stored GCP credentials.
type TokenMetadata struct {
	Claims                    *auth.Claims `json:"claims"`
	RefreshTokenExpiresAt     time.Time    `json:"refreshTokenExpiresAt,omitempty"`
	Flow                      auth.Flow    `json:"flow"`
	ClientID                  string       `json:"clientId,omitempty"`
	Project                   string       `json:"project,omitempty"`
	ImpersonateServiceAccount string       `json:"impersonateServiceAccount,omitempty"`
	Scopes                    []string     `json:"scopes,omitempty"`
	ServiceAccountEmail       string       `json:"serviceAccountEmail,omitempty"`

	// SessionID is a stable identifier for the authentication session.
	// Generated once at login time and preserved across refresh-token rotations.
	SessionID string `json:"sessionId,omitempty"`
}

// TokenResponse represents the response from GCP token endpoints.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`  //nolint:gosec // G117: not a hardcoded credential, stores runtime token data
	RefreshToken string `json:"refresh_token"` //nolint:gosec // G117: not a hardcoded credential, stores runtime token data
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	IDToken      string `json:"id_token,omitempty"`
}

// TokenErrorResponse represents an error from GCP token endpoint.
type TokenErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// storeCredentials securely stores the refresh token and metadata.
func (h *Handler) storeCredentials(ctx context.Context, tokenResp *TokenResponse, flow auth.Flow, scopes []string, sessionID string) error {
	// Store refresh token if present (ADC browser flow)
	if tokenResp.RefreshToken != "" {
		if err := h.secretStore.Set(ctx, SecretKeyRefreshToken, []byte(tokenResp.RefreshToken)); err != nil {
			return fmt.Errorf("failed to store refresh token: %w", err)
		}
	}

	// Extract claims
	claims, err := extractClaims(tokenResp)
	if err != nil {
		claims = &auth.Claims{
			Issuer: "https://accounts.google.com",
		}
	}

	// Generate a new session ID on initial login; preserve across rotations.
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	metadata := &TokenMetadata{
		Claims:                    claims,
		Flow:                      flow,
		ClientID:                  h.config.ClientID,
		Project:                   h.config.Project,
		ImpersonateServiceAccount: h.config.ImpersonateServiceAccount,
		Scopes:                    scopes,
		SessionID:                 sessionID,
	}

	if tokenResp.RefreshToken != "" {
		// Refresh tokens don't have a fixed expiry from Google, but
		// they can be revoked. We set a long expiry for display purposes.
		metadata.RefreshTokenExpiresAt = time.Now().Add(180 * 24 * time.Hour)
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

// storeMetadataOnly stores metadata without a refresh token (for SA/WI/metadata flows).
func (h *Handler) storeMetadataOnly(ctx context.Context, claims *auth.Claims, flow auth.Flow, scopes []string) error {
	metadata := &TokenMetadata{
		Claims:                    claims,
		Flow:                      flow,
		Project:                   h.config.Project,
		ImpersonateServiceAccount: h.config.ImpersonateServiceAccount,
		Scopes:                    scopes,
		SessionID:                 uuid.New().String(),
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

// extractClaims extracts normalized claims from a GCP token response.
// Uses the ID token (JWT) if available.
func extractClaims(tokenResp *TokenResponse) (*auth.Claims, error) {
	if tokenResp.IDToken == "" {
		return &auth.Claims{
			Issuer: "https://accounts.google.com",
		}, nil
	}

	return extractClaimsFromIDToken(tokenResp.IDToken)
}

// extractClaimsFromIDToken extracts claims from a GCP ID token JWT.
func extractClaimsFromIDToken(idToken string) (*auth.Claims, error) {
	parts := splitJWT(idToken)
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid ID token format")
	}

	payload, err := base64URLDecode(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode ID token payload: %w", err)
	}

	var idTokenClaims struct {
		Issuer    string `json:"iss"`
		Subject   string `json:"sub"`
		Email     string `json:"email"`
		Name      string `json:"name"`
		IssuedAt  int64  `json:"iat"`
		ExpiresAt int64  `json:"exp"`
	}

	if err := json.Unmarshal(payload, &idTokenClaims); err != nil {
		return nil, fmt.Errorf("failed to parse ID token claims: %w", err)
	}

	username := ""
	if idTokenClaims.Email != "" {
		if idx := strings.Index(idTokenClaims.Email, "@"); idx > 0 {
			username = idTokenClaims.Email[:idx]
		}
	}

	return &auth.Claims{
		Issuer:   idTokenClaims.Issuer,
		Subject:  idTokenClaims.Subject,
		Email:    idTokenClaims.Email,
		Name:     idTokenClaims.Name,
		Username: username,
		IssuedAt: time.Unix(idTokenClaims.IssuedAt, 0),
	}, nil
}

// fetchUserinfoClaims fetches user info from the Google userinfo endpoint.
func (h *Handler) fetchUserinfoClaims(ctx context.Context, accessToken string) (*auth.Claims, error) {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("fetching user info from Google userinfo endpoint")

	resp, err := h.httpClient.Get(ctx, "https://openidconnect.googleapis.com/v1/userinfo", map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if err != nil {
		return nil, fmt.Errorf("userinfo request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("userinfo request failed with status %d", resp.StatusCode)
	}

	var userinfo struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
		Name  string `json:"name"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&userinfo); err != nil {
		return nil, fmt.Errorf("failed to parse userinfo response: %w", err)
	}

	username := ""
	if userinfo.Email != "" {
		if idx := strings.Index(userinfo.Email, "@"); idx > 0 {
			username = userinfo.Email[:idx]
		}
	}

	return &auth.Claims{
		Issuer:   "https://accounts.google.com",
		Subject:  userinfo.Sub,
		Email:    userinfo.Email,
		Name:     userinfo.Name,
		Username: username,
	}, nil
}

// splitJWT splits a JWT into its parts.
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
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.URLEncoding.DecodeString(s)
}
