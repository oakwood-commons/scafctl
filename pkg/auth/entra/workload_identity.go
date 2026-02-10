// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package entra provides Microsoft Entra ID (Azure AD) authentication.
package entra

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/logger"
)

// Environment variable names for workload identity (Azure SDK convention).
const (
	// EnvAzureFederatedTokenFile is the path to the projected service account token.
	EnvAzureFederatedTokenFile = "AZURE_FEDERATED_TOKEN_FILE" //nolint:gosec // This is the env var name, not a credential

	// EnvAzureFederatedToken is the raw federated token (for testing/debugging).
	// Takes precedence over EnvAzureFederatedTokenFile if both are set.
	EnvAzureFederatedToken = "AZURE_FEDERATED_TOKEN" //nolint:gosec // This is the env var name, not a credential

	// EnvAzureAuthorityHost is the Azure AD authority host (optional).
	EnvAzureAuthorityHost = "AZURE_AUTHORITY_HOST"

	// clientAssertionType is the OAuth2 assertion type for federated tokens.
	clientAssertionType = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"
)

// WorkloadIdentityCredentials holds the configuration for workload identity authentication.
type WorkloadIdentityCredentials struct {
	ClientID  string
	TenantID  string
	TokenFile string // Path to token file (empty if using direct token)
	Token     string // Direct token value (takes precedence over TokenFile)
	Authority string
}

// GetWorkloadIdentityCredentials retrieves workload identity configuration from environment variables.
// Returns nil if workload identity is not configured.
// Priority: AZURE_FEDERATED_TOKEN (direct) > AZURE_FEDERATED_TOKEN_FILE (file path)
func GetWorkloadIdentityCredentials() *WorkloadIdentityCredentials {
	// Check for direct token first (useful for testing)
	directToken := os.Getenv(EnvAzureFederatedToken)
	tokenFile := os.Getenv(EnvAzureFederatedTokenFile)

	// Need either a direct token or a valid token file
	hasDirectToken := directToken != ""
	hasTokenFile := false
	if tokenFile != "" {
		if _, err := os.Stat(tokenFile); err == nil {
			hasTokenFile = true
		}
	}

	if !hasDirectToken && !hasTokenFile {
		return nil
	}

	clientID := os.Getenv(EnvAzureClientID)
	tenantID := os.Getenv(EnvAzureTenantID)

	// Client ID and Tenant ID are required
	if clientID == "" || tenantID == "" {
		return nil
	}

	authority := os.Getenv(EnvAzureAuthorityHost)
	if authority == "" {
		authority = "https://login.microsoftonline.com"
	}

	return &WorkloadIdentityCredentials{
		ClientID:  clientID,
		TenantID:  tenantID,
		TokenFile: tokenFile,
		Token:     directToken,
		Authority: authority,
	}
}

// HasWorkloadIdentityCredentials checks if workload identity is configured and available.
func HasWorkloadIdentityCredentials() bool {
	return GetWorkloadIdentityCredentials() != nil
}

// GetFederatedToken returns the federated token.
// If a direct token is set, it returns that. Otherwise, reads from the token file.
// The file is read fresh on each call as Kubernetes rotates the file.
func (c *WorkloadIdentityCredentials) GetFederatedToken() (string, error) {
	// Direct token takes precedence (useful for testing)
	if c.Token != "" {
		return c.Token, nil
	}

	// Read from file
	if c.TokenFile == "" {
		return "", fmt.Errorf("no federated token configured: set %s or %s", EnvAzureFederatedToken, EnvAzureFederatedTokenFile)
	}

	data, err := os.ReadFile(c.TokenFile)
	if err != nil {
		return "", fmt.Errorf("failed to read federated token file %s: %w", c.TokenFile, err)
	}

	token := string(data)
	if token == "" {
		return "", fmt.Errorf("federated token file is empty: %s", c.TokenFile)
	}

	return token, nil
}

