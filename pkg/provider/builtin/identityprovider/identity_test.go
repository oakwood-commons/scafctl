// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package identityprovider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewIdentityProvider(t *testing.T) {
	p := NewIdentityProvider()
	require.NotNil(t, p)

	desc := p.Descriptor()
	assert.Equal(t, ProviderName, desc.Name)
	assert.Equal(t, "Identity", desc.DisplayName)
	assert.Equal(t, "v1", desc.APIVersion)
	assert.Contains(t, desc.Capabilities, provider.CapabilityFrom)
	assert.Equal(t, "security", desc.Category)
}

func TestIdentityProvider_Descriptor(t *testing.T) {
	p := NewIdentityProvider()
	desc := p.Descriptor()

	// Validate schema
	assert.Contains(t, desc.Schema.Properties, "operation")
	assert.Contains(t, desc.Schema.Properties, "handler")

	// Validate output schema
	outputSchema, ok := desc.OutputSchemas[provider.CapabilityFrom]
	require.True(t, ok)
	assert.Contains(t, outputSchema.Properties, "authenticated")
	assert.Contains(t, outputSchema.Properties, "claims")
	assert.Contains(t, outputSchema.Properties, "identityType")

	// Validate examples exist
	assert.NotEmpty(t, desc.Examples)
}

func TestIdentityProvider_ExecuteStatus(t *testing.T) {
	p := NewIdentityProvider()

	tests := []struct {
		name         string
		setupHandler func(*auth.MockHandler)
		handlerName  string
		wantAuth     bool
		wantErr      bool
		checkResult  func(*testing.T, map[string]any)
	}{
		{
			name: "authenticated user",
			setupHandler: func(m *auth.MockHandler) {
				m.StatusResult = &auth.Status{
					Authenticated: true,
					IdentityType:  auth.IdentityTypeUser,
					TenantID:      "test-tenant",
					ExpiresAt:     time.Now().Add(1 * time.Hour),
					Claims: &auth.Claims{
						Email: "user@example.com",
						Name:  "Test User",
					},
				}
			},
			wantAuth: true,
			checkResult: func(t *testing.T, result map[string]any) {
				assert.Equal(t, "user", result["identityType"])
				assert.Equal(t, "test-tenant", result["tenantId"])
				assert.NotEmpty(t, result["expiresAt"])
				assert.NotEmpty(t, result["expiresIn"])
			},
		},
		{
			name: "not authenticated",
			setupHandler: func(m *auth.MockHandler) {
				m.StatusResult = &auth.Status{Authenticated: false}
			},
			wantAuth: false,
		},
		{
			name: "service principal",
			setupHandler: func(m *auth.MockHandler) {
				m.StatusResult = &auth.Status{
					Authenticated: true,
					IdentityType:  auth.IdentityTypeServicePrincipal,
					TenantID:      "sp-tenant",
					ClientID:      "app-id-123",
				}
			},
			wantAuth: true,
			checkResult: func(t *testing.T, result map[string]any) {
				assert.Equal(t, "service-principal", result["identityType"])
				assert.Equal(t, "app-id-123", result["clientId"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mock handler
			mockHandler := auth.NewMockHandler("entra")
			tt.setupHandler(mockHandler)

			// Setup registry with handler
			registry := auth.NewRegistry()
			require.NoError(t, registry.Register(mockHandler))
			ctx := auth.WithRegistry(context.Background(), registry)

			// Execute
			inputs := map[string]any{"operation": "status"}
			if tt.handlerName != "" {
				inputs["handler"] = tt.handlerName
			}

			output, err := p.Execute(ctx, inputs)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, output)

			result, ok := output.Data.(map[string]any)
			require.True(t, ok)

			assert.Equal(t, "status", result["operation"])
			assert.Equal(t, "entra", result["handler"])
			assert.Equal(t, tt.wantAuth, result["authenticated"])

			if tt.checkResult != nil {
				tt.checkResult(t, result)
			}
		})
	}
}

func TestIdentityProvider_ExecuteClaims(t *testing.T) {
	p := NewIdentityProvider()

	tests := []struct {
		name         string
		claims       *auth.Claims
		wantAuth     bool
		wantWarnings bool
	}{
		{
			name: "full claims",
			claims: &auth.Claims{
				Issuer:    "https://login.example.com",
				Subject:   "user-123",
				TenantID:  "tenant-abc",
				ObjectID:  "obj-456",
				Email:     "user@example.com",
				Name:      "Test User",
				Username:  "testuser",
				IssuedAt:  time.Now().Add(-1 * time.Hour),
				ExpiresAt: time.Now().Add(1 * time.Hour),
			},
			wantAuth: true,
		},
		{
			name:         "not authenticated",
			claims:       nil,
			wantAuth:     false,
			wantWarnings: true,
		},
		{
			name: "partial claims",
			claims: &auth.Claims{
				Email: "user@example.com",
			},
			wantAuth: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockHandler := auth.NewMockHandler("entra")
			if tt.wantAuth {
				mockHandler.StatusResult = &auth.Status{
					Authenticated: true,
					IdentityType:  auth.IdentityTypeUser,
					Claims:        tt.claims,
				}
			} else {
				mockHandler.StatusResult = &auth.Status{Authenticated: false}
			}

			registry := auth.NewRegistry()
			require.NoError(t, registry.Register(mockHandler))
			ctx := auth.WithRegistry(context.Background(), registry)

			output, err := p.Execute(ctx, map[string]any{"operation": "claims"})
			require.NoError(t, err)
			require.NotNil(t, output)

			result, ok := output.Data.(map[string]any)
			require.True(t, ok)

			assert.Equal(t, "claims", result["operation"])
			assert.Equal(t, tt.wantAuth, result["authenticated"])

			if tt.wantWarnings {
				assert.NotEmpty(t, output.Warnings)
			}

			if tt.wantAuth && tt.claims != nil {
				claims, ok := result["claims"].(map[string]any)
				require.True(t, ok)

				if tt.claims.Email != "" {
					assert.Equal(t, tt.claims.Email, claims["email"])
				}
				if tt.claims.Name != "" {
					assert.Equal(t, tt.claims.Name, claims["name"])
				}
			}
		})
	}
}

