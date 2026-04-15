// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/hashicorp/go-plugin"
	sdkplugin "github.com/oakwood-commons/scafctl-plugin-sdk/plugin"
	"github.com/oakwood-commons/scafctl-plugin-sdk/plugin/proto"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// AuthHandlerPluginName is the name used to identify the auth handler plugin.
const AuthHandlerPluginName = sdkplugin.AuthHandlerPluginName

// AuthHandlerGRPCPlugin implements plugin.GRPCPlugin from hashicorp/go-plugin
// for auth handler plugins.
type AuthHandlerGRPCPlugin struct {
	plugin.Plugin
	Impl AuthHandlerPlugin
	// HostDeps holds host-side dependencies for the HostService callback server.
	// Set by the host before starting the plugin. Nil on the plugin side.
	HostDeps *HostServiceDeps
}

// GRPCServer registers the auth handler gRPC server.
//
//nolint:revive // broker is required by go-plugin interface
func (p *AuthHandlerGRPCPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	proto.RegisterAuthHandlerServiceServer(s, &AuthHandlerGRPCServer{Impl: p.Impl, broker: broker})
	return nil
}

// GRPCClient returns the auth handler gRPC client.
// If HostDeps is non-nil, a HostService gRPC server is started via the broker.
func (p *AuthHandlerGRPCPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (any, error) {
	var hostServiceID uint32
	if p.HostDeps != nil {
		hostServiceID = broker.NextId()
		deps := p.HostDeps
		lgr := logger.FromContext(ctx)
		// AcceptAndServe blocks until the broker connection is closed, which
		// happens automatically when the plugin client is killed (Kill()).
		// The goroutine therefore does not leak.
		go func() {
			broker.AcceptAndServe(hostServiceID, func(opts []grpc.ServerOption) *grpc.Server {
				s := grpc.NewServer(opts...)
				proto.RegisterHostServiceServer(s, &HostServiceServer{Deps: *deps})
				return s
			})
			lgr.V(1).Info("auth handler HostService broker stopped", "serviceID", hostServiceID)
		}()
	}
	return &AuthHandlerGRPCClient{
		client:        proto.NewAuthHandlerServiceClient(c),
		broker:        broker,
		hostServiceID: hostServiceID,
	}, nil
}

// ---- gRPC Server (runs inside the plugin process) ----

// AuthHandlerGRPCServer implements the gRPC server for auth handler plugins.
type AuthHandlerGRPCServer struct {
	proto.UnimplementedAuthHandlerServiceServer
	Impl   AuthHandlerPlugin
	broker *plugin.GRPCBroker
}

// GetAuthHandlers implements the GetAuthHandlers RPC.
//
//nolint:revive // req is required by gRPC interface even if unused
func (s *AuthHandlerGRPCServer) GetAuthHandlers(ctx context.Context, _ *proto.GetAuthHandlersRequest) (*proto.GetAuthHandlersResponse, error) {
	handlers, err := s.Impl.GetAuthHandlers(ctx)
	if err != nil {
		return nil, err
	}

	resp := &proto.GetAuthHandlersResponse{
		Handlers: make([]*proto.AuthHandlerInfo, len(handlers)),
	}
	for i, h := range handlers {
		flows := make([]string, len(h.Flows))
		for j, f := range h.Flows {
			flows[j] = string(f)
		}
		caps := make([]string, len(h.Capabilities))
		for j, c := range h.Capabilities {
			caps[j] = string(c)
		}
		resp.Handlers[i] = &proto.AuthHandlerInfo{
			Name:         h.Name,
			DisplayName:  h.DisplayName,
			Flows:        flows,
			Capabilities: caps,
		}
	}
	return resp, nil
}

// Login implements the Login RPC with server-side streaming.
func (s *AuthHandlerGRPCServer) Login(req *proto.LoginRequest, stream grpc.ServerStreamingServer[proto.LoginStreamMessage]) error {
	ctx := stream.Context()

	// Build the device code callback that sends prompts over the stream.
	deviceCodeCb := func(prompt DeviceCodePrompt) {
		_ = stream.Send(&proto.LoginStreamMessage{
			Payload: &proto.LoginStreamMessage_DeviceCodePrompt{
				DeviceCodePrompt: &proto.DeviceCodePrompt{
					UserCode:        prompt.UserCode,
					VerificationUri: prompt.VerificationURI,
					Message:         prompt.Message,
				},
			},
		})
	}

	loginReq := LoginRequest{
		TenantID: req.HandlerName, // will be overridden below
		Scopes:   req.Scopes,
		Flow:     auth.Flow(req.Flow),
		Timeout:  time.Duration(req.TimeoutSeconds) * time.Second,
	}
	loginReq.TenantID = req.TenantId

	result, err := s.Impl.Login(ctx, req.HandlerName, loginReq, deviceCodeCb)
	if err != nil {
		return stream.Send(&proto.LoginStreamMessage{
			Payload: &proto.LoginStreamMessage_Error{
				Error: err.Error(),
			},
		})
	}

	// Send the final result.
	return stream.Send(&proto.LoginStreamMessage{
		Payload: &proto.LoginStreamMessage_Result{
			Result: &proto.LoginResult{
				Claims:        claimsToProto(result.Claims),
				ExpiresAtUnix: result.ExpiresAt.Unix(),
			},
		},
	})
}

// Logout implements the Logout RPC.
func (s *AuthHandlerGRPCServer) Logout(ctx context.Context, req *proto.LogoutRequest) (*proto.LogoutResponse, error) {
	if err := s.Impl.Logout(ctx, req.HandlerName); err != nil {
		return nil, err
	}
	return &proto.LogoutResponse{}, nil
}

// GetStatus implements the GetStatus RPC.
func (s *AuthHandlerGRPCServer) GetStatus(ctx context.Context, req *proto.GetStatusRequest) (*proto.GetStatusResponse, error) {
	status, err := s.Impl.GetStatus(ctx, req.HandlerName)
	if err != nil {
		return nil, err
	}
	return statusToProto(status), nil
}

// GetToken implements the GetToken RPC.
func (s *AuthHandlerGRPCServer) GetToken(ctx context.Context, req *proto.GetTokenRequest) (*proto.GetTokenResponse, error) {
	tokenReq := TokenRequest{
		Scope:        req.Scope,
		MinValidFor:  time.Duration(req.MinValidForSeconds) * time.Second,
		ForceRefresh: req.ForceRefresh,
	}
	token, err := s.Impl.GetToken(ctx, req.HandlerName, tokenReq)
	if err != nil {
		return nil, err
	}
	return tokenResponseToProto(token), nil
}

// ListCachedTokens implements the ListCachedTokens RPC.
func (s *AuthHandlerGRPCServer) ListCachedTokens(ctx context.Context, req *proto.ListCachedTokensRequest) (*proto.ListCachedTokensResponse, error) {
	tokens, err := s.Impl.ListCachedTokens(ctx, req.HandlerName)
	if err != nil {
		return nil, err
	}
	resp := &proto.ListCachedTokensResponse{
		Tokens: make([]*proto.CachedTokenInfo, len(tokens)),
	}
	for i, t := range tokens {
		resp.Tokens[i] = cachedTokenInfoToProto(t)
	}
	return resp, nil
}

// PurgeExpiredTokens implements the PurgeExpiredTokens RPC.
func (s *AuthHandlerGRPCServer) PurgeExpiredTokens(ctx context.Context, req *proto.PurgeExpiredTokensRequest) (*proto.PurgeExpiredTokensResponse, error) {
	count, err := s.Impl.PurgeExpiredTokens(ctx, req.HandlerName)
	if err != nil {
		return nil, err
	}
	return &proto.PurgeExpiredTokensResponse{
		//nolint:gosec // count is small, overflow is acceptable
		PurgedCount: int32(count),
	}, nil
}

// ConfigureAuthHandler implements the ConfigureAuthHandler RPC.
func (s *AuthHandlerGRPCServer) ConfigureAuthHandler(ctx context.Context, req *proto.ConfigureAuthHandlerRequest) (*proto.ConfigureAuthHandlerResponse, error) {
	settings := make(map[string]json.RawMessage, len(req.Settings))
	for k, v := range req.Settings {
		settings[k] = json.RawMessage(v)
	}

	cfg := ProviderConfig{
		Quiet:      req.Quiet,
		NoColor:    req.NoColor,
		BinaryName: req.BinaryName,
		Settings:   settings,
	}
	if req.HostServiceId != 0 && s.broker != nil {
		cfg.HostServiceID = req.HostServiceId
	}

	if err := s.Impl.ConfigureAuthHandler(ctx, req.HandlerName, cfg); err != nil {
		//nolint:nilerr // Error is communicated via response, not gRPC error
		return &proto.ConfigureAuthHandlerResponse{Error: err.Error()}, nil
	}
	return &proto.ConfigureAuthHandlerResponse{
		ProtocolVersion: PluginProtocolVersion,
	}, nil
}

// StopAuthHandler implements the StopAuthHandler RPC.
func (s *AuthHandlerGRPCServer) StopAuthHandler(ctx context.Context, req *proto.StopAuthHandlerRequest) (*proto.StopAuthHandlerResponse, error) {
	if err := s.Impl.StopAuthHandler(ctx, req.HandlerName); err != nil {
		//nolint:nilerr // Error is communicated via response, not gRPC error
		return &proto.StopAuthHandlerResponse{Error: err.Error()}, nil
	}
	return &proto.StopAuthHandlerResponse{}, nil
}

// ---- gRPC Client (runs in the scafctl host process) ----

// AuthHandlerGRPCClient implements AuthHandlerPlugin by calling the gRPC service.
type AuthHandlerGRPCClient struct {
	client        proto.AuthHandlerServiceClient
	broker        *plugin.GRPCBroker
	hostServiceID uint32
}

// GetAuthHandlers implements AuthHandlerPlugin.GetAuthHandlers.
func (c *AuthHandlerGRPCClient) GetAuthHandlers(ctx context.Context) ([]AuthHandlerInfo, error) {
	resp, err := c.client.GetAuthHandlers(ctx, &proto.GetAuthHandlersRequest{})
	if err != nil {
		return nil, err
	}

	handlers := make([]AuthHandlerInfo, len(resp.Handlers))
	for i, h := range resp.Handlers {
		flows := make([]auth.Flow, len(h.Flows))
		for j, f := range h.Flows {
			flows[j] = auth.Flow(f)
		}
		caps := make([]auth.Capability, len(h.Capabilities))
		for j, cap := range h.Capabilities {
			caps[j] = auth.Capability(cap)
		}
		handlers[i] = AuthHandlerInfo{
			Name:         h.Name,
			DisplayName:  h.DisplayName,
			Flows:        flows,
			Capabilities: caps,
		}
	}
	return handlers, nil
}

// Login implements AuthHandlerPlugin.Login with streaming device code support.
func (c *AuthHandlerGRPCClient) Login(ctx context.Context, handlerName string, req LoginRequest, deviceCodeCb func(DeviceCodePrompt)) (*LoginResponse, error) {
	stream, err := c.client.Login(ctx, &proto.LoginRequest{
		HandlerName:    handlerName,
		TenantId:       req.TenantID,
		Scopes:         req.Scopes,
		Flow:           string(req.Flow),
		TimeoutSeconds: int64(req.Timeout / time.Second),
	})
	if err != nil {
		return nil, fmt.Errorf("login RPC failed: %w", err)
	}

	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("login stream ended without result")
		}
		if err != nil {
			return nil, fmt.Errorf("login stream error: %w", err)
		}

		switch p := msg.Payload.(type) {
		case *proto.LoginStreamMessage_DeviceCodePrompt:
			if deviceCodeCb != nil {
				deviceCodeCb(DeviceCodePrompt{
					UserCode:        p.DeviceCodePrompt.UserCode,
					VerificationURI: p.DeviceCodePrompt.VerificationUri,
					Message:         p.DeviceCodePrompt.Message,
				})
			}
		case *proto.LoginStreamMessage_Result:
			return &LoginResponse{
				Claims:    protoToClaims(p.Result.Claims),
				ExpiresAt: time.Unix(p.Result.ExpiresAtUnix, 0),
			}, nil
		case *proto.LoginStreamMessage_Error:
			return nil, fmt.Errorf("plugin login error: %s", p.Error)
		}
	}
}

