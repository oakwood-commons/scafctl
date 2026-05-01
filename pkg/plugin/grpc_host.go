// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/oakwood-commons/scafctl-plugin-sdk/plugin/proto"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"google.golang.org/grpc"
)

// secretNameMaxLen is the maximum allowed length for a secret name.
const secretNameMaxLen = 256

// validSecretName matches alphanumeric characters, underscores, hyphens, dots,
// and forward slashes. This prevents path traversal and shell metacharacter injection.
var validSecretName = regexp.MustCompile(`^[a-zA-Z0-9_\-./]+$`)

// validateSecretName checks that a secret name is non-empty, within length
// bounds, and contains only safe characters.
func validateSecretName(name string) error {
	if name == "" {
		return fmt.Errorf("secret name must not be empty")
	}
	if len(name) > secretNameMaxLen {
		return fmt.Errorf("secret name exceeds maximum length of %d", secretNameMaxLen)
	}
	if !validSecretName.MatchString(name) {
		return fmt.Errorf("secret name contains invalid characters")
	}
	// Reject path traversal sequences
	if strings.Contains(name, "..") {
		return fmt.Errorf("secret name must not contain path traversal sequences")
	}
	return nil
}

// HostServiceDeps holds the host-side dependencies that the HostService
// exposes to plugins via gRPC callbacks. This is an internal dependency-injection
// struct passed by value into HostServiceServer; fields are not serialized.
type HostServiceDeps struct {
	// SecretStore provides access to the host's secret store (may be nil).
	SecretStore secrets.Store `json:"-" yaml:"-" doc:"Host secret store (not serialized)."`
	// AllowedSecretPrefix restricts secret access to names starting with this prefix.
	// If empty, all secrets are accessible (for backward compatibility).
	AllowedSecretPrefix string `json:"allowedSecretPrefix,omitempty" yaml:"allowedSecretPrefix,omitempty" doc:"Prefix restriction for secret access."`
	// AuthIdentityFunc returns claims for a named auth handler and scope.
	// handler may be empty for the default handler. May be nil if auth is unavailable.
	AuthIdentityFunc func(ctx context.Context, handler, scope string) (*proto.Claims, error) `json:"-" yaml:"-" doc:"Auth identity callback (not serialized)."`
	// AllowedAuthHandlers restricts which auth handler names a plugin may request.
	// If nil or empty, all handlers are allowed (for backward compatibility).
	AllowedAuthHandlers []string `json:"allowedAuthHandlers,omitempty" yaml:"allowedAuthHandlers,omitempty" doc:"Allowed auth handler names."`
	// AuthHandlersFunc returns available auth handler names and the default handler.
	// May be nil if auth is unavailable.
	AuthHandlersFunc func(ctx context.Context) (handlers []string, defaultHandler string, err error) `json:"-" yaml:"-" doc:"Auth handlers callback (not serialized)."`
	// AuthTokenFunc retrieves a valid access token from the host's auth registry.
	// Plugins call this for authenticated HTTP requests and token refresh on 401.
	// May be nil if auth is unavailable.
	AuthTokenFunc func(ctx context.Context, handler, scope string, minValidFor int64, forceRefresh bool) (*proto.GetAuthTokenResponse, error) `json:"-" yaml:"-" doc:"Auth token callback (not serialized)."`
	// AuthGroupsFunc retrieves group memberships for the authenticated user.
	// Only handlers that implement group queries (e.g. Entra) will return results.
	// May be nil if group queries are unavailable.
	// TODO: Wire this callback in production code (e.g. via RootOptions or plugin SDK)
	// once an auth handler with group query support is available.
	AuthGroupsFunc func(ctx context.Context, handler string) ([]string, error) `json:"-" yaml:"-" doc:"Auth groups callback (not serialized)."`
}

// isSecretAllowed checks whether the given secret name is within the allowed
// prefix scope. Returns true when no prefix restriction is configured.
func (d *HostServiceDeps) isSecretAllowed(name string) bool {
	if d.AllowedSecretPrefix == "" {
		return true
	}
	return strings.HasPrefix(name, d.AllowedSecretPrefix)
}

// isAuthHandlerAllowed checks whether the given handler name is in the
// AllowedAuthHandlers list. Returns true when no restriction is configured.
// When an allowlist is configured, an empty handler name is rejected to
// prevent bypassing the restriction via the default handler.
func (d *HostServiceDeps) isAuthHandlerAllowed(handler string) bool {
	if len(d.AllowedAuthHandlers) == 0 {
		return true
	}
	if handler == "" {
		return false
	}
	for _, h := range d.AllowedAuthHandlers {
		if h == handler {
			return true
		}
	}
	return false
}