func TestIdentityProvider_ExecuteList(t *testing.T) {
	p := NewIdentityProvider()

	// Setup multiple handlers
	entraHandler := auth.NewMockHandler("entra")
	entraHandler.DisplayNameValue = "Microsoft Entra ID"
	entraHandler.CapabilitiesValue = []auth.Capability{auth.CapScopesOnLogin, auth.CapScopesOnTokenRequest, auth.CapTenantID}
	entraHandler.StatusResult = &auth.Status{
		Authenticated: true,
		Claims:        &auth.Claims{Email: "user@example.com"},
	}

	githubHandler := auth.NewMockHandler("github")
	githubHandler.DisplayNameValue = "GitHub"
	githubHandler.CapabilitiesValue = []auth.Capability{auth.CapScopesOnLogin, auth.CapHostname}
	githubHandler.StatusResult = &auth.Status{Authenticated: false}

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(entraHandler))
	require.NoError(t, registry.Register(githubHandler))
	ctx := auth.WithRegistry(context.Background(), registry)

	output, err := p.Execute(ctx, map[string]any{"operation": "list"})
	require.NoError(t, err)
	require.NotNil(t, output)

	result, ok := output.Data.(map[string]any)
	require.True(t, ok)

	assert.Equal(t, "list", result["operation"])
	assert.Equal(t, 2, result["count"])

	handlers, ok := result["handlers"].([]map[string]any)
	require.True(t, ok)
	assert.Len(t, handlers, 2)

	// Find entra handler in list
	var entraInfo map[string]any
	for _, h := range handlers {
		if h["name"] == "entra" {
			entraInfo = h
			break
		}
	}
	require.NotNil(t, entraInfo)
	assert.Equal(t, "Microsoft Entra ID", entraInfo["displayName"])
	assert.Equal(t, true, entraInfo["authenticated"])
	assert.Equal(t, "user@example.com", entraInfo["identity"])

	// Verify capabilities are included in handler info
	entraCapabilities, ok := entraInfo["capabilities"].([]string)
	require.True(t, ok)
	assert.Contains(t, entraCapabilities, string(auth.CapScopesOnTokenRequest))
	assert.Contains(t, entraCapabilities, string(auth.CapTenantID))
}