// Logout implements AuthHandlerPlugin.Logout.
func (c *AuthHandlerGRPCClient) Logout(ctx context.Context, handlerName string) error {
	_, err := c.client.Logout(ctx, &proto.LogoutRequest{HandlerName: handlerName})
	return err
}

// GetStatus implements AuthHandlerPlugin.GetStatus.
func (c *AuthHandlerGRPCClient) GetStatus(ctx context.Context, handlerName string) (*auth.Status, error) {
	resp, err := c.client.GetStatus(ctx, &proto.GetStatusRequest{HandlerName: handlerName})
	if err != nil {
		return nil, err
	}
	return protoToStatus(resp), nil
}

// GetToken implements AuthHandlerPlugin.GetToken.
func (c *AuthHandlerGRPCClient) GetToken(ctx context.Context, handlerName string, req TokenRequest) (*TokenResponse, error) {
	resp, err := c.client.GetToken(ctx, &proto.GetTokenRequest{
		HandlerName:        handlerName,
		Scope:              req.Scope,
		MinValidForSeconds: int64(req.MinValidFor / time.Second),
		ForceRefresh:       req.ForceRefresh,
	})
	if err != nil {
		return nil, err
	}
	return protoToTokenResponse(resp), nil
}

// ListCachedTokens implements AuthHandlerPlugin.ListCachedTokens.
func (c *AuthHandlerGRPCClient) ListCachedTokens(ctx context.Context, handlerName string) ([]*auth.CachedTokenInfo, error) {
	resp, err := c.client.ListCachedTokens(ctx, &proto.ListCachedTokensRequest{HandlerName: handlerName})
	if err != nil {
		return nil, err
	}
	tokens := make([]*auth.CachedTokenInfo, len(resp.Tokens))
	for i, t := range resp.Tokens {
		tokens[i] = protoToCachedTokenInfo(t)
	}
	return tokens, nil
}

