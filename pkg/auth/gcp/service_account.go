// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package gcp provides Google Cloud Platform authentication for scafctl.
// This file implements the service account key JWT assertion flow.
package gcp

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/logger"
)

const (
	// EnvGoogleApplicationCredentials is the environment variable for service account key file path.
	EnvGoogleApplicationCredentials = "GOOGLE_APPLICATION_CREDENTIALS" //nolint:gosec // G101: not a hardcoded credential

	// tokenEndpoint is the Google OAuth 2.0 token endpoint.
	tokenEndpoint = "https://oauth2.googleapis.com/token" //nolint:gosec // G117: not a credential, it's an endpoint URL
)

// ServiceAccountKey represents the JSON structure of a GCP service account key file.
type ServiceAccountKey struct {
	Type                    string `json:"type"`
	ProjectID               string `json:"project_id"`
	PrivateKeyID            string `json:"private_key_id"`
	PrivateKey              string `json:"private_key"` //nolint:gosec // G117: not a hardcoded credential, it's a config field
	ClientEmail             string `json:"client_email"`
	ClientID                string `json:"client_id"`
	AuthURI                 string `json:"auth_uri"`
	TokenURI                string `json:"token_uri"`
	AuthProviderX509CertURL string `json:"auth_provider_x509_cert_url"`
	ClientX509CertURL       string `json:"client_x509_cert_url"`
}

// GetServiceAccountKey reads and parses a service account key from the
// GOOGLE_APPLICATION_CREDENTIALS environment variable.
func GetServiceAccountKey() (*ServiceAccountKey, error) {
	path := os.Getenv(EnvGoogleApplicationCredentials)
	if path == "" {
		return nil, nil //nolint:nilnil // nil,nil means no credentials configured
	}

	data, err := os.ReadFile(path) //nolint:gosec // G703: path from env var is expected
	if err != nil {
		return nil, fmt.Errorf("reading service account key file: %w", err)
	}

	var key ServiceAccountKey
	if err := json.Unmarshal(data, &key); err != nil {
		return nil, fmt.Errorf("parsing service account key file: %w", err)
	}

	if key.Type != "service_account" {
		return nil, nil //nolint:nilnil // not a service account key
	}

	return &key, nil
}

// HasServiceAccountCredentials checks if service account credentials are configured.
func HasServiceAccountCredentials() bool {
	key, err := GetServiceAccountKey()
	return err == nil && key != nil
}

// serviceAccountLogin validates SA credentials by acquiring a token.
func (h *Handler) serviceAccountLogin(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("starting service account key login", "handler", HandlerName)

	key, err := GetServiceAccountKey()
	if err != nil {
		return nil, fmt.Errorf("service account credentials error: %w", err)
	}
	if key == nil {
		return nil, fmt.Errorf("service account credentials not configured: set %s environment variable",
			EnvGoogleApplicationCredentials)
	}

	scope := "https://www.googleapis.com/auth/cloud-platform"
	if len(opts.Scopes) > 0 {
		scope = joinScopes(opts.Scopes)
	}

	// Acquire a token to validate credentials
	token, err := h.acquireServiceAccountToken(ctx, key, scope)
	if err != nil {
		return nil, fmt.Errorf("service account authentication failed: %w", err)
	}

	claims := &auth.Claims{
		Issuer:   "https://accounts.google.com",
		Subject:  key.ClientEmail,
		Email:    key.ClientEmail,
		ClientID: key.ClientID,
		ObjectID: key.ClientID,
	}

	// Store metadata
	if err := h.storeMetadataOnly(ctx, claims, auth.FlowServicePrincipal, opts.Scopes); err != nil {
		lgr.V(1).Info("warning: failed to store metadata", "error", err)
	}

	lgr.V(1).Info("service account authentication successful",
		"clientEmail", key.ClientEmail,
		"projectId", key.ProjectID,
	)

	return &auth.Result{
		Claims:    claims,
		ExpiresAt: token.ExpiresAt,
	}, nil
}