func TestIdentityProvider_ExecuteDryRun(t *testing.T) {
	p := NewIdentityProvider()

	// No registry needed for dry run
	ctx := provider.WithDryRun(context.Background(), true)

	tests := []struct {
		operation string
	}{
		{"status"},
		{"claims"},
		{"list"},
	}

	for _, tt := range tests {
		t.Run(tt.operation, func(t *testing.T) {
			output, err := p.Execute(ctx, map[string]any{"operation": tt.operation})
			require.NoError(t, err)
			require.NotNil(t, output)

			result, ok := output.Data.(map[string]any)
			require.True(t, ok)
			assert.Equal(t, tt.operation, result["operation"])

			// Check dry-run metadata
			assert.True(t, output.Metadata["dryRun"].(bool))
		})
	}
}

func TestIdentityProvider_NoRegistry(t *testing.T) {
	p := NewIdentityProvider()

	// Context without registry
	ctx := context.Background()

	_, err := p.Execute(ctx, map[string]any{"operation": "status"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no auth registry")
}

func TestIdentityProvider_HandlerNotFound(t *testing.T) {
	p := NewIdentityProvider()

	registry := auth.NewRegistry()
	ctx := auth.WithRegistry(context.Background(), registry)

	_, err := p.Execute(ctx, map[string]any{
		"operation": "status",
		"handler":   "nonexistent",
	})
	assert.Error(t, err)
}

func TestIdentityProvider_InvalidOperation(t *testing.T) {
	p := NewIdentityProvider()

	mockHandler := auth.NewMockHandler("entra")
	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mockHandler))
	ctx := auth.WithRegistry(context.Background(), registry)

	_, err := p.Execute(ctx, map[string]any{"operation": "invalid"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported operation")
}

func TestIdentityProvider_InvalidInput(t *testing.T) {
	p := NewIdentityProvider()

	tests := []struct {
		name  string
		input any
	}{
		{"not a map", "invalid"},
		{"missing operation", map[string]any{}},
		{"operation not string", map[string]any{"operation": 123}},
	}

	registry := auth.NewRegistry()
	ctx := auth.WithRegistry(context.Background(), registry)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := p.Execute(ctx, tt.input)
			assert.Error(t, err)
		})
	}
}

func TestIdentityProvider_AutoSelectAuthenticatedHandler(t *testing.T) {
	p := NewIdentityProvider()

	// First handler not authenticated
	handler1 := auth.NewMockHandler("handler1")
	handler1.StatusResult = &auth.Status{Authenticated: false}

	// Second handler authenticated
	handler2 := auth.NewMockHandler("handler2")
	handler2.StatusResult = &auth.Status{
		Authenticated: true,
		IdentityType:  auth.IdentityTypeUser,
		Claims:        &auth.Claims{Email: "test@example.com"},
	}

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(handler1))
	require.NoError(t, registry.Register(handler2))
	ctx := auth.WithRegistry(context.Background(), registry)

	// Should auto-select handler2 since it's authenticated
	output, err := p.Execute(ctx, map[string]any{"operation": "status"})
	require.NoError(t, err)

	result, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "handler2", result["handler"])
	assert.True(t, result["authenticated"].(bool))
}

func TestFlowsToStrings(t *testing.T) {
	flows := []auth.Flow{auth.FlowDeviceCode, auth.FlowInteractive, auth.FlowServicePrincipal}
	result := flowsToStrings(flows)

	assert.Len(t, result, 3)
	assert.Contains(t, result, "device_code")
	assert.Contains(t, result, "interactive")
	assert.Contains(t, result, "service_principal")
}

func TestValidateDescriptor(t *testing.T) {
	p := NewIdentityProvider()
	err := provider.ValidateDescriptor(p.Descriptor())
	assert.NoError(t, err)
}

// groupCapableMockHandler wraps auth.MockHandler and also implements auth.GroupsProvider.
type groupCapableMockHandler struct {
	*auth.MockHandler
	groups []string
	err    error
}

func (g *groupCapableMockHandler) GetGroups(_ context.Context) ([]string, error) {
	return g.groups, g.err
}