// HostServiceServer implements the HostService gRPC server (runs on the host).
// Plugins call these RPCs back into the host process via the go-plugin GRPCBroker.
type HostServiceServer struct {
	proto.UnimplementedHostServiceServer
	Deps HostServiceDeps
}

// GetSecret implements HostService.GetSecret
func (h *HostServiceServer) GetSecret(ctx context.Context, req *proto.GetSecretRequest) (*proto.GetSecretResponse, error) {
	if h.Deps.SecretStore == nil {
		return &proto.GetSecretResponse{
			Error: "secret store not available",
		}, nil
	}

	if err := validateSecretName(req.Name); err != nil {
		//nolint:nilerr // Error is communicated via response, not gRPC error
		return &proto.GetSecretResponse{Error: err.Error()}, nil
	}
	if !h.Deps.isSecretAllowed(req.Name) {
		return &proto.GetSecretResponse{Error: "access denied: secret name outside allowed scope"}, nil
	}

	value, err := h.Deps.SecretStore.Get(ctx, req.Name)
	if err != nil {
		if errors.Is(err, secrets.ErrNotFound) {
			return &proto.GetSecretResponse{Found: false}, nil
		}
		return &proto.GetSecretResponse{
			Error: fmt.Sprintf("get secret %q: %v", req.Name, err),
		}, nil
	}

	return &proto.GetSecretResponse{
		Value: string(value),
		Found: true,
	}, nil
}

// SetSecret implements HostService.SetSecret
func (h *HostServiceServer) SetSecret(ctx context.Context, req *proto.SetSecretRequest) (*proto.SetSecretResponse, error) {
	if h.Deps.SecretStore == nil {
		return &proto.SetSecretResponse{
			Error: "secret store not available",
		}, nil
	}

	if err := validateSecretName(req.Name); err != nil {
		//nolint:nilerr // Error is communicated via response, not gRPC error
		return &proto.SetSecretResponse{Error: err.Error()}, nil
	}
	if !h.Deps.isSecretAllowed(req.Name) {
		return &proto.SetSecretResponse{Error: "access denied: secret name outside allowed scope"}, nil
	}

	if err := h.Deps.SecretStore.Set(ctx, req.Name, []byte(req.Value)); err != nil {
		return &proto.SetSecretResponse{
			Error: fmt.Sprintf("set secret %q: %v", req.Name, err),
		}, nil
	}

	return &proto.SetSecretResponse{}, nil
}

// DeleteSecret implements HostService.DeleteSecret
func (h *HostServiceServer) DeleteSecret(ctx context.Context, req *proto.DeleteSecretRequest) (*proto.DeleteSecretResponse, error) {
	if h.Deps.SecretStore == nil {
		return &proto.DeleteSecretResponse{
			Error: "secret store not available",
		}, nil
	}

	if err := validateSecretName(req.Name); err != nil {
		//nolint:nilerr // Error is communicated via response, not gRPC error
		return &proto.DeleteSecretResponse{Error: err.Error()}, nil
	}
	if !h.Deps.isSecretAllowed(req.Name) {
		return &proto.DeleteSecretResponse{Error: "access denied: secret name outside allowed scope"}, nil
	}

	if err := h.Deps.SecretStore.Delete(ctx, req.Name); err != nil {
		return &proto.DeleteSecretResponse{
			Error: fmt.Sprintf("delete secret %q: %v", req.Name, err),
		}, nil
	}

	return &proto.DeleteSecretResponse{}, nil
}

// maxSecretPatternLength limits the length of the regex pattern to prevent ReDoS.
const maxSecretPatternLength = 256

// ListSecrets implements HostService.ListSecrets
func (h *HostServiceServer) ListSecrets(ctx context.Context, req *proto.ListSecretsRequest) (*proto.ListSecretsResponse, error) {
	if h.Deps.SecretStore == nil {
		return &proto.ListSecretsResponse{
			Error: "secret store not available",
		}, nil
	}

	names, err := h.Deps.SecretStore.List(ctx)
	if err != nil {
		return &proto.ListSecretsResponse{
			Error: fmt.Sprintf("list secrets: %v", err),
		}, nil
	}

	// Filter to only secrets within the allowed scope
	if h.Deps.AllowedSecretPrefix != "" {
		filtered := make([]string, 0, len(names))
		for _, n := range names {
			if strings.HasPrefix(n, h.Deps.AllowedSecretPrefix) {
				filtered = append(filtered, n)
			}
		}
		names = filtered
	}

	// Apply optional regex pattern filter
	if req.Pattern != "" {
		if len(req.Pattern) > maxSecretPatternLength {
			return &proto.ListSecretsResponse{
				Error: fmt.Sprintf("pattern too long: max %d characters", maxSecretPatternLength),
			}, nil
		}
		re, err := regexp.Compile(req.Pattern)
		if err != nil {
			return &proto.ListSecretsResponse{
				Error: fmt.Sprintf("invalid pattern: %v", err),
			}, nil
		}
		filtered := make([]string, 0, len(names))
		for _, n := range names {
			if re.MatchString(n) {
				filtered = append(filtered, n)
			}
		}
		names = filtered
	}

	return &proto.ListSecretsResponse{
		Names: names,
	}, nil
}

