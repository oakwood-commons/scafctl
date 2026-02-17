// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/logger"
)

// DeviceCodeResponse represents the response from GitHub's device code endpoint.
type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// deviceCodeLogin performs the device code authentication flow.
func (h *Handler) deviceCodeLogin(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("starting GitHub device code authentication flow")

	// Determine scopes
	scopes := opts.Scopes
	if len(scopes) == 0 {
		scopes = h.config.DefaultScopes
	}

	// Determine timeout
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return h.performDeviceCodeFlow(ctx, opts, scopes)
}

// performDeviceCodeFlow executes the device code authentication flow.
func (h *Handler) performDeviceCodeFlow(ctx context.Context, opts auth.LoginOptions, scopes []string) (*auth.Result, error) {
	lgr := logger.FromContext(ctx)

	// Step 1: Request device code
	deviceCode, err := h.requestDeviceCode(ctx, scopes)
	if err != nil {
		return nil, auth.NewError(HandlerName, "device_code_request", err)
	}

	lgr.V(1).Info("device code obtained",
		"userCode", deviceCode.UserCode,
		"verificationURI", deviceCode.VerificationURI,
	)

	// Step 2: Notify callback with device code info
	if opts.DeviceCodeCallback != nil {
		opts.DeviceCodeCallback(deviceCode.UserCode, deviceCode.VerificationURI, "")
	}

	// Step 3: Poll for token
	tokenResp, err := h.pollForToken(ctx, deviceCode)
	if err != nil {
		return nil, auth.NewError(HandlerName, "token_poll", err)
	}

	// Step 4: Store credentials securely and fetch claims
	claims, err := h.storeCredentials(ctx, tokenResp, scopes)
	if err != nil {
		return nil, auth.NewError(HandlerName, "store_credentials", err)
	}

	lgr.V(1).Info("authentication successful",
		"subject", claims.Subject,
		"name", claims.Name,
	)

	// Determine expiry
	expiresAt := time.Now().Add(8 * time.Hour) // Default for non-expiring tokens
	if tokenResp.RefreshTokenExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(tokenResp.RefreshTokenExpiresIn) * time.Second)
	}

	return &auth.Result{
		Claims:    claims,
		ExpiresAt: expiresAt,
	}, nil
}

// requestDeviceCode requests a device code from GitHub.
func (h *Handler) requestDeviceCode(ctx context.Context, scopes []string) (*DeviceCodeResponse, error) {
	endpoint := fmt.Sprintf("%s/login/device/code", h.config.GetOAuthBaseURL())

	data := makeFormData(map[string]string{
		"client_id": h.config.ClientID,
		"scope":     strings.Join(scopes, " "),
	})

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

// pollForToken polls GitHub's token endpoint until the user completes authentication.
func (h *Handler) pollForToken(ctx context.Context, deviceCode *DeviceCodeResponse) (*TokenResponse, error) {
	lgr := logger.FromContext(ctx)
	endpoint := fmt.Sprintf("%s/login/oauth/access_token", h.config.GetOAuthBaseURL())

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
			data := makeFormData(map[string]string{
				"client_id":   h.config.ClientID,
				"device_code": deviceCode.DeviceCode,
				"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
			})

			resp, err := h.httpClient.PostForm(ctx, endpoint, data)
			if err != nil {
				// Network error, log and continue polling
				lgr.V(1).Info("transient network error during token poll, continuing", "error", err)
				continue
			}

			// GitHub returns 200 even for errors, and includes the error in JSON body
			body := struct {
				TokenResponse
				TokenErrorResponse
			}{}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				resp.Body.Close()
				return nil, fmt.Errorf("failed to parse token response: %w", err)
			}
			resp.Body.Close()

			// Check for success (access_token present)
			if body.AccessToken != "" {
				return &TokenResponse{
					AccessToken:           body.AccessToken,
					RefreshToken:          body.RefreshToken,
					TokenType:             body.TokenType,
					Scope:                 body.Scope,
					ExpiresIn:             body.ExpiresIn,
					RefreshTokenExpiresIn: body.RefreshTokenExpiresIn,
				}, nil
			}

			// Handle errors
			switch body.Error {
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
			case "access_denied":
				return nil, auth.ErrUserCancelled
			default:
				return nil, fmt.Errorf("token request failed: %s - %s", body.Error, body.ErrorDescription)
			}
		}
	}
}