func TestIdentityProvider_ExecuteGroups_Success(t *testing.T) {
	p := NewIdentityProvider()

	mock := &groupCapableMockHandler{
		MockHandler: auth.NewMockHandler("entra"),
		groups:      []string{"group-aaa", "group-bbb", "group-ccc"},
	}
	mock.StatusResult = &auth.Status{Authenticated: true, IdentityType: auth.IdentityTypeUser}

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mock))
	ctx := auth.WithRegistry(context.Background(), registry)

	output, err := p.Execute(ctx, map[string]any{
		"operation": "groups",
		"handler":   "entra",
	})
	require.NoError(t, err)
	require.NotNil(t, output)

	result, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "groups", result["operation"])
	assert.Equal(t, "entra", result["handler"])
	assert.Equal(t, 3, result["count"])

	groups, ok := result["groups"].([]string)
	require.True(t, ok)
	assert.Equal(t, []string{"group-aaa", "group-bbb", "group-ccc"}, groups)
}

func TestIdentityProvider_ExecuteGroups_Empty(t *testing.T) {
	p := NewIdentityProvider()

	mock := &groupCapableMockHandler{
		MockHandler: auth.NewMockHandler("entra"),
		groups:      []string{},
	}
	mock.StatusResult = &auth.Status{Authenticated: true}

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mock))
	ctx := auth.WithRegistry(context.Background(), registry)

	output, err := p.Execute(ctx, map[string]any{"operation": "groups"})
	require.NoError(t, err)

	result := output.Data.(map[string]any)
	assert.Equal(t, 0, result["count"])

	groups, ok := result["groups"].([]string)
	require.True(t, ok)
	assert.Empty(t, groups)
}

func TestIdentityProvider_ExecuteGroups_HandlerError(t *testing.T) {
	p := NewIdentityProvider()

	mock := &groupCapableMockHandler{
		MockHandler: auth.NewMockHandler("entra"),
		err:         fmt.Errorf("graph API unavailable"),
	}

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mock))
	ctx := auth.WithRegistry(context.Background(), registry)

	_, err := p.Execute(ctx, map[string]any{"operation": "groups"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to retrieve group memberships")
	assert.Contains(t, err.Error(), "graph API unavailable")
}

func TestIdentityProvider_ExecuteGroups_NotSupported(t *testing.T) {
	p := NewIdentityProvider()

	// Regular MockHandler does NOT implement GroupsProvider.
	mockHandler := auth.NewMockHandler("github")
	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mockHandler))
	ctx := auth.WithRegistry(context.Background(), registry)

	_, err := p.Execute(ctx, map[string]any{"operation": "groups"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support group membership queries")
}

func TestIdentityProvider_ExecuteGroups_DryRun(t *testing.T) {
	p := NewIdentityProvider()
	ctx := provider.WithDryRun(context.Background(), true)

	output, err := p.Execute(ctx, map[string]any{"operation": "groups"})
	require.NoError(t, err)
	require.NotNil(t, output)

	result := output.Data.(map[string]any)
	assert.Equal(t, "groups", result["operation"])
	assert.Equal(t, 0, result["count"])
	assert.True(t, output.Metadata["dryRun"].(bool))
}

// --- Scope feature tests ---

// testBuildJWT constructs a minimal unsigned JWT from a claims map for testing.
func testBuildJWT(t *testing.T, payload map[string]any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	encodedBody := base64.RawURLEncoding.EncodeToString(body)
	return header + "." + encodedBody + ".sig"
}

func TestIdentityProvider_ScopedClaims_Success(t *testing.T) {
	p := NewIdentityProvider()

	accessToken := testBuildJWT(t, map[string]any{
		"iss":                "https://login.microsoftonline.com/tenant-123/v2.0",
		"sub":                "scoped-subject",
		"aud":                "api://my-app",
		"tid":                "tenant-123",
		"oid":                "obj-789",
		"email":              "scoped@example.com",
		"preferred_username": "scoped@example.com",
		"name":               "Scoped User",
		"iat":                1700000000,
		"exp":                1700003600,
	})

	mockHandler := auth.NewMockHandler("entra")
	mockHandler.SetToken(&auth.Token{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Scope:       "api://my-app/.default",
		Flow:        auth.FlowDeviceCode,
		SessionID:   "session-123",
	})

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mockHandler))
	ctx := auth.WithRegistry(context.Background(), registry)

	output, err := p.Execute(ctx, map[string]any{
		"operation": "claims",
		"scope":     "api://my-app/.default",
	})
	require.NoError(t, err)
	require.NotNil(t, output)

	result, ok := output.Data.(map[string]any)
	require.True(t, ok)

	assert.Equal(t, "claims", result["operation"])
	assert.Equal(t, "entra", result["handler"])
	assert.True(t, result["authenticated"].(bool))
	assert.True(t, result["scopedToken"].(bool))
	assert.Equal(t, "api://my-app/.default", result["tokenScope"])

	claims, ok := result["claims"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "scoped@example.com", claims["email"])
	assert.Equal(t, "Scoped User", claims["name"])
	assert.Equal(t, "tenant-123", claims["tenantId"])
	assert.Equal(t, "scoped-subject", claims["subject"])
	assert.Equal(t, "user", result["identityType"])
	assert.NotEmpty(t, claims["displayIdentity"])

	// Verify GetToken was called with the correct scope
	require.Len(t, mockHandler.GetTokenCalls, 1)
	assert.Equal(t, "api://my-app/.default", mockHandler.GetTokenCalls[0].Scope)

	// No warnings for a valid JWT
	assert.Empty(t, output.Warnings)
}

