// Package identityprovider provides authentication identity information from auth handlers.
// It exposes non-sensitive identity data like claims, authentication status, and identity type.
// It never exposes tokens or other secrets.
package identityprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/ptrs"
)

const (
	// ProviderName is the unique identifier for this provider.
	ProviderName = "identity"
	// Version is the semantic version of this provider.
	Version = "1.0.0"
)

// IdentityProvider provides authentication identity information from auth handlers.
// It exposes claims, authentication status, and identity type without revealing tokens or secrets.
type IdentityProvider struct {
	descriptor *provider.Descriptor
}

// NewIdentityProvider creates a new identity provider instance.
func NewIdentityProvider() *IdentityProvider {
	version, _ := semver.NewVersion(Version)

	return &IdentityProvider{
		descriptor: &provider.Descriptor{
			Name:         ProviderName,
			DisplayName:  "Identity",
			APIVersion:   "v1",
			Description:  "Provides authentication identity information (claims, status, identity type) from auth handlers without exposing tokens or secrets",
			Version:      version,
			Category:     "security",
			MockBehavior: "Returns mock identity information without accessing actual authentication state",
			Capabilities: []provider.Capability{
				provider.CapabilityFrom,
			},
			Tags: []string{"auth", "identity", "claims", "security"},
			Schema: schemahelper.ObjectSchema([]string{"operation"}, map[string]*jsonschema.Schema{
				"operation": schemahelper.StringProp("Operation to perform: 'status' to get auth status, 'claims' to get identity claims, 'list' to list available handlers",
					schemahelper.WithEnum("status", "claims", "list"),
					schemahelper.WithExample("claims"),
					schemahelper.WithMaxLength(*ptrs.IntPtr(10))),
				"handler": schemahelper.StringProp("Name of the auth handler to query (e.g., 'entra'). If not specified, uses the first available authenticated handler.",
					schemahelper.WithMaxLength(*ptrs.IntPtr(50)),
					schemahelper.WithPattern(`^[a-z][a-z0-9-]*$`),
					schemahelper.WithExample("entra")),
			}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"operation":     schemahelper.StringProp("Operation that was performed", schemahelper.WithExample("claims")),
					"handler":       schemahelper.StringProp("Name of the auth handler queried", schemahelper.WithExample("entra")),
					"authenticated": schemahelper.BoolProp("Whether the user is authenticated", schemahelper.WithExample(true)),
					"identityType":  schemahelper.StringProp("Type of identity: 'user', 'service-principal', or 'workload-identity'", schemahelper.WithExample("user")),
					"claims":        schemahelper.AnyProp("Identity claims (name, email, subject, etc.)"),
					"tenantId":      schemahelper.StringProp("Tenant ID for the authenticated identity", schemahelper.WithExample("12345678-1234-1234-1234-123456789012")),
					"expiresAt":     schemahelper.StringProp("Token expiration time in RFC3339 format", schemahelper.WithExample("2024-01-15T10:30:00Z")),
					"expiresIn":     schemahelper.StringProp("Human-readable duration until token expires", schemahelper.WithExample("55m30s")),
					"handlers":      schemahelper.ArrayProp("List of available auth handler names (for 'list' operation)"),
				}),
			},
			Examples: []provider.Example{
				{
					Name:        "Get identity claims",
					Description: "Retrieve claims (name, email, etc.) from the authenticated user",
					YAML: `name: get-claims
provider: identity
inputs:
  operation: claims`,
				},
				{
					Name:        "Check authentication status",
					Description: "Check if user is authenticated and get status details",
					YAML: `name: check-auth
provider: identity
inputs:
  operation: status
  handler: entra`,
				},
				{
					Name:        "List available handlers",
					Description: "List all registered auth handlers",
					YAML: `name: list-handlers
provider: identity
inputs:
  operation: list`,
				},
			},
		},
	}
}

// Descriptor returns the provider's metadata and schema.
func (p *IdentityProvider) Descriptor() *provider.Descriptor {
	return p.descriptor
}

// Execute performs the identity operation.
func (p *IdentityProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	inputs, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected map[string]any, got %T", ProviderName, input)
	}

	operation, ok := inputs["operation"].(string)
	if !ok {
		return nil, fmt.Errorf("%s: operation is required and must be a string", ProviderName)
	}

	handlerName, _ := inputs["handler"].(string)

	lgr.V(1).Info("executing provider", "provider", ProviderName, "operation", operation, "handler", handlerName)

	// Check for dry-run mode
	if dryRun := provider.DryRunFromContext(ctx); dryRun {
		return p.executeDryRun(operation, handlerName)
	}

	var result *provider.Output
	var err error

	switch operation {
	case "status":
		result, err = p.executeStatus(ctx, handlerName)
	case "claims":
		result, err = p.executeClaims(ctx, handlerName)
	case "list":
		result, err = p.executeList(ctx)
	default:
		return nil, fmt.Errorf("%s: unsupported operation: %s", ProviderName, operation)
	}

	if err != nil {
		return nil, fmt.Errorf("%s: %w", ProviderName, err)
	}

	lgr.V(1).Info("provider execution completed", "provider", ProviderName, "operation", operation)

	return result, nil
}

// getHandler retrieves the appropriate auth handler.
// If handlerName is empty, returns the first authenticated handler, or the first handler if none are authenticated.
func (p *IdentityProvider) getHandler(ctx context.Context, handlerName string) (auth.Handler, error) {
	registry := auth.RegistryFromContext(ctx)
	if registry == nil {
		return nil, fmt.Errorf("no auth registry in context")
	}

	// If handler name specified, get that specific handler
	if handlerName != "" {
		return registry.Get(handlerName)
	}

	// Otherwise, find the first available handler
	handlers := registry.List()
	if len(handlers) == 0 {
		return nil, fmt.Errorf("no auth handlers registered")
	}

	// Try to find an authenticated handler first
	for _, name := range handlers {
		handler, err := registry.Get(name)
		if err != nil {
			continue
		}
		status, err := handler.Status(ctx)
		if err == nil && status.Authenticated {
			return handler, nil
		}
	}

	// Fall back to the first handler
	return registry.Get(handlers[0])
}

