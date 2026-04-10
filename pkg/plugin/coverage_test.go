// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/plugin/proto"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// --- Mock gRPC PluginServiceClient ---

type mockPluginServiceClient struct {
	proto.PluginServiceClient

	getProvidersResp          *proto.GetProvidersResponse
	getProvidersErr           error
	getProviderDescriptorResp *proto.GetProviderDescriptorResponse
	getProviderDescriptorErr  error
	executeProviderResp       *proto.ExecuteProviderResponse
	executeProviderErr        error
	configureProviderResp     *proto.ConfigureProviderResponse
	configureProviderErr      error
	lastConfigureReq          *proto.ConfigureProviderRequest
	describeWhatIfResp        *proto.DescribeWhatIfResponse
	describeWhatIfErr         error
	extractDependenciesResp   *proto.ExtractDependenciesResponse
	extractDependenciesErr    error
	stopProviderResp          *proto.StopProviderResponse
	stopProviderErr           error
	executeProviderStreamFunc func(ctx context.Context, req *proto.ExecuteProviderRequest) (proto.PluginService_ExecuteProviderStreamClient, error)
}

func (m *mockPluginServiceClient) GetProviders(_ context.Context, _ *proto.GetProvidersRequest, _ ...grpc.CallOption) (*proto.GetProvidersResponse, error) {
	return m.getProvidersResp, m.getProvidersErr
}

func (m *mockPluginServiceClient) GetProviderDescriptor(_ context.Context, _ *proto.GetProviderDescriptorRequest, _ ...grpc.CallOption) (*proto.GetProviderDescriptorResponse, error) {
	return m.getProviderDescriptorResp, m.getProviderDescriptorErr
}

func (m *mockPluginServiceClient) ExecuteProvider(_ context.Context, _ *proto.ExecuteProviderRequest, _ ...grpc.CallOption) (*proto.ExecuteProviderResponse, error) {
	return m.executeProviderResp, m.executeProviderErr
}

func (m *mockPluginServiceClient) ConfigureProvider(_ context.Context, req *proto.ConfigureProviderRequest, _ ...grpc.CallOption) (*proto.ConfigureProviderResponse, error) {
	m.lastConfigureReq = req
	return m.configureProviderResp, m.configureProviderErr
}

func (m *mockPluginServiceClient) DescribeWhatIf(_ context.Context, _ *proto.DescribeWhatIfRequest, _ ...grpc.CallOption) (*proto.DescribeWhatIfResponse, error) {
	return m.describeWhatIfResp, m.describeWhatIfErr
}

func (m *mockPluginServiceClient) ExtractDependencies(_ context.Context, _ *proto.ExtractDependenciesRequest, _ ...grpc.CallOption) (*proto.ExtractDependenciesResponse, error) {
	return m.extractDependenciesResp, m.extractDependenciesErr
}

func (m *mockPluginServiceClient) ExecuteProviderStream(ctx context.Context, req *proto.ExecuteProviderRequest, _ ...grpc.CallOption) (proto.PluginService_ExecuteProviderStreamClient, error) {
	if m.executeProviderStreamFunc != nil {
		return m.executeProviderStreamFunc(ctx, req)
	}
	return nil, status.Error(codes.Unimplemented, "streaming not supported")
}

func (m *mockPluginServiceClient) StopProvider(_ context.Context, _ *proto.StopProviderRequest, _ ...grpc.CallOption) (*proto.StopProviderResponse, error) {
	return m.stopProviderResp, m.stopProviderErr
}

// --- Mock gRPC stream client ---

type mockStreamClient struct {
	grpc.ClientStream
	chunks []*proto.ExecuteProviderStreamChunk
	idx    int
}

func (s *mockStreamClient) Recv() (*proto.ExecuteProviderStreamChunk, error) {
	if s.idx >= len(s.chunks) {
		return nil, io.EOF
	}
	chunk := s.chunks[s.idx]
	s.idx++
	return chunk, nil
}

type mockStreamClientError struct {
	grpc.ClientStream
	err error
}

func (s *mockStreamClientError) Recv() (*proto.ExecuteProviderStreamChunk, error) {
	return nil, s.err
}

// --- Mock gRPC server stream ---

type mockServerStream struct {
	grpc.ServerStream
	chunks []*proto.ExecuteProviderStreamChunk
	ctx    context.Context
	err    error
}

func (s *mockServerStream) Send(chunk *proto.ExecuteProviderStreamChunk) error {
	if s.err != nil {
		return s.err
	}
	s.chunks = append(s.chunks, chunk)
	return nil
}

func (s *mockServerStream) Context() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

// --- Mock HostServiceClient ---

type mockHostServiceClient struct {
	proto.HostServiceClient

	getSecretResp        *proto.GetSecretResponse
	getSecretErr         error
	setSecretResp        *proto.SetSecretResponse
	setSecretErr         error
	deleteSecretResp     *proto.DeleteSecretResponse
	deleteSecretErr      error
	listSecretsResp      *proto.ListSecretsResponse
	listSecretsErr       error
	getAuthIdentityResp  *proto.GetAuthIdentityResponse
	getAuthIdentityErr   error
	listAuthHandlersResp *proto.ListAuthHandlersResponse
	listAuthHandlersErr  error
	getAuthTokenResp     *proto.GetAuthTokenResponse
	getAuthTokenErr      error
}

func (m *mockHostServiceClient) GetSecret(_ context.Context, _ *proto.GetSecretRequest, _ ...grpc.CallOption) (*proto.GetSecretResponse, error) {
	return m.getSecretResp, m.getSecretErr
}

func (m *mockHostServiceClient) SetSecret(_ context.Context, _ *proto.SetSecretRequest, _ ...grpc.CallOption) (*proto.SetSecretResponse, error) {
	return m.setSecretResp, m.setSecretErr
}

func (m *mockHostServiceClient) DeleteSecret(_ context.Context, _ *proto.DeleteSecretRequest, _ ...grpc.CallOption) (*proto.DeleteSecretResponse, error) {
	return m.deleteSecretResp, m.deleteSecretErr
}

func (m *mockHostServiceClient) ListSecrets(_ context.Context, _ *proto.ListSecretsRequest, _ ...grpc.CallOption) (*proto.ListSecretsResponse, error) {
	return m.listSecretsResp, m.listSecretsErr
}

func (m *mockHostServiceClient) GetAuthIdentity(_ context.Context, _ *proto.GetAuthIdentityRequest, _ ...grpc.CallOption) (*proto.GetAuthIdentityResponse, error) {
	return m.getAuthIdentityResp, m.getAuthIdentityErr
}

func (m *mockHostServiceClient) ListAuthHandlers(_ context.Context, _ *proto.ListAuthHandlersRequest, _ ...grpc.CallOption) (*proto.ListAuthHandlersResponse, error) {
	return m.listAuthHandlersResp, m.listAuthHandlersErr
}

func (m *mockHostServiceClient) GetAuthToken(_ context.Context, _ *proto.GetAuthTokenRequest, _ ...grpc.CallOption) (*proto.GetAuthTokenResponse, error) {
	return m.getAuthTokenResp, m.getAuthTokenErr
}

// --- streamingMockPlugin wraps MockProviderPlugin with real streaming ---

type streamingMockPlugin struct {
	*MockProviderPlugin
	streamFunc func(ctx context.Context, name string, input map[string]any, cb func(StreamChunk)) error
}

func (s *streamingMockPlugin) ExecuteProviderStream(ctx context.Context, name string, input map[string]any, cb func(StreamChunk)) error {
	if s.streamFunc != nil {
		return s.streamFunc(ctx, name, input, cb)
	}
	return ErrStreamingNotSupported
}

// --- describeWhatIfErrorMock ---

type describeWhatIfErrorMock struct {
	*MockProviderPlugin
}

func (m *describeWhatIfErrorMock) DescribeWhatIf(_ context.Context, _ string, _ map[string]any) (string, error) {
	return "", fmt.Errorf("whatif failed")
}

// --- errorSecretStore ---

type errorSecretStore struct{}

func (e *errorSecretStore) Get(_ context.Context, name string) ([]byte, error) {
	return nil, fmt.Errorf("store error for %s", name)
}

func (e *errorSecretStore) Set(_ context.Context, name string, _ []byte) error {
	return fmt.Errorf("store error for %s", name)
}