func TestIdentityProvider_ScopedClaims_OpaqueToken(t *testing.T) {
	p := NewIdentityProvider()

	mockHandler := auth.NewMockHandler("entra")
	mockHandler.SetToken(&auth.Token{
		AccessToken: "opaque-encrypted-token-not-a-jwt",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Scope:       "https://graph.microsoft.com/.default",
	})

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mockHandler))
	ctx := auth.WithRegistry(context.Background(), registry)

	output, err := p.Execute(ctx, map[string]any{
		"operation": "claims",
		"scope":     "https://graph.microsoft.com/.default",
	})
	require.NoError(t, err)
	require.NotNil(t, output)

	result := output.Data.(map[string]any)
	assert.True(t, result["scopedToken"].(bool))
	assert.Nil(t, result["claims"])

	// Should have a warning about opaque token
	require.NotEmpty(t, output.Warnings)
	assert.Contains(t, output.Warnings[0], "not a decodable JWT")

	// Should still include token expiry metadata
	assert.NotEmpty(t, result["expiresAt"])
}

func TestIdentityProvider_ScopedClaims_TokenError(t *testing.T) {
	p := NewIdentityProvider()

	mockHandler := auth.NewMockHandler("entra")
	mockHandler.SetTokenError(fmt.Errorf("token acquisition failed: consent required"))

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mockHandler))
	ctx := auth.WithRegistry(context.Background(), registry)

	_, err := p.Execute(ctx, map[string]any{
		"operation": "claims",
		"scope":     "api://my-app/.default",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to mint scoped token")
	assert.Contains(t, err.Error(), "consent required")
}

func TestIdentityProvider_ScopedStatus_Success(t *testing.T) {
	p := NewIdentityProvider()

	accessToken := testBuildJWT(t, map[string]any{
		"iss":   "https://login.microsoftonline.com/tenant-abc/v2.0",
		"sub":   "status-subject",
		"tid":   "tenant-abc",
		"email": "user@test.com",
		"name":  "Status User",
		"iat":   1700000000,
		"exp":   1700003600,
	})

	mockHandler := auth.NewMockHandler("entra")
	mockHandler.SetToken(&auth.Token{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(30 * time.Minute),
		Scope:       "https://management.azure.com/.default",
		Flow:        auth.FlowDeviceCode,
		SessionID:   "sess-456",
	})

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mockHandler))
	ctx := auth.WithRegistry(context.Background(), registry)

	output, err := p.Execute(ctx, map[string]any{
		"operation": "status",
		"scope":     "https://management.azure.com/.default",
		"handler":   "entra",
	})
	require.NoError(t, err)
	require.NotNil(t, output)

	result := output.Data.(map[string]any)
	assert.Equal(t, "status", result["operation"])
	assert.Equal(t, "entra", result["handler"])
	assert.True(t, result["authenticated"].(bool))
	assert.True(t, result["scopedToken"].(bool))
	assert.Equal(t, "https://management.azure.com/.default", result["tokenScope"])
	assert.Equal(t, "Bearer", result["tokenType"])
	assert.Equal(t, "device_code", result["flow"])
	assert.Equal(t, "sess-456", result["sessionId"])
	assert.NotEmpty(t, result["expiresAt"])
	assert.NotEmpty(t, result["expiresIn"])
	assert.Equal(t, "tenant-abc", result["tenantId"])
	assert.Equal(t, "user", result["identityType"])

	// No warnings for a valid JWT
	assert.Empty(t, output.Warnings)
}

