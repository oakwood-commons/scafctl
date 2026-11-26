// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	goplugin "github.com/hashicorp/go-plugin"
	sdkplugin "github.com/oakwood-commons/scafctl-plugin-sdk/plugin"
	"github.com/oakwood-commons/scafctl-plugin-sdk/plugin/proto"
	"github.com/oakwood-commons/scafctl/pkg/plugin/plugintest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"
	protobuf "google.golang.org/protobuf/proto"
)

const (
	// inputSchema and outputSchema are deliberately permissive: the fixture
	// accepts any object so tests can drive it with arbitrary inputs.
	inputSchema  = `{"type":"object","additionalProperties":true,"properties":{"action":{"type":"string"}}}`
	outputSchema = `{"type":"object","additionalProperties":true}`

	// Field numbers far outside the published schema, used to simulate a
	// newer plugin that sends fields this host has never heard of.
	unknownVarintField = 9000
	unknownBytesField  = 9001
)

// fixture is the provider service. It holds the process-level Config and the
// few pieces of state the actions need.
type fixture struct {
	proto.UnimplementedPluginServiceServer

	cfg    plugintest.Config
	log    *callLog
	broker *goplugin.GRPCBroker
	seq    atomic.Int64

	// mu guards hostID and host. The broker accepts a single connection per
	// service ID, so the host client is dialed once and then reused.
	mu     sync.Mutex
	hostID uint32
	host   proto.HostServiceClient
}

func (f *fixture) providers() []string {
	if len(f.cfg.Providers) > 0 {
		return f.cfg.Providers
	}
	return []string{plugintest.ProviderName}
}

func (f *fixture) GetProviders(_ context.Context, _ *proto.GetProvidersRequest) (*proto.GetProvidersResponse, error) {
	f.log.record("GetProviders", "")
	resp := &proto.GetProvidersResponse{ProviderNames: f.providers()}
	f.decorate(resp)
	return resp, nil
}

func (f *fixture) GetProviderDescriptor(_ context.Context, req *proto.GetProviderDescriptorRequest) (*proto.GetProviderDescriptorResponse, error) {
	f.log.record("GetProviderDescriptor", req.GetProviderName())
	if !slices.Contains(f.providers(), req.GetProviderName()) {
		return nil, status.Errorf(codes.NotFound, "unknown provider %q", req.GetProviderName())
	}
	resp := &proto.GetProviderDescriptorResponse{Descriptor_: descriptor(req.GetProviderName())}
	f.decorate(resp)
	return resp, nil
}

func (f *fixture) ConfigureProvider(_ context.Context, req *proto.ConfigureProviderRequest) (*proto.ConfigureProviderResponse, error) {
	f.log.record("ConfigureProvider", fmt.Sprintf("protocolVersion=%d hostService=%t", req.GetProtocolVersion(), req.GetHostServiceId() != 0))
	f.mu.Lock()
	f.hostID = req.GetHostServiceId()
	f.mu.Unlock()

	switch f.cfg.Configure {
	case plugintest.ConfigureUnimplemented:
		return nil, status.Error(codes.Unimplemented, "ConfigureProvider is not implemented")
	case plugintest.ConfigureError:
		resp := &proto.ConfigureProviderResponse{Error: "fixture: configure failed"}
		f.decorate(resp)
		return resp, nil
	case plugintest.ConfigureOK:
	}

	version := f.cfg.ProtocolVersion
	if version == 0 {
		version = sdkplugin.PluginProtocolVersion
	}
	resp := &proto.ConfigureProviderResponse{ProtocolVersion: version}
	f.decorate(resp)
	return resp, nil
}

func (f *fixture) ExecuteProvider(ctx context.Context, req *proto.ExecuteProviderRequest) (*proto.ExecuteProviderResponse, error) {
	in, err := decodeInput(req.GetInput())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "decode input: %v", err)
	}
	f.log.record("ExecuteProvider", "action="+actionOf(in))

	resp, err := f.execute(ctx, req.GetProviderName(), in)
	if err != nil {
		return nil, err
	}
	f.decorate(resp)
	return resp, nil
}

func (f *fixture) ExecuteProviderStream(req *proto.ExecuteProviderRequest, stream proto.PluginService_ExecuteProviderStreamServer) error {
	in, err := decodeInput(req.GetInput())
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "decode input: %v", err)
	}
	action := actionOf(in)
	f.log.record("ExecuteProviderStream", "action="+action)

	switch action {
	case plugintest.ActionStream:
		return f.streamNormal(stream, req.GetProviderName(), in)
	case plugintest.ActionPartialStream:
		return f.streamPartial(stream, in)
	}

	resp, err := f.execute(stream.Context(), req.GetProviderName(), in)
	if err != nil {
		return err
	}
	return f.send(stream, &proto.ExecuteProviderStreamChunk{
		Chunk: &proto.ExecuteProviderStreamChunk_Result{Result: resp},
	})
}

