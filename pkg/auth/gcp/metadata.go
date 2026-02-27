// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package gcp provides Google Cloud Platform authentication for scafctl.
// This file implements the GCE metadata server token acquisition flow.
package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/logger"
)

const (
	// EnvGCEMetadataHost allows overriding the metadata server host for testing.
	EnvGCEMetadataHost = "GCE_METADATA_HOST"

	// defaultMetadataHost is the default GCE metadata server host.
	defaultMetadataHost = "metadata.google.internal"

	// metadataFlavorHeader is the required header for metadata server requests.
	metadataFlavorHeader = "Metadata-Flavor"

	// metadataFlavorValue is the required value for the Metadata-Flavor header.
	metadataFlavorValue = "Google"
)

// MetadataTokenResponse represents the token response from the metadata server.
type MetadataTokenResponse struct {
	AccessToken string `json:"access_token"` //nolint:gosec // G117: not a hardcoded credential, stores runtime token data
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

// getMetadataHost returns the metadata server host, respecting env var override.
func getMetadataHost() string {
	if host := os.Getenv(EnvGCEMetadataHost); host != "" {
		return host
	}
	return defaultMetadataHost
}

// getMetadataTokenURL returns the full URL for the metadata server token endpoint.
func getMetadataTokenURL() string {
	return fmt.Sprintf("http://%s/computeMetadata/v1/instance/service-accounts/default/token", getMetadataHost())
}

// getMetadataEmailURL returns the full URL for the metadata server email endpoint.
func getMetadataEmailURL() string {
	return fmt.Sprintf("http://%s/computeMetadata/v1/instance/service-accounts/default/email", getMetadataHost())
}

// IsMetadataServerAvailable checks if the GCE metadata server is reachable.
// Uses a short timeout for probing.
func IsMetadataServerAvailable(ctx context.Context, httpClient HTTPClient) bool {
	// Try to reach the metadata server with a short timeout
	headers := map[string]string{
		metadataFlavorHeader: metadataFlavorValue,
	}
	resp, err := httpClient.Get(ctx, getMetadataTokenURL(), headers)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

// metadataLogin validates metadata server access by acquiring a token.
func (h *Handler) metadataLogin(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("starting metadata server login", "handler", HandlerName)

	// Acquire a token to validate access
	token, err := h.acquireMetadataToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("metadata server authentication failed: %w", err)
	}

	// Try to get the service account email
	claims := &auth.Claims{
		Issuer: "https://accounts.google.com",
	}

	email, emailErr := h.fetchMetadataEmail(ctx)
	if emailErr == nil && email != "" {
		claims.Subject = email
		claims.Email = email
	}

	// Store metadata
	if err := h.storeMetadataOnly(ctx, claims, auth.FlowMetadata, opts.Scopes); err != nil {
		lgr.V(1).Info("warning: failed to store metadata", "error", err)
	}

	lgr.V(1).Info("metadata server authentication successful", "email", email)

	return &auth.Result{
		Claims:    claims,
		ExpiresAt: token.ExpiresAt,
	}, nil
}

// acquireMetadataToken acquires a token from the GCE metadata server.
func (h *Handler) acquireMetadataToken(ctx context.Context) (*auth.Token, error) {
	lgr := logger.FromContext(ctx)

	tokenURL := getMetadataTokenURL()
	headers := map[string]string{
		metadataFlavorHeader: metadataFlavorValue,
	}

	lgr.V(1).Info("requesting token from metadata server", "url", tokenURL)

	resp, err := h.httpClient.Get(ctx, tokenURL, headers)
	if err != nil {
		return nil, fmt.Errorf("metadata server request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("metadata server not available: not running on Google Cloud? (status %d)", resp.StatusCode)
	}

	var tokenResp MetadataTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse metadata token response: %w", err)
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	lgr.V(1).Info("acquired metadata server token",
		"expiresIn", tokenResp.ExpiresIn,
	)

	return &auth.Token{
		AccessToken: tokenResp.AccessToken,
		TokenType:   tokenResp.TokenType,
		ExpiresAt:   expiresAt,
		Scope:       "https://www.googleapis.com/auth/cloud-platform",
		Flow:        auth.FlowMetadata,
	}, nil
}

// fetchMetadataEmail fetches the service account email from the metadata server.
func (h *Handler) fetchMetadataEmail(ctx context.Context) (string, error) {
	emailURL := getMetadataEmailURL()
	headers := map[string]string{
		metadataFlavorHeader: metadataFlavorValue,
	}

	resp, err := h.httpClient.Get(ctx, emailURL, headers)
	if err != nil {
		return "", fmt.Errorf("metadata email request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("metadata email request failed with status %d", resp.StatusCode)
	}

	var email []byte
	buf := make([]byte, 256)
	n, _ := resp.Body.Read(buf)
	email = buf[:n]

	return string(email), nil
}

// getMetadataToken gets a token from metadata server, with caching.
func (h *Handler) getMetadataToken(ctx context.Context, opts auth.TokenOptions) (*auth.Token, error) {
	if opts.Scope == "" {
		return nil, auth.ErrInvalidScope
	}

	lgr := logger.FromContext(ctx)

	minValidFor := opts.MinValidFor
	if minValidFor == 0 {
		minValidFor = auth.DefaultMinValidFor
	}

	// Check cache first
	if !opts.ForceRefresh {
		cached, err := h.tokenCache.Get(ctx, auth.FlowMetadata, auth.FingerprintHash(""), opts.Scope)
		if err == nil && cached != nil && cached.IsValidFor(minValidFor) {
			lgr.V(1).Info("using cached metadata token", "scope", opts.Scope)
			return cached, nil
		}
	}

	// Acquire new token
	token, err := h.acquireMetadataToken(ctx)
	if err != nil {
		return nil, err
	}

	// Cache the token
	if err := h.tokenCache.Set(ctx, auth.FlowMetadata, auth.FingerprintHash(""), opts.Scope, token); err != nil {
		lgr.V(1).Info("failed to cache metadata token", "error", err)
	}

	return token, nil
}
