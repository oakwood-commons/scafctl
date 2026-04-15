// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/hashicorp/go-plugin"
	sdkplugin "github.com/oakwood-commons/scafctl-plugin-sdk/plugin"
	"github.com/oakwood-commons/scafctl-plugin-sdk/plugin/proto"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// PluginName is the name used to identify the provider plugin.
const PluginName = sdkplugin.PluginName

// GRPCPlugin implements plugin.GRPCPlugin from hashicorp/go-plugin
type GRPCPlugin struct {
	plugin.Plugin
	Impl ProviderPlugin
	// HostDeps holds host-side dependencies for the HostService callback server.
	// Set by the host before starting the plugin. Nil on the plugin side.
	HostDeps *HostServiceDeps
}

// GRPCServer registers the gRPC server (plugin side).
// When HostDeps is set, a HostService callback server is started on the host via
// the broker and its service ID is stored so that ConfigureProvider can relay it.
func (p *GRPCPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	proto.RegisterPluginServiceServer(s, &GRPCServer{Impl: p.Impl, broker: broker})
	return nil
}

// GRPCClient returns the gRPC client (host side).
// If HostDeps is non-nil, a HostService gRPC server is started via the broker and
// the returned GRPCClient holds the broker service ID for later ConfigureProvider calls.
func (p *GRPCPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (any, error) {
	var hostServiceID uint32
	if p.HostDeps != nil {
		hostServiceID = broker.NextId()
		deps := p.HostDeps
		lgr := logger.FromContext(ctx)
		// AcceptAndServe blocks until the plugin connection closes, which is
		// managed by the hashicorp/go-plugin framework. The broker server is
		// shut down automatically when Client.Kill() is called, so no explicit
		// cancellation is needed here.
		go func() {
			broker.AcceptAndServe(hostServiceID, func(opts []grpc.ServerOption) *grpc.Server {
				s := grpc.NewServer(opts...)
				proto.RegisterHostServiceServer(s, &HostServiceServer{Deps: *deps})
				return s
			})
			lgr.V(1).Info("HostService broker stopped", "serviceID", hostServiceID)
		}()
	}
	return &GRPCClient{
		client:        proto.NewPluginServiceClient(c),
		broker:        broker,
		hostServiceID: hostServiceID,
	}, nil
}

// GRPCServer implements the gRPC server for the plugin
type GRPCServer struct {
	proto.UnimplementedPluginServiceServer
	Impl   ProviderPlugin
	broker *plugin.GRPCBroker
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

	ctx, ctxErr := applyRequestContext(ctx, req)
	if ctxErr != nil {
		//nolint:nilerr // Error is communicated via response, not gRPC error
		return &proto.ExecuteProviderResponse{Error: ctxErr.Error()}, nil
	}

	// Execute provider
	output, err := s.Impl.ExecuteProvider(ctx, req.ProviderName, input)
	if err != nil {
		//nolint:nilerr // Error is communicated via response, not gRPC error
		resp := &proto.ExecuteProviderResponse{
			Error:    err.Error(),
			ExitCode: safeIntToInt32(exitcode.GetCode(err)),
		}
		resp.Diagnostics = errorToDiagnostics(err)
		return resp, nil
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

// ConfigureProvider implements the ConfigureProvider RPC
func (s *GRPCServer) ConfigureProvider(ctx context.Context, req *proto.ConfigureProviderRequest) (*proto.ConfigureProviderResponse, error) {
	settings := make(map[string]json.RawMessage, len(req.Settings))
	for k, v := range req.Settings {
		settings[k] = json.RawMessage(v)
	}

	cfg := ProviderConfig{
		Quiet:         req.Quiet,
		NoColor:       req.NoColor,
		BinaryName:    req.BinaryName,
		HostServiceID: req.HostServiceId,
		Settings:      settings,
	}

	if err := s.Impl.ConfigureProvider(ctx, req.ProviderName, cfg); err != nil {
		//nolint:nilerr // Error is communicated via response, not gRPC error
		return &proto.ConfigureProviderResponse{
			Error: err.Error(),
		}, nil
	}

	return &proto.ConfigureProviderResponse{
		ProtocolVersion: PluginProtocolVersion,
	}, nil
}

// unmarshalIterationContext deserializes a proto.IterationContext and injects
// it into the Go context. Returns the unmodified context when iter is nil.
func unmarshalIterationContext(ctx context.Context, iter *proto.IterationContext) (context.Context, error) {
	if iter == nil {
		return ctx, nil
	}
	var item any
	if len(iter.Item) > 0 {
		if err := json.Unmarshal(iter.Item, &item); err != nil {
			return ctx, fmt.Errorf("failed to decode iteration item: %w", err)
		}
	}
	return provider.WithIterationContext(ctx, &provider.IterationContext{
		Item:       item,
		Index:      int(iter.Index),
		ItemAlias:  iter.ItemAlias,
		IndexAlias: iter.IndexAlias,
	}), nil
}

// unmarshalSolutionMeta injects proto.SolutionMeta into the Go context.
// Returns the unmodified context when meta is nil.
func unmarshalSolutionMeta(ctx context.Context, meta *proto.SolutionMeta) context.Context {
	if meta == nil {
		return ctx
	}
	return provider.WithSolutionMetadata(ctx, &provider.SolutionMeta{
		Name:        meta.Name,
		Version:     meta.Version,
		DisplayName: meta.DisplayName,
		Description: meta.Description,
		Category:    meta.Category,
		Tags:        meta.Tags,
	})
}

// safeIntToInt32 converts an int to int32 with clamping to prevent overflow.
func safeIntToInt32(v int) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return int32(v) //nolint:gosec // bounds checked above
}

// errorToDiagnostics converts an error into a slice of proto Diagnostics.
// If the error is an ExitError, the summary includes the exit code description.
func errorToDiagnostics(err error) []*proto.Diagnostic {
	if err == nil {
		return nil
	}
	d := &proto.Diagnostic{
		Severity: proto.Diagnostic_ERROR,
		Summary:  err.Error(),
	}
	var exitErr *exitcode.ExitError
	if errors.As(err, &exitErr) {
		d.Detail = fmt.Sprintf("exit code %d: %s", exitErr.Code, exitcode.Description(exitErr.Code))
	}
	return []*proto.Diagnostic{d}
}

// diagnosticsToError converts proto Diagnostics into a Go error. Returns nil
// when there are no ERROR-level diagnostics. Warnings are logged but do not
// produce an error.
func diagnosticsToError(ctx context.Context, diags []*proto.Diagnostic) error {
	if len(diags) == 0 {
		return nil
	}
	var errs []error
	lgr := logger.FromContext(ctx)
	for _, d := range diags {
		switch d.Severity {
		case proto.Diagnostic_ERROR:
			summary := d.Summary
			if summary == "" {
				summary = "unknown error (empty diagnostic summary)"
			}
			switch {
			case d.Attribute != "" && d.Detail != "":
				errs = append(errs, fmt.Errorf("%s: %s (attribute: %s)", summary, d.Detail, d.Attribute))
			case d.Attribute != "":
				errs = append(errs, fmt.Errorf("%s (attribute: %s)", summary, d.Attribute))
			case d.Detail != "":
				errs = append(errs, fmt.Errorf("%s: %s", summary, d.Detail))
			default:
				errs = append(errs, errors.New(summary))
			}
		case proto.Diagnostic_WARNING:
			lgr.V(1).Info("plugin warning", "summary", d.Summary, "detail", d.Detail, "attribute", d.Attribute)
		case proto.Diagnostic_INVALID:
			lgr.V(1).Info("plugin diagnostic with invalid severity", "summary", d.Summary, "detail", d.Detail, "attribute", d.Attribute)
		}
	}
	return errors.Join(errs...)
}

// applyRequestContext decodes context data, iteration context, parameters,
// and other execution-scoped values from an ExecuteProviderRequest into the
// Go context. Returns the enriched context and an error if any decoding fails.
func applyRequestContext(ctx context.Context, req *proto.ExecuteProviderRequest) (context.Context, error) {
	// Decode context data
	if len(req.Context) > 0 {
		var contextData map[string]any
		if err := json.Unmarshal(req.Context, &contextData); err != nil {
			return ctx, fmt.Errorf("failed to decode context: %w", err)
		}
		if resolverCtx, ok := contextData["resolverContext"].(map[string]any); ok {
			ctx = provider.WithResolverContext(ctx, resolverCtx)
		}
	}

	ctx = provider.WithDryRun(ctx, req.DryRun)

	if req.ExecutionMode != "" {
		ctx = provider.WithExecutionMode(ctx, provider.Capability(req.ExecutionMode))
	}
	if req.WorkingDirectory != "" {
		ctx = provider.WithWorkingDirectory(ctx, req.WorkingDirectory)
	}
	if req.OutputDirectory != "" {
		ctx = provider.WithOutputDirectory(ctx, req.OutputDirectory)
	}
	if req.ConflictStrategy != "" {
		ctx = provider.WithConflictStrategy(ctx, req.ConflictStrategy)
	}
	if req.Backup {
		ctx = provider.WithBackup(ctx, req.Backup)
	}

	// Iteration context
	var err error
	ctx, err = unmarshalIterationContext(ctx, req.IterationContext)
	if err != nil {
		return ctx, err
	}

	// Parameters
	if len(req.Parameters) > 0 {
		var params map[string]any
		if err := json.Unmarshal(req.Parameters, &params); err != nil {
			return ctx, fmt.Errorf("failed to decode parameters: %w", err)
		}
		ctx = provider.WithParameters(ctx, params)
	}

	// Solution metadata
	ctx = unmarshalSolutionMeta(ctx, req.SolutionMetadata)

	return ctx, nil
}

// streamForwarder converts StreamChunk values into proto stream messages,
// serialising the send path behind a mutex so the callback is safe for
// concurrent use. It tracks the first send error so later writes to a broken
// stream are silently dropped.
type streamForwarder struct {
	stream proto.PluginService_ExecuteProviderStreamServer
	mu     sync.Mutex
	err    error
}

func newStreamForwarder(stream proto.PluginService_ExecuteProviderStreamServer) *streamForwarder {
	return &streamForwarder{stream: stream}
}

func (f *streamForwarder) forward(chunk StreamChunk) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.err != nil {
		return
	}

	var protoChunk proto.ExecuteProviderStreamChunk
	switch {
	case chunk.Stdout != nil:
		protoChunk.Chunk = &proto.ExecuteProviderStreamChunk_Stdout{Stdout: chunk.Stdout}
	case chunk.Stderr != nil:
		protoChunk.Chunk = &proto.ExecuteProviderStreamChunk_Stderr{Stderr: chunk.Stderr}
	case chunk.Result != nil || chunk.Error != "":
		outputBytes, marshalErr := json.Marshal(chunk.Result)
		if marshalErr != nil {
			protoChunk.Chunk = &proto.ExecuteProviderStreamChunk_Result{
				Result: &proto.ExecuteProviderResponse{
					Error: fmt.Sprintf("failed to encode output: %v", marshalErr),
				},
			}
		} else {
			protoChunk.Chunk = &proto.ExecuteProviderStreamChunk_Result{
				Result: &proto.ExecuteProviderResponse{
					Output: outputBytes,
					Error:  chunk.Error,
				},
			}
		}
	default:
		// Empty chunk (no stdout, stderr, result, or error) — drop silently.
		return
	}

	if sErr := f.stream.Send(&protoChunk); sErr != nil {
		f.err = sErr
	}
}

// ExecuteProviderStream implements the streaming ExecuteProvider RPC
func (s *GRPCServer) ExecuteProviderStream(req *proto.ExecuteProviderRequest, stream proto.PluginService_ExecuteProviderStreamServer) error {
	ctx := stream.Context()

	// Decode input
	var input map[string]any
	if len(req.Input) > 0 {
		if err := json.Unmarshal(req.Input, &input); err != nil {
			return stream.Send(&proto.ExecuteProviderStreamChunk{
				Chunk: &proto.ExecuteProviderStreamChunk_Result{
					Result: &proto.ExecuteProviderResponse{
						Error: fmt.Sprintf("failed to decode input: %v", err),
					},
				},
			})
		}
	}

	ctx, ctxErr := applyRequestContext(ctx, req)
	if ctxErr != nil {
		return stream.Send(&proto.ExecuteProviderStreamChunk{
			Chunk: &proto.ExecuteProviderStreamChunk_Result{
				Result: &proto.ExecuteProviderResponse{Error: ctxErr.Error()},
			},
		})
	}

	forwarder := newStreamForwarder(stream)
	err := s.Impl.ExecuteProviderStream(ctx, req.ProviderName, input, forwarder.forward)
	if err != nil {
		// Return gRPC Unimplemented so the client can detect streaming-not-supported
		// and fall back to unary execution.
		if errors.Is(err, ErrStreamingNotSupported) {
			return status.Error(codes.Unimplemented, err.Error())
		}
		return stream.Send(&proto.ExecuteProviderStreamChunk{
			Chunk: &proto.ExecuteProviderStreamChunk_Result{
				Result: &proto.ExecuteProviderResponse{
					Error:       err.Error(),
					ExitCode:    safeIntToInt32(exitcode.GetCode(err)),
					Diagnostics: errorToDiagnostics(err),
				},
			},
		})
	}

	return nil
}

// DescribeWhatIf implements the DescribeWhatIf RPC
func (s *GRPCServer) DescribeWhatIf(ctx context.Context, req *proto.DescribeWhatIfRequest) (*proto.DescribeWhatIfResponse, error) {
	var input map[string]any
	if len(req.Input) > 0 {
		if err := json.Unmarshal(req.Input, &input); err != nil {
			//nolint:nilerr // Error is communicated via response, not gRPC error
			return &proto.DescribeWhatIfResponse{
				Error: fmt.Sprintf("failed to decode input: %v", err),
			}, nil
		}
	}

	description, err := s.Impl.DescribeWhatIf(ctx, req.ProviderName, input)
	if err != nil {
		//nolint:nilerr // Error is communicated via response, not gRPC error
		return &proto.DescribeWhatIfResponse{
			Error: err.Error(),
		}, nil
	}

	return &proto.DescribeWhatIfResponse{
		Description: description,
	}, nil
}

// ExtractDependencies implements the ExtractDependencies RPC
func (s *GRPCServer) ExtractDependencies(ctx context.Context, req *proto.ExtractDependenciesRequest) (*proto.ExtractDependenciesResponse, error) {
	var inputs map[string]any
	if len(req.Inputs) > 0 {
		if err := json.Unmarshal(req.Inputs, &inputs); err != nil {
			//nolint:nilerr // Error is communicated via response, not gRPC error
			return &proto.ExtractDependenciesResponse{
				Error: fmt.Sprintf("failed to decode inputs: %v", err),
			}, nil
		}
	}

	deps, err := s.Impl.ExtractDependencies(ctx, req.ProviderName, inputs)
	if err != nil {
		//nolint:nilerr // Error is communicated via response, not gRPC error
		return &proto.ExtractDependenciesResponse{
			Error: err.Error(),
		}, nil
	}

	return &proto.ExtractDependenciesResponse{
		Dependencies: deps,
	}, nil
}

// StopProvider implements the StopProvider RPC
func (s *GRPCServer) StopProvider(ctx context.Context, req *proto.StopProviderRequest) (*proto.StopProviderResponse, error) {
	if err := s.Impl.StopProvider(ctx, req.ProviderName); err != nil {
		//nolint:nilerr // Error is communicated via response, not gRPC error
		return &proto.StopProviderResponse{Error: err.Error()}, nil
	}
	return &proto.StopProviderResponse{}, nil
}

// GRPCClient implements the gRPC client for the plugin
type GRPCClient struct {
	client        proto.PluginServiceClient
	broker        *plugin.GRPCBroker
	hostServiceID uint32
}

// HostServiceID returns the broker service ID of the HostService callback server.
// Returns 0 if no HostService was registered.
func (c *GRPCClient) HostServiceID() uint32 {
	return c.hostServiceID
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
	return protoToDescriptor(resp.GetDescriptor_())
}

// ExecuteProvider implements ProviderPlugin.ExecuteProvider
func (c *GRPCClient) ExecuteProvider(ctx context.Context, providerName string, input map[string]any) (*provider.Output, error) {
	// Encode input
	inputBytes, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to encode input: %w", err)
	}

	req, err := buildExecuteProviderRequest(ctx, providerName, inputBytes)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.ExecuteProvider(ctx, req)
	if err != nil {
		return nil, err
	}

	if resp.Error != "" {
		err := fmt.Errorf("provider execution failed: %s", resp.Error)
		// Prefer diagnostics for richer context, fall back to plain error
		if diagErr := diagnosticsToError(ctx, resp.Diagnostics); diagErr != nil {
			err = diagErr
		}
		// Reconstruct ExitError when the plugin transmitted a non-zero exit code
		if resp.ExitCode != 0 {
			return nil, exitcode.WithCode(err, int(resp.ExitCode))
		}
		return nil, err
	}

	// Decode output
	var output provider.Output
	if err := json.Unmarshal(resp.Output, &output); err != nil {
		return nil, fmt.Errorf("failed to decode output: %w", err)
	}

	return &output, nil
}