func (f *fixture) DescribeWhatIf(_ context.Context, req *proto.DescribeWhatIfRequest) (*proto.DescribeWhatIfResponse, error) {
	f.log.record("DescribeWhatIf", req.GetProviderName())
	resp := &proto.DescribeWhatIfResponse{Description: "lifecycle fixture would run its action"}
	f.decorate(resp)
	return resp, nil
}

func (f *fixture) ExtractDependencies(_ context.Context, req *proto.ExtractDependenciesRequest) (*proto.ExtractDependenciesResponse, error) {
	f.log.record("ExtractDependencies", req.GetProviderName())
	resp := &proto.ExtractDependenciesResponse{}
	f.decorate(resp)
	return resp, nil
}

// StopProvider is recorded but does not end the process: per the protocol it
// aborts in-flight work, while process shutdown is the host's Kill.
func (f *fixture) StopProvider(_ context.Context, req *proto.StopProviderRequest) (*proto.StopProviderResponse, error) {
	f.log.record("StopProvider", req.GetProviderName())
	resp := &proto.StopProviderResponse{}
	f.decorate(resp)
	return resp, nil
}

func (f *fixture) streamNormal(stream proto.PluginService_ExecuteProviderStreamServer, name string, in map[string]any) error {
	chunks := []*proto.ExecuteProviderStreamChunk{
		{Chunk: &proto.ExecuteProviderStreamChunk_Stdout{Stdout: []byte("chunk-1\n")}},
		{Chunk: &proto.ExecuteProviderStreamChunk_Stdout{Stdout: []byte("chunk-2\n")}},
		{Chunk: &proto.ExecuteProviderStreamChunk_Stderr{Stderr: []byte("note\n")}},
	}
	for _, c := range chunks {
		if err := f.send(stream, c); err != nil {
			return err
		}
	}
	resp, err := okResponse(f.echo(name, in))
	if err != nil {
		return err
	}
	return f.send(stream, &proto.ExecuteProviderStreamChunk{
		Chunk: &proto.ExecuteProviderStreamChunk_Result{Result: resp},
	})
}

// streamPartial sends one chunk and then ends the stream without the result
// message the protocol requires, in the way selected by the "kind" input.
func (f *fixture) streamPartial(stream proto.PluginService_ExecuteProviderStreamServer, in map[string]any) error {
	first := &proto.ExecuteProviderStreamChunk{
		Chunk: &proto.ExecuteProviderStreamChunk_Stdout{Stdout: []byte("partial\n")},
	}
	if err := f.send(stream, first); err != nil {
		return err
	}
	switch str(in, "kind") {
	case plugintest.PartialAbort:
		return status.Error(codes.Internal, "fixture: stream aborted")
	case plugintest.PartialCrash:
		// Give the first chunk a moment to reach the wire before dying.
		time.Sleep(crashFlushDelay)
		crash(in)
	}
	return nil
}

func (f *fixture) send(stream proto.PluginService_ExecuteProviderStreamServer, chunk *proto.ExecuteProviderStreamChunk) error {
	f.decorate(chunk)
	return stream.Send(chunk)
}

// decorate appends protobuf fields that are not in the published schema to
// msg when the control file asks for it.
func (f *fixture) decorate(msg protobuf.Message) {
	if !f.cfg.UnknownFields {
		return
	}
	var raw []byte
	raw = protowire.AppendTag(raw, unknownVarintField, protowire.VarintType)
	raw = protowire.AppendVarint(raw, 42)
	raw = protowire.AppendTag(raw, unknownBytesField, protowire.BytesType)
	raw = protowire.AppendString(raw, "field-from-a-newer-plugin")
	msg.ProtoReflect().SetUnknown(raw)
}

func descriptor(name string) *proto.ProviderDescriptor {
	return &proto.ProviderDescriptor{
		Name:         name,
		DisplayName:  "Lifecycle Fixture",
		Description:  "Configurable provider used to test plugin lifecycle and protocol behavior",
		Version:      "1.0.0",
		Category:     "utility",
		ApiVersion:   "v1",
		Capabilities: []string{"from", "transform"},
		RawSchema:    []byte(inputSchema),
		RawOutputSchemas: map[string][]byte{
			"from":      []byte(outputSchema),
			"transform": []byte(outputSchema),
		},
	}
}

func decodeInput(raw []byte) (map[string]any, error) {
	in := map[string]any{}
	if len(raw) == 0 {
		return in, nil
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	return in, nil
}
