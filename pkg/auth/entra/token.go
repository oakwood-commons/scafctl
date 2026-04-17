// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package entra

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// TokenMetadata stores information about the stored credentials.
type TokenMetadata struct {
	Claims                *auth.Claims `json:"claims"`
	RefreshTokenExpiresAt time.Time    `json:"refreshTokenExpiresAt"`
	LastRefresh           time.Time    `json:"lastRefresh"`
	TenantID              string       `json:"tenantId"`
	ClientID              string       `json:"clientId,omitempty"`
	Scopes                []string     `json:"scopes,omitempty"`

	// LoginFlow records the authentication flow used during the original login
	// (e.g. "device_code"). Stored so that tokens minted from a stored refresh
	// token can report the originating flow to callers.
	LoginFlow auth.Flow `json:"loginFlow,omitempty"`

	// SessionID is a stable identifier for the authentication session.
	// Generated once at login time and preserved across refresh-token rotations
	// so that every access token minted from a given login can be traced back
	// to the originating session.
	SessionID string `json:"sessionId,omitempty"`
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

	// Use the client ID that was used during login (stored in metadata).
	// This ensures the refresh token is always paired with the client ID
	// that originally obtained it. If missing, the user must re-login.
	if metadata.ClientID == "" {
		return nil, fmt.Errorf("stored credentials are missing client ID, please re-authenticate with '%s auth login entra': %w", settings.BinaryNameFromContext(ctx), auth.ErrNotAuthenticated)
	}

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", metadata.ClientID)
	data.Set("refresh_token", refreshToken)
	data.Set("scope", ensureOfflineAccess(scope))

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

		// Claims challenge: Conditional Access requires step-up authentication.
		// Return a structured error so the caller can trigger interactive re-auth
		// with the claims parameter.
		if errResp.Claims != "" || strings.Contains(errResp.ErrorDescription, "AADSTS53003") {
			lgr.V(0).Info("claims challenge received, interactive re-authentication required",
				"scope", scope,
				"hasClaims", errResp.Claims != "",
			)
			return nil, &auth.ClaimsChallengeError{
				Claims: errResp.Claims,
				Scope:  scope,
			}
		}

		// Check if refresh token is expired or consent is required
		if errResp.Error == "invalid_grant" {
			lgr.V(0).Info("token refresh failed with invalid_grant",
				"errorDescription", errResp.ErrorDescription,
				"scope", scope,
			)

			// AADSTS700016: application not found — this is a misconfiguration, not a
			// revoked token.  Return a clear error without wiping stored credentials.
			if strings.Contains(errResp.ErrorDescription, "AADSTS700016") {
				return nil, formatAADSTSError(fmt.Sprintf("token refresh failed for scope %q", scope), errResp)
			}

			// AADSTS70000: generic invalid grant (revoked / rotated refresh token).
			if strings.Contains(errResp.ErrorDescription, "AADSTS70000") {
				return nil, fmt.Errorf("scope %q: %s: %w", scope, errResp.ErrorDescription, auth.ErrGrantInvalid)
			}

			// AADSTS65001: consent not granted for the requested scope
			// AADSTS70011: invalid scope value
			// In these cases the refresh token is still valid — don't logout
			if strings.Contains(errResp.ErrorDescription, "AADSTS65001") ||
				strings.Contains(errResp.ErrorDescription, "AADSTS70011") {
				return nil, fmt.Errorf("scope %q: %s: %w", scope, errResp.ErrorDescription, auth.ErrConsentRequired)
			}

			// For genuine token expiry / revocation, clear stored credentials
			_ = h.Logout(ctx)
			if errResp.ErrorDescription != "" {
				return nil, fmt.Errorf("%s: %w", errResp.ErrorDescription, auth.ErrTokenExpired)
			}
			return nil, auth.ErrTokenExpired
		}

		if strings.Contains(errResp.ErrorDescription, "AADSTS") {
			return nil, formatAADSTSError("token request failed", errResp)
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
		if err := h.storeCredentials(ctx, metadata.TenantID, &tokenResp, metadata.ClientID, metadata.Scopes, metadata.LoginFlow, metadata.SessionID); err != nil {
			lgr.V(1).Info("warning: failed to update refresh token", "error", err)
		}
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	lgr.V(1).Info("access token minted successfully",
		"expiresIn", tokenResp.ExpiresIn,
		"expiresAt", expiresAt,
		"scope", scope,
		"sessionId", metadata.SessionID,
	)

	return &auth.Token{
		AccessToken: tokenResp.AccessToken,
		TokenType:   tokenResp.TokenType,
		ExpiresAt:   expiresAt,
		Scope:       scope,
		Flow:        metadata.LoginFlow,
		SessionID:   metadata.SessionID,
	}, nil
}

// storeCredentials securely stores the refresh token and metadata.
// clientID is the application client ID that was used to obtain the token —
// this MUST be the same value that was used during the original login so that
// future refresh-token exchanges continue to use the correct client.
// scopes records which OAuth scopes were used during login so they can be
// surfaced later (e.g. in `auth status`).
// loginFlow records the authentication flow (e.g. auth.FlowDeviceCode) so that
// tokens minted from the stored refresh token can surface the originating flow.
// sessionID is a stable identifier for the authentication session.  Pass an
// empty string on initial login to auto-generate a new ID; pass the existing
// ID during refresh-token rotation to preserve the session lineage.
func (h *Handler) storeCredentials(ctx context.Context, tenantID string, tokenResp *TokenResponse, clientID string, scopes []string, loginFlow auth.Flow, sessionID string) error {
	// Validate refresh token is present
	if tokenResp.RefreshToken == "" {
		return fmt.Errorf("no refresh token in response (offline_access scope may be missing)")
	}

	// Store refresh token
	if err := h.secretStore.Set(ctx, SecretKeyRefreshToken, []byte(tokenResp.RefreshToken)); err != nil {
		return fmt.Errorf("failed to store refresh token: %w", err)
	}

	// Generate a new session ID on initial login; preserve across rotations.
	if sessionID == "" {
		sessionID = uuid.New().String()
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
		ClientID:              clientID,
		Scopes:                scopes,
		LoginFlow:             loginFlow,
		SessionID:             sessionID,
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
// Delegates to the shared auth.ParseJWTClaims parser.
func (h *Handler) extractClaims(tokenResp *TokenResponse) (*auth.Claims, error) {
	if tokenResp.IDToken == "" {
		return &auth.Claims{}, nil
	}

	return auth.ParseJWTClaims(tokenResp.IDToken)
}

// ensureOfflineAccess appends "offline_access" to a space-delimited scope
// string when it is not already present.  This guarantees the token endpoint
// returns a refresh token alongside the access token, enabling token rotation.
func ensureOfflineAccess(scope string) string {
	for _, s := range strings.Split(scope, " ") {
		if s == "offline_access" {
			return scope
		}
	}
	return scope + " offline_access"
}
