// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/golang-jwt/jwt/v5"
)

// contextKey type for storing auth claims in context.
type contextKey string

const claimsContextKey contextKey = "auth_claims"

// AuthClaims holds the validated JWT claims extracted from an Entra OIDC token.
type AuthClaims struct {
	Subject   string   `json:"sub"`
	Name      string   `json:"name"`
	Email     string   `json:"email"`
	TenantID  string   `json:"tid"`
	ObjectID  string   `json:"oid"`
	Groups    []string `json:"groups"`
	Roles     []string `json:"roles"`
	Audience  string   `json:"aud"`
	Issuer    string   `json:"iss"`
	ExpiresAt int64    `json:"exp"`
}

// ClaimsFromContext extracts auth claims from the request context.
func ClaimsFromContext(ctx context.Context) *AuthClaims {
	claims, _ := ctx.Value(claimsContextKey).(*AuthClaims)
	return claims
}

// jwksCache caches the JWKS endpoint response.
type jwksCache struct {
	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	expiresAt time.Time
	jwksURL   string
	client    *http.Client
}

// jwksKey represents a single JWK key.
type jwksKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// jwksResponse represents the JWKS endpoint response.
type jwksResponse struct {
	Keys []jwksKey `json:"keys"`
}

const (
	jwksCacheTTL     = 1 * time.Hour
	jwksFetchTimeout = 10 * time.Second
)

// validateIdentifierForURL checks that an identifier does not contain characters
// that would corrupt the URL constructed from it (slash, query, fragment, whitespace).
func validateIdentifierForURL(value, name string) error {
	if len(value) > 253 {
		return fmt.Errorf("%s exceeds maximum length of 253 characters", name)
	}
	for _, c := range value {
		if c == '/' || c == '?' || c == '#' || c == ' ' || c == '\n' || c == '\r' || c == '\t' {
			return fmt.Errorf("%s contains invalid character %q for URL construction", name, c)
		}
	}
	return nil
}

// NewAzureOIDCAuth creates authentication middleware for Azure AD OIDC JWT validation.
func NewAzureOIDCAuth(tenantID, clientID string, lgr logr.Logger) (func(http.Handler) http.Handler, error) {
	if tenantID == "" || clientID == "" {
		return nil, fmt.Errorf("tenantID and clientID are required")
	}
	if err := validateIdentifierForURL(tenantID, "tenantID"); err != nil {
		return nil, fmt.Errorf("invalid tenantID: %w", err)
	}
	if err := validateIdentifierForURL(clientID, "clientID"); err != nil {
		return nil, fmt.Errorf("invalid clientID: %w", err)
	}

	jwksURL := fmt.Sprintf("https://login.microsoftonline.com/%s/discovery/v2.0/keys", tenantID)
	issuer := fmt.Sprintf("https://login.microsoftonline.com/%s/v2.0", tenantID)

	cache := &jwksCache{
		jwksURL: jwksURL,
		client: &http.Client{
			Timeout: jwksFetchTimeout,
		},
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeJSONError(w, `{"title":"Unauthorized","status":401,"detail":"missing Authorization header"}`, http.StatusUnauthorized)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				writeJSONError(w, `{"title":"Unauthorized","status":401,"detail":"invalid Authorization header format"}`, http.StatusUnauthorized)
				return
			}
			tokenStr := parts[1]

			// Parse token without validation first to get the kid
			unverified, _, err := jwt.NewParser().ParseUnverified(tokenStr, jwt.MapClaims{})
			if err != nil {
				lgr.V(2).Info("failed to parse JWT", "error", err)
				writeJSONError(w, `{"title":"Unauthorized","status":401,"detail":"invalid token"}`, http.StatusUnauthorized)
				return
			}

			kid, _ := unverified.Header["kid"].(string)
			if kid == "" {
				writeJSONError(w, `{"title":"Unauthorized","status":401,"detail":"token missing kid header"}`, http.StatusUnauthorized)
				return
			}

			// Get the signing key
			key, err := cache.getKey(kid)
			if err != nil {
				lgr.V(1).Info("failed to fetch JWKS key", "kid", kid, "error", err)
				writeJSONError(w, `{"title":"Unauthorized","status":401,"detail":"unable to verify token signature"}`, http.StatusUnauthorized)
				return
			}

			// Validate the token
			token, err := jwt.Parse(tokenStr, func(_ *jwt.Token) (any, error) {
				return key, nil
			},
				jwt.WithValidMethods([]string{"RS256"}),
				jwt.WithAudience(clientID),
				jwt.WithIssuer(issuer),
				jwt.WithExpirationRequired(),
			)
			if err != nil || !token.Valid {
				lgr.V(2).Info("JWT validation failed", "error", err)
				writeJSONError(w, `{"title":"Unauthorized","status":401,"detail":"invalid or expired token"}`, http.StatusUnauthorized)
				return
			}

			// Extract claims
			mapClaims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				writeJSONError(w, `{"title":"Unauthorized","status":401,"detail":"failed to extract claims"}`, http.StatusUnauthorized)
				return
			}

			claims := &AuthClaims{
				Subject:  claimString(mapClaims, "sub"),
				Name:     claimString(mapClaims, "name"),
				Email:    claimString(mapClaims, "email"),
				TenantID: claimString(mapClaims, "tid"),
				ObjectID: claimString(mapClaims, "oid"),
				Audience: claimString(mapClaims, "aud"),
				Issuer:   claimString(mapClaims, "iss"),
				Groups:   claimStringSlice(mapClaims, "groups"),
				Roles:    claimStringSlice(mapClaims, "roles"),
			}
			if exp, ok := mapClaims["exp"].(float64); ok {
				claims.ExpiresAt = int64(exp)
			}

			ctx := context.WithValue(r.Context(), claimsContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}, nil
}

