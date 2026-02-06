//go:build integration

// Package entra provides live integration tests for the Entra ID auth handler.
//
// These tests require real Azure credentials and are NOT run by default.
// To run these tests:
//
//	go test ./pkg/auth/entra/... -tags=integration -v -run "Live"
//
// Required environment variables:
//
//	SCAFCTL_TEST_ENTRA_TENANT_ID   - Azure tenant ID
//	SCAFCTL_TEST_ENTRA_CLIENT_ID   - Azure application (client) ID
//	SCAFCTL_TEST_ENTRA_SCOPE       - OAuth scope to test (e.g., "https://graph.microsoft.com/.default")
//
// Optional environment variables:
//
//	SCAFCTL_TEST_ENTRA_SKIP_INTERACTIVE - Set to "1" to skip interactive tests
//
// NOTE: These tests may require interactive authentication (device code flow).
// They are designed for manual testing and CI environments with pre-authenticated
// credentials.
package entra

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// liveTestConfig holds configuration for live integration tests.
type liveTestConfig struct {
	TenantID        string
	ClientID        string
	Scope           string
	SkipInteractive bool
}

func getLiveTestConfig(t *testing.T) *liveTestConfig {
	t.Helper()

	tenantID := os.Getenv("SCAFCTL_TEST_ENTRA_TENANT_ID")
	clientID := os.Getenv("SCAFCTL_TEST_ENTRA_CLIENT_ID")
	scope := os.Getenv("SCAFCTL_TEST_ENTRA_SCOPE")

	if tenantID == "" || clientID == "" || scope == "" {
		t.Skip("Skipping live integration test: required environment variables not set. " +
			"Set SCAFCTL_TEST_ENTRA_TENANT_ID, SCAFCTL_TEST_ENTRA_CLIENT_ID, and SCAFCTL_TEST_ENTRA_SCOPE")
	}

	return &liveTestConfig{
		TenantID:        tenantID,
		ClientID:        clientID,
		Scope:           scope,
		SkipInteractive: os.Getenv("SCAFCTL_TEST_ENTRA_SKIP_INTERACTIVE") == "1",
	}
}