// workloadIdentityLogin validates workload identity credentials by acquiring a token.
func (h *Handler) workloadIdentityLogin(ctx context.Context, _ auth.LoginOptions) (*auth.Result, error) {
	lgr := logger.FromContext(ctx)

	creds := GetWorkloadIdentityCredentials()
	if creds == nil {
		// Provide helpful error message
		tokenFile := os.Getenv(EnvAzureFederatedTokenFile)
		if tokenFile == "" {
			return nil, fmt.Errorf("workload identity not configured: %s environment variable not set", EnvAzureFederatedTokenFile)
		}
		if _, err := os.Stat(tokenFile); err != nil {
			return nil, fmt.Errorf("workload identity token file not found: %s\nHint: Ensure the pod has the azure-workload-identity webhook labels and the service account is properly configured", tokenFile)
		}
		if os.Getenv(EnvAzureClientID) == "" {
			return nil, fmt.Errorf("workload identity not configured: %s environment variable not set", EnvAzureClientID)
		}
		if os.Getenv(EnvAzureTenantID) == "" {
			return nil, fmt.Errorf("workload identity not configured: %s environment variable not set", EnvAzureTenantID)
		}
		return nil, fmt.Errorf("workload identity credentials not configured")
	}

	lgr.V(1).Info("validating workload identity credentials",
		"clientId", creds.ClientID,
		"tenantId", creds.TenantID,
		"tokenFile", creds.TokenFile,
	)

	// Validate by acquiring a token for Azure Resource Manager
	_, err := h.acquireWorkloadIdentityToken(ctx, creds, "https://management.azure.com/.default")
	if err != nil {
		return nil, fmt.Errorf("workload identity authentication failed: %w", err)
	}

	lgr.Info("workload identity authentication successful",
		"clientId", creds.ClientID,
		"tenantId", creds.TenantID,
	)

	return &auth.Result{
		Claims: &auth.Claims{
			Subject:  creds.ClientID,
			TenantID: creds.TenantID,
			ClientID: creds.ClientID,
			Name:     fmt.Sprintf("Workload Identity (%s...)", creds.ClientID[:8]),
		},
		ExpiresAt: time.Time{}, // Workload identity doesn't have credential expiry
	}, nil
}

// acquireWorkloadIdentityToken exchanges the federated token for an Azure AD access token.
func (h *Handler) acquireWorkloadIdentityToken(ctx context.Context, creds *WorkloadIdentityCredentials, scope string) (*auth.Token, error) {
	lgr := logger.FromContext(ctx)

	// Read the federated token fresh (Kubernetes may have rotated it)
	federatedToken, err := creds.GetFederatedToken()
	if err != nil {
		return nil, err
	}

	lgr.V(1).Info("exchanging federated token for access token",
		"scope", scope,
		"tokenFile", creds.TokenFile,
	)

	// Build token endpoint URL
	tokenURL := fmt.Sprintf("%s/%s/oauth2/v2.0/token", creds.Authority, creds.TenantID)

	// Build request body for client_credentials with client_assertion
	data := url.Values{
		"grant_type":            {"client_credentials"},
		"client_id":             {creds.ClientID},
		"client_assertion_type": {clientAssertionType},
		"client_assertion":      {federatedToken},
		"scope":                 {scope},
	}

	// Make the token request
	resp, err := h.httpClient.PostForm(ctx, tokenURL, data)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check for error response
	if resp.StatusCode != http.StatusOK {
		var errResp TokenErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			return nil, fmt.Errorf("token request failed with status %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("token exchange failed: %s: %s", errResp.Error, errResp.ErrorDescription)
	}

	// Parse success response
	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token")
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	lgr.V(1).Info("successfully acquired access token via workload identity",
		"scope", scope,
		"expiresIn", tokenResp.ExpiresIn,
		"expiresAt", expiresAt,
	)

	return &auth.Token{
		AccessToken: tokenResp.AccessToken,
		TokenType:   tokenResp.TokenType,
		ExpiresAt:   expiresAt,
		Scope:       scope,
	}, nil
}

// getWorkloadIdentityToken gets an access token using workload identity, with caching.
func (h *Handler) getWorkloadIdentityToken(ctx context.Context, opts auth.TokenOptions) (*auth.Token, error) {
	return getCachedOrAcquireToken(
		ctx,
		h,
		opts,
		GetWorkloadIdentityCredentials,
		func(creds *WorkloadIdentityCredentials) bool { return creds == nil },
		h.acquireWorkloadIdentityToken,
		"workload identity",
	)
}

// workloadIdentityStatus returns the status for workload identity authentication.
func (h *Handler) workloadIdentityStatus(_ context.Context) (*auth.Status, error) {
	creds := GetWorkloadIdentityCredentials()
	if creds == nil {
		return &auth.Status{
			Authenticated: false,
		}, nil
	}

	// Determine token source for display
	tokenFile := creds.TokenFile
	if creds.Token != "" {
		tokenFile = "(direct token)" // Indicate token was provided directly
	}

	// For workload identity, we're "authenticated" if credentials are configured
	// The actual token acquisition happens on demand
	return &auth.Status{
		Authenticated: true,
		Claims: &auth.Claims{
			Subject:  creds.ClientID,
			TenantID: creds.TenantID,
			ClientID: creds.ClientID,
			Name:     fmt.Sprintf("Workload Identity (%s...)", creds.ClientID[:8]),
		},
		TenantID:     creds.TenantID,
		IdentityType: auth.IdentityTypeWorkloadIdentity,
		ClientID:     creds.ClientID,
		TokenFile:    tokenFile,
	}, nil
}
