// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/hashicorp/go-plugin"
	"github.com/oakwood-commons/scafctl/pkg/plugin/proto"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"google.golang.org/grpc"
)

const (
	// PluginName is the name used to identify the provider plugin
	PluginName = "provider"
)

// GRPCPlugin implements plugin.GRPCPlugin from hashicorp/go-plugin
type GRPCPlugin struct {
	plugin.Plugin
	Impl ProviderPlugin
}

// GRPCServer registers the gRPC server
//
//nolint:revive // broker is required by go-plugin interface
func (p *GRPCPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	proto.RegisterPluginServiceServer(s, &GRPCServer{Impl: p.Impl})
	return nil
}

// GRPCClient returns the gRPC client
//
//nolint:revive // ctx and broker are required by go-plugin interface
func (p *GRPCPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (any, error) {
	return &GRPCClient{client: proto.NewPluginServiceClient(c)}, nil
}

// GRPCServer implements the gRPC server for the plugin
type GRPCServer struct {
	proto.UnimplementedPluginServiceServer
	Impl ProviderPlugin
}

// GetProviders implements the GetProviders RPC
//
//nolint:revive // req is required by gRPC interface even if unused
func (s *GRPCServer) GetProviders(ctx context.Context, _ *proto.GetProvidersRequest) (*proto.GetProvidersResponse, error) {
	providers, err := s.Impl.GetProviders(ctx)
	if err != nil {
		return nil, err
	}
	return &proto.GetProvidersResponse{
		ProviderNames: providers,
	}, nil
}

// GetProviderDescriptor implements the GetProviderDescriptor RPC
func (s *GRPCServer) GetProviderDescriptor(ctx context.Context, req *proto.GetProviderDescriptorRequest) (*proto.GetProviderDescriptorResponse, error) {
	desc, err := s.Impl.GetProviderDescriptor(ctx, req.ProviderName)
	if err != nil {
		return nil, err
	}

	// Convert provider.Descriptor to proto.ProviderDescriptor
	protoDesc := descriptorToProto(desc)
	resp := &proto.GetProviderDescriptorResponse{
		Descriptor_: protoDesc,
	}
	return resp, nil
}

// ExecuteProvider implements the ExecuteProvider RPC
func (s *GRPCServer) ExecuteProvider(ctx context.Context, req *proto.ExecuteProviderRequest) (*proto.ExecuteProviderResponse, error) {
	// Decode input
	var input map[string]any
	if len(req.Input) > 0 {
		if err := json.Unmarshal(req.Input, &input); err != nil {
			//nolint:nilerr // Error is communicated via response, not gRPC error
			return &proto.ExecuteProviderResponse{
				Error: fmt.Sprintf("failed to decode input: %v", err),
			}, nil
		}
	}

	// Decode context data and enhance the context
	if len(req.Context) > 0 {
		var contextData map[string]any
		if err := json.Unmarshal(req.Context, &contextData); err != nil {
			//nolint:nilerr // Error is communicated via response, not gRPC error
			return &proto.ExecuteProviderResponse{
				Error: fmt.Sprintf("failed to decode context: %v", err),
			}, nil
		}
		// Add resolver context if provided
		if resolverCtx, ok := contextData["resolverContext"].(map[string]any); ok {
			ctx = provider.WithResolverContext(ctx, resolverCtx)
		}
	}

	// Set dry-run flag
	ctx = provider.WithDryRun(ctx, req.DryRun)

	// Execute provider
	output, err := s.Impl.ExecuteProvider(ctx, req.ProviderName, input)
	if err != nil {
		//nolint:nilerr // Error is communicated via response, not gRPC error
		return &proto.ExecuteProviderResponse{
			Error: err.Error(),
		}, nil
	}

	// Encode output
	outputBytes, err := json.Marshal(output)
	if err != nil {
		return &proto.ExecuteProviderResponse{
			Error: fmt.Sprintf("failed to encode output: %v", err),
		}, nil //nolint:nilerr // Error is communicated via response, not gRPC error
	}

	return &proto.ExecuteProviderResponse{
		Output: outputBytes,
	}, nil
}

// GRPCClient implements the gRPC client for the plugin
type GRPCClient struct {
	client proto.PluginServiceClient
}

// GetProviders implements ProviderPlugin.GetProviders
func (c *GRPCClient) GetProviders(ctx context.Context) ([]string, error) {
	resp, err := c.client.GetProviders(ctx, &proto.GetProvidersRequest{})
	if err != nil {
		return nil, err
	}
	return resp.ProviderNames, nil
}

// GetProviderDescriptor implements ProviderPlugin.GetProviderDescriptor
func (c *GRPCClient) GetProviderDescriptor(ctx context.Context, providerName string) (*provider.Descriptor, error) {
	resp, err := c.client.GetProviderDescriptor(ctx, &proto.GetProviderDescriptorRequest{
		ProviderName: providerName,
	})
	if err != nil {
		return nil, err
	}

	// Convert proto.ProviderDescriptor to provider.Descriptor
	return protoToDescriptor(resp.GetDescriptor_()), nil
}

// ExecuteProvider implements ProviderPlugin.ExecuteProvider
func (c *GRPCClient) ExecuteProvider(ctx context.Context, providerName string, input map[string]any) (*provider.Output, error) {
	// Encode input
	inputBytes, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to encode input: %w", err)
	}

	// Encode context data
	contextData := make(map[string]any)
	if resolverCtx, ok := provider.ResolverContextFromContext(ctx); ok {
		contextData["resolverContext"] = resolverCtx
	}
	contextBytes, err := json.Marshal(contextData)
	if err != nil {
		return nil, fmt.Errorf("failed to encode context: %w", err)
	}

	resp, err := c.client.ExecuteProvider(ctx, &proto.ExecuteProviderRequest{
		ProviderName: providerName,
		Input:        inputBytes,
		Context:      contextBytes,
		DryRun:       provider.DryRunFromContext(ctx),
	})
	if err != nil {
		return nil, err
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("provider execution failed: %s", resp.Error)
	}

	// Decode output
	var output provider.Output
	if err := json.Unmarshal(resp.Output, &output); err != nil {
		return nil, fmt.Errorf("failed to decode output: %w", err)
	}

	return &output, nil
}