// ConfigureProvider implements ProviderPlugin.ConfigureProvider
func (c *GRPCClient) ConfigureProvider(ctx context.Context, providerName string, cfg ProviderConfig) error {
	protoSettings := make(map[string][]byte, len(cfg.Settings))
	for k, v := range cfg.Settings {
		protoSettings[k] = []byte(v)
	}

	resp, err := c.client.ConfigureProvider(ctx, &proto.ConfigureProviderRequest{
		ProviderName:    providerName,
		HostServiceId:   c.hostServiceID,
		Quiet:           cfg.Quiet,
		NoColor:         cfg.NoColor,
		BinaryName:      cfg.BinaryName,
		Settings:        protoSettings,
		ProtocolVersion: PluginProtocolVersion,
	})
	if err != nil {
		// Older plugins may not implement ConfigureProvider.
		if s, ok := status.FromError(err); ok && s.Code() == codes.Unimplemented {
			return nil
		}
		return err
	}

	if resp.Error != "" {
		return fmt.Errorf("configure provider failed: %s", resp.Error)
	}

	return nil
}

// ExecuteProviderStream implements ProviderPlugin.ExecuteProviderStream
func (c *GRPCClient) ExecuteProviderStream(ctx context.Context, providerName string, input map[string]any, cb func(StreamChunk)) error {
	inputBytes, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("failed to encode input: %w", err)
	}

	req, err := buildExecuteProviderRequest(ctx, providerName, inputBytes)
	if err != nil {
		return err
	}
	stream, err := c.client.ExecuteProviderStream(ctx, req)
	if err != nil {
		// Older plugins may not implement streaming.
		if s, ok := status.FromError(err); ok && s.Code() == codes.Unimplemented {
			return ErrStreamingNotSupported
		}
		return err
	}

	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			// Handle streaming-not-supported on the receive path too. In
			// server-streaming RPCs, the gRPC status error may arrive on
			// the first Recv() rather than the initial call.
			if s, ok := status.FromError(err); ok && s.Code() == codes.Unimplemented {
				return ErrStreamingNotSupported
			}
			return fmt.Errorf("stream receive error: %w", err)
		}

		switch v := chunk.Chunk.(type) {
		case *proto.ExecuteProviderStreamChunk_Stdout:
			cb(StreamChunk{Stdout: v.Stdout})
		case *proto.ExecuteProviderStreamChunk_Stderr:
			cb(StreamChunk{Stderr: v.Stderr})
		case *proto.ExecuteProviderStreamChunk_Result:
			if v.Result.Error != "" {
				cb(StreamChunk{Error: v.Result.Error})
				err := fmt.Errorf("provider execution failed: %s", v.Result.Error)
				// Prefer diagnostics for richer context, fall back to plain error
				if diagErr := diagnosticsToError(ctx, v.Result.Diagnostics); diagErr != nil {
					err = diagErr
				}
				// Reconstruct ExitError when the plugin transmitted a non-zero exit code
				if v.Result.ExitCode != 0 {
					return exitcode.WithCode(err, int(v.Result.ExitCode))
				}
				return err
			}
			var output provider.Output
			if err := json.Unmarshal(v.Result.Output, &output); err != nil {
				return fmt.Errorf("failed to decode output: %w", err)
			}
			cb(StreamChunk{Result: &output})
		}
	}
}