func (e *errorSecretStore) Delete(_ context.Context, name string) error {
	return fmt.Errorf("store error for %s", name)
}

func (e *errorSecretStore) List(_ context.Context) ([]string, error) {
	return nil, fmt.Errorf("store error")
}

func (e *errorSecretStore) Exists(_ context.Context, _ string) (bool, error) { return false, nil }
func (e *errorSecretStore) Rotate(_ context.Context) error                   { return nil }
func (e *errorSecretStore) KeyringBackend() string                           { return "error" }

// =====================================================================
// GRPCClient tests
// =====================================================================

func TestGRPCClient_GetProviders_Success(t *testing.T) {
	mock := &mockPluginServiceClient{
		getProvidersResp: &proto.GetProvidersResponse{ProviderNames: []string{"a", "b"}},
	}
	client := &GRPCClient{client: mock}

	providers, err := client.GetProviders(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, providers)
}

func TestGRPCClient_GetProviders_Error(t *testing.T) {
	mock := &mockPluginServiceClient{getProvidersErr: fmt.Errorf("connection lost")}
	client := &GRPCClient{client: mock}

	_, err := client.GetProviders(context.Background())
	require.Error(t, err)
}

func TestGRPCClient_GetProviderDescriptor_Success(t *testing.T) {
	protoDesc := descriptorToProto(&provider.Descriptor{
		Name: "test", APIVersion: "v1", Version: semver.MustParse("1.0.0"),
	})
	mock := &mockPluginServiceClient{
		getProviderDescriptorResp: &proto.GetProviderDescriptorResponse{Descriptor_: protoDesc},
	}
	client := &GRPCClient{client: mock}

	desc, err := client.GetProviderDescriptor(context.Background(), "test")
	require.NoError(t, err)
	assert.Equal(t, "test", desc.Name)
}

func TestGRPCClient_GetProviderDescriptor_Error(t *testing.T) {
	mock := &mockPluginServiceClient{getProviderDescriptorErr: fmt.Errorf("not found")}
	client := &GRPCClient{client: mock}

	_, err := client.GetProviderDescriptor(context.Background(), "missing")
	require.Error(t, err)
}