// GetAuthIdentity implements HostService.GetAuthIdentity
func (h *HostServiceServer) GetAuthIdentity(ctx context.Context, req *proto.GetAuthIdentityRequest) (*proto.GetAuthIdentityResponse, error) {
	if h.Deps.AuthIdentityFunc == nil {
		return &proto.GetAuthIdentityResponse{
			Error: "auth identity not available",
		}, nil
	}

	if !h.Deps.isAuthHandlerAllowed(req.HandlerName) {
		return &proto.GetAuthIdentityResponse{
			Error: fmt.Sprintf("access denied: auth handler %q is not in the allowed set", req.HandlerName),
		}, nil
	}

	claims, err := h.Deps.AuthIdentityFunc(ctx, req.HandlerName, req.Scope)
	if err != nil {
		return &proto.GetAuthIdentityResponse{
			Error: fmt.Sprintf("get auth identity: %v", err),
		}, nil
	}

	return &proto.GetAuthIdentityResponse{
		Claims: claims,
	}, nil
}

// ListAuthHandlers implements HostService.ListAuthHandlers
//
//nolint:revive // req is required by gRPC interface even if unused
func (h *HostServiceServer) ListAuthHandlers(ctx context.Context, _ *proto.ListAuthHandlersRequest) (*proto.ListAuthHandlersResponse, error) {
	if h.Deps.AuthHandlersFunc == nil {
		return &proto.ListAuthHandlersResponse{}, nil
	}

	handlers, defaultHandler, err := h.Deps.AuthHandlersFunc(ctx)
	if err != nil {
		// ListAuthHandlersResponse has no Error field, so log and degrade
		// gracefully instead of returning internal errors to the plugin.
		lgr := logger.FromContext(ctx)
		lgr.V(1).Info("list auth handlers failed, returning empty", "error", err)
		return &proto.ListAuthHandlersResponse{}, nil
	}

	// Filter handlers to only those allowed for this plugin. This prevents
	// plugins from discovering handler names they are not permitted to use.
	if len(h.Deps.AllowedAuthHandlers) > 0 {
		allowed := make(map[string]struct{}, len(h.Deps.AllowedAuthHandlers))
		for _, a := range h.Deps.AllowedAuthHandlers {
			allowed[a] = struct{}{}
		}
		filtered := make([]string, 0, len(handlers))
		for _, name := range handlers {
			if _, ok := allowed[name]; ok {
				filtered = append(filtered, name)
			}
		}
		handlers = filtered
		// Redact default handler if not in allowed set
		if _, ok := allowed[defaultHandler]; !ok {
			defaultHandler = ""
		}
	}

	return &proto.ListAuthHandlersResponse{
		HandlerNames:   handlers,
		DefaultHandler: defaultHandler,
	}, nil
}

// GetAuthToken implements HostService.GetAuthToken
func (h *HostServiceServer) GetAuthToken(ctx context.Context, req *proto.GetAuthTokenRequest) (*proto.GetAuthTokenResponse, error) {
	if h.Deps.AuthTokenFunc == nil {
		return &proto.GetAuthTokenResponse{
			Error: "auth token service not available",
		}, nil
	}

	if !h.Deps.isAuthHandlerAllowed(req.HandlerName) {
		return &proto.GetAuthTokenResponse{
			Error: fmt.Sprintf("access denied: auth handler %q is not in the allowed set", req.HandlerName),
		}, nil
	}

	resp, err := h.Deps.AuthTokenFunc(ctx, req.HandlerName, req.Scope, req.MinValidForSeconds, req.ForceRefresh)
	if err != nil {
		return &proto.GetAuthTokenResponse{
			Error: fmt.Sprintf("get auth token: %v", err),
		}, nil
	}

	return resp, nil
}

// GetAuthGroups implements HostService.GetAuthGroups
func (h *HostServiceServer) GetAuthGroups(ctx context.Context, req *proto.GetAuthGroupsRequest) (*proto.GetAuthGroupsResponse, error) {
	if h.Deps.AuthGroupsFunc == nil {
		return &proto.GetAuthGroupsResponse{
			Error: "auth groups not available",
		}, nil
	}

	if !h.Deps.isAuthHandlerAllowed(req.HandlerName) {
		return &proto.GetAuthGroupsResponse{
			Error: fmt.Sprintf("access denied: auth handler %q is not in the allowed set", req.HandlerName),
		}, nil
	}

	groups, err := h.Deps.AuthGroupsFunc(ctx, req.HandlerName)
	if err != nil {
		return &proto.GetAuthGroupsResponse{
			Error: fmt.Sprintf("get auth groups: %v", err),
		}, nil
	}

	return &proto.GetAuthGroupsResponse{
		Groups: groups,
	}, nil
}

