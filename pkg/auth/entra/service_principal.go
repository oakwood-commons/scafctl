// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package entra provides Microsoft Entra ID authentication for scafctl.
// This file implements the service principal (client credentials) flow.
package entra

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/logger"
)

// Service principal environment variable names (following Azure SDK conventions).
const (
	// EnvAzureClientID is the environment variable for the service principal client ID.
	EnvAzureClientID = "AZURE_CLIENT_ID"

	// EnvAzureTenantID is the environment variable for the Azure tenant ID.
	EnvAzureTenantID = "AZURE_TENANT_ID"

	// EnvAzureClientSecret is the environment variable for the client secret.
	EnvAzureClientSecret = "AZURE_CLIENT_SECRET" //nolint:gosec // This is the env var name, not a credential
)

// ServicePrincipalCredentials holds the credentials for service principal authentication.
type ServicePrincipalCredentials struct {
	ClientID     string
	TenantID     string
	ClientSecret string //nolint:gosec // G117: not a hardcoded credential, stores runtime secret from env vars
}

// GetServicePrincipalCredentials retrieves SP credentials from environment variables.
// Returns nil if credentials are not configured.
func GetServicePrincipalCredentials() *ServicePrincipalCredentials {
	clientID := os.Getenv(EnvAzureClientID)
	tenantID := os.Getenv(EnvAzureTenantID)
	clientSecret := os.Getenv(EnvAzureClientSecret)

	// All three are required for SP auth
	if clientID == "" || tenantID == "" || clientSecret == "" {
		return nil
	}

	return &ServicePrincipalCredentials{
		ClientID:     clientID,
		TenantID:     tenantID,
		ClientSecret: clientSecret,
	}
}

// HasServicePrincipalCredentials checks if SP credentials are configured.
func HasServicePrincipalCredentials() bool {
	return GetServicePrincipalCredentials() != nil
}

// servicePrincipalLogin validates SP credentials by acquiring a token.
// This is used by the login command to verify credentials work.
func (h *Handler) servicePrincipalLogin(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("starting service principal login", "handler", HandlerName)

	creds := GetServicePrincipalCredentials()
	if creds == nil {
		return nil, fmt.Errorf("service principal credentials not configured: set %s, %s, and %s environment variables",
			EnvAzureClientID, EnvAzureTenantID, EnvAzureClientSecret)
	}

	// Use a default scope if none provided
	scope := "https://graph.microsoft.com/.default"
	if len(opts.Scopes) > 0 {
		scope = opts.Scopes[0]
	}

	// Acquire a token to validate credentials
	token, err := h.acquireServicePrincipalToken(ctx, creds, scope)
	if err != nil {
		return nil, fmt.Errorf("service principal authentication failed: %w", err)
	}

	lgr.V(1).Info("service principal authentication successful",
		"clientId", creds.ClientID,
		"tenantId", creds.TenantID,
	)

	// Return a result with SP identity
	return &auth.Result{
		Claims: &auth.Claims{
			Subject:  creds.ClientID,
			TenantID: creds.TenantID,
			ClientID: creds.ClientID,
			// SPs don't have user-like claims (name, email)
		},
		ExpiresAt: token.ExpiresAt,
	}, nil
}

// acquireServicePrincipalToken acquires a token using the client credentials flow.
func (h *Handler) acquireServicePrincipalToken(ctx context.Context, creds *ServicePrincipalCredentials, scope string) (*auth.Token, error) {
	lgr := logger.FromContext(ctx)

	endpoint := fmt.Sprintf("%s/%s/oauth2/v2.0/token", h.config.GetAuthority(), creds.TenantID)

	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", creds.ClientID)
	data.Set("client_secret", creds.ClientSecret)
	data.Set("scope", scope)

	lgr.V(1).Info("requesting token via client credentials flow",
		"endpoint", endpoint,
		"scope", scope,
	)

	resp, err := h.httpClient.PostForm(ctx, endpoint, data)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		var errResp TokenErrorResponse
		_ = json.NewDecoder(resp.Body).Decode(&errResp)

		if errResp.Error == "invalid_client" {
			// Use the shared hint helper which knows about AADSTS7000215 (expired/wrong
			// secret) and AADSTS700016 (app not found) among others.
			hint := aadstsHint(errResp.ErrorDescription)
			if hint == "" {
				// Generic guidance: the secret is the most common cause.
				hint = fmt.Sprintf("verify %s contains the correct secret value (not the secret ID); "+
					"if the secret has been rotated or expired, regenerate it in the Azure portal",
					EnvAzureClientSecret)
			}
			return nil, fmt.Errorf("invalid client credentials: %s\nHint: %s", errResp.ErrorDescription, hint)
		}
		if errResp.Error == "unauthorized_client" {
			// Common cause: the service principal has not been granted the required
			// API permissions or an admin has not consented.
			return nil, fmt.Errorf("client not authorized: %s\nHint: ensure the app registration has the required API permissions "+
				"and that an administrator has granted consent in the Azure portal "+
				"(app registrations → API permissions → Grant admin consent)",
				errResp.ErrorDescription)
		}

		// Check for other known AADSTS codes (e.g. AADSTS700016 - app not found).
		if strings.Contains(errResp.ErrorDescription, "AADSTS") {
			return nil, formatAADSTSError("token request failed", errResp)
		}

		return nil, fmt.Errorf("token request failed: %s - %s", errResp.Error, errResp.ErrorDescription)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	lgr.V(1).Info("acquired service principal token",
		"expiresIn", tokenResp.ExpiresIn,
		"scope", scope,
	)

	return &auth.Token{
		AccessToken: tokenResp.AccessToken,
		TokenType:   tokenResp.TokenType,
		ExpiresAt:   expiresAt,
		Scope:       scope,
		Flow:        auth.FlowServicePrincipal,
	}, nil
}

// getServicePrincipalToken gets a token for SP auth, using cache when valid.
func (h *Handler) getServicePrincipalToken(ctx context.Context, opts auth.TokenOptions) (*auth.Token, error) {
	return getCachedOrAcquireToken(
		ctx,
		h,
		opts,
		GetServicePrincipalCredentials,
		func(creds *ServicePrincipalCredentials) bool { return creds == nil },
		h.acquireServicePrincipalToken,
		"SP",
	)
}

// servicePrincipalStatus returns the status for SP authentication.
func (h *Handler) servicePrincipalStatus(_ context.Context) (*auth.Status, error) {
	creds := GetServicePrincipalCredentials()
	if creds == nil {
		return &auth.Status{
			Authenticated: false,
		}, nil
	}

	// For SP, we're "authenticated" if credentials are configured
	// The actual token acquisition happens on demand
	return &auth.Status{
		Authenticated: true,
		Claims: &auth.Claims{
			Subject:  creds.ClientID,
			TenantID: creds.TenantID,
			ClientID: creds.ClientID,
			// Use ClientID as a display name for SP
			Name: fmt.Sprintf("Service Principal (%s)", creds.ClientID[:8]+"..."),
		},
		TenantID:     creds.TenantID,
		IdentityType: auth.IdentityTypeServicePrincipal,
		ClientID:     creds.ClientID,
		// ExpiresAt is not applicable for SP (credentials don't expire in the config)
	}, nil
}