// descriptorToProto converts provider.Descriptor to proto.ProviderDescriptor
func descriptorToProto(desc *provider.Descriptor) *proto.ProviderDescriptor {
	version := ""
	if desc.Version != nil {
		version = desc.Version.String()
	}
	protoDesc := &proto.ProviderDescriptor{
		Name:         desc.Name,
		DisplayName:  desc.DisplayName,
		Description:  desc.Description,
		Version:      version,
		Category:     desc.Category,
		Capabilities: make([]string, len(desc.Capabilities)),
	}

	for i, cap := range desc.Capabilities {
		protoDesc.Capabilities[i] = string(cap)
	}

	// Convert schema
	if desc.Schema != nil && len(desc.Schema.Properties) > 0 {
		protoDesc.Schema = &proto.Schema{
			Parameters: make(map[string]*proto.Parameter),
		}
		// Build required set
		requiredSet := make(map[string]bool, len(desc.Schema.Required))
		for _, name := range desc.Schema.Required {
			requiredSet[name] = true
		}
		for name, prop := range desc.Schema.Properties {
			defaultValue, _ := json.Marshal(prop.Default)
			exampleStr := ""
			if len(prop.Examples) > 0 {
				exampleBytes, _ := json.Marshal(prop.Examples[0])
				exampleStr = string(exampleBytes)
			}
			maxLen := int32(0)
			if prop.MaxLength != nil {
				//nolint:gosec // MaxLength is user-defined, overflow is acceptable behavior
				maxLen = int32(*prop.MaxLength)
			}
			protoDesc.Schema.Parameters[name] = &proto.Parameter{
				Type:         prop.Type,
				Required:     requiredSet[name],
				Description:  prop.Description,
				DefaultValue: defaultValue,
				Example:      exampleStr,
				MaxLength:    maxLen,
				Pattern:      prop.Pattern,
			}
		}
	}

	// Convert output schemas
	if len(desc.OutputSchemas) > 0 {
		protoDesc.OutputSchemas = make(map[string]*proto.Schema)
		for cap, schema := range desc.OutputSchemas {
			if schema == nil || len(schema.Properties) == 0 {
				continue
			}
			protoSchema := &proto.Schema{
				Parameters: make(map[string]*proto.Parameter),
			}
			// Build required set
			requiredSet := make(map[string]bool, len(schema.Required))
			for _, name := range schema.Required {
				requiredSet[name] = true
			}
			for name, prop := range schema.Properties {
				defaultValue, _ := json.Marshal(prop.Default)
				exampleStr := ""
				if len(prop.Examples) > 0 {
					exampleBytes, _ := json.Marshal(prop.Examples[0])
					exampleStr = string(exampleBytes)
				}
				maxLen := int32(0)
				if prop.MaxLength != nil {
					//nolint:gosec // MaxLength is user-defined, overflow is acceptable behavior
					maxLen = int32(*prop.MaxLength)
				}
				protoSchema.Parameters[name] = &proto.Parameter{
					Type:         prop.Type,
					Required:     requiredSet[name],
					Description:  prop.Description,
					DefaultValue: defaultValue,
					Example:      exampleStr,
					MaxLength:    maxLen,
					Pattern:      prop.Pattern,
				}
			}
			protoDesc.OutputSchemas[string(cap)] = protoSchema
		}
	}

	return protoDesc
}