// marshalIterationContext serializes a provider.IterationContext into a proto.IterationContext.
// Returns nil when no iteration context is present.
func marshalIterationContext(ctx context.Context) (*proto.IterationContext, error) {
	iterCtx, ok := provider.IterationContextFromContext(ctx)
	if !ok {
		return nil, nil
	}
	itemBytes, err := json.Marshal(iterCtx.Item)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal iteration item: %w", err)
	}
	return &proto.IterationContext{
		Item:       itemBytes,
		Index:      int32(iterCtx.Index), //nolint:gosec // Index is bounded by forEach limits
		ItemAlias:  iterCtx.ItemAlias,
		IndexAlias: iterCtx.IndexAlias,
	}, nil
}

// marshalSolutionMeta serializes provider.SolutionMeta from context into a proto.SolutionMeta.
// Returns nil when no solution metadata is present.
func marshalSolutionMeta(ctx context.Context) *proto.SolutionMeta {
	meta, ok := provider.SolutionMetadataFromContext(ctx)
	if !ok {
		return nil
	}
	return &proto.SolutionMeta{
		Name:        meta.Name,
		Version:     meta.Version,
		DisplayName: meta.DisplayName,
		Description: meta.Description,
		Category:    meta.Category,
		Tags:        meta.Tags,
	}
}