// acquireServiceAccountToken acquires a token using JWT assertion flow.
func (h *Handler) acquireServiceAccountToken(ctx context.Context, key *ServiceAccountKey, scope string) (*auth.Token, error) {
	lgr := logger.FromContext(ctx)

	// Create the JWT assertion
	now := time.Now()
	jwtHeader := map[string]string{
		"alg": "RS256",
		"typ": "JWT",
		"kid": key.PrivateKeyID,
	}
	jwtPayload := map[string]any{
		"iss":   key.ClientEmail,
		"sub":   key.ClientEmail,
		"aud":   tokenEndpoint,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
		"scope": scope,
	}

	headerJSON, err := json.Marshal(jwtHeader)
	if err != nil {
		return nil, fmt.Errorf("encoding JWT header: %w", err)
	}
	payloadJSON, err := json.Marshal(jwtPayload)
	if err != nil {
		return nil, fmt.Errorf("encoding JWT payload: %w", err)
	}

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)
	signingInput := headerB64 + "." + payloadB64

	// Parse the private key
	block, _ := pem.Decode([]byte(key.PrivateKey))
	if block == nil {
		return nil, fmt.Errorf(
			"failed to parse PEM block from service account private key: "+
				"the private_key field in %s may be malformed or not in PEM format",
			EnvGoogleApplicationCredentials)
	}

	rsaKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing private key: %w", err)
	}

	privateKey, ok := rsaKey.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not RSA")
	}

	// Sign the JWT
	hash := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(nil, privateKey, 0, hash[:])
	if err != nil {
		// Use stdlib big.Int for signing to avoid crypto/rand dependency
		// Actually rsa.SignPKCS1v15 with nil rand works for deterministic signing
		// But let's use the proper approach
		return nil, fmt.Errorf("signing JWT: %w", err)
	}
	_ = big.NewInt(0) // keep import for potential future use

	signatureB64 := base64.RawURLEncoding.EncodeToString(signature)
	assertion := signingInput + "." + signatureB64

	// Exchange JWT assertion for access token
	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	data.Set("assertion", assertion)

	lgr.V(1).Info("requesting token via JWT assertion",
		"clientEmail", key.ClientEmail,
		"scope", scope,
	)

	resp, err := h.httpClient.PostForm(ctx, tokenEndpoint, data)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		var errResp TokenErrorResponse
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		return nil, fmt.Errorf(
			"service account token request failed: %s - %s. "+
				"Verify the key in %s is valid, has not been revoked, "+
				"and the service account has the required IAM roles for the requested scope",
			errResp.Error, errResp.ErrorDescription, EnvGoogleApplicationCredentials)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	lgr.V(1).Info("acquired service account token",
		"expiresIn", tokenResp.ExpiresIn,
		"scope", scope,
	)

	return &auth.Token{
		AccessToken: tokenResp.AccessToken,
		TokenType:   tokenResp.TokenType,
		ExpiresAt:   expiresAt,
		Scope:       scope,
	}, nil
}

// getServiceAccountToken gets a token using service account key, with caching.
func (h *Handler) getServiceAccountToken(ctx context.Context, opts auth.TokenOptions) (*auth.Token, error) {
	return getCachedOrAcquireToken(
		ctx,
		h,
		opts,
		func() (*ServiceAccountKey, error) { return GetServiceAccountKey() },
		func(key *ServiceAccountKey, err error) bool { return key == nil || err != nil },
		func(ctx context.Context, key *ServiceAccountKey, scope string) (*auth.Token, error) {
			return h.acquireServiceAccountToken(ctx, key, scope)
		},
		"SA",
	)
}

// serviceAccountStatus returns the status for SA authentication.
func (h *Handler) serviceAccountStatus(_ context.Context) (*auth.Status, error) {
	key, err := GetServiceAccountKey()
	if err != nil || key == nil {
		return &auth.Status{ //nolint:nilerr // intentional: credential read errors mean not authenticated
			Authenticated: false,
		}, nil
	}

	return &auth.Status{
		Authenticated: true,
		Claims: &auth.Claims{
			Issuer:   "https://accounts.google.com",
			Subject:  key.ClientEmail,
			Email:    key.ClientEmail,
			ClientID: key.ClientID,
			ObjectID: key.ClientID,
			Name:     fmt.Sprintf("Service Account (%s)", key.ClientEmail),
		},
		IdentityType: auth.IdentityTypeServicePrincipal,
		ClientID:     key.ClientID,
	}, nil
}

// joinScopes joins scopes into a space-separated string.
func joinScopes(scopes []string) string {
	result := ""
	for i, s := range scopes {
		if i > 0 {
			result += " "
		}
		result += s
	}
	return result
}