// PurgeExpiredTokens implements AuthHandlerPlugin.PurgeExpiredTokens.
func (c *AuthHandlerGRPCClient) PurgeExpiredTokens(ctx context.Context, handlerName string) (int, error) {
	resp, err := c.client.PurgeExpiredTokens(ctx, &proto.PurgeExpiredTokensRequest{HandlerName: handlerName})
	if err != nil {
		return 0, err
	}
	return int(resp.PurgedCount), nil
}

// ConfigureAuthHandler implements AuthHandlerPlugin.ConfigureAuthHandler.
func (c *AuthHandlerGRPCClient) ConfigureAuthHandler(ctx context.Context, handlerName string, cfg ProviderConfig) error {
	protoSettings := make(map[string][]byte, len(cfg.Settings))
	for k, v := range cfg.Settings {
		protoSettings[k] = []byte(v)
	}

	resp, err := c.client.ConfigureAuthHandler(ctx, &proto.ConfigureAuthHandlerRequest{
		HandlerName:     handlerName,
		Quiet:           cfg.Quiet,
		NoColor:         cfg.NoColor,
		BinaryName:      cfg.BinaryName,
		HostServiceId:   c.hostServiceID,
		Settings:        protoSettings,
		ProtocolVersion: PluginProtocolVersion,
	})
	if err != nil {
		// Older plugins may not implement ConfigureAuthHandler.
		if s, ok := status.FromError(err); ok && s.Code() == codes.Unimplemented {
			return nil
		}
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("configure auth handler failed: %s", resp.Error)
	}
	return nil
}