func TestIdentityProvider_ScopedStatus_OpaqueToken(t *testing.T) {
	p := NewIdentityProvider()

	mockHandler := auth.NewMockHandler("entra")
	mockHandler.SetToken(&auth.Token{
		AccessToken: "not.a.jwt.with.four.parts",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Flow:        auth.FlowServicePrincipal,
	})

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mockHandler))
	ctx := auth.WithRegistry(context.Background(), registry)

	output, err := p.Execute(ctx, map[string]any{
		"operation": "status",
		"scope":     "api://my-app/.default",
	})
	require.NoError(t, err)
	require.NotNil(t, output)

	result := output.Data.(map[string]any)
	assert.True(t, result["scopedToken"].(bool))
	assert.True(t, result["authenticated"].(bool))

	// Should have a warning about opaque token
	require.NotEmpty(t, output.Warnings)
	assert.Contains(t, output.Warnings[0], "not a decodable JWT")

	// identityType and tenantId should NOT be present (couldn't parse JWT)
	_, hasTenant := result["tenantId"]
	assert.False(t, hasTenant)
}

func TestIdentityProvider_ScopedStatus_TokenError(t *testing.T) {
	p := NewIdentityProvider()

	mockHandler := auth.NewMockHandler("entra")
	mockHandler.SetTokenError(auth.ErrNotAuthenticated)

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mockHandler))
	ctx := auth.WithRegistry(context.Background(), registry)

	_, err := p.Execute(ctx, map[string]any{
		"operation": "status",
		"scope":     "api://my-app/.default",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to mint scoped token")
}

func TestIdentityProvider_ScopeNotSupportedForGroups(t *testing.T) {
	p := NewIdentityProvider()

	mockHandler := auth.NewMockHandler("entra")
	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mockHandler))
	ctx := auth.WithRegistry(context.Background(), registry)

	_, err := p.Execute(ctx, map[string]any{
		"operation": "groups",
		"scope":     "api://my-app/.default",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scope is not supported for the \"groups\" operation")
}

func TestIdentityProvider_ScopeNotSupportedForList(t *testing.T) {
	p := NewIdentityProvider()

	mockHandler := auth.NewMockHandler("entra")
	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mockHandler))
	ctx := auth.WithRegistry(context.Background(), registry)

	_, err := p.Execute(ctx, map[string]any{
		"operation": "list",
		"scope":     "api://my-app/.default",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scope is not supported for the \"list\" operation")
}

func TestIdentityProvider_ScopedDryRun_Claims(t *testing.T) {
	p := NewIdentityProvider()
	ctx := provider.WithDryRun(context.Background(), true)

	output, err := p.Execute(ctx, map[string]any{
		"operation": "claims",
		"scope":     "api://my-app/.default",
	})
	require.NoError(t, err)
	require.NotNil(t, output)

	result := output.Data.(map[string]any)
	assert.Equal(t, "claims", result["operation"])
	assert.True(t, result["scopedToken"].(bool))
	assert.Equal(t, "api://my-app/.default", result["tokenScope"])
	assert.True(t, output.Metadata["dryRun"].(bool))
}

func TestIdentityProvider_ScopedDryRun_Status(t *testing.T) {
	p := NewIdentityProvider()
	ctx := provider.WithDryRun(context.Background(), true)

	output, err := p.Execute(ctx, map[string]any{
		"operation": "status",
		"scope":     "https://management.azure.com/.default",
	})
	require.NoError(t, err)
	require.NotNil(t, output)

	result := output.Data.(map[string]any)
	assert.Equal(t, "status", result["operation"])
	assert.True(t, result["scopedToken"].(bool))
	assert.Equal(t, "https://management.azure.com/.default", result["tokenScope"])
	assert.True(t, output.Metadata["dryRun"].(bool))
}

