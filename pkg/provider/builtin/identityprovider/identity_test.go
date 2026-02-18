// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package identityprovider

import (
	"context"
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
