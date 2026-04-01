// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAzureOIDCAuth_MissingParams(t *testing.T) {
	tests := []struct {
		name     string
		tenant   string
		clientID string
	}{
		{"empty tenant", "", "client-id"},
		{"empty clientID", "tenant-id", ""},
		{"both empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw, err := NewAzureOIDCAuth(tt.tenant, tt.clientID, logr.Discard())
			require.Error(t, err)
			assert.Nil(t, mw)
		})
	}
}

func TestNewAzureOIDCAuth_ValidParams(t *testing.T) {
	mw, err := NewAzureOIDCAuth("tenant-id", "client-id", logr.Discard())
	require.NoError(t, err)
	assert.NotNil(t, mw)
}

func TestAzureOIDCAuth_MissingAuthHeader(t *testing.T) {
	mw, err := NewAzureOIDCAuth("tenant-id", "client-id", logr.Discard())
	require.NoError(t, err)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "missing Authorization header")
}

func TestAzureOIDCAuth_InvalidAuthHeaderFormat(t *testing.T) {
	mw, err := NewAzureOIDCAuth("tenant-id", "client-id", logr.Discard())
	require.NoError(t, err)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid Authorization header format")
}

func TestAzureOIDCAuth_InvalidToken(t *testing.T) {
	mw, err := NewAzureOIDCAuth("tenant-id", "client-id", logr.Discard())
	require.NoError(t, err)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer not-a-valid-jwt")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestClaimsFromContext_Nil(t *testing.T) {
	claims := ClaimsFromContext(context.Background())
	assert.Nil(t, claims)
}

func TestClaimsFromContext_Present(t *testing.T) {
	expected := &AuthClaims{
		Subject:  "user-123",
		Name:     "Test User",
		Email:    "test@example.com",
		TenantID: "tenant-abc",
	}
	ctx := context.WithValue(context.Background(), claimsContextKey, expected)
	claims := ClaimsFromContext(ctx)
	require.NotNil(t, claims)
	assert.Equal(t, "user-123", claims.Subject)
	assert.Equal(t, "Test User", claims.Name)
	assert.Equal(t, "test@example.com", claims.Email)
	assert.Equal(t, "tenant-abc", claims.TenantID)
}

func BenchmarkAzureOIDCAuth_MissingHeader(b *testing.B) {
	mw, err := NewAzureOIDCAuth("tenant-id", "client-id", logr.Discard())
	require.NoError(b, err)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for b.Loop() {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func TestClaimString_Present(t *testing.T) {
	claims := jwt.MapClaims{"sub": "user-123", "num": float64(42)}
	assert.Equal(t, "user-123", claimString(claims, "sub"))
	assert.Equal(t, "", claimString(claims, "num"))     // non-string returns ""
	assert.Equal(t, "", claimString(claims, "missing")) // missing key returns ""
}

func TestClaimStringSlice_Present(t *testing.T) {
	claims := jwt.MapClaims{
		"groups": []any{"admins", "users"},
		"mixed":  []any{"admin", 42}, // 42 is not a string and should be skipped
	}
	assert.Equal(t, []string{"admins", "users"}, claimStringSlice(claims, "groups"))
	assert.Equal(t, []string{"admin"}, claimStringSlice(claims, "mixed"))
}

func TestClaimStringSlice_Missing(t *testing.T) {
	claims := jwt.MapClaims{"notslice": "string-val"}
	assert.Nil(t, claimStringSlice(claims, "missing"))
	assert.Nil(t, claimStringSlice(claims, "notslice")) // not a []any
}

func TestParseRSAPublicKey_Valid(t *testing.T) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	nEncoded := base64.RawURLEncoding.EncodeToString(privKey.N.Bytes())
	eEncoded := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privKey.E)).Bytes())

	jwk := jwksKey{Kty: "RSA", Kid: "test-kid", N: nEncoded, E: eEncoded}
	pubKey, err := parseRSAPublicKey(jwk)
	require.NoError(t, err)
	assert.Equal(t, privKey.N, pubKey.N)
	assert.Equal(t, privKey.E, pubKey.E)
}

func TestParseRSAPublicKey_InvalidN(t *testing.T) {
	jwk := jwksKey{Kty: "RSA", Kid: "bad", N: "!!!bad base64!!!", E: "AQAB"}
	_, err := parseRSAPublicKey(jwk)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "modulus")
}

func TestParseRSAPublicKey_InvalidE(t *testing.T) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	nEncoded := base64.RawURLEncoding.EncodeToString(privKey.N.Bytes())

	jwk := jwksKey{Kty: "RSA", Kid: "bad-e", N: nEncoded, E: "!!!bad!!!"}
	_, err = parseRSAPublicKey(jwk)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exponent")
}

func TestValidateIdentifierForURL_Valid(t *testing.T) {
	assert.NoError(t, validateIdentifierForURL("valid-tenant-id", "tenantID"))
	assert.NoError(t, validateIdentifierForURL("some-client-id-123", "clientID"))
}

