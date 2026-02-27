// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package gcp provides Google Cloud Platform authentication for scafctl.
// This file implements the Workload Identity Federation STS token exchange flow.
package gcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/logger"
)

const (
	// EnvGoogleExternalAccount is the environment variable for external account configuration.
	EnvGoogleExternalAccount = "GOOGLE_EXTERNAL_ACCOUNT"

	// stsEndpoint is the Google STS token exchange endpoint.
	stsEndpoint = "https://sts.googleapis.com/v1/token"
)

// ErrNoWorkloadIdentityConfigured is returned by GetExternalAccountConfig when no
// workload identity credentials are configured (env var missing or wrong type).
var ErrNoWorkloadIdentityConfigured = errors.New("no workload identity configured")

// ExternalAccountConfig represents the external account credential configuration JSON.
type ExternalAccountConfig struct {
	Type                           string           `json:"type"`
	Audience                       string           `json:"audience"`
	SubjectTokenType               string           `json:"subject_token_type"`
	TokenURL                       string           `json:"token_url"`
	ServiceAccountImpersonationURL string           `json:"service_account_impersonation_url,omitempty"`
	CredentialSource               CredentialSource `json:"credential_source"`
}

// CredentialSource defines where to read the subject token from.
type CredentialSource struct {
	File             string                  `json:"file,omitempty"`
	URL              string                  `json:"url,omitempty"`
	Headers          map[string]string       `json:"headers,omitempty"`
	EnvironmentID    string                  `json:"environment_id,omitempty"`
	Format           *CredentialSourceFormat `json:"format,omitempty"`
	RegionURL        string                  `json:"region_url,omitempty"`
	IMDSv2SessionURL string                  `json:"imdsv2_session_token_url,omitempty"`
}

// CredentialSourceFormat defines the format of the credential source.
type CredentialSourceFormat struct {
	Type                  string `json:"type,omitempty"`
	SubjectTokenFieldName string `json:"subject_token_field_name,omitempty"`
}

// STSTokenResponse represents the response from the STS token exchange.
type STSTokenResponse struct {
	AccessToken     string `json:"access_token"` //nolint:gosec // G117: not a hardcoded credential
	IssuedTokenType string `json:"issued_token_type"`
	TokenType       string `json:"token_type"`
	ExpiresIn       int    `json:"expires_in"`
}

// GetExternalAccountConfig reads and parses the external account configuration
// from the GOOGLE_EXTERNAL_ACCOUNT environment variable.
func GetExternalAccountConfig() (*ExternalAccountConfig, error) {
	path := os.Getenv(EnvGoogleExternalAccount)
	if path == "" {
		return nil, ErrNoWorkloadIdentityConfigured
	}

	data, err := os.ReadFile(path) //nolint:gosec // G703: path from env var is expected
	if err != nil {
		return nil, fmt.Errorf("reading external account config: %w", err)
	}

	var cfg ExternalAccountConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing external account config: %w", err)
	}

	if cfg.Type != "external_account" {
		return nil, ErrNoWorkloadIdentityConfigured
	}

	return &cfg, nil
}

// HasWorkloadIdentityCredentials checks if workload identity credentials are configured.
func HasWorkloadIdentityCredentials() bool {
	_, err := GetExternalAccountConfig()
	return err == nil
}

// readSubjectToken reads the subject token from the credential source.
func readSubjectToken(cfg *ExternalAccountConfig) (string, error) {
	if cfg.CredentialSource.File != "" {
		data, err := os.ReadFile(cfg.CredentialSource.File)
		if err != nil {
			return "", fmt.Errorf("reading subject token file: %w", err)
		}

		// Handle structured token formats
		if cfg.CredentialSource.Format != nil && cfg.CredentialSource.Format.Type == "json" {
			var tokenDoc map[string]any
			if err := json.Unmarshal(data, &tokenDoc); err != nil {
				return "", fmt.Errorf("parsing subject token JSON: %w", err)
			}
			fieldName := cfg.CredentialSource.Format.SubjectTokenFieldName
			if fieldName == "" {
				fieldName = "token"
			}
			tokenVal, ok := tokenDoc[fieldName]
			if !ok {
				return "", fmt.Errorf("subject token field %q not found in JSON", fieldName)
			}
			tokenStr, ok := tokenVal.(string)
			if !ok {
				return "", fmt.Errorf("subject token field %q is not a string", fieldName)
			}
			return tokenStr, nil
		}

		return string(data), nil
	}

	return "", fmt.Errorf(
		"unsupported credential source in external account config: "+
			"no file or URL is configured under credential_source. "+
			"Check the external account JSON file referenced by %s",
		EnvGoogleExternalAccount)
}