// buildExecuteProviderRequest constructs an ExecuteProviderRequest with all
// context values serialized from the Go context. Returns an error if any
// required serialization fails.
func buildExecuteProviderRequest(ctx context.Context, providerName string, inputBytes []byte) (*proto.ExecuteProviderRequest, error) {
	// Encode resolver context
	contextData := make(map[string]any)
	if resolverCtx, ok := provider.ResolverContextFromContext(ctx); ok {
		contextData["resolverContext"] = resolverCtx
	}
	contextBytes, err := json.Marshal(contextData)
	if err != nil {
		return nil, fmt.Errorf("failed to encode context: %w", err)
	}

	req := &proto.ExecuteProviderRequest{
		ProviderName: providerName,
		Input:        inputBytes,
		Context:      contextBytes,
		DryRun:       provider.DryRunFromContext(ctx),
	}

	// Execution mode
	if mode, ok := provider.ExecutionModeFromContext(ctx); ok {
		req.ExecutionMode = string(mode)
	}

	// Working directory
	if dir, ok := provider.WorkingDirectoryFromContext(ctx); ok {
		req.WorkingDirectory = dir
	}

	// Output directory
	if dir, ok := provider.OutputDirectoryFromContext(ctx); ok {
		req.OutputDirectory = dir
	}

	// Conflict strategy
	if strategy, ok := provider.ConflictStrategyFromContext(ctx); ok {
		req.ConflictStrategy = strategy
	}

	// Backup
	if backup, ok := provider.BackupFromContext(ctx); ok {
		req.Backup = backup
	}

	// Iteration context
	iterProto, iterErr := marshalIterationContext(ctx)
	if iterErr != nil {
		return nil, iterErr
	}
	req.IterationContext = iterProto

	// Parameters
	if params, ok := provider.ParametersFromContext(ctx); ok {
		paramBytes, marshalErr := json.Marshal(params)
		if marshalErr != nil {
			return nil, fmt.Errorf("failed to marshal parameters: %w", marshalErr)
		}
		req.Parameters = paramBytes
	}

	// Solution metadata
	req.SolutionMetadata = marshalSolutionMeta(ctx)

	return req, nil
}