// TestLive_DeviceCodeLogin tests the full device code flow against real Entra ID.
// This test requires manual interaction to complete authentication.
func TestLive_DeviceCodeLogin(t *testing.T) {
	cfg := getLiveTestConfig(t)
	if cfg.SkipInteractive {
		t.Skip("Skipping interactive test: SCAFCTL_TEST_ENTRA_SKIP_INTERACTIVE=1")
	}

	store := secrets.NewMockStore()
	handler, err := New(
		WithConfig(&Config{
			ClientID: cfg.ClientID,
			TenantID: cfg.TenantID,
		}),
		WithSecretStore(store),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fmt.Println("\n" + repeatString("=", 60))
	fmt.Println("LIVE INTEGRATION TEST: Device Code Login")
	fmt.Println(repeatString("=", 60))
	fmt.Println("This test requires manual authentication.")
	fmt.Println("Please complete the authentication in your browser.")
	fmt.Println()

	result, err := handler.Login(ctx, auth.LoginOptions{
		Timeout: 5 * time.Minute,
		DeviceCodeCallback: func(code, uri, message string) {
			fmt.Println(message)
			fmt.Printf("\nUser Code: %s\n", code)
			fmt.Printf("URL: %s\n\n", uri)
		},
	})

	require.NoError(t, err, "Login should succeed")
	require.NotNil(t, result, "Result should not be nil")
	require.NotNil(t, result.Claims, "Claims should not be nil")

	fmt.Printf("\n✓ Successfully authenticated!\n")
	fmt.Printf("  Identity: %s\n", result.Claims.DisplayIdentity())
	fmt.Printf("  Tenant: %s\n", result.Claims.TenantID)
	fmt.Printf("  Expires: %s\n\n", result.ExpiresAt.Format(time.RFC3339))

	// Verify credentials were stored
	exists, _ := store.Exists(ctx, SecretKeyRefreshToken)
	assert.True(t, exists, "Refresh token should be stored")

	// Now test getting a token
	token, err := handler.GetToken(ctx, auth.TokenOptions{
		Scope: cfg.Scope,
	})
	require.NoError(t, err, "GetToken should succeed")
	require.NotNil(t, token, "Token should not be nil")

	fmt.Printf("✓ Successfully acquired token!\n")
	fmt.Printf("  Scope: %s\n", token.Scope)
	fmt.Printf("  Type: %s\n", token.TokenType)
	fmt.Printf("  Expires: %s\n", token.ExpiresAt.Format(time.RFC3339))
	fmt.Printf("  Token (first 50 chars): %s...\n\n", token.AccessToken[:min(50, len(token.AccessToken))])

	// Test logout
	err = handler.Logout(ctx)
	require.NoError(t, err, "Logout should succeed")

	exists, _ = store.Exists(ctx, SecretKeyRefreshToken)
	assert.False(t, exists, "Refresh token should be cleared after logout")

	fmt.Printf("✓ Successfully logged out!\n")
}

// TestLive_TokenRefresh tests token refresh using existing credentials.
// This test assumes credentials are already stored (e.g., from a previous login).
func TestLive_TokenRefresh(t *testing.T) {
	cfg := getLiveTestConfig(t)

	// Use real secrets store for this test
	store, err := secrets.New()
	if err != nil {
		t.Skipf("Skipping: could not initialize secrets store: %v", err)
	}

	handler, err := New(
		WithConfig(&Config{
			ClientID: cfg.ClientID,
			TenantID: cfg.TenantID,
		}),
		WithSecretStore(store),
	)
	require.NoError(t, err)

	ctx := context.Background()

	// Check if we're authenticated
	status, err := handler.Status(ctx)
	require.NoError(t, err)

	if !status.Authenticated {
		t.Skip("Skipping: not authenticated. Run TestLive_DeviceCodeLogin first or authenticate via CLI")
	}

	fmt.Printf("\n✓ Already authenticated as: %s\n", status.Claims.DisplayIdentity())

	// Test getting a token
	token, err := handler.GetToken(ctx, auth.TokenOptions{
		Scope: cfg.Scope,
	})
	require.NoError(t, err, "GetToken should succeed")
	require.NotNil(t, token, "Token should not be nil")

	fmt.Printf("✓ Token acquired!\n")
	fmt.Printf("  Scope: %s\n", token.Scope)
	fmt.Printf("  Expires: %s\n", token.ExpiresAt.Format(time.RFC3339))

	// Test ForceRefresh
	freshToken, err := handler.GetToken(ctx, auth.TokenOptions{
		Scope:        cfg.Scope,
		ForceRefresh: true,
	})
	require.NoError(t, err, "GetToken with ForceRefresh should succeed")
	require.NotNil(t, freshToken, "Fresh token should not be nil")

	fmt.Printf("✓ Fresh token acquired (ForceRefresh)!\n")
	fmt.Printf("  Expires: %s\n", freshToken.ExpiresAt.Format(time.RFC3339))

	// Tokens might be the same if the server returns the same token
	// but at minimum, the request should succeed
}

// TestLive_TokenMinValidFor tests the MinValidFor token validity check.
func TestLive_TokenMinValidFor(t *testing.T) {
	cfg := getLiveTestConfig(t)

	store, err := secrets.New()
	if err != nil {
		t.Skipf("Skipping: could not initialize secrets store: %v", err)
	}

	handler, err := New(
		WithConfig(&Config{
			ClientID: cfg.ClientID,
			TenantID: cfg.TenantID,
		}),
		WithSecretStore(store),
	)
	require.NoError(t, err)

	ctx := context.Background()

	status, err := handler.Status(ctx)
	require.NoError(t, err)

	if !status.Authenticated {
		t.Skip("Skipping: not authenticated")
	}

	// Request token valid for at least 30 minutes
	token, err := handler.GetToken(ctx, auth.TokenOptions{
		Scope:       cfg.Scope,
		MinValidFor: 30 * time.Minute,
	})
	require.NoError(t, err)
	require.NotNil(t, token)

	// Verify the token meets the validity requirement
	assert.True(t, token.IsValidFor(30*time.Minute),
		"Token should be valid for at least 30 minutes")

	fmt.Printf("✓ Token valid for %.0f minutes (requested 30)\n",
		time.Until(token.ExpiresAt).Minutes())
}

// TestLive_Status tests the status check against real Entra ID.
func TestLive_Status(t *testing.T) {
	cfg := getLiveTestConfig(t)

	store, err := secrets.New()
	if err != nil {
		t.Skipf("Skipping: could not initialize secrets store: %v", err)
	}

	handler, err := New(
		WithConfig(&Config{
			ClientID: cfg.ClientID,
			TenantID: cfg.TenantID,
		}),
		WithSecretStore(store),
	)
	require.NoError(t, err)

	ctx := context.Background()

	status, err := handler.Status(ctx)
	require.NoError(t, err)

	fmt.Printf("\nAuth Status:\n")
	fmt.Printf("  Authenticated: %v\n", status.Authenticated)

	if status.Authenticated {
		fmt.Printf("  Identity: %s\n", status.Claims.DisplayIdentity())
		fmt.Printf("  Tenant: %s\n", status.TenantID)
		fmt.Printf("  Expires: %s\n", status.ExpiresAt.Format(time.RFC3339))
		fmt.Printf("  Last Refresh: %s\n", status.LastRefresh.Format(time.RFC3339))
	}
}

// repeatString repeats a string n times.
func repeatString(s string, count int) string {
	result := ""
	for i := 0; i < count; i++ {
		result += s
	}
	return result
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
