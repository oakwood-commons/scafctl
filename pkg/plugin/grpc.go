package plugin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Masterminds/semver/v3"
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
	if len(desc.Schema.Properties) > 0 {
		protoDesc.Schema = &proto.Schema{
			Parameters: make(map[string]*proto.Parameter),
		}
		for name, prop := range desc.Schema.Properties {
			defaultValue, _ := json.Marshal(prop.Default)
			example, _ := json.Marshal(prop.Example)
			maxLen := int32(0)
			if prop.MaxLength != nil {
				//nolint:gosec // MaxLength is user-defined, overflow is acceptable behavior
				maxLen = int32(*prop.MaxLength)
			}
			protoDesc.Schema.Parameters[name] = &proto.Parameter{
				Type:         string(prop.Type),
				Required:     prop.Required,
				Description:  prop.Description,
				DefaultValue: defaultValue,
				Example:      string(example),
				MaxLength:    maxLen,
				Pattern:      prop.Pattern,
			}
		}
	}

	// Convert output schema
	if len(desc.OutputSchema.Properties) > 0 {
		protoDesc.OutputSchema = &proto.Schema{
			Parameters: make(map[string]*proto.Parameter),
		}
		for name, prop := range desc.OutputSchema.Properties {
			defaultValue, _ := json.Marshal(prop.Default)
			example, _ := json.Marshal(prop.Example)
			maxLen := int32(0)
			if prop.MaxLength != nil {
				//nolint:gosec // MaxLength is user-defined, overflow is acceptable behavior
				maxLen = int32(*prop.MaxLength)
			}
			protoDesc.OutputSchema.Parameters[name] = &proto.Parameter{
				Type:         string(prop.Type),
				Required:     prop.Required,
				Description:  prop.Description,
				DefaultValue: defaultValue,
				Example:      string(example),
				MaxLength:    maxLen,
				Pattern:      prop.Pattern,
			}
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
		desc.Schema = provider.SchemaDefinition{
			Properties: make(map[string]provider.PropertyDefinition),
		}
		for name, param := range protoDesc.Schema.Parameters {
			var defaultValue any
			if len(param.DefaultValue) > 0 {
				_ = json.Unmarshal(param.DefaultValue, &defaultValue)
			}
			var example any
			if param.Example != "" {
				_ = json.Unmarshal([]byte(param.Example), &example)
			}
			var maxLen *int
			if param.MaxLength > 0 {
				ml := int(param.MaxLength)
				maxLen = &ml
			}
			desc.Schema.Properties[name] = provider.PropertyDefinition{
				Type:        provider.PropertyType(param.Type),
				Required:    param.Required,
				Description: param.Description,
				Default:     defaultValue,
				Example:     example,
				MaxLength:   maxLen,
				Pattern:     param.Pattern,
			}
		}
	}

	// Convert output schema
	if protoDesc.OutputSchema != nil {
		desc.OutputSchema = provider.SchemaDefinition{
			Properties: make(map[string]provider.PropertyDefinition),
		}
		for name, param := range protoDesc.OutputSchema.Parameters {
			var defaultValue any
			if len(param.DefaultValue) > 0 {
				_ = json.Unmarshal(param.DefaultValue, &defaultValue)
			}
			var example any
			if param.Example != "" {
				_ = json.Unmarshal([]byte(param.Example), &example)
			}
			var maxLen *int
			if param.MaxLength > 0 {
				ml := int(param.MaxLength)
				maxLen = &ml
			}
			desc.OutputSchema.Properties[name] = provider.PropertyDefinition{
				Type:        provider.PropertyType(param.Type),
				Required:    param.Required,
				Description: param.Description,
				Default:     defaultValue,
				Example:     example,
				MaxLength:   maxLen,
				Pattern:     param.Pattern,
			}
		}
	}

	return desc
}