// DescribeWhatIf implements ProviderPlugin.DescribeWhatIf
func (c *GRPCClient) DescribeWhatIf(ctx context.Context, providerName string, input map[string]any) (string, error) {
	inputBytes, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("failed to encode input: %w", err)
	}

	resp, err := c.client.DescribeWhatIf(ctx, &proto.DescribeWhatIfRequest{
		ProviderName: providerName,
		Input:        inputBytes,
	})
	if err != nil {
		// Older plugins may not implement DescribeWhatIf — return empty
		// so the caller falls back to a generic message.
		if s, ok := status.FromError(err); ok && s.Code() == codes.Unimplemented {
			return "", nil
		}
		return "", err
	}

	if resp.Error != "" {
		return "", fmt.Errorf("DescribeWhatIf failed: %s", resp.Error)
	}

	return resp.Description, nil
}

// ExtractDependencies implements ProviderPlugin.ExtractDependencies
func (c *GRPCClient) ExtractDependencies(ctx context.Context, providerName string, inputs map[string]any) ([]string, error) {
	inputBytes, err := json.Marshal(inputs)
	if err != nil {
		return nil, fmt.Errorf("failed to encode inputs: %w", err)
	}

	resp, err := c.client.ExtractDependencies(ctx, &proto.ExtractDependenciesRequest{
		ProviderName: providerName,
		Inputs:       inputBytes,
	})
	if err != nil {
		// Older plugins may not implement ExtractDependencies — return nil
		// so the host falls back to generic extraction.
		if s, ok := status.FromError(err); ok && s.Code() == codes.Unimplemented {
			return nil, nil
		}
		return nil, err
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("extract dependencies failed: %s", resp.Error)
	}

	return resp.Dependencies, nil
}

