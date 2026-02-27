// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gcp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	"github.com/golang-jwt/jwt/v5"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetServiceAccountKey_NoEnv(t *testing.T) {
	t.Setenv(EnvGoogleApplicationCredentials, "")

	key, err := GetServiceAccountKey()
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoServiceAccountConfigured))
	assert.Nil(t, key)
}

func TestGetServiceAccountKey_FileNotFound(t *testing.T) {
	t.Setenv(EnvGoogleApplicationCredentials, "/nonexistent/path/key.json")

	key, err := GetServiceAccountKey()
	require.Error(t, err)
	assert.Nil(t, key)
}

func TestGetServiceAccountKey_InvalidJSON(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "key.json")
	err := os.WriteFile(tmpFile, []byte("not-valid-json"), 0o600)
	require.NoError(t, err)

	t.Setenv(EnvGoogleApplicationCredentials, tmpFile)

	key, err := GetServiceAccountKey()
	require.Error(t, err)
	assert.Nil(t, key)
}

func TestGetServiceAccountKey_WrongType(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "key.json")
	keyData := map[string]string{
		"type":         "authorized_user",
		"client_email": "user@example.com",
	}
	data, err := json.Marshal(keyData)
	require.NoError(t, err)
	err = os.WriteFile(tmpFile, data, 0o600)
	require.NoError(t, err)

	t.Setenv(EnvGoogleApplicationCredentials, tmpFile)

	key, err := GetServiceAccountKey()
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoServiceAccountConfigured))
	assert.Nil(t, key) // not a service_account type
}

func TestGetServiceAccountKey_Valid(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "key.json")
	keyData := ServiceAccountKey{
		Type:         "service_account",
		ProjectID:    "my-project",
		PrivateKeyID: "key-id",
		ClientEmail:  "sa@my-project.iam.gserviceaccount.com",
		ClientID:     "12345",
		TokenURI:     "https://oauth2.googleapis.com/token",
		PrivateKey:   "-----BEGIN RSA PRIVATE KEY-----\nfake\n-----END RSA PRIVATE KEY-----\n",
	}
	data, err := json.Marshal(keyData)
	require.NoError(t, err)
	err = os.WriteFile(tmpFile, data, 0o600)
	require.NoError(t, err)

	t.Setenv(EnvGoogleApplicationCredentials, tmpFile)

	key, err := GetServiceAccountKey()
	require.NoError(t, err)
	require.NotNil(t, key)
	assert.Equal(t, "service_account", key.Type)
	assert.Equal(t, "my-project", key.ProjectID)
	assert.Equal(t, "sa@my-project.iam.gserviceaccount.com", key.ClientEmail)
}

func TestHasServiceAccountCredentials(t *testing.T) {
	t.Run("no credentials", func(t *testing.T) {
		t.Setenv(EnvGoogleApplicationCredentials, "")
		assert.False(t, HasServiceAccountCredentials())
	})

	t.Run("valid service account", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "key.json")
		keyData := ServiceAccountKey{
			Type:        "service_account",
			ClientEmail: "sa@project.iam.gserviceaccount.com",
			PrivateKey:  "fake-key",
		}
		data, _ := json.Marshal(keyData)
		_ = os.WriteFile(tmpFile, data, 0o600)

		t.Setenv(EnvGoogleApplicationCredentials, tmpFile)
		assert.True(t, HasServiceAccountCredentials())
	})
}

// generateTestRSAKey creates a PEM-encoded RSA private key for testing.
func generateTestRSAKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return key, string(pemBytes)
}

// newTestHandler creates a Handler with the given mock HTTP client for testing.
func newTestHandler(t *testing.T, mock *MockHTTPClient) *Handler {
	t.Helper()
	h, err := New(
		WithHTTPClient(mock),
		WithSecretStore(secrets.NewMockStore()),
		WithLogger(logr.Discard()),
	)
	require.NoError(t, err)
	return h
}