func TestValidateIdentifierForURL_Invalid(t *testing.T) {
	assert.Error(t, validateIdentifierForURL("has/slash", "tenantID"))
	assert.Error(t, validateIdentifierForURL("has?query", "clientID"))
	assert.Error(t, validateIdentifierForURL("has#frag", "tenantID"))
	assert.Error(t, validateIdentifierForURL("has space", "clientID"))
}

func TestValidateIdentifierForURL_TooLong(t *testing.T) {
	long := string(make([]byte, 254))
	assert.Error(t, validateIdentifierForURL(long, "tenantID"))
}

func TestNewAzureOIDCAuth_InvalidTenantID(t *testing.T) {
	_, err := NewAzureOIDCAuth("has/slash", "client-id", logr.Discard())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenantID")
}

func TestNewAzureOIDCAuth_InvalidClientID(t *testing.T) {
	_, err := NewAzureOIDCAuth("tenant-id", "has?query", logr.Discard())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "clientID")
}

func TestAzureOIDCAuth_TokenMissingKid(t *testing.T) {
	mw, err := NewAzureOIDCAuth("tenant-id", "client-id", logr.Discard())
	require.NoError(t, err)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Create a JWT with no kid in the header using an RSA key.
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"sub": "user"})
	token.Header = map[string]any{"alg": "RS256", "typ": "JWT"} // no kid
	tokenStr, err := token.SignedString(privKey)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "kid")
}

func TestGetClaimsFromContext_SetAndGet(t *testing.T) {
	expected := &AuthClaims{Subject: "u1", TenantID: "t1"}
	ctx := context.WithValue(context.Background(), claimsContextKey, expected)
	got := ClaimsFromContext(ctx)
	require.NotNil(t, got)
	assert.Equal(t, "u1", got.Subject)
	assert.Equal(t, "t1", got.TenantID)
}

// jwksFromKey builds a minimal jwksResponse for a given RSA private key and kid.
func jwksFromKey(privKey *rsa.PrivateKey, kid string) jwksResponse {
	nEncoded := base64.RawURLEncoding.EncodeToString(privKey.N.Bytes())
	eEncoded := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privKey.E)).Bytes())
	return jwksResponse{Keys: []jwksKey{{Kty: "RSA", Kid: kid, N: nEncoded, E: eEncoded}}}
}

func TestJWKSCache_Refresh_Success(t *testing.T) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	const kid = "test-kid"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := jwksFromKey(privKey, kid)
		_ = encodeJSON(w, resp)
	}))
	defer srv.Close()

	cache := &jwksCache{
		jwksURL: srv.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}
	require.NoError(t, cache.refresh())
	assert.NotEmpty(t, cache.keys)
}

func TestJWKSCache_Refresh_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	cache := &jwksCache{jwksURL: srv.URL, client: &http.Client{Timeout: 5 * time.Second}}
	err := cache.refresh()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestJWKSCache_Refresh_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer srv.Close()

	cache := &jwksCache{jwksURL: srv.URL, client: &http.Client{Timeout: 5 * time.Second}}
	err := cache.refresh()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing JWKS")
}

func TestJWKSCache_GetKey_CacheHit(t *testing.T) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	const kid = "cached-kid"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = encodeJSON(w, jwksFromKey(privKey, kid))
	}))
	defer srv.Close()

	cache := &jwksCache{jwksURL: srv.URL, client: &http.Client{Timeout: 5 * time.Second}}
	// First call triggers refresh.
	key, err := cache.getKey(kid)
	require.NoError(t, err)
	require.NotNil(t, key)
	// Second call should hit the cache without another network request.
	key2, err := cache.getKey(kid)
	require.NoError(t, err)
	assert.Equal(t, key, key2)
}

func TestJWKSCache_GetKey_NotFound(t *testing.T) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = encodeJSON(w, jwksFromKey(privKey, "other-kid"))
	}))
	defer srv.Close()

	cache := &jwksCache{jwksURL: srv.URL, client: &http.Client{Timeout: 5 * time.Second}}
	_, err = cache.getKey("missing-kid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestJWKSCache_GetKey_CacheFreshUnknownKid(t *testing.T) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	const existingKid = "existing-kid"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = encodeJSON(w, jwksFromKey(privKey, existingKid))
	}))
	defer srv.Close()

	cache := &jwksCache{jwksURL: srv.URL, client: &http.Client{Timeout: 5 * time.Second}}
	// Warm the cache with a known kid.
	_, err = cache.getKey(existingKid)
	require.NoError(t, err)

	// Now request an unknown kid — cache is fresh so should not re-fetch.
	_, err = cache.getKey("unknown-kid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestJWKSCache_Refresh_NetworkError(t *testing.T) {
	cache := &jwksCache{
		jwksURL: "http://127.0.0.1:1", // nothing listens here
		client:  &http.Client{Timeout: 100 * time.Millisecond},
	}
	err := cache.refresh()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetching JWKS")
}

// encodeJSON writes v as JSON to w.
func encodeJSON(w http.ResponseWriter, v any) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(v)
}