func (p *IdentityProvider) executeStatus(ctx context.Context, handlerName string) (*provider.Output, error) {
	handler, err := p.getHandler(ctx, handlerName)
	if err != nil {
		return nil, err
	}

	status, err := handler.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get status from handler %q: %w", handler.Name(), err)
	}

	result := map[string]any{
		"operation":     "status",
		"handler":       handler.Name(),
		"authenticated": status.Authenticated,
	}

	if status.Authenticated {
		result["identityType"] = string(status.IdentityType)
		result["tenantId"] = status.TenantID

		if !status.ExpiresAt.IsZero() {
			result["expiresAt"] = status.ExpiresAt.Format(time.RFC3339)
			remaining := time.Until(status.ExpiresAt)
			if remaining > 0 {
				result["expiresIn"] = remaining.Truncate(time.Second).String()
			} else {
				result["expiresIn"] = "expired"
			}
		}

		if !status.LastRefresh.IsZero() {
			result["lastRefresh"] = status.LastRefresh.Format(time.RFC3339)
		}

		// Include client ID for service principal and workload identity
		if status.ClientID != "" {
			result["clientId"] = status.ClientID
		}
	}

	return &provider.Output{Data: result}, nil
}

func (p *IdentityProvider) executeClaims(ctx context.Context, handlerName string) (*provider.Output, error) {
	handler, err := p.getHandler(ctx, handlerName)
	if err != nil {
		return nil, err
	}

	status, err := handler.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get status from handler %q: %w", handler.Name(), err)
	}

	result := map[string]any{
		"operation":     "claims",
		"handler":       handler.Name(),
		"authenticated": status.Authenticated,
	}

	if !status.Authenticated {
		result["claims"] = nil
		return &provider.Output{
			Data:     result,
			Warnings: []string{"not authenticated - no claims available"},
		}, nil
	}

	// Build claims map from the Claims struct
	claims := make(map[string]any)
	if status.Claims != nil {
		if status.Claims.Issuer != "" {
			claims["issuer"] = status.Claims.Issuer
		}
		if status.Claims.Subject != "" {
			claims["subject"] = status.Claims.Subject
		}
		if status.Claims.TenantID != "" {
			claims["tenantId"] = status.Claims.TenantID
		}
		if status.Claims.ObjectID != "" {
			claims["objectId"] = status.Claims.ObjectID
		}
		if status.Claims.ClientID != "" {
			claims["clientId"] = status.Claims.ClientID
		}
		if status.Claims.Email != "" {
			claims["email"] = status.Claims.Email
		}
		if status.Claims.Name != "" {
			claims["name"] = status.Claims.Name
		}
		if status.Claims.Username != "" {
			claims["username"] = status.Claims.Username
		}
		if !status.Claims.IssuedAt.IsZero() {
			claims["issuedAt"] = status.Claims.IssuedAt.Format(time.RFC3339)
		}
		if !status.Claims.ExpiresAt.IsZero() {
			claims["expiresAt"] = status.Claims.ExpiresAt.Format(time.RFC3339)
		}

		// Provide a display identity for convenience
		claims["displayIdentity"] = status.Claims.DisplayIdentity()
	}

	result["claims"] = claims
	result["identityType"] = string(status.IdentityType)

	return &provider.Output{Data: result}, nil
}

func (p *IdentityProvider) executeList(ctx context.Context) (*provider.Output, error) {
	registry := auth.RegistryFromContext(ctx)
	if registry == nil {
		return nil, fmt.Errorf("no auth registry in context")
	}

	handlers := registry.List()

	// Build detailed handler info
	handlerInfo := make([]map[string]any, 0, len(handlers))
	for _, name := range handlers {
		handler, err := registry.Get(name)
		if err != nil {
			continue
		}

		info := map[string]any{
			"name":        handler.Name(),
			"displayName": handler.DisplayName(),
			"flows":       flowsToStrings(handler.SupportedFlows()),
		}

		// Check authentication status
		status, err := handler.Status(ctx)
		if err == nil {
			info["authenticated"] = status.Authenticated
			if status.Authenticated && status.Claims != nil {
				info["identity"] = status.Claims.DisplayIdentity()
			}
		}

		handlerInfo = append(handlerInfo, info)
	}

	return &provider.Output{
		Data: map[string]any{
			"operation": "list",
			"handlers":  handlerInfo,
			"count":     len(handlerInfo),
		},
	}, nil
}

func (p *IdentityProvider) executeDryRun(operation, handlerName string) (*provider.Output, error) {
	result := map[string]any{
		"operation": operation,
	}

	switch operation {
	case "status":
		result["handler"] = handlerName
		result["authenticated"] = false
		result["identityType"] = "[DRY-RUN] Not checked"
	case "claims":
		result["handler"] = handlerName
		result["authenticated"] = false
		result["claims"] = nil
	case "list":
		result["handlers"] = []map[string]any{}
		result["count"] = 0
	}

	return &provider.Output{
		Data: result,
		Metadata: map[string]any{
			"dryRun": true,
		},
	}, nil
}

// flowsToStrings converts auth flows to string slice for output.
func flowsToStrings(flows []auth.Flow) []string {
	result := make([]string, len(flows))
	for i, f := range flows {
		result[i] = string(f)
	}
	return result
}