// HostServiceClient wraps the HostService gRPC client (used by plugins).
// Plugin code uses this to call back into the host process.
type HostServiceClient struct {
	client proto.HostServiceClient
}

// NewHostServiceClient creates a HostServiceClient from a gRPC connection.
func NewHostServiceClient(conn *grpc.ClientConn) *HostServiceClient {
	return &HostServiceClient{client: proto.NewHostServiceClient(conn)}
}

// GetSecret retrieves a secret from the host's secret store.
func (c *HostServiceClient) GetSecret(ctx context.Context, name string) (string, bool, error) {
	resp, err := c.client.GetSecret(ctx, &proto.GetSecretRequest{Name: name})
	if err != nil {
		return "", false, fmt.Errorf("host GetSecret: %w", err)
	}
	if resp.Error != "" {
		return "", false, fmt.Errorf("host GetSecret: %s", resp.Error)
	}
	return resp.Value, resp.Found, nil
}

// SetSecret stores a secret in the host's secret store.
func (c *HostServiceClient) SetSecret(ctx context.Context, name, value string) error {
	resp, err := c.client.SetSecret(ctx, &proto.SetSecretRequest{Name: name, Value: value})
	if err != nil {
		return fmt.Errorf("host SetSecret: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("host SetSecret: %s", resp.Error)
	}
	return nil
}

// DeleteSecret removes a secret from the host's secret store.
func (c *HostServiceClient) DeleteSecret(ctx context.Context, name string) error {
	resp, err := c.client.DeleteSecret(ctx, &proto.DeleteSecretRequest{Name: name})
	if err != nil {
		return fmt.Errorf("host DeleteSecret: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("host DeleteSecret: %s", resp.Error)
	}
	return nil
}

// ListSecrets lists secret names from the host's secret store.
func (c *HostServiceClient) ListSecrets(ctx context.Context, pattern string) ([]string, error) {
	resp, err := c.client.ListSecrets(ctx, &proto.ListSecretsRequest{Pattern: pattern})
	if err != nil {
		return nil, fmt.Errorf("host ListSecrets: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("host ListSecrets: %s", resp.Error)
	}
	return resp.Names, nil
}

// GetAuthIdentity retrieves identity claims from the host's auth registry.
func (c *HostServiceClient) GetAuthIdentity(ctx context.Context, handler, scope string) (*proto.Claims, error) {
	resp, err := c.client.GetAuthIdentity(ctx, &proto.GetAuthIdentityRequest{
		HandlerName: handler,
		Scope:       scope,
	})
	if err != nil {
		return nil, fmt.Errorf("host GetAuthIdentity: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("host GetAuthIdentity: %s", resp.Error)
	}
	return resp.Claims, nil
}

// ListAuthHandlers lists available auth handlers on the host.
func (c *HostServiceClient) ListAuthHandlers(ctx context.Context) (handlers []string, defaultHandler string, err error) {
	resp, err := c.client.ListAuthHandlers(ctx, &proto.ListAuthHandlersRequest{})
	if err != nil {
		return nil, "", fmt.Errorf("host ListAuthHandlers: %w", err)
	}
	return resp.HandlerNames, resp.DefaultHandler, nil
}

// GetAuthToken retrieves a valid access token from the host's auth registry.
func (c *HostServiceClient) GetAuthToken(ctx context.Context, handler, scope string, minValidFor int64, forceRefresh bool) (*proto.GetAuthTokenResponse, error) {
	resp, err := c.client.GetAuthToken(ctx, &proto.GetAuthTokenRequest{
		HandlerName:        handler,
		Scope:              scope,
		MinValidForSeconds: minValidFor,
		ForceRefresh:       forceRefresh,
	})
	if err != nil {
		return nil, fmt.Errorf("host GetAuthToken: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("host GetAuthToken: %s", resp.Error)
	}
	return resp, nil
}

// GetAuthGroups retrieves group memberships for the authenticated user from
// the host's auth registry.
func (c *HostServiceClient) GetAuthGroups(ctx context.Context, handler string) ([]string, error) {
	resp, err := c.client.GetAuthGroups(ctx, &proto.GetAuthGroupsRequest{
		HandlerName: handler,
	})
	if err != nil {
		return nil, fmt.Errorf("host GetAuthGroups: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("host GetAuthGroups: %s", resp.Error)
	}
	return resp.Groups, nil
}