// StopProvider implements ProviderPlugin.StopProvider
func (c *GRPCClient) StopProvider(ctx context.Context, providerName string) error {
	resp, err := c.client.StopProvider(ctx, &proto.StopProviderRequest{
		ProviderName: providerName,
	})
	if err != nil {
		// Older plugins may not implement StopProvider.
		if s, ok := status.FromError(err); ok && s.Code() == codes.Unimplemented {
			return nil
		}
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("stop provider failed: %s", resp.Error)
	}
	return nil
}

// protoSchemaToJSON converts a proto.Schema to a jsonschema.Schema (structured
// fallback when raw_schema bytes are not available). Preserves all Parameter
// fields supported by the wire format.
func protoSchemaToJSON(ps *proto.Schema) *jsonschema.Schema {
	schema := &jsonschema.Schema{
		Type:       "object",
		Properties: make(map[string]*jsonschema.Schema),
	}
	var required []string
	for name, param := range ps.Parameters {
		prop := protoParamToJSON(param)
		if param.Required {
			required = append(required, name)
		}
		schema.Properties[name] = prop
	}
	schema.Required = required
	return schema
}

// unmarshalSchemaOrFallback deserializes a schema from raw JSON bytes when
// available, falling back to structured proto.Schema conversion for older plugins.
func unmarshalSchemaOrFallback(raw []byte, ps *proto.Schema) *jsonschema.Schema {
	if len(raw) > 0 {
		var schema jsonschema.Schema
		if err := json.Unmarshal(raw, &schema); err == nil {
			return &schema
		}
	}
	if ps != nil {
		return protoSchemaToJSON(ps)
	}
	return nil
}

// unmarshalOutputSchemas deserializes output schemas, preferring raw JSON
// bytes for lossless round-tripping, falling back to structured proto.Schema.
func unmarshalOutputSchemas(rawSchemas map[string][]byte, protoSchemas map[string]*proto.Schema) map[provider.Capability]*jsonschema.Schema {
	if len(rawSchemas) > 0 {
		out := make(map[provider.Capability]*jsonschema.Schema, len(rawSchemas))
		for capStr, raw := range rawSchemas {
			var schema jsonschema.Schema
			if err := json.Unmarshal(raw, &schema); err == nil {
				out[provider.Capability(capStr)] = &schema
			}
		}
		return out
	}
	if len(protoSchemas) > 0 {
		out := make(map[provider.Capability]*jsonschema.Schema, len(protoSchemas))
		for capStr, ps := range protoSchemas {
			if ps == nil {
				continue
			}
			out[provider.Capability(capStr)] = protoSchemaToJSON(ps)
		}
		return out
	}
	return nil
}

// protoParamToJSON converts a proto.Parameter back to a jsonschema.Schema property.
func protoParamToJSON(param *proto.Parameter) *jsonschema.Schema {
	prop := &jsonschema.Schema{
		Type:        param.Type,
		Description: param.Description,
		Pattern:     param.Pattern,
		Format:      param.Format,
	}
	if len(param.DefaultValue) > 0 {
		prop.Default = json.RawMessage(param.DefaultValue)
	}
	if param.Example != "" {
		var example any
		if err := json.Unmarshal([]byte(param.Example), &example); err == nil {
			prop.Examples = []any{example}
		}
		// Malformed JSON example is silently dropped — preserves best-effort
		// round-tripping without failing the entire descriptor conversion.
	}
	if param.MaxLength > 0 {
		ml := int(param.MaxLength)
		prop.MaxLength = &ml
	}
	if param.MinLength > 0 {
		ml := int(param.MinLength)
		prop.MinLength = &ml
	}
	if param.HasMinimum {
		v := param.Minimum
		prop.Minimum = &v
	}
	if param.HasMaximum {
		v := param.Maximum
		prop.Maximum = &v
	}
	if param.HasExclusiveMinimum {
		v := param.ExclusiveMinimum
		prop.ExclusiveMinimum = &v
	}
	if param.HasExclusiveMaximum {
		v := param.ExclusiveMaximum
		prop.ExclusiveMaximum = &v
	}
	if param.MinItems > 0 {
		mi := int(param.MinItems)
		prop.MinItems = &mi
	}
	if param.MaxItems > 0 {
		mi := int(param.MaxItems)
		prop.MaxItems = &mi
	}
	if len(param.EnumValues) > 0 {
		prop.Enum = make([]any, 0, len(param.EnumValues))
		for _, raw := range param.EnumValues {
			var v any
			// Malformed enum entries are dropped individually rather than
			// failing the entire conversion — best-effort round-tripping.
			if err := json.Unmarshal(raw, &v); err == nil {
				prop.Enum = append(prop.Enum, v)
			}
		}
	}
	return prop
}

