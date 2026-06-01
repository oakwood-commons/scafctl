// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/serveridentity"
	"github.com/oakwood-commons/scafctl/pkg/tokenprovider"
	"go.opentelemetry.io/otel/trace"
)

// jwtHeader is the fixed RS256 header, base64url-encoded.
var jwtHeader = base64URLEncode([]byte(`{"alg":"RS256","typ":"JWT"}`))

// buildJWT creates a signed JWT for GitHub App authentication.
// The JWT has a 10-minute expiry and is backdated 60 seconds to account for clock skew.
// The issuer (iss) is the GitHub App's Client ID.
func buildJWT(clientID string, key *rsa.PrivateKey) (string, error) {
	if key == nil {
		return "", fmt.Errorf("private key is required")
	}

	now := time.Now()
	claims := fmt.Sprintf(`{"iss":"%s","iat":%d,"exp":%d}`, clientID, now.Add(-60*time.Second).Unix(), now.Add(10*time.Minute).Unix())

	payload := jwtHeader + "." + base64URLEncode([]byte(claims))

	hash := sha256.Sum256([]byte(payload))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if err != nil {
		return "", fmt.Errorf("signing JWT: %w", err)
	}

	return payload + "." + base64URLEncode(sig), nil
}

// installationTokenResponse is the GitHub API response for installation token creation.
type installationTokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// apiBaseURL returns the GitHub REST API base URL for the given hostname.
// For github.com it returns https://api.github.com.
// For GitHub Enterprise Server it returns https://{hostname}/api/v3.
func apiBaseURL(hostname string) string {
	if hostname == "github.com" {
		return "https://api.github.com"
	}
	return fmt.Sprintf("https://%s/api/v3", hostname)
}

// exchangeForInstallationToken calls GitHub's API to create an installation access token.
func exchangeForInstallationToken(ctx context.Context, jwt, tokenURL string, client *httpc.Client) (tokenprovider.Token, error) {
	log := logger.FromContext(ctx)
	start := time.Now()
	span := trace.SpanFromContext(ctx)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, nil)
	if err != nil {
		serveridentity.SpanError(span, err, "creating request failed")
		return tokenprovider.Token{}, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	if log.V(2).Enabled() {
		log.V(2).Info("installation token request", "url", tokenURL)
	}

	resp, err := client.Do(req)
	if err != nil {
		serveridentity.SpanError(span, err, "requesting installation token failed")
		if log.V(1).Enabled() {
			log.V(1).Error(err, "requesting installation token failed", "url", tokenURL)
		}
		return tokenprovider.Token{}, fmt.Errorf("requesting installation token: %w", err)
	}
	defer resp.Body.Close()

	serveridentity.SpanSetStatusCode(span, resp.StatusCode)

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		serveridentity.SpanError(span, err, "reading response body failed")
		if log.V(1).Enabled() {
			log.V(1).Error(err, "reading response body failed")
		}
		return tokenprovider.Token{}, fmt.Errorf("reading response body: %w", err)
	}

	elapsed := time.Since(start)
	if resp.StatusCode != http.StatusCreated {
		serveridentity.SpanError(span, nil, fmt.Sprintf("GitHub API returned %d", resp.StatusCode))
		if log.V(1).Enabled() {
			log.V(1).Info("installation token error", "status", resp.StatusCode, "elapsed", elapsed)
		}
		return tokenprovider.Token{}, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var result installationTokenResponse
	if err := json.Unmarshal(body, &result); err != nil {
		serveridentity.SpanError(span, err, "parsing response failed")
		return tokenprovider.Token{}, fmt.Errorf("parsing response: %w", err)
	}

	if result.Token == "" {
		serveridentity.SpanError(span, nil, "GitHub API returned empty token")
		if log.V(1).Enabled() {
			log.V(1).Info("GitHub API returned empty token", "elapsed", elapsed)
		}
		return tokenprovider.Token{}, fmt.Errorf("GitHub API returned empty token")
	}

	if log.V(1).Enabled() {
		log.V(1).Info("installation token obtained", "expiresAt", result.ExpiresAt.Format(time.RFC3339), "elapsed", elapsed)
	}

	return tokenprovider.Token{
		AccessToken: result.Token,
		ExpiresAt:   result.ExpiresAt,
		TokenType:   "Bearer",
	}, nil
}

func base64URLEncode(data []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(data), "=")
}
