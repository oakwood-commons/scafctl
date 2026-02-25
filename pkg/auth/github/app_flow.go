// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/logger"
)

// AppInfo represents the response from the GET /app endpoint.
type AppInfo struct {
	ID          int64  `json:"id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Owner       struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
	} `json:"owner"`
}

// InstallationTokenResponse represents the response from
// POST /app/installations/{id}/access_tokens.
type InstallationTokenResponse struct {
	Token       string            `json:"token"`      //nolint:gosec // Not a hardcoded credential
	ExpiresAt   time.Time         `json:"expires_at"` //nolint:gosec // Not a hardcoded credential
	Permissions map[string]string `json:"permissions"`
}

// installationTokenCacheKey is the fixed cache key for GitHub App installation tokens.
const installationTokenCacheKey = "_github_app" //nolint:gosec // Not a credential, just a cache key name

// SecretKeyAppJWT is the secret key for storing the GitHub App JWT metadata.
const SecretKeyAppJWT = "scafctl.auth.github.app_metadata" //nolint:gosec // This is a key name, not a credential

// appLogin performs the GitHub App installation token flow.
// 1. Load private key from config (inline, file, or secret store)
// 2. Create a short-lived JWT signed with the private key (RS256)
// 3. Exchange the JWT for an installation access token
// 4. Store the token and metadata
func (h *Handler) appLogin(ctx context.Context, _ auth.LoginOptions) (*auth.Result, error) {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("starting GitHub App installation token flow")

	// Validate required config
	if err := h.config.ValidateAppConfig(ctx, h.secretStore); err != nil {
		return nil, auth.NewError(HandlerName, "app_config", err)
	}

	appID := h.config.GetAppID()
	installationID := h.config.GetInstallationID()

	// Load and parse private key
	keyBytes, err := h.config.GetPrivateKey(ctx, h.secretStore)
	if err != nil {
		return nil, auth.NewError(HandlerName, "private_key", err)
	}

	privateKey, err := parseRSAPrivateKey(keyBytes)
	if err != nil {
		return nil, auth.NewError(HandlerName, "private_key_parse", err)
	}

	// Create JWT
	jwt, err := createAppJWT(appID, privateKey)
	if err != nil {
		return nil, auth.NewError(HandlerName, "jwt_create", err)
	}

	lgr.V(1).Info("created GitHub App JWT", "appId", appID)

	// Validate JWT by calling GET /app
	appInfo, err := h.getAppInfo(ctx, jwt)
	if err != nil {
		return nil, auth.NewError(HandlerName, "app_validate", fmt.Errorf("failed to validate GitHub App: %w", err))
	}

	lgr.V(1).Info("validated GitHub App",
		"appId", appInfo.ID,
		"slug", appInfo.Slug,
		"name", appInfo.Name,
	)

	// Exchange JWT for installation access token
	installToken, err := h.createInstallationToken(ctx, jwt, installationID)
	if err != nil {
		return nil, auth.NewError(HandlerName, "installation_token", err)
	}

	lgr.V(1).Info("acquired installation token",
		"installationId", installationID,
		"expiresAt", installToken.ExpiresAt,
	)

	// Cache the token
	token := &auth.Token{
		AccessToken: installToken.Token,
		TokenType:   "Bearer",
		ExpiresAt:   installToken.ExpiresAt,
		Flow:        auth.FlowGitHubApp,
	}
	if h.tokenCache != nil {
		if cacheErr := h.tokenCache.Set(ctx, installationTokenCacheKey, token); cacheErr != nil {
			lgr.V(1).Info("failed to cache installation token", "error", cacheErr)
		}
	}

	// Store as primary access token
	if err := h.secretStore.Set(ctx, SecretKeyAccessToken, []byte(installToken.Token)); err != nil {
		return nil, auth.NewError(HandlerName, "store_token", fmt.Errorf("failed to store installation token: %w", err))
	}

	// Build claims from app info
	claims := &auth.Claims{
		Issuer:   h.config.Hostname,
		Subject:  fmt.Sprintf("app/%s", appInfo.Slug),
		ObjectID: strconv.FormatInt(appInfo.ID, 10),
		Name:     appInfo.Name,
		Username: appInfo.Slug,
		IssuedAt: time.Now(),
	}

	// Store metadata
	metadata := &TokenMetadata{
		Claims:       claims,
		LastRefresh:  time.Now(),
		Hostname:     h.config.Hostname,
		IdentityType: string(auth.IdentityTypeServicePrincipal),
	}
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return nil, auth.NewError(HandlerName, "store_metadata", fmt.Errorf("failed to marshal metadata: %w", err))
	}
	if err := h.secretStore.Set(ctx, SecretKeyMetadata, metadataBytes); err != nil {
		return nil, auth.NewError(HandlerName, "store_metadata", fmt.Errorf("failed to store metadata: %w", err))
	}

	return &auth.Result{
		Claims:    claims,
		ExpiresAt: installToken.ExpiresAt,
	}, nil
}

// getAppInfo calls GET /app to validate the JWT and retrieve app information.
func (h *Handler) getAppInfo(ctx context.Context, jwt string) (*AppInfo, error) {
	endpoint := fmt.Sprintf("%s/app", h.config.GetAPIBaseURL())
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", jwt),
	}

	resp, err := h.httpClient.Get(ctx, endpoint, headers)
	if err != nil {
		return nil, fmt.Errorf("GET /app failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GET /app returned status %d — verify app ID and private key are correct", resp.StatusCode)
	}

	var appInfo AppInfo
	if err := json.NewDecoder(resp.Body).Decode(&appInfo); err != nil {
		return nil, fmt.Errorf("failed to parse app info response: %w", err)
	}

	return &appInfo, nil
}

// createInstallationToken exchanges a GitHub App JWT for an installation access token.
func (h *Handler) createInstallationToken(ctx context.Context, jwt string, installationID int64) (*InstallationTokenResponse, error) {
	endpoint := fmt.Sprintf("%s/app/installations/%d/access_tokens", h.config.GetAPIBaseURL(), installationID)
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", jwt),
	}

	resp, err := h.httpClient.PostJSON(ctx, endpoint, nil, headers)
	if err != nil {
		return nil, fmt.Errorf("create installation token failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		return nil, fmt.Errorf("create installation token returned status %d — verify installation ID %d is correct", resp.StatusCode, installationID)
	}

	var tokenResp InstallationTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse installation token response: %w", err)
	}

	return &tokenResp, nil
}

// parseRSAPrivateKey parses a PEM-encoded RSA private key.
// Supports both PKCS#1 (RSA PRIVATE KEY) and PKCS#8 (PRIVATE KEY) formats.
func parseRSAPrivateKey(keyBytes []byte) (*rsa.PrivateKey, error) {
	key, err := jwt.ParseRSAPrivateKeyFromPEM(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse RSA private key: %w", err)
	}
	return key, nil
}

// createAppJWT creates a JWT for a GitHub App.
// The JWT is signed with RS256 and has a 10-minute expiry.
// See: https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-json-web-token-jwt-for-a-github-app
func createAppJWT(appID int64, privateKey *rsa.PrivateKey) (string, error) {
	now := time.Now()

	claims := jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)), // 60 seconds in the past to account for clock drift
		ExpiresAt: jwt.NewNumericDate(now.Add(10 * time.Minute)),  // Maximum 10 minutes
		Issuer:    strconv.FormatInt(appID, 10),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signedToken, err := token.SignedString(privateKey)
	if err != nil {
		return "", fmt.Errorf("signing JWT: %w", err)
	}

	return signedToken, nil
}
