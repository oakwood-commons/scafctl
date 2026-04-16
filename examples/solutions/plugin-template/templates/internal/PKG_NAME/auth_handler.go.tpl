// Package <% .pkg_name %> implements the <% .provider_name %> auth handler plugin.
package <% .pkg_name %>

import (
	"context"
	"fmt"
	"time"

	"github.com/oakwood-commons/scafctl-plugin-sdk/auth"
	sdkplugin "github.com/oakwood-commons/scafctl-plugin-sdk/plugin"
)

const (
	// HandlerName is the unique identifier for this auth handler.
	HandlerName = "<% .provider_name %>"

	// Version is the auth handler version.
	Version = "0.1.0"
)

// Plugin implements the scafctl AuthHandlerPlugin interface.
type Plugin struct {
	cfg sdkplugin.ProviderConfig
}

// GetAuthHandlers returns the list of auth handlers exposed by this plugin.
//
//nolint:revive // ctx required by interface
func (p *Plugin) GetAuthHandlers(_ context.Context) ([]sdkplugin.AuthHandlerInfo, error) {
	return []sdkplugin.AuthHandlerInfo{
		{
			Name:        HandlerName,
			DisplayName: "<% .display_name %>",
			Flows:       []auth.Flow{auth.FlowDeviceCode},
			Capabilities: []auth.Capability{
				auth.CapScopesOnLogin,
			},
		},
	}, nil
}

// ConfigureAuthHandler stores host-side configuration.
//
//nolint:revive // ctx required by interface
func (p *Plugin) ConfigureAuthHandler(_ context.Context, _ string, cfg sdkplugin.ProviderConfig) error {
	p.cfg = cfg
	return nil
}

// Login performs the authentication flow.
//
//nolint:revive // all params required by interface
func (p *Plugin) Login(_ context.Context, handlerName string, _ sdkplugin.LoginRequest, _ func(sdkplugin.DeviceCodePrompt)) (*sdkplugin.LoginResponse, error) {
	if handlerName != HandlerName {
		return nil, fmt.Errorf("unknown handler: %s", handlerName)
	}

	// TODO: implement your authentication flow here
	return &sdkplugin.LoginResponse{
		Claims: &auth.Claims{
			Subject: "user@example.com",
			Email:   "user@example.com",
			Name:    "Example User",
		},
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}, nil
}

// Logout revokes the current session.
//
//nolint:revive // all params required by interface
func (p *Plugin) Logout(_ context.Context, handlerName string) error {
	if handlerName != HandlerName {
		return fmt.Errorf("unknown handler: %s", handlerName)
	}
	return nil
}

// GetStatus returns the current authentication status.
//
//nolint:revive // all params required by interface
func (p *Plugin) GetStatus(_ context.Context, handlerName string) (*auth.Status, error) {
	if handlerName != HandlerName {
		return nil, fmt.Errorf("unknown handler: %s", handlerName)
	}
	return &auth.Status{Authenticated: false}, nil
}

// GetToken returns a valid access token, refreshing if necessary.
//
//nolint:revive // all params required by interface
func (p *Plugin) GetToken(_ context.Context, handlerName string, _ sdkplugin.TokenRequest) (*sdkplugin.TokenResponse, error) {
	if handlerName != HandlerName {
		return nil, fmt.Errorf("unknown handler: %s", handlerName)
	}

	// TODO: implement token retrieval/refresh logic here
	return nil, auth.ErrNotAuthenticated
}

// ListCachedTokens returns information about cached tokens.
//
//nolint:revive // all params required by interface
func (p *Plugin) ListCachedTokens(_ context.Context, handlerName string) ([]*auth.CachedTokenInfo, error) {
	if handlerName != HandlerName {
		return nil, fmt.Errorf("unknown handler: %s", handlerName)
	}
	return nil, nil
}

// PurgeExpiredTokens removes expired tokens from the cache.
//
//nolint:revive // all params required by interface
func (p *Plugin) PurgeExpiredTokens(_ context.Context, handlerName string) (int, error) {
	if handlerName != HandlerName {
		return 0, fmt.Errorf("unknown handler: %s", handlerName)
	}
	return 0, nil
}

// StopAuthHandler performs cleanup for the named handler.
//
//nolint:revive // all params required by interface
func (p *Plugin) StopAuthHandler(_ context.Context, _ string) error {
	return nil
}
