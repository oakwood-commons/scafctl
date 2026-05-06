// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/logger"
)

const (
	// deviceCodeEndpoint is the Google OAuth 2.0 device authorization endpoint.
	deviceCodeEndpoint = "https://oauth2.googleapis.com/device/code"

	// defaultDeviceCodePollInterval is the minimum polling interval for device code flow.
	defaultDeviceCodePollInterval = 5 * time.Second

	// slowDownIncrement is the interval increase when the server returns "slow_down".
	slowDownIncrement = 5 * time.Second
)

// DeviceCodeResponse represents the response from Google's device authorization endpoint.
type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// deviceCodeLogin performs the OAuth 2.0 device code authentication flow.
// This is useful in headless/SSH environments where a browser cannot be opened locally.
func (h *Handler) deviceCodeLogin(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("starting GCP device code authentication flow", "handler", HandlerName)

	// Use configured client ID, or fall back to Google's well-known ADC client credentials
	clientID := h.config.ClientID
	clientSecret := h.config.ClientSecret
	if clientID == "" {
		lgr.V(1).Info("no client ID configured, using Google's default ADC client credentials")
		clientID = DefaultADCClientID
		clientSecret = DefaultADCClientSecret
	}

	// Determine scopes
	scopes := opts.Scopes
	if len(scopes) == 0 {
		scopes = h.config.DefaultScopes
	}
	scopeStr := strings.Join(scopes, " ")

	// Determine timeout
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Step 1: Request device code from Google
	isDefaultClient := clientID == DefaultADCClientID
	deviceCode, err := h.requestGCPDeviceCode(ctx, clientID, scopes)
	if err != nil {
		if isDefaultClient {
			return nil, fmt.Errorf("device code request failed with default ADC client (configure auth.gcp.client_id and auth.gcp.client_secret for device code support): %w", err)
		}
		return nil, fmt.Errorf("device code request failed: %w", err)
	}

	lgr.V(1).Info("device code obtained",
		"userCode", deviceCode.UserCode,
		"verificationURL", deviceCode.VerificationURL,
	)

	// Step 2: Notify callback with device code info for CLI display
	if opts.DeviceCodeCallback != nil {
		opts.DeviceCodeCallback(deviceCode.UserCode, deviceCode.VerificationURL,
			"Enter this code to authenticate with Google Cloud")
	}

	// Step 3: Poll for token
	tokenResp, err := h.pollGCPDeviceToken(ctx, deviceCode, clientID, clientSecret)
	if err != nil {
		return nil, fmt.Errorf("device code polling failed: %w", err)
	}

	// Step 4: Store credentials
	if err := h.storeCredentials(ctx, tokenResp, auth.FlowDeviceCode, scopes, ""); err != nil {
		return nil, fmt.Errorf("failed to store credentials: %w", err)
	}

	// Cache the access token
	if tokenResp.AccessToken != "" {
		expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		if cacheErr := h.tokenCache.Set(ctx, auth.FlowDeviceCode, auth.FingerprintHash(clientID), scopeStr, &auth.Token{
			AccessToken: tokenResp.AccessToken,
			TokenType:   tokenResp.TokenType,
			ExpiresAt:   expiresAt,
			Scope:       scopeStr,
		}); cacheErr != nil {
			lgr.V(1).Info("failed to cache access token", "error", cacheErr)
		}
	}

	// Extract claims
	claims, err := extractClaims(tokenResp)
	if err != nil {
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

	lgr.V(1).Info("GCP device code flow completed successfully",
		"email", claims.Email,
		"expiresIn", tokenResp.ExpiresIn,
	)

	return &auth.Result{
		Claims:    claims,
		ExpiresAt: expiresAt,
	}, nil
}

// requestGCPDeviceCode requests a device code from Google's device authorization endpoint.
func (h *Handler) requestGCPDeviceCode(ctx context.Context, clientID string, scopes []string) (*DeviceCodeResponse, error) {
	data := url.Values{}
	data.Set("client_id", clientID)
	data.Set("scope", strings.Join(scopes, " "))

	resp, err := h.httpClient.PostForm(ctx, deviceCodeEndpoint, data)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		var errResp TokenErrorResponse
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		return nil, fmt.Errorf("device code request failed (%d): %s - %s",
			resp.StatusCode, errResp.Error, errResp.ErrorDescription)
	}

	var deviceCode DeviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&deviceCode); err != nil {
		return nil, fmt.Errorf("failed to parse device code response: %w", err)
	}

	return &deviceCode, nil
}

// pollGCPDeviceToken polls Google's token endpoint until the user completes
// authentication or the device code expires.
func (h *Handler) pollGCPDeviceToken(ctx context.Context, deviceCode *DeviceCodeResponse, clientID, clientSecret string) (*TokenResponse, error) {
	lgr := logger.FromContext(ctx)

	interval := time.Duration(deviceCode.Interval) * time.Second
	if interval < defaultDeviceCodePollInterval {
		interval = defaultDeviceCodePollInterval
	}

	ticker := h.clock.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("authentication timed out or cancelled: %w", auth.ErrTimeout)
		case <-ticker.C():
			data := url.Values{}
			data.Set("client_id", clientID)
			if clientSecret != "" {
				data.Set("client_secret", clientSecret)
			}
			data.Set("device_code", deviceCode.DeviceCode)
			data.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

			resp, err := h.httpClient.PostForm(ctx, tokenEndpoint, data)
			if err != nil {
				lgr.V(1).Info("transient network error during device code poll, continuing", "error", err)
				continue
			}

			var body struct {
				TokenResponse
				TokenErrorResponse
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				resp.Body.Close()
				return nil, fmt.Errorf("failed to parse token response: %w", err)
			}
			resp.Body.Close()

			// Success: access_token present
			if body.AccessToken != "" {
				return &TokenResponse{
					AccessToken:  body.AccessToken,
					RefreshToken: body.RefreshToken,
					TokenType:    body.TokenType,
					ExpiresIn:    body.ExpiresIn,
					Scope:        body.Scope,
					IDToken:      body.IDToken,
				}, nil
			}

			// Handle polling errors per RFC 8628
			switch body.Error {
			case "authorization_pending":
				continue
			case "slow_down":
				interval += slowDownIncrement
				ticker.Reset(interval)
				lgr.V(1).Info("slow_down received, increasing poll interval", "newInterval", interval)
				continue
			case "expired_token":
				return nil, fmt.Errorf("device code expired, please try again: %w", auth.ErrTimeout)
			case "access_denied":
				return nil, fmt.Errorf("user denied access: %w", auth.ErrUserCancelled)
			default:
				return nil, fmt.Errorf("token request failed: %s - %s", body.Error, body.ErrorDescription)
			}
		}
	}
}
