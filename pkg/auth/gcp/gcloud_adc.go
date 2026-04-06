// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package gcp provides Google Cloud Platform authentication for scafctl.
// This file implements the gcloud ADC (Application Default Credentials) fallback
// for users who have already run `gcloud auth application-default login`.
package gcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

const (
	// EnvCloudSDKConfig is the environment variable for custom gcloud config directory.
	EnvCloudSDKConfig = "CLOUDSDK_CONFIG"
)

// ErrNoGcloudADCConfigured is returned by LoadGcloudADCCredentials when no
// gcloud Application Default Credentials are configured.
var ErrNoGcloudADCConfigured = errors.New("no gcloud ADC credentials configured")

// formatGcloudTokenError converts a raw OAuth error response into a clear, actionable error.
// It handles well-known error codes (e.g. invalid_grant / invalid_rapt) with remediation hints.
func formatGcloudTokenError(errResp TokenErrorResponse, binaryName string) error {
	if errResp.Error == "invalid_grant" {
		if strings.Contains(errResp.ErrorDescription, "invalid_rapt") {
			return fmt.Errorf(
				"gcloud ADC credentials require re-authentication (invalid_rapt): "+
					"a security policy requires you to reauthenticate. "+
					"Run: %s auth login gcp",
				binaryName,
			)
		}
		return fmt.Errorf(
			"gcloud ADC credentials have expired or been revoked (%s). "+
				"Run: %s auth login gcp",
			errResp.ErrorDescription,
			binaryName,
		)
	}
	return fmt.Errorf("gcloud ADC token refresh failed: %s - %s", errResp.Error, errResp.ErrorDescription)
}

// GcloudADCCredentials represents the structure of the gcloud ADC JSON file.
type GcloudADCCredentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"` //nolint:gosec // This is a field name, not a credential
	RefreshToken string `json:"refresh_token"` //nolint:gosec // This is a field name, not a credential
	Type         string `json:"type"`
}

// getGcloudADCPath returns the path to the gcloud ADC credentials file.
func getGcloudADCPath() string {
	// Check CLOUDSDK_CONFIG first
	if dir := os.Getenv(EnvCloudSDKConfig); dir != "" {
		return filepath.Join(dir, "application_default_credentials.json")
	}

	// Platform-specific defaults
	switch runtime.GOOS {
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "gcloud", "application_default_credentials.json")
		}
	default:
		// Linux and macOS
		if home, err := paths.HomeDir(); err == nil {
			return filepath.Join(home, ".config", "gcloud", "application_default_credentials.json")
		}
	}

	return ""
}

// LoadGcloudADCCredentials loads gcloud ADC credentials from the well-known location.
func LoadGcloudADCCredentials() (*GcloudADCCredentials, error) {
	path := getGcloudADCPath()
	if path == "" {
		return nil, ErrNoGcloudADCConfigured
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoGcloudADCConfigured
		}
		return nil, fmt.Errorf("reading gcloud ADC file: %w", err)
	}

	var creds GcloudADCCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parsing gcloud ADC file: %w", err)
	}

	if creds.Type != "authorized_user" {
		return nil, ErrNoGcloudADCConfigured
	}

	if creds.RefreshToken == "" {
		return nil, ErrNoGcloudADCConfigured
	}

	return &creds, nil
}

// HasGcloudADCCredentials checks if gcloud ADC credentials exist.
func HasGcloudADCCredentials() bool {
	_, err := LoadGcloudADCCredentials()
	return err == nil
}