// protoToDescriptor converts proto.ProviderDescriptor to provider.Descriptor
//
//nolint:dupl // Schema and OutputSchema conversion are intentionally similar
func protoToDescriptor(protoDesc *proto.ProviderDescriptor) *provider.Descriptor {
	var version *semver.Version
	if protoDesc.Version != "" {
		version, _ = semver.NewVersion(protoDesc.Version)
	}
	desc := &provider.Descriptor{
		Name:         protoDesc.Name,
		DisplayName:  protoDesc.DisplayName,
		Description:  protoDesc.Description,
		Version:      version,
		Category:     protoDesc.Category,
		Capabilities: make([]provider.Capability, len(protoDesc.Capabilities)),
	}

	for i, cap := range protoDesc.Capabilities {
		desc.Capabilities[i] = provider.Capability(cap)
	}

	// Convert schema
	if protoDesc.Schema != nil {
		desc.Schema = &jsonschema.Schema{
			Type:       "object",
			Properties: make(map[string]*jsonschema.Schema),
		}
		var required []string
		for name, param := range protoDesc.Schema.Parameters {
			prop := &jsonschema.Schema{
				Type:        param.Type,
				Description: param.Description,
				Pattern:     param.Pattern,
			}
			if len(param.DefaultValue) > 0 {
				prop.Default = json.RawMessage(param.DefaultValue)
			}
			if param.Example != "" {
				var example any
				_ = json.Unmarshal([]byte(param.Example), &example)
				prop.Examples = []any{example}
			}
			if param.MaxLength > 0 {
				ml := int(param.MaxLength)
				prop.MaxLength = &ml
			}
			if param.Required {
				required = append(required, name)
			}
			desc.Schema.Properties[name] = prop
		}
		desc.Schema.Required = required
	}

	// Convert output schemas
	if len(protoDesc.OutputSchemas) > 0 {
		desc.OutputSchemas = make(map[provider.Capability]*jsonschema.Schema)
		for capStr, protoSchema := range protoDesc.OutputSchemas {
			if protoSchema == nil {
				continue
			}
			schema := &jsonschema.Schema{
				Type:       "object",
				Properties: make(map[string]*jsonschema.Schema),
			}
			var required []string
			for name, param := range protoSchema.Parameters {
				prop := &jsonschema.Schema{
					Type:        param.Type,
					Description: param.Description,
					Pattern:     param.Pattern,
				}
				if len(param.DefaultValue) > 0 {
					prop.Default = json.RawMessage(param.DefaultValue)
				}
				if param.Example != "" {
					var example any
					_ = json.Unmarshal([]byte(param.Example), &example)
					prop.Examples = []any{example}
				}
				if param.MaxLength > 0 {
					ml := int(param.MaxLength)
					prop.MaxLength = &ml
				}
				if param.Required {
					required = append(required, name)
				}
				schema.Properties[name] = prop
			}
			schema.Required = required
			desc.OutputSchemas[provider.Capability(capStr)] = schema
		}
	}

	return desc
}