func TestIdentityProvider_ScopedClaims_ServicePrincipalDetection(t *testing.T) {
	p := NewIdentityProvider()

	// Service principal tokens typically have no name/email/username
	accessToken := testBuildJWT(t, map[string]any{
		"iss":   "https://login.microsoftonline.com/tenant-sp/v2.0",
		"sub":   "sp-subject",
		"aud":   "api://target-app",
		"tid":   "tenant-sp",
		"oid":   "sp-object-id",
		"appid": "sp-app-id",
		"iat":   1700000000,
		"exp":   1700003600,
	})

	mockHandler := auth.NewMockHandler("entra")
	mockHandler.SetToken(&auth.Token{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	})

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mockHandler))
	ctx := auth.WithRegistry(context.Background(), registry)

	output, err := p.Execute(ctx, map[string]any{
		"operation": "claims",
		"scope":     "api://target-app/.default",
	})
	require.NoError(t, err)

	result := output.Data.(map[string]any)
	assert.Equal(t, "service-principal", result["identityType"])
}

func TestIdentityProvider_WithoutScope_UsesStoredMetadata(t *testing.T) {
	// Verify that claims without scope still uses Status() not GetToken()
	p := NewIdentityProvider()

	mockHandler := auth.NewMockHandler("entra")
	mockHandler.StatusResult = &auth.Status{
		Authenticated: true,
		IdentityType:  auth.IdentityTypeUser,
		Claims: &auth.Claims{
			Email: "stored@example.com",
			Name:  "Stored User",
		},
	}

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mockHandler))
	ctx := auth.WithRegistry(context.Background(), registry)

	output, err := p.Execute(ctx, map[string]any{"operation": "claims"})
	require.NoError(t, err)

	result := output.Data.(map[string]any)
	// Should NOT have scopedToken
	_, hasScopedToken := result["scopedToken"]
	assert.False(t, hasScopedToken)

	claims := result["claims"].(map[string]any)
	assert.Equal(t, "stored@example.com", claims["email"])

	// GetToken should NOT have been called
	assert.Empty(t, mockHandler.GetTokenCalls)
}

func TestIdentityProvider_ScopedClaims_SpecificHandler(t *testing.T) {
	p := NewIdentityProvider()

	accessToken := testBuildJWT(t, map[string]any{
		"sub":   "handler2-subject",
		"email": "handler2@example.com",
	})

	handler1 := auth.NewMockHandler("handler1")
	handler2 := auth.NewMockHandler("handler2")
	handler2.SetToken(&auth.Token{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	})

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(handler1))
	require.NoError(t, registry.Register(handler2))
	ctx := auth.WithRegistry(context.Background(), registry)

	output, err := p.Execute(ctx, map[string]any{
		"operation": "claims",
		"scope":     "api://my-app/.default",
		"handler":   "handler2",
	})
	require.NoError(t, err)

	result := output.Data.(map[string]any)
	assert.Equal(t, "handler2", result["handler"])

	claims := result["claims"].(map[string]any)
	assert.Equal(t, "handler2@example.com", claims["email"])

	// Verify handler2 received the call, handler1 didn't
	assert.Len(t, handler2.GetTokenCalls, 1)
	assert.Empty(t, handler1.GetTokenCalls)
}

func TestIdentityProvider_Descriptor_HasScopeField(t *testing.T) {
	p := NewIdentityProvider()
	desc := p.Descriptor()

	// Verify scope is in the input schema
	assert.Contains(t, desc.Schema.Properties, "scope")

	// Verify new output fields exist
	outputSchema, ok := desc.OutputSchemas[provider.CapabilityFrom]
	require.True(t, ok)
	assert.Contains(t, outputSchema.Properties, "scopedToken")
	assert.Contains(t, outputSchema.Properties, "tokenScope")
	assert.Contains(t, outputSchema.Properties, "tokenType")
	assert.Contains(t, outputSchema.Properties, "flow")
	assert.Contains(t, outputSchema.Properties, "sessionId")
}
