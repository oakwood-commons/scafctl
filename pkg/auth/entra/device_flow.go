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

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/logger"
)

// DeviceCodeResponse represents the response from the device code endpoint.
type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
	Message         string `json:"message"`
}

// TokenResponse represents the response from the token endpoint.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	IDToken      string `json:"id_token,omitempty"`
}

// TokenErrorResponse represents an error from the token endpoint.
type TokenErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// deviceCodeLogin performs the device code authentication flow.
func (h *Handler) deviceCodeLogin(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("starting device code authentication flow")

	// Determine tenant
	tenantID := opts.TenantID
	if tenantID == "" {
		tenantID = h.config.TenantID
	}

	// Determine scopes
	scopes := opts.Scopes
	if len(scopes) == 0 {
		scopes = h.config.DefaultScopes
	}

	// Ensure offline_access is included for refresh token
	hasOfflineAccess := false
	for _, s := range scopes {
		if s == "offline_access" {
			hasOfflineAccess = true
			break
		}
	}
	if !hasOfflineAccess {
		scopes = append(scopes, "offline_access")
	}

	// Determine timeout
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Step 1: Request device code
	deviceCode, err := h.requestDeviceCode(ctx, tenantID, scopes)
	if err != nil {
		return nil, auth.NewError(HandlerName, "device_code_request", err)
	}

	lgr.V(1).Info("device code obtained",
		"userCode", deviceCode.UserCode,
		"verificationURI", deviceCode.VerificationURI,
	)

	// Step 2: Notify callback with device code info
	if opts.DeviceCodeCallback != nil {
		opts.DeviceCodeCallback(deviceCode.UserCode, deviceCode.VerificationURI, deviceCode.Message)
	}

	// Step 3: Poll for token
	tokenResp, err := h.pollForToken(ctx, tenantID, deviceCode)
	if err != nil {
		return nil, auth.NewError(HandlerName, "token_poll", err)
	}

	// Step 4: Store refresh token securely
	if err := h.storeCredentials(ctx, tenantID, tokenResp); err != nil {
		return nil, auth.NewError(HandlerName, "store_credentials", err)
	}

	// Step 5: Extract and return claims
	claims, err := h.extractClaims(tokenResp)
	if err != nil {
		return nil, auth.NewError(HandlerName, "extract_claims", err)
	}

	lgr.V(1).Info("authentication successful",
		"subject", claims.Subject,
		"tenantId", claims.TenantID,
	)

	return &auth.Result{
		Claims:    claims,
		ExpiresAt: time.Now().Add(DefaultRefreshTokenLifetime),
	}, nil
}

func (h *Handler) requestDeviceCode(ctx context.Context, tenantID string, scopes []string) (*DeviceCodeResponse, error) {
	endpoint := fmt.Sprintf("%s/%s/oauth2/v2.0/devicecode", h.config.GetAuthority(), tenantID)

	data := url.Values{}
	data.Set("client_id", h.config.ClientID)
	data.Set("scope", strings.Join(scopes, " "))

	resp, err := h.httpClient.PostForm(ctx, endpoint, data)
	if err != nil {
		return nil, fmt.Errorf("device code request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp TokenErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			return nil, fmt.Errorf("device code request failed with status %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("device code request failed: %s - %s", errResp.Error, errResp.ErrorDescription)
	}

	var deviceCode DeviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&deviceCode); err != nil {
		return nil, fmt.Errorf("failed to parse device code response: %w", err)
	}

	return &deviceCode, nil
}

func (h *Handler) pollForToken(ctx context.Context, tenantID string, deviceCode *DeviceCodeResponse) (*TokenResponse, error) {
	lgr := logger.FromContext(ctx)
	endpoint := fmt.Sprintf("%s/%s/oauth2/v2.0/token", h.config.GetAuthority(), tenantID)

	minPollInterval := h.config.MinPollInterval
	if minPollInterval == 0 {
		minPollInterval = DefaultMinPollInterval
	}

	interval := time.Duration(deviceCode.Interval) * time.Second
	if interval < minPollInterval {
		interval = minPollInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, auth.ErrTimeout
		case <-ticker.C:
			data := url.Values{}
			data.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
			data.Set("client_id", h.config.ClientID)
			data.Set("device_code", deviceCode.DeviceCode)

			resp, err := h.httpClient.PostForm(ctx, endpoint, data)
			if err != nil {
				// Network error, log and continue polling
				lgr.V(1).Info("transient network error during token poll, continuing", "error", err)
				continue
			}

			if resp.StatusCode == http.StatusOK {
				var tokenResp TokenResponse
				if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
					resp.Body.Close()
					return nil, fmt.Errorf("failed to parse token response: %w", err)
				}
				resp.Body.Close()
				return &tokenResp, nil
			}

			var errResp TokenErrorResponse
			_ = json.NewDecoder(resp.Body).Decode(&errResp)
			resp.Body.Close()

			switch errResp.Error {
			case "authorization_pending":
				// User hasn't completed authentication yet, continue polling
				continue
			case "slow_down":
				// Increase polling interval
				slowDownIncr := h.config.SlowDownIncrement
				if slowDownIncr == 0 {
					slowDownIncr = 5 * time.Second
				}
				interval += slowDownIncr
				ticker.Reset(interval)
				lgr.V(1).Info("slow_down received, increasing poll interval", "newInterval", interval)
				continue
			case "expired_token":
				return nil, auth.ErrTimeout
			case "authorization_declined":
				return nil, auth.ErrUserCancelled
			default:
				return nil, fmt.Errorf("token request failed: %s - %s", errResp.Error, errResp.ErrorDescription)
			}
		}
	}
}