// getKey fetches the RSA public key for the given kid from JWKS.
func (c *jwksCache) getKey(kid string) (*rsa.PublicKey, error) {
	c.mu.RLock()
	cacheValid := c.keys != nil && time.Now().Before(c.expiresAt)
	if cacheValid {
		if key, ok := c.keys[kid]; ok {
			c.mu.RUnlock()
			return key, nil
		}
		// Cache is fresh but kid is unknown — no point refreshing.
		c.mu.RUnlock()
		return nil, fmt.Errorf("key %q not found in JWKS", kid)
	}
	c.mu.RUnlock()

	// Cache is stale — fetch fresh keys.
	if err := c.refresh(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	key, ok := c.keys[kid]
	if !ok {
		return nil, fmt.Errorf("key %q not found in JWKS", kid)
	}
	return key, nil
}

// refresh fetches the JWKS endpoint and updates the cache.
func (c *jwksCache) refresh() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if c.keys != nil && time.Now().Before(c.expiresAt) {
		return nil
	}

	resp, err := c.client.Get(c.jwksURL) //nolint:noctx // internal HTTP client with timeout
	if err != nil {
		return fmt.Errorf("fetching JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Drain up to 512 bytes for context, then discard to allow connection reuse.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
		return fmt.Errorf("JWKS endpoint returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return fmt.Errorf("reading JWKS response: %w", err)
	}

	var jwks jwksResponse
	if err := json.Unmarshal(body, &jwks); err != nil {
		return fmt.Errorf("parsing JWKS response: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		pubKey, err := parseRSAPublicKey(k)
		if err != nil {
			continue
		}
		keys[k.Kid] = pubKey
	}

	c.keys = keys
	c.expiresAt = time.Now().Add(jwksCacheTTL)
	return nil
}

// parseRSAPublicKey parses a JWK into an RSA public key.
func parseRSAPublicKey(k jwksKey) (*rsa.PublicKey, error) {
	nBytes, err := jwt.NewParser().DecodeSegment(k.N)
	if err != nil {
		return nil, fmt.Errorf("decoding modulus: %w", err)
	}
	eBytes, err := jwt.NewParser().DecodeSegment(k.E)
	if err != nil {
		return nil, fmt.Errorf("decoding exponent: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)

	eVal := e.Int64()
	const minRSAExponent = 3
	const maxRSAExponent = 1<<31 - 1 // rsa.PublicKey.E is int; cap at max int32
	if eVal < minRSAExponent || eVal > maxRSAExponent {
		return nil, fmt.Errorf("RSA exponent %d is out of valid range [%d, %d]", eVal, minRSAExponent, maxRSAExponent)
	}

	return &rsa.PublicKey{
		N: n,
		E: int(eVal),
	}, nil
}

func claimString(claims jwt.MapClaims, key string) string {
	val, _ := claims[key].(string)
	return val
}

func claimStringSlice(claims jwt.MapClaims, key string) []string {
	val, ok := claims[key].([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(val))
	for _, v := range val {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