// paramToProto converts a jsonschema.Schema property to a proto.Parameter,
// preserving all validation fields supported by the wire format.
func paramToProto(prop *jsonschema.Schema, required bool) *proto.Parameter {
	var defaultValue []byte
	if prop.Default != nil {
		var err error
		defaultValue, err = json.Marshal(prop.Default)
		if err != nil {
			// Unserializable default — encode the error as a JSON string so
			// the field is non-nil and the problem is visible on the wire.
			defaultValue = []byte(`"<marshal error>"`)
		}
	}
	exampleStr := ""
	if len(prop.Examples) > 0 {
		exampleBytes, err := json.Marshal(prop.Examples[0])
		if err == nil {
			exampleStr = string(exampleBytes)
		}
		// Unserializable example is silently dropped — the field is optional.
	}
	var maxLen, minLen int32
	if prop.MaxLength != nil {
		//nolint:gosec // MaxLength is user-defined, overflow is acceptable behavior
		maxLen = int32(*prop.MaxLength)
	}
	if prop.MinLength != nil {
		//nolint:gosec // MinLength is user-defined, overflow is acceptable behavior
		minLen = int32(*prop.MinLength)
	}
	var minItems, maxItems int32
	if prop.MinItems != nil {
		//nolint:gosec // MinItems is user-defined, overflow is acceptable behavior
		minItems = int32(*prop.MinItems)
	}
	if prop.MaxItems != nil {
		//nolint:gosec // MaxItems is user-defined, overflow is acceptable behavior
		maxItems = int32(*prop.MaxItems)
	}

	p := &proto.Parameter{
		Type:               prop.Type,
		Required:           required,
		Description:        prop.Description,
		DefaultValue:       defaultValue,
		Example:            exampleStr,
		MaxLength:          maxLen,
		MinLength:          minLen,
		Pattern:            prop.Pattern,
		PatternDescription: "", // jsonschema.Schema doesn't have a PatternDescription field
		Format:             prop.Format,
		MinItems:           minItems,
		MaxItems:           maxItems,
	}

	// Numeric bounds (use hasX flags to distinguish zero from unset)
	if prop.Minimum != nil {
		p.Minimum = *prop.Minimum
		p.HasMinimum = true
	}
	if prop.Maximum != nil {
		p.Maximum = *prop.Maximum
		p.HasMaximum = true
	}
	if prop.ExclusiveMinimum != nil {
		p.ExclusiveMinimum = *prop.ExclusiveMinimum
		p.HasExclusiveMinimum = true
	}
	if prop.ExclusiveMaximum != nil {
		p.ExclusiveMaximum = *prop.ExclusiveMaximum
		p.HasExclusiveMaximum = true
	}

	// Enum values
	if len(prop.Enum) > 0 {
		p.EnumValues = make([][]byte, 0, len(prop.Enum))
		for _, v := range prop.Enum {
			if b, err := json.Marshal(v); err == nil {
				p.EnumValues = append(p.EnumValues, b)
			}
		}
	}

	return p
}

// descriptorToProto converts provider.Descriptor to proto.ProviderDescriptor
// schemaToProto converts a jsonschema.Schema into a proto.Schema with structured
// Parameters. Returns nil if the schema has no properties.
func schemaToProto(schema *jsonschema.Schema) *proto.Schema {
	if schema == nil || len(schema.Properties) == 0 {
		return nil
	}
	protoSchema := &proto.Schema{
		Parameters: make(map[string]*proto.Parameter, len(schema.Properties)),
	}
	requiredSet := make(map[string]bool, len(schema.Required))
	for _, name := range schema.Required {
		requiredSet[name] = true
	}
	for name, prop := range schema.Properties {
		protoSchema.Parameters[name] = paramToProto(prop, requiredSet[name])
	}
	return protoSchema
}

