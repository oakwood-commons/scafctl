// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package gcp provides Google Cloud Platform authentication for scafctl.
// This file implements service account impersonation via the IAM Credentials API.
package gcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

const (
	// iamCredentialsEndpoint is the base URL for the IAM Credentials API.
	iamCredentialsEndpoint = "https://iamcredentials.googleapis.com/v1" //nolint:gosec // G101: not a hardcoded credential
)

// ImpersonationRequest is the request body for generateAccessToken.
type ImpersonationRequest struct {
	Scope    []string `json:"scope"`
	Lifetime string   `json:"lifetime"`
}

// ImpersonationResponse is the response from generateAccessToken.
type ImpersonationResponse struct {
	AccessToken string `json:"accessToken"` //nolint:gosec // G117: not a hardcoded credential
	ExpireTime  string `json:"expireTime"`
}

// impersonateServiceAccount acquires an access token by impersonating a service account.
func (h *Handler) impersonateServiceAccount(ctx context.Context, sourceToken, targetSA string, scopes []string) (*auth.Token, error) {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("impersonating service account",
		"targetServiceAccount", targetSA,
		"scopes", scopes,
	)

	endpoint := fmt.Sprintf("%s/projects/-/serviceAccounts/%s:generateAccessToken",
		iamCredentialsEndpoint, targetSA)

	reqBody := ImpersonationRequest{
		Scope:    scopes,
		Lifetime: "3600s",
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("encoding impersonation request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating impersonation request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+sourceToken)

	resp, err := h.httpClient.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("impersonation request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("impersonation denied: ensure source identity has roles/iam.serviceAccountTokenCreator on %s", targetSA)
	}

	if resp.StatusCode != http.StatusOK {
		var errBody map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		// Extract the message field if the response follows the Google API error format
		msg := ""
		if errObj, ok := errBody["error"].(map[string]any); ok {
			if s, ok := errObj["message"].(string); ok {
				msg = s
			}
		}
		if msg == "" {
			msg = fmt.Sprintf("%v", errBody)
		}
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf(
				"impersonation failed: source identity is not authenticated (401): %s. "+
					"Re-authenticate with: %s auth login gcp",
				msg, settings.BinaryNameFromContext(ctx))
		}
		return nil, fmt.Errorf("impersonation failed (status %d): %s", resp.StatusCode, msg)
	}

	var impResp ImpersonationResponse
	if err := json.NewDecoder(resp.Body).Decode(&impResp); err != nil {
		return nil, fmt.Errorf("parsing impersonation response: %w", err)
	}

	// Parse the expiry time
	expireTime, err := time.Parse(time.RFC3339, impResp.ExpireTime)
	if err != nil {
		// Fallback: assume 1 hour
		expireTime = time.Now().Add(time.Hour)
	}

	lgr.V(1).Info("impersonation successful",
		"targetServiceAccount", targetSA,
		"expiresAt", expireTime,
	)

	return &auth.Token{
		AccessToken: impResp.AccessToken,
		TokenType:   "Bearer",
		ExpiresAt:   expireTime,
		Scope:       strings.Join(scopes, " "),
	}, nil
}

// getImpersonatedToken acquires a token for the impersonated service account.
// It first acquires a source token using the configured flow, then impersonates.
func (h *Handler) getImpersonatedToken(ctx context.Context, opts auth.TokenOptions, sourceTokenFunc func(ctx context.Context, opts auth.TokenOptions) (*auth.Token, error)) (*auth.Token, error) {
	lgr := logger.FromContext(ctx)

	// Get the source token
	sourceToken, err := sourceTokenFunc(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("acquiring source token for impersonation: %w", err)
	}

	targetSA := h.config.ImpersonateServiceAccount

	// Determine scopes
	scopes := []string{opts.Scope}
	if opts.Scope == "" {
		scopes = h.config.DefaultScopes
	}

	// Check cache for impersonated token
	impersonateFingerprint := auth.FingerprintHash(targetSA)
	impersonateFlow := auth.Flow("impersonation")
	if !opts.ForceRefresh {
		minValidFor := opts.MinValidFor
		if minValidFor == 0 {
			minValidFor = auth.DefaultMinValidFor
		}
		cached, cacheErr := h.tokenCache.Get(ctx, impersonateFlow, impersonateFingerprint, opts.Scope)
		if cacheErr == nil && cached != nil && cached.IsValidFor(minValidFor) {
			lgr.V(1).Info("using cached impersonated token",
				"targetServiceAccount", targetSA,
				"scope", opts.Scope,
			)
			return cached, nil
		}
	}

	// Impersonate
	token, err := h.impersonateServiceAccount(ctx, sourceToken.AccessToken, targetSA, scopes)
	if err != nil {
		return nil, err
	}

	// Cache the impersonated token
	if err := h.tokenCache.Set(ctx, impersonateFlow, impersonateFingerprint, opts.Scope, token); err != nil {
		lgr.V(1).Info("failed to cache impersonated token", "error", err)
	}

	return token, nil
}