// workloadIdentityLogin validates workload identity credentials by acquiring a token.
func (h *Handler) workloadIdentityLogin(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("starting workload identity federation login", "handler", HandlerName)

	cfg, err := GetExternalAccountConfig()
	if err != nil {
		if errors.Is(err, ErrNoWorkloadIdentityConfigured) {
			return nil, fmt.Errorf("workload identity credentials not configured: set %s environment variable",
				EnvGoogleExternalAccount)
		}
		return nil, fmt.Errorf("workload identity credentials error: %w", err)
	}

	scope := "https://www.googleapis.com/auth/cloud-platform"
	if len(opts.Scopes) > 0 {
		scope = strings.Join(opts.Scopes, " ")
	}

	// Acquire a token to validate credentials
	token, err := h.acquireWorkloadIdentityToken(ctx, cfg, scope)
	if err != nil {
		return nil, fmt.Errorf("workload identity authentication failed: %w", err)
	}

	claims := &auth.Claims{
		Issuer:  "https://accounts.google.com",
		Subject: cfg.Audience,
	}

	// Store metadata
	if err := h.storeMetadataOnly(ctx, claims, auth.FlowWorkloadIdentity, opts.Scopes); err != nil {
		lgr.V(1).Info("warning: failed to store metadata", "error", err)
	}

	lgr.V(1).Info("workload identity authentication successful",
		"audience", cfg.Audience,
	)

	return &auth.Result{
		Claims:    claims,
		ExpiresAt: token.ExpiresAt,
	}, nil
}

// acquireWorkloadIdentityToken acquires a token via STS token exchange.
func (h *Handler) acquireWorkloadIdentityToken(ctx context.Context, cfg *ExternalAccountConfig, scope string) (*auth.Token, error) {
	lgr := logger.FromContext(ctx)

	// Read the subject token
	subjectToken, err := readSubjectToken(cfg)
	if err != nil {
		return nil, fmt.Errorf("reading subject token: %w", err)
	}

	// Exchange via STS
	endpoint := cfg.TokenURL
	if endpoint == "" {
		endpoint = stsEndpoint
	}

	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:token-exchange")
	data.Set("audience", cfg.Audience)
	data.Set("scope", scope)
	data.Set("requested_token_type", "urn:ietf:params:oauth:token-type:access_token")
	data.Set("subject_token_type", cfg.SubjectTokenType)
	data.Set("subject_token", subjectToken)

	lgr.V(1).Info("requesting token via STS exchange",
		"endpoint", endpoint,
		"audience", cfg.Audience,
	)

	resp, err := h.httpClient.PostForm(ctx, endpoint, data)
	if err != nil {
		return nil, fmt.Errorf("STS token exchange failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		var errResp TokenErrorResponse
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		return nil, fmt.Errorf(
			"STS token exchange failed: %s - %s. "+
				"Verify the audience, subject_token_type, and token_url in the external account config (%s) "+
				"and that the Workload Identity Pool is correctly configured in GCP IAM",
			errResp.Error, errResp.ErrorDescription, EnvGoogleExternalAccount)
	}

	var stsResp STSTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&stsResp); err != nil {
		return nil, fmt.Errorf("failed to parse STS response: %w", err)
	}

	expiresAt := time.Now().Add(time.Duration(stsResp.ExpiresIn) * time.Second)

	lgr.V(1).Info("acquired workload identity token",
		"expiresIn", stsResp.ExpiresIn,
	)

	return &auth.Token{
		AccessToken: stsResp.AccessToken,
		TokenType:   stsResp.TokenType,
		ExpiresAt:   expiresAt,
		Scope:       scope,
		Flow:        auth.FlowWorkloadIdentity,
	}, nil
}

// getWorkloadIdentityToken gets a token using workload identity, with caching.
func (h *Handler) getWorkloadIdentityToken(ctx context.Context, opts auth.TokenOptions) (*auth.Token, error) {
	if opts.Scope == "" {
		return nil, auth.ErrInvalidScope
	}
	return auth.GetCachedOrAcquireToken(
		ctx,
		h.tokenCache,
		opts,
		auth.FlowWorkloadIdentity,
		opts.Scope,
		func() (*ExternalAccountConfig, error) { return GetExternalAccountConfig() },
		func(_ *ExternalAccountConfig) bool { return false },
		func(cfg *ExternalAccountConfig) string {
			return auth.FingerprintHash(cfg.Audience)
		},
		func(ctx context.Context, cfg *ExternalAccountConfig, scope string) (*auth.Token, error) {
			return h.acquireWorkloadIdentityToken(ctx, cfg, scope)
		},
		"WI",
	)
}

// workloadIdentityStatus returns the status for workload identity authentication.
func (h *Handler) workloadIdentityStatus(_ context.Context) (*auth.Status, error) {
	cfg, err := GetExternalAccountConfig()
	if err != nil {
		return &auth.Status{ //nolint:nilerr // intentional: credential read errors mean not authenticated
			Authenticated: false,
		}, nil
	}

	tokenFile := cfg.CredentialSource.File

	return &auth.Status{
		Authenticated: true,
		Claims: &auth.Claims{
			Issuer:  "https://accounts.google.com",
			Subject: cfg.Audience,
			Name:    "Workload Identity Federation",
		},
		IdentityType: auth.IdentityTypeWorkloadIdentity,
		TokenFile:    tokenFile,
	}, nil
}