func descriptorToProto(desc *provider.Descriptor) *proto.ProviderDescriptor {
	version := ""
	if desc.Version != nil {
		version = desc.Version.String()
	}
	protoDesc := &proto.ProviderDescriptor{
		Name:                   desc.Name,
		DisplayName:            desc.DisplayName,
		Description:            desc.Description,
		Version:                version,
		Category:               desc.Category,
		Capabilities:           make([]string, len(desc.Capabilities)),
		ApiVersion:             desc.APIVersion,
		SensitiveFields:        desc.SensitiveFields,
		Tags:                   desc.Tags,
		Icon:                   desc.Icon,
		Deprecated:             desc.IsDeprecated,
		Beta:                   desc.Beta,
		HasExtractDependencies: desc.ExtractDependencies != nil,
	}

	for i, cap := range desc.Capabilities {
		protoDesc.Capabilities[i] = string(cap)
	}

	// Convert links
	for _, link := range desc.Links {
		protoDesc.Links = append(protoDesc.Links, &proto.Link{
			Name: link.Name,
			Url:  link.URL,
		})
	}

	// Convert examples
	for _, ex := range desc.Examples {
		protoDesc.Examples = append(protoDesc.Examples, &proto.Example{
			Name:        ex.Name,
			Description: ex.Description,
			Yaml:        ex.YAML,
		})
	}

	// Convert maintainers
	for _, m := range desc.Maintainers {
		protoDesc.Maintainers = append(protoDesc.Maintainers, &proto.Contact{
			Name:  m.Name,
			Email: m.Email,
		})
	}

	// Convert schema (structured Parameters for backward compatibility)
	protoDesc.Schema = schemaToProto(desc.Schema)

	// Serialize full schema as JSON for lossless round-tripping.
	if desc.Schema != nil {
		if raw, err := json.Marshal(desc.Schema); err == nil {
			protoDesc.RawSchema = raw
		}
	}

	// Convert output schemas
	if len(desc.OutputSchemas) > 0 {
		protoDesc.OutputSchemas = make(map[string]*proto.Schema)
		protoDesc.RawOutputSchemas = make(map[string][]byte, len(desc.OutputSchemas))
		for cap, schema := range desc.OutputSchemas {
			if schema == nil {
				continue
			}
			// Raw JSON for lossless round-tripping.
			if raw, err := json.Marshal(schema); err == nil {
				protoDesc.RawOutputSchemas[string(cap)] = raw
			}
			if ps := schemaToProto(schema); ps != nil {
				protoDesc.OutputSchemas[string(cap)] = ps
			}
		}
	}

	return protoDesc
}

// protoToDescriptor converts proto.ProviderDescriptor to provider.Descriptor
func protoToDescriptor(protoDesc *proto.ProviderDescriptor) (*provider.Descriptor, error) {
	var version *semver.Version
	if protoDesc.Version != "" {
		var err error
		version, err = semver.NewVersion(protoDesc.Version)
		if err != nil {
			return nil, fmt.Errorf("plugin %q has invalid semver version %q: %w", protoDesc.Name, protoDesc.Version, err)
		}
	}
	desc := &provider.Descriptor{
		Name:            protoDesc.Name,
		DisplayName:     protoDesc.DisplayName,
		Description:     protoDesc.Description,
		Version:         version,
		Category:        protoDesc.Category,
		Capabilities:    make([]provider.Capability, len(protoDesc.Capabilities)),
		APIVersion:      protoDesc.ApiVersion,
		SensitiveFields: protoDesc.SensitiveFields,
		Tags:            protoDesc.Tags,
		Icon:            protoDesc.Icon,
		IsDeprecated:    protoDesc.Deprecated,
		Beta:            protoDesc.Beta,
	}

	for i, cap := range protoDesc.Capabilities {
		desc.Capabilities[i] = provider.Capability(cap)
	}

	// Convert links
	for _, link := range protoDesc.Links {
		desc.Links = append(desc.Links, provider.Link{
			Name: link.Name,
			URL:  link.Url,
		})
	}

	// Convert examples
	for _, ex := range protoDesc.Examples {
		desc.Examples = append(desc.Examples, provider.Example{
			Name:        ex.Name,
			Description: ex.Description,
			YAML:        ex.Yaml,
		})
	}

	// Convert maintainers
	for _, m := range protoDesc.Maintainers {
		desc.Maintainers = append(desc.Maintainers, provider.Contact{
			Name:  m.Name,
			Email: m.Email,
		})
	}

	// Convert schema — prefer raw JSON for lossless round-tripping.
	desc.Schema = unmarshalSchemaOrFallback(protoDesc.RawSchema, protoDesc.Schema)

	// Convert output schemas — prefer raw JSON.
	desc.OutputSchemas = unmarshalOutputSchemas(protoDesc.RawOutputSchemas, protoDesc.OutputSchemas)

	// Mark that the plugin implements custom ExtractDependencies.
	// The placeholder is replaced by NewProviderWrapper with a closure that
	// calls the ExtractDependencies RPC. When false, the host uses generic
	// extraction and this field remains nil.
	if protoDesc.HasExtractDependencies {
		desc.ExtractDependencies = func(_ map[string]any) []string {
			// Placeholder — replaced by NewProviderWrapper with real RPC call.
			return nil
		}
	}

	return desc, nil
}
