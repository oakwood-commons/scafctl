// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package identityprovider provides authentication identity information from auth handlers.
// It exposes non-sensitive identity data like claims, authentication status, and identity type.
// It never exposes tokens or other secrets.
package identityprovider

import (
	"context"
	"errors"
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
			Name:        ProviderName,
			DisplayName: "Identity",
			APIVersion:  "v1",
			Description: "Provides authentication identity information (claims, status, identity type) from auth handlers without exposing tokens or secrets",
			Version:     version,
			Category:    "security",
			Capabilities: []provider.Capability{
				provider.CapabilityFrom,
			},
			Tags: []string{"auth", "identity", "claims", "security"},
			Schema: schemahelper.ObjectSchema([]string{"operation"}, map[string]*jsonschema.Schema{
				"operation": schemahelper.StringProp("Operation to perform: 'status' to get auth status, 'claims' to get identity claims, 'groups' to get group memberships (Entra only), 'list' to list available handlers",
					schemahelper.WithEnum("status", "claims", "groups", "list"),
					schemahelper.WithExample("claims"),
					schemahelper.WithMaxLength(*ptrs.IntPtr(10))),
				"handler": schemahelper.StringProp("Name of the auth handler to query (e.g., 'entra'). If not specified, uses the first available authenticated handler.",
					schemahelper.WithMaxLength(*ptrs.IntPtr(50)),
					schemahelper.WithPattern(`^[a-z][a-z0-9-]*$`),
					schemahelper.WithExample("entra")),
				"scope": schemahelper.StringProp("OAuth scope for scoped token operations. When set, 'claims' and 'status' operations mint a token with this scope and return its details instead of stored metadata. Not supported for 'groups' or 'list' operations.",
					schemahelper.WithMaxLength(*ptrs.IntPtr(1024)),
					schemahelper.WithExample("api://my-app/.default")),
			}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"operation":     schemahelper.StringProp("Operation that was performed", schemahelper.WithExample("claims")),
					"handler":       schemahelper.StringProp("Name of the auth handler queried", schemahelper.WithExample("entra")),
					"authenticated": schemahelper.BoolProp("Whether the user is authenticated", schemahelper.WithExample(true)),
					"identityType":  schemahelper.StringProp("Type of identity: 'user', 'service-principal', or 'workload-identity'", schemahelper.WithExample("user")),
					"claims":        schemahelper.AnyProp("Identity claims (name, email, subject, etc.)"),
					"groups":        schemahelper.ArrayProp("List of Entra group ObjectIDs the user belongs to (groups operation only)"),
					"count":         schemahelper.AnyProp("Number of groups returned (groups operation only)"),
					"tenantId":      schemahelper.StringProp("Tenant ID for the authenticated identity", schemahelper.WithExample("12345678-1234-1234-1234-123456789012")),
					"expiresAt":     schemahelper.StringProp("Token expiration time in RFC3339 format", schemahelper.WithExample("2024-01-15T10:30:00Z")),
					"expiresIn":     schemahelper.StringProp("Human-readable duration until token expires", schemahelper.WithExample("55m30s")),
					"handlers":      schemahelper.ArrayProp("List of available auth handler names (for 'list' operation)"),
					"scopedToken":   schemahelper.BoolProp("Whether the response was derived from a scoped access token (true when scope input was provided)"),
					"tokenScope":    schemahelper.StringProp("The OAuth scope the token was minted for (present when scope input was provided)", schemahelper.WithExample("api://my-app/.default")),
					"tokenType":     schemahelper.StringProp("Token type, typically Bearer (present when scope input was provided)", schemahelper.WithExample("Bearer")),
					"flow":          schemahelper.StringProp("Authentication flow that produced the token (present when scope input was provided)", schemahelper.WithExample("device_code")),
					"sessionId":     schemahelper.StringProp("Stable identifier of the authentication session (present when scope input was provided)"),
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
					Name:        "Get Entra group memberships",
					Description: "Retrieve all Entra group ObjectIDs the authenticated user belongs to. Requires GroupMember.Read.All or Directory.Read.All consent.",
					YAML: `name: user-groups
provider: identity
inputs:
  operation: groups
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
				{
					Name:        "Get claims from a scoped token",
					Description: "Mint a token for a specific OAuth scope and return the claims parsed from the access token JWT",
					YAML: `name: scoped-claims
provider: identity
inputs:
  operation: claims
  scope: api://my-app/.default`,
				},
				{
					Name:        "Check scoped token status",
					Description: "Mint a token for a specific OAuth scope and return its metadata (expiry, flow, type)",
					YAML: `name: scoped-status
provider: identity
inputs:
  operation: status
  scope: https://management.azure.com/.default
  handler: entra`,
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
	scope, _ := inputs["scope"].(string)

	lgr.V(1).Info("executing provider", "provider", ProviderName, "operation", operation, "handler", handlerName, "scope", scope)

	// Validate scope is only used with supported operations
	if scope != "" && (operation == "groups" || operation == "list") {
		return nil, fmt.Errorf("%s: scope is not supported for the %q operation; it can only be used with 'claims' or 'status'", ProviderName, operation)
	}

	// Check for dry-run mode
	if dryRun := provider.DryRunFromContext(ctx); dryRun {
		return p.executeDryRun(operation, handlerName, scope)
	}

	var result *provider.Output
	var err error

	switch operation {
	case "status":
		if scope != "" {
			result, err = p.executeScopedStatus(ctx, handlerName, scope)
		} else {
			result, err = p.executeStatus(ctx, handlerName)
		}
	case "claims":
		if scope != "" {
			result, err = p.executeScopedClaims(ctx, handlerName, scope)
		} else {
			result, err = p.executeClaims(ctx, handlerName)
		}
	case "groups":
		result, err = p.executeGroups(ctx, handlerName)
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
	claims := claimsToMap(status.Claims)

	result["claims"] = claims
	result["identityType"] = string(status.IdentityType)

	return &provider.Output{Data: result}, nil
}

// executeScopedStatus mints a token for the given scope and returns status
// derived from the token metadata rather than stored session metadata.
func (p *IdentityProvider) executeScopedStatus(ctx context.Context, handlerName, scope string) (*provider.Output, error) {
	handler, err := p.getHandler(ctx, handlerName)
	if err != nil {
		return nil, err
	}

	token, err := handler.GetToken(ctx, auth.TokenOptions{Scope: scope})
	if err != nil {
		return nil, fmt.Errorf("failed to mint scoped token from handler %q for scope %q: %w", handler.Name(), scope, err)
	}

	result := map[string]any{
		"operation":     "status",
		"handler":       handler.Name(),
		"authenticated": true,
		"scopedToken":   true,
		"tokenScope":    scope,
	}

	if token.TokenType != "" {
		result["tokenType"] = token.TokenType
	}
	if string(token.Flow) != "" {
		result["flow"] = string(token.Flow)
	}
	if token.SessionID != "" {
		result["sessionId"] = token.SessionID
	}

	if !token.ExpiresAt.IsZero() {
		result["expiresAt"] = token.ExpiresAt.Format(time.RFC3339)
		remaining := time.Until(token.ExpiresAt)
		if remaining > 0 {
			result["expiresIn"] = remaining.Truncate(time.Second).String()
		} else {
			result["expiresIn"] = "expired"
		}
	}

	// Try to extract identity info from the access token JWT
	var warnings []string
	claims, parseErr := auth.ParseJWTClaims(token.AccessToken)
	if parseErr != nil {
		if errors.Is(parseErr, auth.ErrOpaqueToken) {
			warnings = append(warnings, fmt.Sprintf("access token is not a decodable JWT — identity details unavailable: %v", parseErr))
		} else {
			warnings = append(warnings, fmt.Sprintf("failed to parse access token claims: %v", parseErr))
		}
	} else {
		if claims.TenantID != "" {
			result["tenantId"] = claims.TenantID
		}
		if string(identityTypeFromClaims(claims)) != "" {
			result["identityType"] = string(identityTypeFromClaims(claims))
		}
	}

	output := &provider.Output{Data: result}
	if len(warnings) > 0 {
		output.Warnings = warnings
	}
	return output, nil
}

// executeScopedClaims mints a token for the given scope and returns claims
// parsed from the access token JWT.
func (p *IdentityProvider) executeScopedClaims(ctx context.Context, handlerName, scope string) (*provider.Output, error) {
	handler, err := p.getHandler(ctx, handlerName)
	if err != nil {
		return nil, err
	}

	token, err := handler.GetToken(ctx, auth.TokenOptions{Scope: scope})
	if err != nil {
		return nil, fmt.Errorf("failed to mint scoped token from handler %q for scope %q: %w", handler.Name(), scope, err)
	}

	result := map[string]any{
		"operation":     "claims",
		"handler":       handler.Name(),
		"authenticated": true,
		"scopedToken":   true,
		"tokenScope":    scope,
	}

	var warnings []string
	parsedClaims, parseErr := auth.ParseJWTClaims(token.AccessToken)
	if parseErr != nil {
		// Opaque/encrypted token — return token metadata without claims
		if errors.Is(parseErr, auth.ErrOpaqueToken) {
			warnings = append(warnings, fmt.Sprintf("access token is not a decodable JWT — claims unavailable: %v", parseErr))
		} else {
			warnings = append(warnings, fmt.Sprintf("failed to parse access token claims: %v", parseErr))
		}
		result["claims"] = nil

		// Still include what we know from the token metadata
		if !token.ExpiresAt.IsZero() {
			result["expiresAt"] = token.ExpiresAt.Format(time.RFC3339)
		}
	} else {
		// Build claims map from parsed JWT
		claims := claimsToMap(parsedClaims)
		result["claims"] = claims
		result["identityType"] = string(identityTypeFromClaims(parsedClaims))
	}

	output := &provider.Output{Data: result}
	if len(warnings) > 0 {
		output.Warnings = warnings
	}
	return output, nil
}

// claimsToMap converts an auth.Claims struct to a map[string]any,
// omitting zero-value fields.
func claimsToMap(c *auth.Claims) map[string]any {
	if c == nil {
		return nil
	}
	claims := make(map[string]any)
	if c.Issuer != "" {
		claims["issuer"] = c.Issuer
	}
	if c.Subject != "" {
		claims["subject"] = c.Subject
	}
	if c.TenantID != "" {
		claims["tenantId"] = c.TenantID
	}
	if c.ObjectID != "" {
		claims["objectId"] = c.ObjectID
	}
	if c.ClientID != "" {
		claims["clientId"] = c.ClientID
	}
	if c.Email != "" {
		claims["email"] = c.Email
	}
	if c.Name != "" {
		claims["name"] = c.Name
	}
	if c.Username != "" {
		claims["username"] = c.Username
	}
	if !c.IssuedAt.IsZero() {
		claims["issuedAt"] = c.IssuedAt.Format(time.RFC3339)
	}
	if !c.ExpiresAt.IsZero() {
		claims["expiresAt"] = c.ExpiresAt.Format(time.RFC3339)
	}
	claims["displayIdentity"] = c.DisplayIdentity()
	return claims
}

// identityTypeFromClaims infers the identity type from JWT claims.
// If ObjectID is present but no human-readable fields (Name, Email, Username),
// this is likely a service principal. Otherwise, it's a user.
func identityTypeFromClaims(c *auth.Claims) auth.IdentityType {
	if c == nil {
		return auth.IdentityTypeUser
	}
	// Service principals typically have no name/email/username in the access token
	if c.Name == "" && c.Email == "" && c.Username == "" && c.ObjectID != "" {
		return auth.IdentityTypeServicePrincipal
	}
	return auth.IdentityTypeUser
}

func (p *IdentityProvider) executeGroups(ctx context.Context, handlerName string) (*provider.Output, error) {
	handler, err := p.getHandler(ctx, handlerName)
	if err != nil {
		return nil, err
	}

	gp, ok := handler.(auth.GroupsProvider)
	if !ok {
		return nil, fmt.Errorf("handler %q does not support group membership queries; only the Entra handler implements this operation", handler.Name())
	}

	groups, err := gp.GetGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve group memberships from handler %q: %w", handler.Name(), err)
	}

	// Normalise nil slice → empty slice so JSON output is [] not null.
	if groups == nil {
		groups = []string{}
	}

	return &provider.Output{
		Data: map[string]any{
			"operation": "groups",
			"handler":   handler.Name(),
			"groups":    groups,
			"count":     len(groups),
		},
	}, nil
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
			"name":         handler.Name(),
			"displayName":  handler.DisplayName(),
			"flows":        flowsToStrings(handler.SupportedFlows()),
			"capabilities": capabilitiesToStrings(handler.Capabilities()),
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

func (p *IdentityProvider) executeDryRun(operation, handlerName, scope string) (*provider.Output, error) {
	result := map[string]any{
		"operation": operation,
	}

	switch operation {
	case "status":
		result["handler"] = handlerName
		result["authenticated"] = false
		result["identityType"] = "[DRY-RUN] Not checked"
		if scope != "" {
			result["scopedToken"] = true
			result["tokenScope"] = scope
		}
	case "claims":
		result["handler"] = handlerName
		result["authenticated"] = false
		result["claims"] = nil
		if scope != "" {
			result["scopedToken"] = true
			result["tokenScope"] = scope
		}
	case "groups":
		result["handler"] = handlerName
		result["groups"] = []string{}
		result["count"] = 0
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

// capabilitiesToStrings converts auth capabilities to string slice for output.
func capabilitiesToStrings(caps []auth.Capability) []string {
	result := make([]string, len(caps))
	for i, c := range caps {
		result[i] = string(c)
	}
	return result
}