// gcloudADCLogin uses existing gcloud ADC credentials to authenticate.
func (h *Handler) gcloudADCLogin(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("attempting gcloud ADC fallback login")

	creds, err := LoadGcloudADCCredentials()
	if err != nil {
		if errors.Is(err, ErrNoGcloudADCConfigured) {
			return nil, fmt.Errorf("no gcloud ADC credentials found; run 'gcloud auth application-default login' or configure a client ID for %s", settings.BinaryNameFromContext(ctx))
		}
		return nil, fmt.Errorf("loading gcloud ADC credentials: %w", err)
	}

	// Use gcloud's refresh token to get an access token
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", creds.ClientID)
	data.Set("client_secret", creds.ClientSecret)
	data.Set("refresh_token", creds.RefreshToken)

	resp, err := h.httpClient.PostForm(ctx, tokenEndpoint, data)
	if err != nil {
		return nil, fmt.Errorf("gcloud ADC token refresh failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		var errResp TokenErrorResponse
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		return nil, formatGcloudTokenError(errResp, settings.BinaryNameFromContext(ctx))
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	// Extract claims from ID token or userinfo
	var claims *auth.Claims
	claims, err = extractClaims(&tokenResp)
	if err != nil || claims.Email == "" {
		// Try userinfo endpoint
		if tokenResp.AccessToken != "" {
			claims, err = h.fetchUserinfoClaims(ctx, tokenResp.AccessToken)
			if err != nil {
				claims = &auth.Claims{Issuer: "https://accounts.google.com"}
			}
		}
	}

	// Store metadata (but NOT the refresh token — we leave that in gcloud's file)
	if err := h.storeMetadataOnly(ctx, claims, auth.FlowInteractive, opts.Scopes); err != nil {
		lgr.V(1).Info("warning: failed to store metadata", "error", err)
	}

	// Cache the access token
	scopeStr := tokenResp.Scope
	if scopeStr == "" {
		scopeStr = "https://www.googleapis.com/auth/cloud-platform"
	}
	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	_ = h.tokenCache.Set(ctx, auth.FlowGcloudADC, auth.FingerprintHash(""), scopeStr, &auth.Token{
		AccessToken: tokenResp.AccessToken,
		TokenType:   tokenResp.TokenType,
		ExpiresAt:   expiresAt,
		Scope:       scopeStr,
	})

	lgr.V(1).Info("gcloud ADC fallback login successful", "email", claims.Email)

	return &auth.Result{
		Claims:    claims,
		ExpiresAt: expiresAt,
	}, nil
}

// getGcloudADCToken refreshes a token using gcloud ADC credentials, with caching.
func (h *Handler) getGcloudADCToken(ctx context.Context, opts auth.TokenOptions) (*auth.Token, error) {
	lgr := logger.FromContext(ctx)

	if opts.Scope == "" {
		return nil, auth.ErrInvalidScope
	}

	creds, err := LoadGcloudADCCredentials()
	if err != nil {
		return nil, auth.ErrNotAuthenticated
	}

	fingerprint := auth.FingerprintHash(creds.ClientID)

	minValidFor := opts.MinValidFor
	if minValidFor == 0 {
		minValidFor = auth.DefaultMinValidFor
	}

	// Check cache first
	if !opts.ForceRefresh {
		cached, err := h.tokenCache.Get(ctx, auth.FlowGcloudADC, fingerprint, opts.Scope)
		if err == nil && cached != nil && cached.IsValidFor(minValidFor) {
			lgr.V(1).Info("using cached gcloud ADC token", "scope", opts.Scope)
			return cached, nil
		}
	}

	// Refresh token using gcloud's credentials
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", creds.ClientID)
	data.Set("client_secret", creds.ClientSecret)
	data.Set("refresh_token", creds.RefreshToken)

	resp, err := h.httpClient.PostForm(ctx, tokenEndpoint, data)
	if err != nil {
		return nil, fmt.Errorf("gcloud ADC token refresh failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		var errResp TokenErrorResponse
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		return nil, formatGcloudTokenError(errResp, settings.BinaryNameFromContext(ctx))
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	token := &auth.Token{
		AccessToken: tokenResp.AccessToken,
		TokenType:   tokenResp.TokenType,
		ExpiresAt:   expiresAt,
		Scope:       opts.Scope,
		Flow:        auth.FlowGcloudADC,
	}

	binaryName := settings.BinaryNameFromContext(ctx)
	lgr.V(1).Info("acquired token via gcloud ADC fallback",
		"hint", fmt.Sprintf("to use %s-managed credentials run: %s auth login gcp", binaryName, binaryName))
	if err := h.tokenCache.Set(ctx, auth.FlowGcloudADC, fingerprint, opts.Scope, token); err != nil {
		lgr.V(1).Info("failed to cache gcloud ADC token", "error", err)
	}

	return token, nil
}