func TestAcquireServiceAccountToken_Success(t *testing.T) {
	rsaKey, pemKey := generateTestRSAKey(t)

	mockClient := NewMockHTTPClient().AddResponse(200, TokenResponse{
		AccessToken: "mock-access-token",
		TokenType:   "Bearer",
		ExpiresIn:   3600,
		Scope:       "https://www.googleapis.com/auth/cloud-platform",
	})

	h := newTestHandler(t, mockClient)
	ctx := context.Background()

	saKey := &ServiceAccountKey{
		Type:         "service_account",
		ClientEmail:  "test-sa@my-project.iam.gserviceaccount.com",
		PrivateKeyID: "key-123",
		PrivateKey:   pemKey,
		TokenURI:     tokenEndpoint,
	}

	token, err := h.acquireServiceAccountToken(ctx, saKey, "https://www.googleapis.com/auth/cloud-platform")
	require.NoError(t, err)
	require.NotNil(t, token)

	assert.Equal(t, "mock-access-token", token.AccessToken)
	assert.Equal(t, "Bearer", token.TokenType)
	assert.Equal(t, "https://www.googleapis.com/auth/cloud-platform", token.Scope)
	assert.False(t, token.ExpiresAt.IsZero())

	// Verify the JWT assertion that was sent to the mock
	require.Len(t, mockClient.Requests, 1)
	req := mockClient.Requests[0]
	assert.Equal(t, "POST", req.Method)
	assert.Equal(t, tokenEndpoint, req.Endpoint)
	assert.Equal(t, "urn:ietf:params:oauth:grant-type:jwt-bearer", req.Data.Get("grant_type"))

	// Parse and validate the JWT assertion
	assertion := req.Data.Get("assertion")
	require.NotEmpty(t, assertion)

	parsed, err := jwt.Parse(assertion, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return &rsaKey.PublicKey, nil
	})
	require.NoError(t, err)
	require.True(t, parsed.Valid)

	claims, ok := parsed.Claims.(jwt.MapClaims)
	require.True(t, ok)
	assert.Equal(t, "test-sa@my-project.iam.gserviceaccount.com", claims["iss"])
	assert.Equal(t, "test-sa@my-project.iam.gserviceaccount.com", claims["sub"])
	assert.Equal(t, tokenEndpoint, claims["aud"])
	assert.Equal(t, "https://www.googleapis.com/auth/cloud-platform", claims["scope"])

	// Verify kid header
	assert.Equal(t, "key-123", parsed.Header["kid"])
}

func TestAcquireServiceAccountToken_InvalidPEM(t *testing.T) {
	mockClient := NewMockHTTPClient()
	h := newTestHandler(t, mockClient)
	ctx := context.Background()

	saKey := &ServiceAccountKey{
		Type:         "service_account",
		ClientEmail:  "test-sa@project.iam.gserviceaccount.com",
		PrivateKeyID: "key-123",
		PrivateKey:   "not-a-valid-pem-key",
	}

	token, err := h.acquireServiceAccountToken(ctx, saKey, "https://www.googleapis.com/auth/cloud-platform")
	require.Error(t, err)
	assert.Nil(t, token)
	assert.Contains(t, err.Error(), "failed to parse service account private key")

	// No HTTP request should have been made
	assert.Empty(t, mockClient.Requests)
}

func TestAcquireServiceAccountToken_HTTPError(t *testing.T) {
	_, pemKey := generateTestRSAKey(t)

	mockClient := NewMockHTTPClient().AddError(fmt.Errorf("connection refused"))
	h := newTestHandler(t, mockClient)
	ctx := context.Background()

	saKey := &ServiceAccountKey{
		Type:         "service_account",
		ClientEmail:  "test-sa@project.iam.gserviceaccount.com",
		PrivateKeyID: "key-123",
		PrivateKey:   pemKey,
	}

	token, err := h.acquireServiceAccountToken(ctx, saKey, "https://www.googleapis.com/auth/cloud-platform")
	require.Error(t, err)
	assert.Nil(t, token)
	assert.Contains(t, err.Error(), "token request failed")
}

func TestAcquireServiceAccountToken_ErrorResponse(t *testing.T) {
	_, pemKey := generateTestRSAKey(t)

	mockClient := NewMockHTTPClient().AddResponse(400, TokenErrorResponse{
		Error:            "invalid_grant",
		ErrorDescription: "key has been revoked",
	})
	h := newTestHandler(t, mockClient)
	ctx := context.Background()

	saKey := &ServiceAccountKey{
		Type:         "service_account",
		ClientEmail:  "test-sa@project.iam.gserviceaccount.com",
		PrivateKeyID: "key-123",
		PrivateKey:   pemKey,
	}

	token, err := h.acquireServiceAccountToken(ctx, saKey, "https://www.googleapis.com/auth/cloud-platform")
	require.Error(t, err)
	assert.Nil(t, token)
	assert.Contains(t, err.Error(), "invalid_grant")
	assert.Contains(t, err.Error(), "key has been revoked")
}

func TestAcquireServiceAccountToken_MultipleScopes(t *testing.T) {
	rsaKey, pemKey := generateTestRSAKey(t)

	mockClient := NewMockHTTPClient().AddResponse(200, TokenResponse{
		AccessToken: "multi-scope-token",
		TokenType:   "Bearer",
		ExpiresIn:   3600,
	})
	h := newTestHandler(t, mockClient)
	ctx := context.Background()

	saKey := &ServiceAccountKey{
		Type:         "service_account",
		ClientEmail:  "test-sa@project.iam.gserviceaccount.com",
		PrivateKeyID: "key-456",
		PrivateKey:   pemKey,
	}

	scope := "https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/compute"
	token, err := h.acquireServiceAccountToken(ctx, saKey, scope)
	require.NoError(t, err)
	assert.Equal(t, "multi-scope-token", token.AccessToken)

	// Verify the scope claim in the JWT
	assertion := mockClient.Requests[0].Data.Get("assertion")
	parsed, err := jwt.Parse(assertion, func(token *jwt.Token) (any, error) {
		return &rsaKey.PublicKey, nil
	})
	require.NoError(t, err)
	claims := parsed.Claims.(jwt.MapClaims)
	assert.Equal(t, scope, claims["scope"])
}