// StopAuthHandler implements AuthHandlerPlugin.StopAuthHandler.
func (c *AuthHandlerGRPCClient) StopAuthHandler(ctx context.Context, handlerName string) error {
	resp, err := c.client.StopAuthHandler(ctx, &proto.StopAuthHandlerRequest{
		HandlerName: handlerName,
	})
	if err != nil {
		// Older plugins may not implement StopAuthHandler.
		if s, ok := status.FromError(err); ok && s.Code() == codes.Unimplemented {
			return nil
		}
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("stop auth handler failed: %s", resp.Error)
	}
	return nil
}

// ---- Conversion helpers ----

func claimsToProto(c *auth.Claims) *proto.Claims {
	if c == nil {
		return nil
	}
	return &proto.Claims{
		Issuer:        c.Issuer,
		Subject:       c.Subject,
		TenantId:      c.TenantID,
		ObjectId:      c.ObjectID,
		ClientId:      c.ClientID,
		Email:         c.Email,
		Name:          c.Name,
		Username:      c.Username,
		IssuedAtUnix:  c.IssuedAt.Unix(),
		ExpiresAtUnix: c.ExpiresAt.Unix(),
	}
}

func protoToClaims(c *proto.Claims) *auth.Claims {
	if c == nil {
		return nil
	}
	return &auth.Claims{
		Issuer:    c.Issuer,
		Subject:   c.Subject,
		TenantID:  c.TenantId,
		ObjectID:  c.ObjectId,
		ClientID:  c.ClientId,
		Email:     c.Email,
		Name:      c.Name,
		Username:  c.Username,
		IssuedAt:  time.Unix(c.IssuedAtUnix, 0),
		ExpiresAt: time.Unix(c.ExpiresAtUnix, 0),
	}
}

