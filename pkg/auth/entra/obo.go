// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package entra

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"golang.org/x/sync/singleflight"
)

// OBO grant type and constants.
const (
	// OBOGrantType is the OAuth 2.0 grant type for On-Behalf-Of flow.
	OBOGrantType = "urn:ietf:params:oauth:grant-type:jwt-bearer"

	// OBORequestedTokenUse is the required parameter for OBO requests.
	OBORequestedTokenUse = "on_behalf_of"

	// FlowOnBehalfOf identifies the OBO flow.  Defined here because the SDK
	// does not yet include it; once the SDK is updated this constant should
	// become an alias like the other flow constants in pkg/auth/handler.go.
	FlowOnBehalfOf = "on_behalf_of"

	// oboExpiryBuffer is subtracted from the token expiry to ensure tokens
	// are refreshed before they actually expire.
	oboExpiryBuffer = 30 * time.Second
)

// OBOTokenOptions configures an On-Behalf-Of token acquisition.
// This is a separate type from auth.TokenOptions because the SDK does not
// yet include an Assertion field.
type OBOTokenOptions struct {
	// Assertion is the access token of the upstream caller (the "subject" token).
	Assertion string `json:"-" yaml:"-" doc:"Upstream caller access token" maxLength:"8192"`

	// Scope is the target resource scope to acquire.
	Scope string `json:"scope" yaml:"scope" doc:"Target resource scope" example:"https://graph.microsoft.com/.default" maxLength:"2048"`

	// ClientSecret is the confidential client secret.  Required for OBO
	// because only confidential clients can use the OBO flow.
	ClientSecret string `json:"-" yaml:"-"`
}

// oboCache is a concurrency-safe, in-memory cache for OBO tokens.
// Each entry is keyed by sha256(assertion + "\x00" + scope) so that
// different upstream callers and target scopes are cached independently.
type oboCache struct {
	mu    sync.RWMutex
	items map[string]*oboCacheEntry
	group singleflight.Group
}

type oboCacheEntry struct {
	token     *auth.Token
	expiresAt time.Time
}

func newOBOCache() *oboCache {
	return &oboCache{
		items: make(map[string]*oboCacheEntry),
	}
}

// oboCacheKey computes a deterministic cache key for an assertion+scope pair.
func oboCacheKey(assertion, scope string) string {
	h := sha256.Sum256([]byte(assertion + "\x00" + scope))
	return hex.EncodeToString(h[:])
}

// get returns a cached token if it exists and is still valid.
// Expired entries are evicted on read to prevent unbounded growth.
func (c *oboCache) get(assertion, scope string) (*auth.Token, bool) {
	key := oboCacheKey(assertion, scope)
	c.mu.RLock()
	entry, ok := c.items[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expiresAt) {
		c.mu.Lock()
		delete(c.items, key)
		c.mu.Unlock()
		return nil, false
	}
	return entry.token, true
}

// set stores a token in the cache.
func (c *oboCache) set(assertion, scope string, token *auth.Token) {
	key := oboCacheKey(assertion, scope)
	c.mu.Lock()
	c.items[key] = &oboCacheEntry{
		token:     token,
		expiresAt: token.ExpiresAt.Add(-oboExpiryBuffer),
	}
	c.mu.Unlock()
}

// GetOBOToken acquires a token using the On-Behalf-Of flow.
// This exchanges the upstream caller's access token (assertion) for a new
// token scoped to a different resource.  Results are cached per
// assertion+scope pair and deduplicated via singleflight.
//
// OBO requires a confidential client (client_id + client_secret).
func (h *Handler) GetOBOToken(ctx context.Context, opts OBOTokenOptions) (*auth.Token, error) {
	if opts.Assertion == "" {
		return nil, fmt.Errorf("OBO assertion (upstream access token) is required: %w", auth.ErrNotAuthenticated)
	}
	if opts.Scope == "" {
		return nil, auth.ErrInvalidScope
	}
	if opts.ClientSecret == "" {
		return nil, fmt.Errorf("OBO flow requires a client secret (confidential client): %w", auth.ErrAuthenticationFailed)
	}

	// Qualify bare scopes
	qualifiedScope := QualifyScope(opts.Scope)

	// Check in-memory cache
	if token, ok := h.oboCache.get(opts.Assertion, qualifiedScope); ok {
		return token, nil
	}

	// Deduplicate concurrent requests for the same assertion+scope
	key := oboCacheKey(opts.Assertion, qualifiedScope)
	v, err, _ := h.oboCache.group.Do(key, func() (any, error) {
		// Double-check cache after winning the singleflight race
		if token, ok := h.oboCache.get(opts.Assertion, qualifiedScope); ok {
			return token, nil
		}
		token, mintErr := h.mintOBOToken(ctx, opts.Assertion, qualifiedScope, opts.ClientSecret)
		if mintErr != nil {
			return nil, mintErr
		}
		// Cache inside the closure so only the winning goroutine writes.
		h.oboCache.set(opts.Assertion, qualifiedScope, token)
		return token, nil
	})
	if err != nil {
		return nil, err
	}

	token, ok := v.(*auth.Token)
	if !ok {
		return nil, fmt.Errorf("unexpected OBO token type: %T", v)
	}
	return token, nil
}

// mintOBOToken performs the actual OBO token exchange against the Entra token
// endpoint.
func (h *Handler) mintOBOToken(ctx context.Context, assertion, scope, clientSecret string) (*auth.Token, error) {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("minting OBO token", "scope", scope)

	endpoint := fmt.Sprintf("%s/%s/oauth2/v2.0/token", h.config.GetAuthority(), h.config.TenantID)

	data := url.Values{}
	data.Set("grant_type", OBOGrantType)
	data.Set("client_id", h.config.ClientID)
	data.Set("client_secret", clientSecret)
	data.Set("assertion", assertion)
	data.Set("scope", scope)
	data.Set("requested_token_use", OBORequestedTokenUse)

	resp, err := h.httpClient.PostForm(ctx, endpoint, data)
	if err != nil {
		return nil, fmt.Errorf("OBO token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp TokenErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			return nil, fmt.Errorf("OBO token request failed with status %d", resp.StatusCode)
		}

		if strings.Contains(errResp.ErrorDescription, "AADSTS") {
			return nil, formatAADSTSError("OBO token request failed", errResp)
		}
		return nil, fmt.Errorf("OBO token request failed: %s - %s", errResp.Error, errResp.ErrorDescription)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse OBO token response: %w", err)
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	lgr.V(1).Info("OBO token minted successfully",
		"expiresIn", tokenResp.ExpiresIn,
		"expiresAt", expiresAt,
		"scope", scope,
	)

	return &auth.Token{
		AccessToken: tokenResp.AccessToken,
		TokenType:   tokenResp.TokenType,
		ExpiresAt:   expiresAt,
		Scope:       scope,
		Flow:        FlowOnBehalfOf,
	}, nil
}