func TestGRPCClient_ExecuteProvider_Success(t *testing.T) {
	output := &provider.Output{Data: map[string]any{"ok": true}}
	outputBytes, _ := json.Marshal(output)
	mock := &mockPluginServiceClient{
		executeProviderResp: &proto.ExecuteProviderResponse{Output: outputBytes},
	}
	client := &GRPCClient{client: mock}

	result, err := client.ExecuteProvider(context.Background(), "test", map[string]any{"key": "val"})
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestGRPCClient_ExecuteProvider_RPCError(t *testing.T) {
	mock := &mockPluginServiceClient{executeProviderErr: fmt.Errorf("transport error")}
	client := &GRPCClient{client: mock}

	_, err := client.ExecuteProvider(context.Background(), "test", map[string]any{})
	require.Error(t, err)
}

func TestGRPCClient_ExecuteProvider_ResponseError(t *testing.T) {
	mock := &mockPluginServiceClient{
		executeProviderResp: &proto.ExecuteProviderResponse{Error: "provider failed"},
	}
	client := &GRPCClient{client: mock}

	_, err := client.ExecuteProvider(context.Background(), "test", map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider failed")
}

func TestGRPCClient_ExecuteProvider_BadOutput(t *testing.T) {
	mock := &mockPluginServiceClient{
		executeProviderResp: &proto.ExecuteProviderResponse{Output: []byte(`{invalid`)},
	}
	client := &GRPCClient{client: mock}

	_, err := client.ExecuteProvider(context.Background(), "test", map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode output")
}

func TestGRPCClient_ConfigureProvider_Success(t *testing.T) {
	mock := &mockPluginServiceClient{
		configureProviderResp: &proto.ConfigureProviderResponse{},
	}
	client := &GRPCClient{client: mock}

	err := client.ConfigureProvider(context.Background(), "test", ProviderConfig{
		Quiet:      true,
		BinaryName: "mycli",
		Settings:   map[string]json.RawMessage{"key": json.RawMessage(`"value"`)},
	})
	require.NoError(t, err)
}

func TestGRPCClient_ConfigureProvider_SendsHostServiceID(t *testing.T) {
	mock := &mockPluginServiceClient{
		configureProviderResp: &proto.ConfigureProviderResponse{},
	}
	client := &GRPCClient{
		client:        mock,
		hostServiceID: 42,
	}

	err := client.ConfigureProvider(context.Background(), "test", ProviderConfig{
		BinaryName: "mycli",
	})
	require.NoError(t, err)
	require.NotNil(t, mock.lastConfigureReq)
	assert.Equal(t, uint32(42), mock.lastConfigureReq.HostServiceId)
	assert.Equal(t, "test", mock.lastConfigureReq.ProviderName)
}

func TestGRPCClient_ConfigureProvider_Unimplemented(t *testing.T) {
	mock := &mockPluginServiceClient{
		configureProviderErr: status.Error(codes.Unimplemented, "not implemented"),
	}
	client := &GRPCClient{client: mock}

	err := client.ConfigureProvider(context.Background(), "test", ProviderConfig{})
	require.NoError(t, err)
}

func TestGRPCClient_ConfigureProvider_ResponseError(t *testing.T) {
	mock := &mockPluginServiceClient{
		configureProviderResp: &proto.ConfigureProviderResponse{Error: "invalid config"},
	}
	client := &GRPCClient{client: mock}

	err := client.ConfigureProvider(context.Background(), "test", ProviderConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid config")
}

func TestGRPCClient_ConfigureProvider_OtherRPCError(t *testing.T) {
	mock := &mockPluginServiceClient{
		configureProviderErr: status.Error(codes.Internal, "internal failure"),
	}
	client := &GRPCClient{client: mock}

	err := client.ConfigureProvider(context.Background(), "test", ProviderConfig{})
	require.Error(t, err)
}

func TestGRPCClient_DescribeWhatIf_Success(t *testing.T) {
	mock := &mockPluginServiceClient{
		describeWhatIfResp: &proto.DescribeWhatIfResponse{Description: "would create file"},
	}
	client := &GRPCClient{client: mock}

	desc, err := client.DescribeWhatIf(context.Background(), "test", map[string]any{"path": "/tmp"})
	require.NoError(t, err)
	assert.Equal(t, "would create file", desc)
}

func TestGRPCClient_DescribeWhatIf_ResponseError(t *testing.T) {
	mock := &mockPluginServiceClient{
		describeWhatIfResp: &proto.DescribeWhatIfResponse{Error: "describe failed"},
	}
	client := &GRPCClient{client: mock}

	_, err := client.DescribeWhatIf(context.Background(), "test", map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "describe failed")
}

func TestGRPCClient_DescribeWhatIf_RPCError(t *testing.T) {
	mock := &mockPluginServiceClient{describeWhatIfErr: fmt.Errorf("connection refused")}
	client := &GRPCClient{client: mock}

	_, err := client.DescribeWhatIf(context.Background(), "test", map[string]any{})
	require.Error(t, err)
}

func TestGRPCClient_ExtractDependencies_Success(t *testing.T) {
	mock := &mockPluginServiceClient{
		extractDependenciesResp: &proto.ExtractDependenciesResponse{Dependencies: []string{"dep1", "dep2"}},
	}
	client := &GRPCClient{client: mock}

	deps, err := client.ExtractDependencies(context.Background(), "test", map[string]any{"key": "val"})
	require.NoError(t, err)
	assert.Equal(t, []string{"dep1", "dep2"}, deps)
}

func TestGRPCClient_ExtractDependencies_Unimplemented(t *testing.T) {
	mock := &mockPluginServiceClient{
		extractDependenciesErr: status.Error(codes.Unimplemented, "not implemented"),
	}
	client := &GRPCClient{client: mock}

	deps, err := client.ExtractDependencies(context.Background(), "test", map[string]any{})
	require.NoError(t, err)
	assert.Nil(t, deps)
}

func TestGRPCClient_ExtractDependencies_ResponseError(t *testing.T) {
	mock := &mockPluginServiceClient{
		extractDependenciesResp: &proto.ExtractDependenciesResponse{Error: "extraction failed"},
	}
	client := &GRPCClient{client: mock}

	_, err := client.ExtractDependencies(context.Background(), "test", map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "extraction failed")
}

func TestGRPCClient_ExtractDependencies_RPCError(t *testing.T) {
	mock := &mockPluginServiceClient{extractDependenciesErr: fmt.Errorf("transport error")}
	client := &GRPCClient{client: mock}

	_, err := client.ExtractDependencies(context.Background(), "test", map[string]any{})
	require.Error(t, err)
}

func TestGRPCClient_HostServiceID(t *testing.T) {
	assert.Equal(t, uint32(42), (&GRPCClient{hostServiceID: 42}).HostServiceID())
	assert.Equal(t, uint32(0), (&GRPCClient{}).HostServiceID())
}

// =====================================================================
// GRPCClient Streaming tests
// =====================================================================

func TestGRPCClient_ExecuteProviderStream_Success(t *testing.T) {
	output := &provider.Output{Data: map[string]any{"done": true}}
	outputBytes, _ := json.Marshal(output)

	mock := &mockPluginServiceClient{
		executeProviderStreamFunc: func(_ context.Context, _ *proto.ExecuteProviderRequest) (proto.PluginService_ExecuteProviderStreamClient, error) {
			return &mockStreamClient{chunks: []*proto.ExecuteProviderStreamChunk{
				{Chunk: &proto.ExecuteProviderStreamChunk_Stdout{Stdout: []byte("hello")}},
				{Chunk: &proto.ExecuteProviderStreamChunk_Stderr{Stderr: []byte("warn")}},
				{Chunk: &proto.ExecuteProviderStreamChunk_Result{Result: &proto.ExecuteProviderResponse{Output: outputBytes}}},
			}}, nil
		},
	}
	client := &GRPCClient{client: mock}

	var gotStdout, gotStderr []byte
	var gotResult *provider.Output
	err := client.ExecuteProviderStream(context.Background(), "test", map[string]any{"k": "v"}, func(chunk StreamChunk) {
		if chunk.Stdout != nil {
			gotStdout = append(gotStdout, chunk.Stdout...)
		}
		if chunk.Stderr != nil {
			gotStderr = append(gotStderr, chunk.Stderr...)
		}
		if chunk.Result != nil {
			gotResult = chunk.Result
		}
	})
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), gotStdout)
	assert.Equal(t, []byte("warn"), gotStderr)
	require.NotNil(t, gotResult)
}

func TestGRPCClient_ExecuteProviderStream_Unimplemented(t *testing.T) {
	client := &GRPCClient{client: &mockPluginServiceClient{}}

	err := client.ExecuteProviderStream(context.Background(), "test", map[string]any{}, func(_ StreamChunk) {})
	assert.True(t, errors.Is(err, ErrStreamingNotSupported))
}

func TestGRPCClient_ExecuteProviderStream_UnimplementedOnRecv(t *testing.T) {
	mock := &mockPluginServiceClient{
		executeProviderStreamFunc: func(_ context.Context, _ *proto.ExecuteProviderRequest) (proto.PluginService_ExecuteProviderStreamClient, error) {
			return &mockStreamClientError{err: status.Error(codes.Unimplemented, "not supported")}, nil
		},
	}
	client := &GRPCClient{client: mock}

	err := client.ExecuteProviderStream(context.Background(), "test", map[string]any{}, func(_ StreamChunk) {})
	assert.True(t, errors.Is(err, ErrStreamingNotSupported))
}

func TestGRPCClient_ExecuteProviderStream_ResultError(t *testing.T) {
	mock := &mockPluginServiceClient{
		executeProviderStreamFunc: func(_ context.Context, _ *proto.ExecuteProviderRequest) (proto.PluginService_ExecuteProviderStreamClient, error) {
			return &mockStreamClient{chunks: []*proto.ExecuteProviderStreamChunk{
				{Chunk: &proto.ExecuteProviderStreamChunk_Result{Result: &proto.ExecuteProviderResponse{Error: "provider crashed"}}},
			}}, nil
		},
	}
	client := &GRPCClient{client: mock}

	var gotErr string
	err := client.ExecuteProviderStream(context.Background(), "test", map[string]any{}, func(chunk StreamChunk) {
		if chunk.Error != "" {
			gotErr = chunk.Error
		}
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider crashed")
	assert.Equal(t, "provider crashed", gotErr)
}

func TestGRPCClient_ExecuteProviderStream_RecvError(t *testing.T) {
	mock := &mockPluginServiceClient{
		executeProviderStreamFunc: func(_ context.Context, _ *proto.ExecuteProviderRequest) (proto.PluginService_ExecuteProviderStreamClient, error) {
			return &mockStreamClientError{err: fmt.Errorf("broken pipe")}, nil
		},
	}
	client := &GRPCClient{client: mock}

	err := client.ExecuteProviderStream(context.Background(), "test", map[string]any{}, func(_ StreamChunk) {})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stream receive error")
}

// =====================================================================
// GRPCServer streaming tests
// =====================================================================

func TestGRPCServer_ExecuteProviderStream_Success(t *testing.T) {
	sm := &streamingMockPlugin{
		MockProviderPlugin: &MockProviderPlugin{},
		streamFunc: func(_ context.Context, _ string, _ map[string]any, cb func(StreamChunk)) error {
			cb(StreamChunk{Stdout: []byte("line1\n")})
			cb(StreamChunk{Stderr: []byte("warning\n")})
			cb(StreamChunk{Result: &provider.Output{Data: map[string]any{"done": true}}})
			return nil
		},
	}
	server := &GRPCServer{Impl: sm}
	stream := &mockServerStream{ctx: context.Background()}
	inputBytes, _ := json.Marshal(map[string]any{"key": "value"})

	err := server.ExecuteProviderStream(&proto.ExecuteProviderRequest{
		ProviderName: "test",
		Input:        inputBytes,
	}, stream)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(stream.chunks), 3)
}

func TestGRPCServer_ExecuteProviderStream_InvalidInput(t *testing.T) {
	server := &GRPCServer{Impl: &MockProviderPlugin{}}
	stream := &mockServerStream{ctx: context.Background()}

	err := server.ExecuteProviderStream(&proto.ExecuteProviderRequest{
		ProviderName: "test",
		Input:        []byte(`{invalid`),
	}, stream)
	require.NoError(t, err)
	require.Len(t, stream.chunks, 1)
	assert.Contains(t, stream.chunks[0].GetResult().Error, "failed to decode input")
}

func TestGRPCServer_ExecuteProviderStream_Unimplemented(t *testing.T) {
	server := &GRPCServer{Impl: &MockProviderPlugin{}}
	stream := &mockServerStream{ctx: context.Background()}

	err := server.ExecuteProviderStream(&proto.ExecuteProviderRequest{
		ProviderName: "test",
		Input:        []byte(`{}`),
	}, stream)
	require.Error(t, err)
	s, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unimplemented, s.Code())
}

func TestGRPCServer_ExecuteProviderStream_PluginError(t *testing.T) {
	sm := &streamingMockPlugin{
		MockProviderPlugin: &MockProviderPlugin{},
		streamFunc: func(_ context.Context, _ string, _ map[string]any, _ func(StreamChunk)) error {
			return fmt.Errorf("something broke")
		},
	}
	server := &GRPCServer{Impl: sm}
	stream := &mockServerStream{ctx: context.Background()}

	err := server.ExecuteProviderStream(&proto.ExecuteProviderRequest{
		ProviderName: "test",
		Input:        []byte(`{}`),
	}, stream)
	require.NoError(t, err)
	require.Len(t, stream.chunks, 1)
	assert.Contains(t, stream.chunks[0].GetResult().Error, "something broke")
}

func TestGRPCServer_ExecuteProviderStream_ContextError(t *testing.T) {
	server := &GRPCServer{Impl: &MockProviderPlugin{}}
	stream := &mockServerStream{ctx: context.Background()}

	err := server.ExecuteProviderStream(&proto.ExecuteProviderRequest{
		ProviderName: "test",
		Input:        []byte(`{}`),
		Context:      []byte(`{invalid`),
	}, stream)
	require.NoError(t, err)
	require.Len(t, stream.chunks, 1)
	assert.Contains(t, stream.chunks[0].GetResult().Error, "failed to decode context")
}

// =====================================================================
// streamForwarder tests
// =====================================================================

func TestStreamForwarder_StdoutStderrResult(t *testing.T) {
	stream := &mockServerStream{ctx: context.Background()}
	fwd := newStreamForwarder(stream)

	fwd.forward(StreamChunk{Stdout: []byte("out")})
	fwd.forward(StreamChunk{Stderr: []byte("err")})
	fwd.forward(StreamChunk{Result: &provider.Output{Data: map[string]any{"ok": true}}})

	require.Len(t, stream.chunks, 3)
	assert.NotNil(t, stream.chunks[0].GetStdout())
	assert.NotNil(t, stream.chunks[1].GetStderr())
	assert.NotNil(t, stream.chunks[2].GetResult())
}

func TestStreamForwarder_ErrorChunk(t *testing.T) {
	stream := &mockServerStream{ctx: context.Background()}
	fwd := newStreamForwarder(stream)

	fwd.forward(StreamChunk{Error: "something failed"})

	require.Len(t, stream.chunks, 1)
	assert.Equal(t, "something failed", stream.chunks[0].GetResult().Error)
}

func TestStreamForwarder_SendErrorDropsSubsequent(t *testing.T) {
	stream := &mockServerStream{
		ctx: context.Background(),
		err: fmt.Errorf("broken stream"),
	}
	fwd := newStreamForwarder(stream)

	fwd.forward(StreamChunk{Stdout: []byte("first")})
	fwd.forward(StreamChunk{Stdout: []byte("second")})

	assert.Empty(t, stream.chunks)
}

func TestStreamForwarder_EmptyChunkDropped(t *testing.T) {
	stream := &mockServerStream{ctx: context.Background()}
	fwd := newStreamForwarder(stream)

	// Send an empty chunk (no stdout, stderr, result, or error)
	fwd.forward(StreamChunk{})

	// The empty chunk should be silently dropped
	assert.Empty(t, stream.chunks)

	// A valid chunk after the empty one should still work
	fwd.forward(StreamChunk{Stdout: []byte("hello")})
	require.Len(t, stream.chunks, 1)
	assert.NotNil(t, stream.chunks[0].GetStdout())
}

// =====================================================================
// GRPCServer DescribeWhatIf tests
// =====================================================================

func TestGRPCServer_DescribeWhatIf_Success(t *testing.T) {
	server := &GRPCServer{Impl: &MockProviderPlugin{}}
	inputBytes, _ := json.Marshal(map[string]any{"path": "/tmp"})

	resp, err := server.DescribeWhatIf(context.Background(), &proto.DescribeWhatIfRequest{
		ProviderName: "test-provider",
		Input:        inputBytes,
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.Contains(t, resp.Description, "Would execute test-provider")
}

func TestGRPCServer_DescribeWhatIf_InvalidInput(t *testing.T) {
	server := &GRPCServer{Impl: &MockProviderPlugin{}}

	resp, err := server.DescribeWhatIf(context.Background(), &proto.DescribeWhatIfRequest{
		ProviderName: "test",
		Input:        []byte(`{invalid`),
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "failed to decode input")
}

func TestGRPCServer_DescribeWhatIf_PluginError(t *testing.T) {
	server := &GRPCServer{Impl: &describeWhatIfErrorMock{MockProviderPlugin: &MockProviderPlugin{}}}

	resp, err := server.DescribeWhatIf(context.Background(), &proto.DescribeWhatIfRequest{
		ProviderName: "test",
		Input:        []byte(`{}`),
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "whatif failed")
}

// =====================================================================
// HostServiceClient tests
// =====================================================================

func TestHostServiceClient_GetSecret(t *testing.T) {
	tests := []struct {
		name      string
		resp      *proto.GetSecretResponse
		rpcErr    error
		wantVal   string
		wantFound bool
		wantErr   bool
	}{
		{
			name:      "success",
			resp:      &proto.GetSecretResponse{Value: "secret-val", Found: true},
			wantVal:   "secret-val",
			wantFound: true,
		},
		{
			name:      "not found",
			resp:      &proto.GetSecretResponse{Found: false},
			wantFound: false,
		},
		{
			name:    "rpc error",
			rpcErr:  fmt.Errorf("transport error"),
			wantErr: true,
		},
		{
			name:    "response error",
			resp:    &proto.GetSecretResponse{Error: "unavailable"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &HostServiceClient{client: &mockHostServiceClient{
				getSecretResp: tt.resp,
				getSecretErr:  tt.rpcErr,
			}}

			val, found, err := client.GetSecret(context.Background(), "test-key")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantVal, val)
			assert.Equal(t, tt.wantFound, found)
		})
	}
}

func TestHostServiceClient_SetSecret(t *testing.T) {
	tests := []struct {
		name    string
		resp    *proto.SetSecretResponse
		rpcErr  error
		wantErr bool
	}{
		{name: "success", resp: &proto.SetSecretResponse{}},
		{name: "rpc error", rpcErr: fmt.Errorf("err"), wantErr: true},
		{name: "response error", resp: &proto.SetSecretResponse{Error: "write failed"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &HostServiceClient{client: &mockHostServiceClient{
				setSecretResp: tt.resp,
				setSecretErr:  tt.rpcErr,
			}}

			err := client.SetSecret(context.Background(), "key", "value")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestHostServiceClient_DeleteSecret(t *testing.T) {
	tests := []struct {
		name    string
		resp    *proto.DeleteSecretResponse
		rpcErr  error
		wantErr bool
	}{
		{name: "success", resp: &proto.DeleteSecretResponse{}},
		{name: "rpc error", rpcErr: fmt.Errorf("err"), wantErr: true},
		{name: "response error", resp: &proto.DeleteSecretResponse{Error: "failed"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &HostServiceClient{client: &mockHostServiceClient{
				deleteSecretResp: tt.resp,
				deleteSecretErr:  tt.rpcErr,
			}}

			err := client.DeleteSecret(context.Background(), "key")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestHostServiceClient_ListSecrets(t *testing.T) {
	tests := []struct {
		name      string
		resp      *proto.ListSecretsResponse
		rpcErr    error
		wantNames []string
		wantErr   bool
	}{
		{
			name:      "success",
			resp:      &proto.ListSecretsResponse{Names: []string{"a", "b"}},
			wantNames: []string{"a", "b"},
		},
		{name: "rpc error", rpcErr: fmt.Errorf("err"), wantErr: true},
		{name: "response error", resp: &proto.ListSecretsResponse{Error: "failed"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &HostServiceClient{client: &mockHostServiceClient{
				listSecretsResp: tt.resp,
				listSecretsErr:  tt.rpcErr,
			}}

			names, err := client.ListSecrets(context.Background(), "*")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantNames, names)
		})
	}
}

func TestHostServiceClient_GetAuthIdentity(t *testing.T) {
	tests := []struct {
		name    string
		resp    *proto.GetAuthIdentityResponse
		rpcErr  error
		wantErr bool
	}{
		{
			name: "success",
			resp: &proto.GetAuthIdentityResponse{Claims: &proto.Claims{Subject: "user@test"}},
		},
		{name: "rpc error", rpcErr: fmt.Errorf("err"), wantErr: true},
		{name: "response error", resp: &proto.GetAuthIdentityResponse{Error: "auth failed"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &HostServiceClient{client: &mockHostServiceClient{
				getAuthIdentityResp: tt.resp,
				getAuthIdentityErr:  tt.rpcErr,
			}}

			claims, err := client.GetAuthIdentity(context.Background(), "github", "read")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "user@test", claims.Subject)
		})
	}
}

func TestHostServiceClient_ListAuthHandlers(t *testing.T) {
	tests := []struct {
		name         string
		resp         *proto.ListAuthHandlersResponse
		rpcErr       error
		wantHandlers []string
		wantDefault  string
		wantErr      bool
	}{
		{
			name: "success",
			resp: &proto.ListAuthHandlersResponse{
				HandlerNames:   []string{"github", "azure"},
				DefaultHandler: "github",
			},
			wantHandlers: []string{"github", "azure"},
			wantDefault:  "github",
		},
		{name: "rpc error", rpcErr: fmt.Errorf("err"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &HostServiceClient{client: &mockHostServiceClient{
				listAuthHandlersResp: tt.resp,
				listAuthHandlersErr:  tt.rpcErr,
			}}

			handlers, def, err := client.ListAuthHandlers(context.Background())
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantHandlers, handlers)
			assert.Equal(t, tt.wantDefault, def)
		})
	}
}

// =====================================================================
// HostServiceServer additional coverage
// =====================================================================

func TestHostServiceServer_SetSecret_WithStore(t *testing.T) {
	store := &fakeSecretStore{secrets: map[string][]byte{}}
	server := &HostServiceServer{Deps: HostServiceDeps{SecretStore: store}}

	resp, err := server.SetSecret(context.Background(), &proto.SetSecretRequest{Name: "new-key", Value: "new-val"})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.Equal(t, []byte("new-val"), store.secrets["new-key"])
}

func TestHostServiceServer_DeleteSecret_WithStore(t *testing.T) {
	store := &fakeSecretStore{secrets: map[string][]byte{"key": []byte("val")}}
	server := &HostServiceServer{Deps: HostServiceDeps{SecretStore: store}}

	resp, err := server.DeleteSecret(context.Background(), &proto.DeleteSecretRequest{Name: "key"})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	_, ok := store.secrets["key"]
	assert.False(t, ok)
}

func TestHostServiceServer_DeleteSecret_ValidationError(t *testing.T) {
	server := &HostServiceServer{
		Deps: HostServiceDeps{SecretStore: &fakeSecretStore{secrets: map[string][]byte{}}},
	}

	resp, err := server.DeleteSecret(context.Background(), &proto.DeleteSecretRequest{Name: "../bad"})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "path traversal")
}

func TestHostServiceServer_DeleteSecret_ScopeError(t *testing.T) {
	server := &HostServiceServer{
		Deps: HostServiceDeps{
			SecretStore:         &fakeSecretStore{secrets: map[string][]byte{"global": []byte("val")}},
			AllowedSecretPrefix: "plugins/echo/",
		},
	}

	resp, err := server.DeleteSecret(context.Background(), &proto.DeleteSecretRequest{Name: "global"})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "access denied")
}

func TestHostServiceServer_ListAuthHandlers_Error(t *testing.T) {
	server := &HostServiceServer{
		Deps: HostServiceDeps{
			AuthHandlersFunc: func(_ context.Context) ([]string, string, error) {
				return nil, "", fmt.Errorf("auth backend down")
			},
		},
	}

	resp, err := server.ListAuthHandlers(context.Background(), &proto.ListAuthHandlersRequest{})
	require.NoError(t, err)
	assert.Empty(t, resp.HandlerNames)
}

func TestHostServiceServer_GetAuthIdentity_FuncError(t *testing.T) {
	server := &HostServiceServer{
		Deps: HostServiceDeps{
			AuthIdentityFunc: func(_ context.Context, _, _ string) (*proto.Claims, error) {
				return nil, fmt.Errorf("identity lookup failed")
			},
		},
	}

	resp, err := server.GetAuthIdentity(context.Background(), &proto.GetAuthIdentityRequest{HandlerName: "github"})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "identity lookup failed")
}

func TestHostServiceServer_SetSecret_StoreError(t *testing.T) {
	server := &HostServiceServer{Deps: HostServiceDeps{SecretStore: &errorSecretStore{}}}

	resp, err := server.SetSecret(context.Background(), &proto.SetSecretRequest{Name: "key", Value: "val"})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "set secret")
}

func TestHostServiceServer_DeleteSecret_StoreError(t *testing.T) {
	server := &HostServiceServer{Deps: HostServiceDeps{SecretStore: &errorSecretStore{}}}

	resp, err := server.DeleteSecret(context.Background(), &proto.DeleteSecretRequest{Name: "key"})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "delete secret")
}

func TestHostServiceServer_GetSecret_StoreError(t *testing.T) {
	server := &HostServiceServer{Deps: HostServiceDeps{SecretStore: &errorSecretStore{}}}

	resp, err := server.GetSecret(context.Background(), &proto.GetSecretRequest{Name: "key"})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "get secret")
}

func TestHostServiceServer_ListSecrets_StoreError(t *testing.T) {
	server := &HostServiceServer{Deps: HostServiceDeps{SecretStore: &errorSecretStore{}}}

	resp, err := server.ListSecrets(context.Background(), &proto.ListSecretsRequest{})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "list secrets")
}

// =====================================================================
// ProviderWrapper tests
// =====================================================================

func TestNewProviderWrapper_Success(t *testing.T) {
	mock := &MockProviderPlugin{
		descriptors: map[string]*provider.Descriptor{
			"test": {Name: "test", APIVersion: "v1", Version: semver.MustParse("1.0.0")},
		},
	}
	client := &Client{plugin: mock, name: "test-plugin", path: "/path/to/plugin"}

	wrapper, err := NewProviderWrapper(client, "test")
	require.NoError(t, err)
	assert.Equal(t, "test", wrapper.Descriptor().Name)
	assert.Same(t, client, wrapper.Client())
}

func TestNewProviderWrapper_WithExtractDependencies(t *testing.T) {
	mock := &MockProviderPlugin{
		descriptors: map[string]*provider.Descriptor{
			"test": {
				Name:                "test",
				APIVersion:          "v1",
				Version:             semver.MustParse("1.0.0"),
				ExtractDependencies: func(_ map[string]any) []string { return nil },
			},
		},
		extractDepsFunc: func(_ context.Context, _ string, _ map[string]any) ([]string, error) {
			return []string{"dep1"}, nil
		},
	}
	client := &Client{plugin: mock, name: "test-plugin"}

	wrapper, err := NewProviderWrapper(client, "test")
	require.NoError(t, err)
	deps := wrapper.Descriptor().ExtractDependencies(map[string]any{"k": "v"})
	assert.Equal(t, []string{"dep1"}, deps)
}

func TestNewProviderWrapper_UnknownProvider(t *testing.T) {
	client := &Client{plugin: &MockProviderPlugin{}, name: "p"}

	_, err := NewProviderWrapper(client, "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get provider descriptor")
}

func TestProviderWrapper_Execute_UnaryPath(t *testing.T) {
	output := &provider.Output{Data: map[string]any{"result": "ok"}}
	mock := &MockProviderPlugin{
		descriptors: map[string]*provider.Descriptor{
			"test": {Name: "test", APIVersion: "v1", Version: semver.MustParse("1.0.0")},
		},
		execFunc: func(_ context.Context, _ string, _ map[string]any) (*provider.Output, error) {
			return output, nil
		},
	}
	client := &Client{plugin: mock, name: "p"}

	wrapper, err := NewProviderWrapper(client, "test")
	require.NoError(t, err)

	result, err := wrapper.Execute(context.Background(), map[string]any{"key": "val"})
	require.NoError(t, err)
	assert.Equal(t, output, result)
}

func TestProviderWrapper_Execute_BadInputType(t *testing.T) {
	mock := &MockProviderPlugin{
		descriptors: map[string]*provider.Descriptor{
			"test": {Name: "test", APIVersion: "v1", Version: semver.MustParse("1.0.0")},
		},
	}
	wrapper, err := NewProviderWrapper(&Client{plugin: mock, name: "p"}, "test")
	require.NoError(t, err)

	_, err = wrapper.Execute(context.Background(), "not-a-map")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected map[string]any")
}

func TestProviderWrapper_Execute_StreamingPath(t *testing.T) {
	sm := &streamingMockPlugin{
		MockProviderPlugin: &MockProviderPlugin{
			descriptors: map[string]*provider.Descriptor{
				"test": {Name: "test", APIVersion: "v1", Version: semver.MustParse("1.0.0")},
			},
		},
		streamFunc: func(_ context.Context, _ string, _ map[string]any, cb func(StreamChunk)) error {
			cb(StreamChunk{Stdout: []byte("output\n")})
			cb(StreamChunk{Result: &provider.Output{Data: map[string]any{"streamed": true}}})
			return nil
		},
	}
	client := &Client{plugin: sm, name: "p"}
	wrapper, err := NewProviderWrapper(client, "test")
	require.NoError(t, err)

	var stdout bytes.Buffer
	ctx := provider.WithIOStreams(context.Background(), &provider.IOStreams{
		Out:    &stdout,
		ErrOut: io.Discard,
	})

	result, err := wrapper.Execute(ctx, map[string]any{"key": "val"})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "output\n", stdout.String())
}

func TestProviderWrapper_Execute_StreamingFallbackToUnary(t *testing.T) {
	output := &provider.Output{Data: map[string]any{"unary": true}}
	mock := &MockProviderPlugin{
		descriptors: map[string]*provider.Descriptor{
			"test": {Name: "test", APIVersion: "v1", Version: semver.MustParse("1.0.0")},
		},
		execFunc: func(_ context.Context, _ string, _ map[string]any) (*provider.Output, error) {
			return output, nil
		},
	}
	client := &Client{plugin: mock, name: "p"}
	wrapper, err := NewProviderWrapper(client, "test")
	require.NoError(t, err)

	ctx := provider.WithIOStreams(context.Background(), &provider.IOStreams{
		Out:    io.Discard,
		ErrOut: io.Discard,
	})

	result, err := wrapper.Execute(ctx, map[string]any{"key": "val"})
	require.NoError(t, err)
	assert.Equal(t, output, result)
}

func TestProviderWrapper_Execute_StreamingError(t *testing.T) {
	sm := &streamingMockPlugin{
		MockProviderPlugin: &MockProviderPlugin{
			descriptors: map[string]*provider.Descriptor{
				"test": {Name: "test", APIVersion: "v1", Version: semver.MustParse("1.0.0")},
			},
		},
		streamFunc: func(_ context.Context, _ string, _ map[string]any, _ func(StreamChunk)) error {
			return fmt.Errorf("stream broke")
		},
	}
	wrapper, err := NewProviderWrapper(&Client{plugin: sm, name: "p"}, "test")
	require.NoError(t, err)

	ctx := provider.WithIOStreams(context.Background(), &provider.IOStreams{
		Out:    io.Discard,
		ErrOut: io.Discard,
	})

	_, err = wrapper.Execute(ctx, map[string]any{"key": "val"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stream broke")
}

func TestProviderWrapper_Execute_StreamingChunkError(t *testing.T) {
	sm := &streamingMockPlugin{
		MockProviderPlugin: &MockProviderPlugin{
			descriptors: map[string]*provider.Descriptor{
				"test": {Name: "test", APIVersion: "v1", Version: semver.MustParse("1.0.0")},
			},
		},
		streamFunc: func(_ context.Context, _ string, _ map[string]any, cb func(StreamChunk)) error {
			cb(StreamChunk{Error: "chunk-level error"})
			return nil
		},
	}
	wrapper, err := NewProviderWrapper(&Client{plugin: sm, name: "p"}, "test")
	require.NoError(t, err)

	ctx := provider.WithIOStreams(context.Background(), &provider.IOStreams{
		Out:    io.Discard,
		ErrOut: io.Discard,
	})

	_, err = wrapper.Execute(ctx, map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chunk-level error")
}

func TestProviderWrapper_Execute_StreamingNoResult(t *testing.T) {
	sm := &streamingMockPlugin{
		MockProviderPlugin: &MockProviderPlugin{
			descriptors: map[string]*provider.Descriptor{
				"test": {Name: "test", APIVersion: "v1", Version: semver.MustParse("1.0.0")},
			},
		},
		streamFunc: func(_ context.Context, _ string, _ map[string]any, cb func(StreamChunk)) error {
			cb(StreamChunk{Stdout: []byte("data")})
			return nil
		},
	}
	wrapper, err := NewProviderWrapper(&Client{plugin: sm, name: "p"}, "test")
	require.NoError(t, err)

	ctx := provider.WithIOStreams(context.Background(), &provider.IOStreams{
		Out:    io.Discard,
		ErrOut: io.Discard,
	})

	result, err := wrapper.Execute(ctx, map[string]any{})
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestProviderWrapper_Configure_Idempotent(t *testing.T) {
	mock := &MockProviderPlugin{
		descriptors: map[string]*provider.Descriptor{
			"test": {Name: "test", APIVersion: "v1", Version: semver.MustParse("1.0.0")},
		},
	}
	wrapper, err := NewProviderWrapper(&Client{plugin: mock, name: "p"}, "test")
	require.NoError(t, err)

	cfg := ProviderConfig{BinaryName: "mycli", Quiet: true}
	require.NoError(t, wrapper.Configure(context.Background(), cfg))
	require.NoError(t, wrapper.Configure(context.Background(), cfg))
}

func TestProviderWrapper_Configure_Error(t *testing.T) {
	mock := &MockProviderPlugin{
		descriptors: map[string]*provider.Descriptor{
			"test": {Name: "test", APIVersion: "v1", Version: semver.MustParse("1.0.0")},
		},
		configureErr: fmt.Errorf("config rejected"),
	}
	wrapper, err := NewProviderWrapper(&Client{plugin: mock, name: "p"}, "test")
	require.NoError(t, err)

	err = wrapper.Configure(context.Background(), ProviderConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config rejected")
}

func TestProviderWrapper_ExecuteStream_Success(t *testing.T) {
	sm := &streamingMockPlugin{
		MockProviderPlugin: &MockProviderPlugin{
			descriptors: map[string]*provider.Descriptor{
				"test": {Name: "test", APIVersion: "v1", Version: semver.MustParse("1.0.0")},
			},
		},
		streamFunc: func(_ context.Context, _ string, _ map[string]any, cb func(StreamChunk)) error {
			cb(StreamChunk{Stdout: []byte("data")})
			return nil
		},
	}
	wrapper, err := NewProviderWrapper(&Client{plugin: sm, name: "p"}, "test")
	require.NoError(t, err)

	var gotChunks []StreamChunk
	err = wrapper.ExecuteStream(context.Background(), map[string]any{"k": "v"}, func(chunk StreamChunk) {
		gotChunks = append(gotChunks, chunk)
	})
	require.NoError(t, err)
	assert.Len(t, gotChunks, 1)
}

func TestProviderWrapper_ExecuteStream_BadInput(t *testing.T) {
	mock := &MockProviderPlugin{
		descriptors: map[string]*provider.Descriptor{
			"test": {Name: "test", APIVersion: "v1", Version: semver.MustParse("1.0.0")},
		},
	}
	wrapper, err := NewProviderWrapper(&Client{plugin: mock, name: "p"}, "test")
	require.NoError(t, err)

	err = wrapper.ExecuteStream(context.Background(), "bad-type", func(_ StreamChunk) {})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected map[string]any")
}

// =====================================================================
// Client thin wrapper tests
// =====================================================================

func TestClient_ThinWrappers(t *testing.T) {
	output := &provider.Output{Data: map[string]any{"ok": true}}
	mock := &MockProviderPlugin{
		providers: []string{"p1"},
		descriptors: map[string]*provider.Descriptor{
			"p1": {Name: "p1", APIVersion: "v1", Version: semver.MustParse("1.0.0")},
		},
		execFunc: func(_ context.Context, _ string, _ map[string]any) (*provider.Output, error) {
			return output, nil
		},
		extractDepsFunc: func(_ context.Context, _ string, _ map[string]any) ([]string, error) {
			return []string{"d1"}, nil
		},
	}
	c := &Client{plugin: mock, name: "test-plugin", path: "/path/to/plugin"}

	providers, err := c.GetProviders(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"p1"}, providers)

	desc, err := c.GetProviderDescriptor(context.Background(), "p1")
	require.NoError(t, err)
	assert.Equal(t, "p1", desc.Name)

	result, err := c.ExecuteProvider(context.Background(), "p1", map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, output, result)

	require.NoError(t, c.ConfigureProvider(context.Background(), "p1", ProviderConfig{BinaryName: "cli"}))
	assert.ErrorIs(t, c.ExecuteProviderStream(context.Background(), "p1", map[string]any{}, func(_ StreamChunk) {}), ErrStreamingNotSupported)

	whatIf, err := c.DescribeWhatIf(context.Background(), "p1", map[string]any{})
	require.NoError(t, err)
	assert.NotEmpty(t, whatIf)

	deps, err := c.ExtractDependencies(context.Background(), "p1", map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, []string{"d1"}, deps)

	c.Kill()
	assert.Equal(t, "test-plugin", c.Name())
	assert.Equal(t, "/path/to/plugin", c.Path())
}

func TestPluginNameFromPath_Cases(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/usr/local/bin/scafctl-plugin-echo", "scafctl-plugin-echo"},
		{"/path/to/plugin.exe", "plugin"},
		{"simple", "simple"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.want, pluginNameFromPath(tt.path))
		})
	}
}

// =====================================================================
// unmarshalOutputSchemas tests
// =====================================================================

func TestUnmarshalOutputSchemas_ProtoFallback(t *testing.T) {
	protoSchemas := map[string]*proto.Schema{
		"from": {
			Parameters: map[string]*proto.Parameter{
				"status": {Type: "string", Description: "Status field"},
			},
		},
	}

	result := unmarshalOutputSchemas(nil, protoSchemas)
	require.NotNil(t, result)
	assert.Contains(t, result[provider.Capability("from")].Properties, "status")
}

func TestUnmarshalOutputSchemas_NilProtoSchema(t *testing.T) {
	result := unmarshalOutputSchemas(nil, map[string]*proto.Schema{"from": nil})
	require.NotNil(t, result)
	assert.NotContains(t, result, provider.Capability("from"))
}

func TestUnmarshalOutputSchemas_Empty(t *testing.T) {
	assert.Nil(t, unmarshalOutputSchemas(nil, nil))
}

func TestUnmarshalSchemaOrFallback_InvalidJSON(t *testing.T) {
	protoSchema := &proto.Schema{
		Parameters: map[string]*proto.Parameter{"key": {Type: "string"}},
	}

	result := unmarshalSchemaOrFallback([]byte(`{invalid`), protoSchema)
	require.NotNil(t, result)
	assert.Contains(t, result.Properties, "key")
}

// =====================================================================
// GRPCServer ExecuteProvider edge cases
// =====================================================================

func TestGRPCServer_ExecuteProvider_InvalidInput(t *testing.T) {
	server := &GRPCServer{Impl: &MockProviderPlugin{}}

	resp, err := server.ExecuteProvider(context.Background(), &proto.ExecuteProviderRequest{
		ProviderName: "test",
		Input:        []byte(`{invalid`),
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "failed to decode input")
}

func TestGRPCServer_ExecuteProvider_InvalidContext(t *testing.T) {
	server := &GRPCServer{Impl: &MockProviderPlugin{}}

	resp, err := server.ExecuteProvider(context.Background(), &proto.ExecuteProviderRequest{
		ProviderName: "test",
		Input:        []byte(`{}`),
		Context:      []byte(`{invalid`),
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "failed to decode context")
}

func TestGRPCServer_ExtractDependencies_InvalidInput(t *testing.T) {
	server := &GRPCServer{Impl: &MockProviderPlugin{}}

	resp, err := server.ExtractDependencies(context.Background(), &proto.ExtractDependenciesRequest{
		ProviderName: "test",
		Inputs:       []byte(`{invalid`),
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "failed to decode inputs")
}

func TestGRPCServer_ExtractDependencies_Error(t *testing.T) {
	mock := &MockProviderPlugin{
		extractDepsFunc: func(_ context.Context, _ string, _ map[string]any) ([]string, error) {
			return nil, fmt.Errorf("extraction error")
		},
	}
	server := &GRPCServer{Impl: mock}

	resp, err := server.ExtractDependencies(context.Background(), &proto.ExtractDependenciesRequest{
		ProviderName: "test",
		Inputs:       []byte(`{"k":"v"}`),
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "extraction error")
}

// =====================================================================
// WhatIf wiring in NewProviderWrapper
// =====================================================================

func TestNewProviderWrapper_WhatIfWiring(t *testing.T) {
	mock := &MockProviderPlugin{
		descriptors: map[string]*provider.Descriptor{
			"test": {Name: "test", APIVersion: "v1", Version: semver.MustParse("1.0.0")},
		},
	}
	wrapper, err := NewProviderWrapper(&Client{plugin: mock, name: "p"}, "test")
	require.NoError(t, err)

	whatIf, err := wrapper.Descriptor().WhatIf(context.Background(), map[string]any{"key": "val"})
	require.NoError(t, err)
	assert.Contains(t, whatIf, "Would execute test provider")
}

func TestNewProviderWrapper_WhatIf_NilInput(t *testing.T) {
	mock := &MockProviderPlugin{
		descriptors: map[string]*provider.Descriptor{
			"test": {Name: "test", APIVersion: "v1", Version: semver.MustParse("1.0.0")},
		},
	}
	wrapper, err := NewProviderWrapper(&Client{plugin: mock, name: "p"}, "test")
	require.NoError(t, err)

	whatIf, err := wrapper.Descriptor().WhatIf(context.Background(), nil)
	require.NoError(t, err)
	assert.NotEmpty(t, whatIf)
}

func TestNewProviderWrapper_WhatIf_NonMapInput(t *testing.T) {
	mock := &MockProviderPlugin{
		descriptors: map[string]*provider.Descriptor{
			"test": {Name: "test", APIVersion: "v1", Version: semver.MustParse("1.0.0")},
		},
	}
	wrapper, err := NewProviderWrapper(&Client{plugin: mock, name: "p"}, "test")
	require.NoError(t, err)

	whatIf, err := wrapper.Descriptor().WhatIf(context.Background(), "not-a-map")
	require.NoError(t, err)
	assert.Empty(t, whatIf)
}

// =====================================================================
// protoParamToJSON edge cases
// =====================================================================

func TestProtoParamToJSON_AllFields(t *testing.T) {
	param := &proto.Parameter{
		Type:                "number",
		Description:         "A number param",
		DefaultValue:        []byte(`42`),
		Example:             `42`,
		MinLength:           5,
		MaxLength:           100,
		Pattern:             "^[0-9]+$",
		Format:              "int32",
		HasMinimum:          true,
		Minimum:             0,
		HasMaximum:          true,
		Maximum:             1000,
		HasExclusiveMinimum: true,
		ExclusiveMinimum:    -1,
		HasExclusiveMaximum: true,
		ExclusiveMaximum:    1001,
		MinItems:            1,
		MaxItems:            50,
		EnumValues:          [][]byte{[]byte(`1`), []byte(`2`)},
	}

	schema := protoParamToJSON(param)
	assert.Equal(t, "number", schema.Type)
	assert.Equal(t, "^[0-9]+$", schema.Pattern)
	assert.Equal(t, "int32", schema.Format)
	require.NotNil(t, schema.MinLength)
	assert.Equal(t, 5, *schema.MinLength)
	require.NotNil(t, schema.MaxLength)
	assert.Equal(t, 100, *schema.MaxLength)
	require.NotNil(t, schema.Minimum)
	assert.InDelta(t, 0, *schema.Minimum, 0.001)
	require.NotNil(t, schema.Maximum)
	assert.InDelta(t, 1000, *schema.Maximum, 0.001)
	require.NotNil(t, schema.ExclusiveMinimum)
	assert.InDelta(t, -1, *schema.ExclusiveMinimum, 0.001)
	require.NotNil(t, schema.ExclusiveMaximum)
	assert.InDelta(t, 1001, *schema.ExclusiveMaximum, 0.001)
	require.NotNil(t, schema.MinItems)
	assert.Equal(t, 1, *schema.MinItems)
	require.NotNil(t, schema.MaxItems)
	assert.Equal(t, 50, *schema.MaxItems)
	assert.Len(t, schema.Enum, 2)
	assert.NotNil(t, schema.Default)
	assert.Len(t, schema.Examples, 1)
}

func TestProtoParamToJSON_MalformedExample(t *testing.T) {
	schema := protoParamToJSON(&proto.Parameter{Type: "string", Example: `{not-json`})
	assert.Empty(t, schema.Examples)
}

func TestProtoParamToJSON_MalformedEnum(t *testing.T) {
	schema := protoParamToJSON(&proto.Parameter{
		Type:       "string",
		EnumValues: [][]byte{[]byte(`"valid"`), []byte(`{bad`), []byte(`"ok"`)},
	})
	assert.Len(t, schema.Enum, 2)
}

// =====================================================================
// Benchmarks
// =====================================================================

func BenchmarkHostServiceClient_GetSecret(b *testing.B) {
	mock := &mockHostServiceClient{
		getSecretResp: &proto.GetSecretResponse{Value: "val", Found: true},
	}
	client := &HostServiceClient{client: mock}
	ctx := context.Background()

	b.ResetTimer()
	for b.Loop() {
		_, _, _ = client.GetSecret(ctx, "key")
	}
}

func BenchmarkGRPCClient_ExecuteProvider(b *testing.B) {
	output := &provider.Output{Data: map[string]any{"ok": true}}
	outputBytes, _ := json.Marshal(output)
	mock := &mockPluginServiceClient{
		executeProviderResp: &proto.ExecuteProviderResponse{Output: outputBytes},
	}
	client := &GRPCClient{client: mock}
	ctx := context.Background()

	b.ResetTimer()
	for b.Loop() {
		_, _ = client.ExecuteProvider(ctx, "test", map[string]any{"k": "v"})
	}
}

// --- StopProvider tests ---

func TestGRPCClient_StopProvider_Success(t *testing.T) {
	mock := &mockPluginServiceClient{
		stopProviderResp: &proto.StopProviderResponse{},
	}
	client := &GRPCClient{client: mock}

	err := client.StopProvider(context.Background(), "test-provider")
	assert.NoError(t, err)
}

func TestGRPCClient_StopProvider_Error(t *testing.T) {
	mock := &mockPluginServiceClient{
		stopProviderResp: &proto.StopProviderResponse{Error: "shutdown failed"},
	}
	client := &GRPCClient{client: mock}

	err := client.StopProvider(context.Background(), "test-provider")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "shutdown failed")
}

func TestGRPCClient_StopProvider_Unimplemented(t *testing.T) {
	mock := &mockPluginServiceClient{
		stopProviderErr: status.Error(codes.Unimplemented, "not implemented"),
	}
	client := &GRPCClient{client: mock}

	err := client.StopProvider(context.Background(), "test-provider")
	assert.NoError(t, err, "unimplemented should be treated as no-op")
}

func TestGRPCClient_StopProvider_RPCError(t *testing.T) {
	mock := &mockPluginServiceClient{
		stopProviderErr: status.Error(codes.Unavailable, "connection lost"),
	}
	client := &GRPCClient{client: mock}

	err := client.StopProvider(context.Background(), "")
	assert.Error(t, err)
}

func TestGRPCServer_StopProvider(t *testing.T) {
	mock := &MockProviderPlugin{}
	server := &GRPCServer{Impl: mock}

	resp, err := server.StopProvider(context.Background(), &proto.StopProviderRequest{
		ProviderName: "test-provider",
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
}

// --- GetAuthToken tests ---

func TestHostServiceServer_GetAuthToken_Success(t *testing.T) {
	deps := HostServiceDeps{
		AllowedAuthHandlers: []string{"github"},
		AuthTokenFunc: func(_ context.Context, handler, scope string, minValidFor int64, forceRefresh bool) (*proto.GetAuthTokenResponse, error) {
			return &proto.GetAuthTokenResponse{
				AccessToken:   "tok-123",
				TokenType:     "Bearer",
				ExpiresAtUnix: 1700000000,
				Scope:         scope,
			}, nil
		},
	}
	server := &HostServiceServer{Deps: deps}

	resp, err := server.GetAuthToken(context.Background(), &proto.GetAuthTokenRequest{
		HandlerName:        "github",
		Scope:              "repo",
		MinValidForSeconds: 60,
	})
	require.NoError(t, err)
	assert.Equal(t, "tok-123", resp.AccessToken)
	assert.Equal(t, "Bearer", resp.TokenType)
	assert.Empty(t, resp.Error)
}

func TestHostServiceServer_GetAuthToken_NotAvailable(t *testing.T) {
	server := &HostServiceServer{Deps: HostServiceDeps{}}

	resp, err := server.GetAuthToken(context.Background(), &proto.GetAuthTokenRequest{
		HandlerName: "github",
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "not available")
}

func TestHostServiceServer_GetAuthToken_Denied(t *testing.T) {
	deps := HostServiceDeps{
		AllowedAuthHandlers: []string{"azure"},
		AuthTokenFunc: func(_ context.Context, _, _ string, _ int64, _ bool) (*proto.GetAuthTokenResponse, error) {
			t.Fatal("should not be called")
			return nil, nil
		},
	}
	server := &HostServiceServer{Deps: deps}

	resp, err := server.GetAuthToken(context.Background(), &proto.GetAuthTokenRequest{
		HandlerName: "github",
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "access denied")
}

func TestHostServiceServer_GetAuthToken_FuncError(t *testing.T) {
	deps := HostServiceDeps{
		AllowedAuthHandlers: []string{"github"},
		AuthTokenFunc: func(_ context.Context, _, _ string, _ int64, _ bool) (*proto.GetAuthTokenResponse, error) {
			return nil, errors.New("token refresh failed")
		},
	}
	server := &HostServiceServer{Deps: deps}

	resp, err := server.GetAuthToken(context.Background(), &proto.GetAuthTokenRequest{
		HandlerName: "github",
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "token refresh failed")
}

func TestHostServiceClient_GetAuthToken_Success(t *testing.T) {
	mock := &mockHostServiceClient{
		getAuthTokenResp: &proto.GetAuthTokenResponse{
			AccessToken: "tok-456",
			TokenType:   "Bearer",
		},
	}
	client := &HostServiceClient{client: mock}

	resp, err := client.GetAuthToken(context.Background(), "github", "repo", 60, false)
	require.NoError(t, err)
	assert.Equal(t, "tok-456", resp.AccessToken)
}

func TestHostServiceClient_GetAuthToken_RPCError(t *testing.T) {
	mock := &mockHostServiceClient{
		getAuthTokenErr: status.Error(codes.Unavailable, "connection lost"),
	}
	client := &HostServiceClient{client: mock}

	_, err := client.GetAuthToken(context.Background(), "github", "repo", 0, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "host GetAuthToken")
}

func TestHostServiceClient_GetAuthToken_ResponseError(t *testing.T) {
	mock := &mockHostServiceClient{
		getAuthTokenResp: &proto.GetAuthTokenResponse{
			Error: "access denied",
		},
	}
	client := &HostServiceClient{client: mock}

	_, err := client.GetAuthToken(context.Background(), "github", "repo", 0, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "access denied")
}

// --- KillAll tests ---

func TestKillAll_NilSlice(t *testing.T) {
	// Should not panic on nil
	KillAll(nil)
}

func TestKillAll_EmptySlice(t *testing.T) {
	// Should not panic on empty
	KillAll([]*Client{})
}

// --- Protocol version tests ---

func TestGRPCClient_ConfigureProvider_SendsProtocolVersion(t *testing.T) {
	mock := &mockPluginServiceClient{
		configureProviderResp: &proto.ConfigureProviderResponse{},
	}
	client := &GRPCClient{client: mock}

	err := client.ConfigureProvider(context.Background(), "test", ProviderConfig{
		BinaryName: "scafctl",
	})
	require.NoError(t, err)
	require.NotNil(t, mock.lastConfigureReq)
	assert.Equal(t, PluginProtocolVersion, mock.lastConfigureReq.ProtocolVersion)
}

// --- WrapperOption tests ---

func TestNewProviderWrapper_WithContext(t *testing.T) {
	mock := &MockProviderPlugin{}
	client := &Client{plugin: mock, name: "test-plugin"}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapper, err := NewProviderWrapper(client, "test-provider", WithContext(ctx))
	require.NoError(t, err)
	assert.Equal(t, "test-provider", wrapper.Descriptor().Name)
}

// --- Descriptor caching tests ---

func TestClient_DescriptorCache_HitOnSecondCall(t *testing.T) {
	callCount := 0
	mock := &MockProviderPlugin{
		descriptors: map[string]*provider.Descriptor{
			"test-provider": {
				Name: "test-provider",
			},
		},
	}
	// Wrap the mock to count calls
	wrapper := &descriptorCountingPlugin{MockProviderPlugin: mock, count: &callCount}
	client := &Client{plugin: wrapper, name: "test-plugin"}

	ctx := context.Background()

	desc1, err := client.GetProviderDescriptor(ctx, "test-provider")
	require.NoError(t, err)
	assert.Equal(t, "test-provider", desc1.Name)
	assert.Equal(t, 1, callCount)

	// Second call should hit cache
	desc2, err := client.GetProviderDescriptor(ctx, "test-provider")
	require.NoError(t, err)
	assert.Equal(t, desc1, desc2)
	assert.Equal(t, 1, callCount, "second call should hit cache, not invoke RPC")
}

// descriptorCountingPlugin wraps MockProviderPlugin and counts GetProviderDescriptor calls.
type descriptorCountingPlugin struct {
	*MockProviderPlugin
	count *int
}

func (d *descriptorCountingPlugin) GetProviderDescriptor(ctx context.Context, name string) (*provider.Descriptor, error) {
	*d.count++
	return d.MockProviderPlugin.GetProviderDescriptor(ctx, name)
}