func statusToProto(s *auth.Status) *proto.GetStatusResponse {
	if s == nil {
		return &proto.GetStatusResponse{}
	}
	return &proto.GetStatusResponse{
		Authenticated:   s.Authenticated,
		Claims:          claimsToProto(s.Claims),
		ExpiresAtUnix:   s.ExpiresAt.Unix(),
		LastRefreshUnix: s.LastRefresh.Unix(),
		TenantId:        s.TenantID,
		IdentityType:    string(s.IdentityType),
		ClientId:        s.ClientID,
		TokenFile:       s.TokenFile,
		Scopes:          s.Scopes,
	}
}

func protoToStatus(resp *proto.GetStatusResponse) *auth.Status {
	if resp == nil {
		return &auth.Status{}
	}
	return &auth.Status{
		Authenticated: resp.Authenticated,
		Claims:        protoToClaims(resp.Claims),
		ExpiresAt:     time.Unix(resp.ExpiresAtUnix, 0),
		LastRefresh:   time.Unix(resp.LastRefreshUnix, 0),
		TenantID:      resp.TenantId,
		IdentityType:  auth.IdentityType(resp.IdentityType),
		ClientID:      resp.ClientId,
		TokenFile:     resp.TokenFile,
		Scopes:        resp.Scopes,
	}
}

func tokenResponseToProto(t *TokenResponse) *proto.GetTokenResponse {
	if t == nil {
		return &proto.GetTokenResponse{}
	}
	return &proto.GetTokenResponse{
		AccessToken:   t.AccessToken,
		TokenType:     t.TokenType,
		ExpiresAtUnix: t.ExpiresAt.Unix(),
		Scope:         t.Scope,
		CachedAtUnix:  t.CachedAt.Unix(),
		Flow:          string(t.Flow),
		SessionId:     t.SessionID,
	}
}

func protoToTokenResponse(resp *proto.GetTokenResponse) *TokenResponse {
	if resp == nil {
		return &TokenResponse{}
	}
	return &TokenResponse{
		AccessToken: resp.AccessToken,
		TokenType:   resp.TokenType,
		ExpiresAt:   time.Unix(resp.ExpiresAtUnix, 0),
		Scope:       resp.Scope,
		CachedAt:    time.Unix(resp.CachedAtUnix, 0),
		Flow:        auth.Flow(resp.Flow),
		SessionID:   resp.SessionId,
	}
}

func cachedTokenInfoToProto(t *auth.CachedTokenInfo) *proto.CachedTokenInfo {
	if t == nil {
		return &proto.CachedTokenInfo{}
	}
	return &proto.CachedTokenInfo{
		Handler:       t.Handler,
		TokenKind:     t.TokenKind,
		Scope:         t.Scope,
		TokenType:     t.TokenType,
		Flow:          string(t.Flow),
		ExpiresAtUnix: t.ExpiresAt.Unix(),
		CachedAtUnix:  t.CachedAt.Unix(),
		IsExpired:     t.IsExpired,
		SessionId:     t.SessionID,
	}
}

func protoToCachedTokenInfo(t *proto.CachedTokenInfo) *auth.CachedTokenInfo {
	if t == nil {
		return &auth.CachedTokenInfo{}
	}
	return &auth.CachedTokenInfo{
		Handler:   t.Handler,
		TokenKind: t.TokenKind,
		Scope:     t.Scope,
		TokenType: t.TokenType,
		Flow:      auth.Flow(t.Flow),
		ExpiresAt: time.Unix(t.ExpiresAtUnix, 0),
		CachedAt:  time.Unix(t.CachedAtUnix, 0),
		IsExpired: t.IsExpired,
		SessionID: t.SessionId,
	}
}
