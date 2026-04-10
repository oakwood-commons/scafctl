// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/plugin/proto"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGRPCServer_ConfigureProvider(t *testing.T) {
	tests := []struct {
		name         string
		configureErr error
		wantErr      string
	}{
		{
			name: "success",
		},
		{
			name:         "plugin returns error",
			configureErr: assert.AnError,
			wantErr:      assert.AnError.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockProviderPlugin{
				configureErr: tt.configureErr,
			}
			server := &GRPCServer{Impl: mock}

			resp, err := server.ConfigureProvider(context.Background(), &proto.ConfigureProviderRequest{
				ProviderName:  "test-provider",
				Quiet:         true,
				NoColor:       true,
				BinaryName:    "mycli",
				HostServiceId: 99,
				Settings: map[string][]byte{
					"maxBodySize": []byte(`1048576`),
				},
			})
			require.NoError(t, err)

			if tt.wantErr != "" {
				assert.Contains(t, resp.Error, tt.wantErr)
				return
			}

			assert.Empty(t, resp.Error)
			require.NotNil(t, mock.lastConfig)
			assert.True(t, mock.lastConfig.Quiet)
			assert.True(t, mock.lastConfig.NoColor)
			assert.Equal(t, "mycli", mock.lastConfig.BinaryName)
			assert.Equal(t, uint32(99), mock.lastConfig.HostServiceID)
			assert.Contains(t, mock.lastConfig.Settings, "maxBodySize")
		})
	}
}

func TestGRPCClient_ConfigureProvider(t *testing.T) {
	mock := &MockProviderPlugin{}
	server := &GRPCServer{Impl: mock}

	resp, err := server.ConfigureProvider(context.Background(), &proto.ConfigureProviderRequest{
		ProviderName: "test-provider",
		BinaryName:   "scafctl",
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	require.NotNil(t, mock.lastConfig)
	assert.Equal(t, "scafctl", mock.lastConfig.BinaryName)
}

func TestGRPCServer_ExecuteProvider_ExtendedContext(t *testing.T) {
	var capturedCtx context.Context
	mock := &MockProviderPlugin{
		execFunc: func(ctx context.Context, _ string, _ map[string]any) (*provider.Output, error) {
			capturedCtx = ctx
			return &provider.Output{Data: map[string]any{"ok": true}}, nil
		},
	}

	server := &GRPCServer{Impl: mock}
	inputBytes, _ := json.Marshal(map[string]any{"key": "value"})
	paramBytes, _ := json.Marshal(map[string]any{"env": "prod"})
	iterItemBytes, _ := json.Marshal("item-1")

	resp, err := server.ExecuteProvider(context.Background(), &proto.ExecuteProviderRequest{
		ProviderName:     "test",
		Input:            inputBytes,
		DryRun:           true,
		ExecutionMode:    "action",
		WorkingDirectory: "/tmp/work",
		OutputDirectory:  "/tmp/output",
		ConflictStrategy: "overwrite",
		Backup:           true,
		Parameters:       paramBytes,
		IterationContext: &proto.IterationContext{
			Item:       iterItemBytes,
			Index:      2,
			ItemAlias:  "server",
			IndexAlias: "i",
		},
		SolutionMetadata: &proto.SolutionMeta{
			Name:        "my-solution",
			Version:     "1.0.0",
			DisplayName: "My Solution",
			Description: "A test solution",
			Category:    "test",
			Tags:        []string{"go", "test"},
		},
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	require.NotNil(t, capturedCtx)

	// Verify all context values were injected
	assert.True(t, provider.DryRunFromContext(capturedCtx))

	mode, ok := provider.ExecutionModeFromContext(capturedCtx)
	assert.True(t, ok)
	assert.Equal(t, provider.Capability("action"), mode)

	dir, ok := provider.WorkingDirectoryFromContext(capturedCtx)
	assert.True(t, ok)
	assert.Equal(t, "/tmp/work", dir)

	outDir, ok := provider.OutputDirectoryFromContext(capturedCtx)
	assert.True(t, ok)
	assert.Equal(t, "/tmp/output", outDir)

	strategy, ok := provider.ConflictStrategyFromContext(capturedCtx)
	assert.True(t, ok)
	assert.Equal(t, "overwrite", strategy)

	backup, ok := provider.BackupFromContext(capturedCtx)
	assert.True(t, ok)
	assert.True(t, backup)

	params, ok := provider.ParametersFromContext(capturedCtx)
	assert.True(t, ok)
	assert.Equal(t, "prod", params["env"])

	iterCtx, ok := provider.IterationContextFromContext(capturedCtx)
	assert.True(t, ok)
	assert.Equal(t, "item-1", iterCtx.Item)
	assert.Equal(t, 2, iterCtx.Index)
	assert.Equal(t, "server", iterCtx.ItemAlias)
	assert.Equal(t, "i", iterCtx.IndexAlias)

	meta, ok := provider.SolutionMetadataFromContext(capturedCtx)
	assert.True(t, ok)
	assert.Equal(t, "my-solution", meta.Name)
	assert.Equal(t, "1.0.0", meta.Version)
	assert.Equal(t, "My Solution", meta.DisplayName)
	assert.Equal(t, "A test solution", meta.Description)
	assert.Equal(t, "test", meta.Category)
	assert.Equal(t, []string{"go", "test"}, meta.Tags)
}

func TestBuildExecuteProviderRequest_RoundTrip(t *testing.T) {
	ctx := context.Background()
	ctx = provider.WithDryRun(ctx, true)
	ctx = provider.WithExecutionMode(ctx, provider.CapabilityAction)
	ctx = provider.WithWorkingDirectory(ctx, "/work")
	ctx = provider.WithOutputDirectory(ctx, "/out")
	ctx = provider.WithConflictStrategy(ctx, "skip")
	ctx = provider.WithBackup(ctx, true)
	ctx = provider.WithParameters(ctx, map[string]any{"region": "us-east-1"})
	ctx = provider.WithResolverContext(ctx, map[string]any{"resolver1": map[string]any{"val": 42}})
	ctx = provider.WithIterationContext(ctx, &provider.IterationContext{
		Item:       "elem",
		Index:      5,
		ItemAlias:  "x",
		IndexAlias: "idx",
	})
	ctx = provider.WithSolutionMetadata(ctx, &provider.SolutionMeta{
		Name:    "test-sol",
		Version: "2.0.0",
	})

	req, err := buildExecuteProviderRequest(ctx, "my-provider", []byte(`{"hello":"world"}`))
	require.NoError(t, err)

	assert.Equal(t, "my-provider", req.ProviderName)
	assert.True(t, req.DryRun)
	assert.Equal(t, "action", req.ExecutionMode)
	assert.Equal(t, "/work", req.WorkingDirectory)
	assert.Equal(t, "/out", req.OutputDirectory)
	assert.Equal(t, "skip", req.ConflictStrategy)
	assert.True(t, req.Backup)

	require.NotNil(t, req.IterationContext)
	assert.Equal(t, int32(5), req.IterationContext.Index)
	assert.Equal(t, "x", req.IterationContext.ItemAlias)

	require.NotNil(t, req.SolutionMetadata)
	assert.Equal(t, "test-sol", req.SolutionMetadata.Name)
	assert.Equal(t, "2.0.0", req.SolutionMetadata.Version)

	var params map[string]any
	require.NoError(t, json.Unmarshal(req.Parameters, &params))
	assert.Equal(t, "us-east-1", params["region"])
}

func TestHostServiceServer_SecretStore_Nil(t *testing.T) {
	server := &HostServiceServer{Deps: HostServiceDeps{}}

	resp, err := server.GetSecret(context.Background(), &proto.GetSecretRequest{Name: "test"})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "not available")

	setResp, err := server.SetSecret(context.Background(), &proto.SetSecretRequest{Name: "test", Value: "val"})
	require.NoError(t, err)
	assert.Contains(t, setResp.Error, "not available")

	delResp, err := server.DeleteSecret(context.Background(), &proto.DeleteSecretRequest{Name: "test"})
	require.NoError(t, err)
	assert.Contains(t, delResp.Error, "not available")

	listResp, err := server.ListSecrets(context.Background(), &proto.ListSecretsRequest{})
	require.NoError(t, err)
	assert.Contains(t, listResp.Error, "not available")
}

func TestHostServiceServer_Auth_Nil(t *testing.T) {
	server := &HostServiceServer{Deps: HostServiceDeps{}}

	identResp, err := server.GetAuthIdentity(context.Background(), &proto.GetAuthIdentityRequest{})
	require.NoError(t, err)
	assert.Contains(t, identResp.Error, "not available")

	handlersResp, err := server.ListAuthHandlers(context.Background(), &proto.ListAuthHandlersRequest{})
	require.NoError(t, err)
	assert.Empty(t, handlersResp.HandlerNames)
}

func TestHostServiceServer_Auth_WithFunc(t *testing.T) {
	server := &HostServiceServer{
		Deps: HostServiceDeps{
			AuthHandlersFunc: func(_ context.Context) ([]string, string, error) {
				return []string{"github", "azure"}, "github", nil
			},
			AuthIdentityFunc: func(_ context.Context, handler, _ string) (*proto.Claims, error) {
				return &proto.Claims{
					Subject: "user@example.com",
					Name:    "Test User",
				}, nil
			},
		},
	}

	handlersResp, err := server.ListAuthHandlers(context.Background(), &proto.ListAuthHandlersRequest{})
	require.NoError(t, err)
	assert.Equal(t, []string{"github", "azure"}, handlersResp.HandlerNames)
	assert.Equal(t, "github", handlersResp.DefaultHandler)

	identResp, err := server.GetAuthIdentity(context.Background(), &proto.GetAuthIdentityRequest{
		HandlerName: "github",
	})
	require.NoError(t, err)
	assert.Empty(t, identResp.Error)
	require.NotNil(t, identResp.Claims)
	assert.Equal(t, "user@example.com", identResp.Claims.Subject)
	assert.Equal(t, "Test User", identResp.Claims.Name)
}

func TestProviderWrapper_Configure(t *testing.T) {
	// Create a mock that tracks configuration
	mock := &MockProviderPlugin{}
	server := &GRPCServer{Impl: mock}

	// Test the server-side ConfigureProvider directly
	resp, err := server.ConfigureProvider(context.Background(), &proto.ConfigureProviderRequest{
		ProviderName: "test-provider",
		Quiet:        true,
		BinaryName:   "testcli",
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.True(t, mock.lastConfig.Quiet)
	assert.Equal(t, "testcli", mock.lastConfig.BinaryName)
}

func TestErrStreamingNotSupported(t *testing.T) {
	assert.EqualError(t, ErrStreamingNotSupported, "streaming execution not supported")
}

// TestPluginLifecycle exercises the full lifecycle:
// descriptor → configure → extract dependencies → execute, all via the
// GRPCServer/GRPCClient layer with a mock plugin underneath.
func TestPluginLifecycle(t *testing.T) {
	extractDepsFunc := func(_ context.Context, _ string, inputs map[string]any) ([]string, error) {
		// Custom extraction: look for "rslvr" keys in inputs
		deps := make([]string, 0, len(inputs))
		for _, v := range inputs {
			if m, ok := v.(map[string]any); ok {
				if rslvr, ok := m["rslvr"].(string); ok {
					deps = append(deps, rslvr)
				}
			}
		}
		return deps, nil
	}

	mock := &MockProviderPlugin{
		providers: []string{"custom"},
		descriptors: map[string]*provider.Descriptor{
			"custom": {
				Name:        "custom",
				DisplayName: "Custom Provider",
				Description: "A lifecycle-test provider",
				APIVersion:  "v1",
				Version:     semver.MustParse("2.1.0"),
				Category:    "test",
				Capabilities: []provider.Capability{
					provider.CapabilityFrom,
					provider.CapabilityTransform,
				},
				Schema: schemahelper.ObjectSchema([]string{"expression"}, map[string]*jsonschema.Schema{
					"expression": schemahelper.StringProp("CEL expression"),
				}),
				ExtractDependencies: func(inputs map[string]any) []string {
					return []string{"dep1", "dep2"}
				},
			},
		},
		execFunc: func(_ context.Context, name string, input map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: map[string]any{"echo": input, "provider": name}}, nil
		},
	}
	// Override the interface-level ExtractDependencies to use our mock
	mock.extractDepsFunc = extractDepsFunc

	server := &GRPCServer{Impl: mock}

	// --- 1. GetProviders ---
	providersResp, err := server.GetProviders(context.Background(), &proto.GetProvidersRequest{})
	require.NoError(t, err)
	assert.Equal(t, []string{"custom"}, providersResp.ProviderNames)

	// --- 2. GetProviderDescriptor ---
	descResp, err := server.GetProviderDescriptor(context.Background(), &proto.GetProviderDescriptorRequest{
		ProviderName: "custom",
	})
	require.NoError(t, err)
	protoDesc := descResp.GetDescriptor_()
	assert.Equal(t, "custom", protoDesc.Name)
	assert.Equal(t, "Custom Provider", protoDesc.DisplayName)
	assert.True(t, protoDesc.HasExtractDependencies, "expected HasExtractDependencies flag")
	assert.NotEmpty(t, protoDesc.RawSchema, "expected RawSchema to be set")

	// Verify lossless schema round-trip
	desc, err := protoToDescriptor(protoDesc)
	require.NoError(t, err)
	assert.Equal(t, "custom", desc.Name)
	require.NotNil(t, desc.Schema)
	assert.Contains(t, desc.Schema.Properties, "expression")
	assert.Equal(t, []string{"expression"}, desc.Schema.Required)

	// --- 3. ConfigureProvider ---
	cfgResp, err := server.ConfigureProvider(context.Background(), &proto.ConfigureProviderRequest{
		ProviderName: "custom",
		Quiet:        true,
		NoColor:      false,
		BinaryName:   "myapp",
	})
	require.NoError(t, err)
	assert.Empty(t, cfgResp.Error)
	require.NotNil(t, mock.lastConfig)
	assert.True(t, mock.lastConfig.Quiet)
	assert.Equal(t, "myapp", mock.lastConfig.BinaryName)

	// --- 4. ExtractDependencies ---
	inputBytes, _ := json.Marshal(map[string]any{
		"ref1": map[string]any{"rslvr": "resolver-a"},
		"ref2": map[string]any{"rslvr": "resolver-b"},
	})
	depsResp, err := server.ExtractDependencies(context.Background(), &proto.ExtractDependenciesRequest{
		ProviderName: "custom",
		Inputs:       inputBytes,
	})
	require.NoError(t, err)
	assert.Empty(t, depsResp.Error)
	assert.ElementsMatch(t, []string{"resolver-a", "resolver-b"}, depsResp.Dependencies)

	// --- 5. ExecuteProvider ---
	execInputBytes, _ := json.Marshal(map[string]any{"key": "value"})
	execResp, err := server.ExecuteProvider(context.Background(), &proto.ExecuteProviderRequest{
		ProviderName: "custom",
		Input:        execInputBytes,
	})
	require.NoError(t, err)
	assert.Empty(t, execResp.Error)

	var output provider.Output
	require.NoError(t, json.Unmarshal(execResp.Output, &output))
	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "custom", data["provider"])
}

// TestSchemaRoundTrip_Lossless verifies that jsonschema.Schema survives a
// descriptorToProto → protoToDescriptor round-trip via raw JSON bytes,
// including fields not represented in the structured Parameter format.
func TestSchemaRoundTrip_Lossless(t *testing.T) {
	minLen := 3
	maxLen := 100
	minVal := 1.0
	maxVal := 1000.0
	exclMin := 0.0
	exclMax := 1001.0
	minItems := 1
	maxItems := 50

	original := &provider.Descriptor{
		Name:         "schema-test",
		Description:  "Tests lossless schema round-trip",
		APIVersion:   "v1",
		Version:      semver.MustParse("1.0.0"),
		Capabilities: []provider.Capability{provider.CapabilityFrom},
		Schema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"name": {
					Type:        "string",
					Description: "User name",
					MinLength:   &minLen,
					MaxLength:   &maxLen,
					Pattern:     "^[a-zA-Z]+$",
					Format:      "hostname",
				},
				"age": {
					Type:             "integer",
					Minimum:          &minVal,
					Maximum:          &maxVal,
					ExclusiveMinimum: &exclMin,
					ExclusiveMaximum: &exclMax,
				},
				"tags": {
					Type:     "array",
					MinItems: &minItems,
					MaxItems: &maxItems,
				},
				"role": {
					Type: "string",
					Enum: []any{"admin", "user", "guest"},
				},
			},
			Required: []string{"name"},
		},
		OutputSchemas: map[provider.Capability]*jsonschema.Schema{
			provider.CapabilityFrom: {
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"status": {Type: "string", Enum: []any{"ok", "error"}},
				},
			},
		},
	}

	// Serialize to proto
	protoDesc := descriptorToProto(original)
	assert.NotEmpty(t, protoDesc.RawSchema, "RawSchema should be populated")
	assert.NotEmpty(t, protoDesc.RawOutputSchemas, "RawOutputSchemas should be populated")

	// Deserialize back
	desc, err := protoToDescriptor(protoDesc)
	require.NoError(t, err)
	require.NotNil(t, desc.Schema)

	// Verify lossless fields
	nameProp := desc.Schema.Properties["name"]
	require.NotNil(t, nameProp)
	assert.Equal(t, "string", nameProp.Type)
	assert.Equal(t, "hostname", nameProp.Format)
	require.NotNil(t, nameProp.MinLength)
	assert.Equal(t, 3, *nameProp.MinLength)
	require.NotNil(t, nameProp.MaxLength)
	assert.Equal(t, 100, *nameProp.MaxLength)

	ageProp := desc.Schema.Properties["age"]
	require.NotNil(t, ageProp)
	require.NotNil(t, ageProp.Minimum)
	assert.InDelta(t, 1.0, *ageProp.Minimum, 0.001)
	require.NotNil(t, ageProp.Maximum)
	assert.InDelta(t, 1000.0, *ageProp.Maximum, 0.001)
	require.NotNil(t, ageProp.ExclusiveMinimum)
	assert.InDelta(t, 0.0, *ageProp.ExclusiveMinimum, 0.001)
	require.NotNil(t, ageProp.ExclusiveMaximum)
	assert.InDelta(t, 1001.0, *ageProp.ExclusiveMaximum, 0.001)

	tagsProp := desc.Schema.Properties["tags"]
	require.NotNil(t, tagsProp)
	require.NotNil(t, tagsProp.MinItems)
	assert.Equal(t, 1, *tagsProp.MinItems)
	require.NotNil(t, tagsProp.MaxItems)
	assert.Equal(t, 50, *tagsProp.MaxItems)

	roleProp := desc.Schema.Properties["role"]
	require.NotNil(t, roleProp)
	assert.Len(t, roleProp.Enum, 3)

	// Verify output schemas round-tripped
	require.Contains(t, desc.OutputSchemas, provider.CapabilityFrom)
	outSchema := desc.OutputSchemas[provider.CapabilityFrom]
	require.NotNil(t, outSchema)
	statusProp := outSchema.Properties["status"]
	require.NotNil(t, statusProp)
	assert.Len(t, statusProp.Enum, 2)
}

// TestSchemaRoundTrip_StructuredFallback verifies that the structured
// Parameter fields work correctly when raw JSON is not available (e.g.,
// talking to an older plugin).
func TestSchemaRoundTrip_StructuredFallback(t *testing.T) {
	// Build a proto descriptor manually without RawSchema
	protoDesc := &proto.ProviderDescriptor{
		Name:         "fallback-test",
		Description:  "Tests structured fallback",
		Version:      "1.0.0",
		ApiVersion:   "v1",
		Capabilities: []string{"from"},
		Schema: &proto.Schema{
			Parameters: map[string]*proto.Parameter{
				"name": {
					Type:        "string",
					Required:    true,
					Description: "User name",
					MinLength:   5,
					MaxLength:   50,
					Pattern:     "^[a-z]+$",
					Format:      "email",
				},
				"count": {
					Type:        "integer",
					HasMinimum:  true,
					Minimum:     0,
					HasMaximum:  true,
					Maximum:     100,
					Description: "Item count",
				},
				"status": {
					Type:       "string",
					EnumValues: [][]byte{[]byte(`"active"`), []byte(`"inactive"`)},
				},
			},
		},
	}

	desc, err := protoToDescriptor(protoDesc)
	require.NoError(t, err)
	require.NotNil(t, desc.Schema)

	nameProp := desc.Schema.Properties["name"]
	require.NotNil(t, nameProp)
	require.NotNil(t, nameProp.MinLength)
	assert.Equal(t, 5, *nameProp.MinLength)
	assert.Equal(t, "email", nameProp.Format)

	countProp := desc.Schema.Properties["count"]
	require.NotNil(t, countProp)
	require.NotNil(t, countProp.Minimum)
	assert.InDelta(t, 0.0, *countProp.Minimum, 0.001)
	require.NotNil(t, countProp.Maximum)
	assert.InDelta(t, 100.0, *countProp.Maximum, 0.001)

	statusProp := desc.Schema.Properties["status"]
	require.NotNil(t, statusProp)
	assert.Len(t, statusProp.Enum, 2)

	assert.Contains(t, desc.Schema.Required, "name")
}

// TestExtractDependencies_RPC tests the ExtractDependencies server and client.
func TestExtractDependencies_RPC(t *testing.T) {
	mock := &MockProviderPlugin{
		extractDepsFunc: func(_ context.Context, _ string, inputs map[string]any) ([]string, error) {
			deps := make([]string, 0, len(inputs))
			for k := range inputs {
				deps = append(deps, k)
			}
			return deps, nil
		},
	}
	server := &GRPCServer{Impl: mock}

	inputBytes, _ := json.Marshal(map[string]any{
		"resolver-a": "val1",
		"resolver-b": "val2",
	})
	resp, err := server.ExtractDependencies(context.Background(), &proto.ExtractDependenciesRequest{
		ProviderName: "test",
		Inputs:       inputBytes,
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.ElementsMatch(t, []string{"resolver-a", "resolver-b"}, resp.Dependencies)
}

// TestExtractDependencies_Nil tests that nil implementation returns empty.
func TestExtractDependencies_Nil(t *testing.T) {
	mock := &MockProviderPlugin{}
	server := &GRPCServer{Impl: mock}

	resp, err := server.ExtractDependencies(context.Background(), &proto.ExtractDependenciesRequest{
		ProviderName: "test",
		Inputs:       []byte(`{"x":"y"}`),
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.Empty(t, resp.Dependencies)
}

// TestHostDeps_WiringInGRPCPlugin verifies that HostDeps flows through
// GRPCPlugin.GRPCClient when set.
func TestHostDeps_WiringInGRPCPlugin(t *testing.T) {
	// When HostDeps is nil, hostServiceID should be 0
	pluginNoDeps := &GRPCPlugin{Impl: &MockProviderPlugin{}}
	assert.Nil(t, pluginNoDeps.HostDeps)

	// When HostDeps is set, the GRPCPlugin struct stores it
	deps := &HostServiceDeps{
		SecretStore: nil, // nil is fine — HostServiceServer handles nil gracefully
	}
	pluginWithDeps := &GRPCPlugin{
		Impl:     &MockProviderPlugin{},
		HostDeps: deps,
	}
	assert.NotNil(t, pluginWithDeps.HostDeps)
}

func TestValidateSecretName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "valid simple", input: "my-secret", wantErr: false},
		{name: "valid with dots", input: "plugin.my-secret", wantErr: false},
		{name: "valid with slash", input: "plugins/echo/key", wantErr: false},
		{name: "empty", input: "", wantErr: true},
		{name: "path traversal", input: "../etc/passwd", wantErr: true},
		{name: "double dot mid", input: "a/../b", wantErr: true},
		{name: "special chars", input: "secret;rm -rf", wantErr: true},
		{name: "newline", input: "secret\nname", wantErr: true},
		{name: "too long", input: strings.Repeat("a", 300), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSecretName(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestHostServiceServer_ListSecrets_PatternFiltering(t *testing.T) {
	store := &fakeSecretStore{
		secrets: map[string][]byte{
			"db/host":     []byte("localhost"),
			"db/password": []byte("secret"),
			"api/key":     []byte("key-val"),
			"api/token":   []byte("tok-val"),
		},
	}
	server := &HostServiceServer{
		Deps: HostServiceDeps{SecretStore: store},
	}
	ctx := context.Background()

	tests := []struct {
		name      string
		pattern   string
		wantNames []string
		wantErr   string
	}{
		{
			name:      "no pattern returns all",
			pattern:   "",
			wantNames: []string{"db/host", "db/password", "api/key", "api/token"},
		},
		{
			name:      "prefix pattern",
			pattern:   "^db/",
			wantNames: []string{"db/host", "db/password"},
		},
		{
			name:      "suffix pattern",
			pattern:   "key$",
			wantNames: []string{"api/key"},
		},
		{
			name:    "invalid regex",
			pattern: "[invalid",
			wantErr: "invalid pattern",
		},
		{
			name:    "pattern too long",
			pattern: strings.Repeat("a", maxSecretPatternLength+1),
			wantErr: "pattern too long",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := server.ListSecrets(ctx, &proto.ListSecretsRequest{Pattern: tt.pattern})
			require.NoError(t, err)

			if tt.wantErr != "" {
				assert.Contains(t, resp.Error, tt.wantErr)
				return
			}

			assert.Empty(t, resp.Error)
			assert.ElementsMatch(t, tt.wantNames, resp.Names)
		})
	}
}

func TestHostServiceServer_SecretScopeEnforcement(t *testing.T) {
	store := &fakeSecretStore{
		secrets: map[string][]byte{
			"plugins/echo/api-key":  []byte("secret-val"),
			"plugins/other/api-key": []byte("other-val"),
			"global-secret":         []byte("global-val"),
		},
	}
	server := &HostServiceServer{
		Deps: HostServiceDeps{
			SecretStore:         store,
			AllowedSecretPrefix: "plugins/echo/",
		},
	}
	ctx := context.Background()

	// Allowed: within scope
	resp, err := server.GetSecret(ctx, &proto.GetSecretRequest{Name: "plugins/echo/api-key"})
	require.NoError(t, err)
	assert.True(t, resp.Found)
	assert.Equal(t, "secret-val", resp.Value)

	// Denied: outside scope
	resp, err = server.GetSecret(ctx, &proto.GetSecretRequest{Name: "plugins/other/api-key"})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "access denied")

	// Denied: global secret
	resp, err = server.GetSecret(ctx, &proto.GetSecretRequest{Name: "global-secret"})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "access denied")

	// List should filter
	listResp, err := server.ListSecrets(ctx, &proto.ListSecretsRequest{})
	require.NoError(t, err)
	assert.Equal(t, []string{"plugins/echo/api-key"}, listResp.Names)

	// Set denied outside scope
	setResp, err := server.SetSecret(ctx, &proto.SetSecretRequest{Name: "global-secret", Value: "hacked"})
	require.NoError(t, err)
	assert.Contains(t, setResp.Error, "access denied")

	// Delete denied outside scope
	delResp, err := server.DeleteSecret(ctx, &proto.DeleteSecretRequest{Name: "global-secret"})
	require.NoError(t, err)
	assert.Contains(t, delResp.Error, "access denied")
}

func TestHostServiceServer_AuthHandlerRestriction(t *testing.T) {
	called := false
	server := &HostServiceServer{
		Deps: HostServiceDeps{
			AllowedAuthHandlers: []string{"azure", "github"},
			AuthIdentityFunc: func(_ context.Context, _, _ string) (*proto.Claims, error) {
				called = true
				return &proto.Claims{Subject: "user@test"}, nil
			},
		},
	}
	ctx := context.Background()

	// Allowed handler
	resp, err := server.GetAuthIdentity(ctx, &proto.GetAuthIdentityRequest{HandlerName: "azure", Scope: "read"})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.True(t, called)

	// Denied handler
	called = false
	resp, err = server.GetAuthIdentity(ctx, &proto.GetAuthIdentityRequest{HandlerName: "aws", Scope: "admin"})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "access denied")
	assert.False(t, called)

	// Empty handler is denied when an allowlist is configured (prevents
	// bypassing the restriction via the default handler).
	called = false
	resp, err = server.GetAuthIdentity(ctx, &proto.GetAuthIdentityRequest{HandlerName: "", Scope: "read"})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "access denied")
	assert.False(t, called)
}

func TestHostServiceServer_SecretNameValidation(t *testing.T) {
	store := &fakeSecretStore{secrets: map[string][]byte{}}
	server := &HostServiceServer{
		Deps: HostServiceDeps{SecretStore: store},
	}
	ctx := context.Background()

	// Path traversal rejected
	resp, err := server.GetSecret(ctx, &proto.GetSecretRequest{Name: "../etc/passwd"})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "path traversal")

	// Empty name rejected
	resp, err = server.GetSecret(ctx, &proto.GetSecretRequest{Name: ""})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "must not be empty")

	// Shell metacharacters rejected
	setResp, err := server.SetSecret(ctx, &proto.SetSecretRequest{Name: "key;ls", Value: "val"})
	require.NoError(t, err)
	assert.Contains(t, setResp.Error, "invalid characters")
}

func TestHostServiceServer_GetSecret_NotFound(t *testing.T) {
	store := &fakeSecretStore{secrets: map[string][]byte{
		"exists": []byte("val"),
	}}
	server := &HostServiceServer{
		Deps: HostServiceDeps{SecretStore: store},
	}
	ctx := context.Background()

	// Existing secret returns Found: true
	resp, err := server.GetSecret(ctx, &proto.GetSecretRequest{Name: "exists"})
	require.NoError(t, err)
	assert.True(t, resp.Found)
	assert.Empty(t, resp.Error)

	// Missing secret returns Found: false with no error
	resp, err = server.GetSecret(ctx, &proto.GetSecretRequest{Name: "missing"})
	require.NoError(t, err)
	assert.False(t, resp.Found)
	assert.Empty(t, resp.Error)
}

// fakeSecretStore is a minimal in-memory secret store for testing.
type fakeSecretStore struct {
	secrets map[string][]byte
}

func (f *fakeSecretStore) Get(_ context.Context, name string) ([]byte, error) {
	v, ok := f.secrets[name]
	if !ok {
		return nil, fmt.Errorf("%s: %w", name, secrets.ErrNotFound)
	}
	return v, nil
}

func (f *fakeSecretStore) Set(_ context.Context, name string, value []byte) error {
	f.secrets[name] = value
	return nil
}

func (f *fakeSecretStore) Delete(_ context.Context, name string) error {
	delete(f.secrets, name)
	return nil
}

func (f *fakeSecretStore) List(_ context.Context) ([]string, error) {
	names := make([]string, 0, len(f.secrets))
	for k := range f.secrets {
		names = append(names, k)
	}
	return names, nil
}

func (f *fakeSecretStore) Exists(_ context.Context, name string) (bool, error) {
	_, ok := f.secrets[name]
	return ok, nil
}

func (f *fakeSecretStore) Rotate(_ context.Context) error { return nil }

func (f *fakeSecretStore) KeyringBackend() string { return "fake" }

// --- Benchmarks ---

func BenchmarkDescriptorToProto_Lossless(b *testing.B) {
	desc := &provider.Descriptor{
		Name:        "bench-provider",
		DisplayName: "Bench Provider",
		Description: "A benchmark provider",
		APIVersion:  "v1",
		Version:     semver.MustParse("1.0.0"),
		Category:    "test",
		Capabilities: []provider.Capability{
			provider.CapabilityFrom,
			provider.CapabilityTransform,
		},
		Schema: schemahelper.ObjectSchema([]string{"expression", "name"}, map[string]*jsonschema.Schema{
			"expression": schemahelper.StringProp("CEL expression"),
			"name":       schemahelper.StringProp("Name field"),
		}),
	}

	b.ResetTimer()
	for b.Loop() {
		descriptorToProto(desc)
	}
}

func BenchmarkProtoToDescriptor_Lossless(b *testing.B) {
	desc := &provider.Descriptor{
		Name:        "bench-provider",
		DisplayName: "Bench Provider",
		Description: "A benchmark provider",
		APIVersion:  "v1",
		Version:     semver.MustParse("1.0.0"),
		Category:    "test",
		Capabilities: []provider.Capability{
			provider.CapabilityFrom,
		},
		Schema: schemahelper.ObjectSchema([]string{"expression"}, map[string]*jsonschema.Schema{
			"expression": schemahelper.StringProp("CEL expression"),
		}),
	}
	protoDesc := descriptorToProto(desc)

	b.ResetTimer()
	for b.Loop() {
		_, _ = protoToDescriptor(protoDesc)
	}
}

func BenchmarkBuildExecuteProviderRequest(b *testing.B) {
	ctx := context.Background()
	ctx = provider.WithDryRun(ctx, true)
	ctx = provider.WithExecutionMode(ctx, provider.CapabilityAction)
	ctx = provider.WithWorkingDirectory(ctx, "/work")
	ctx = provider.WithOutputDirectory(ctx, "/out")
	ctx = provider.WithParameters(ctx, map[string]any{"region": "us-east-1"})
	ctx = provider.WithResolverContext(ctx, map[string]any{"r1": map[string]any{"v": 1}})

	inputBytes, _ := json.Marshal(map[string]any{"key": "value"})

	b.ResetTimer()
	for b.Loop() {
		_, _ = buildExecuteProviderRequest(ctx, "bench-provider", inputBytes)
	}
}

func BenchmarkValidateSecretName(b *testing.B) {
	b.Run("valid", func(b *testing.B) {
		for b.Loop() {
			_ = validateSecretName("plugins/echo/api-key")
		}
	})
	b.Run("invalid", func(b *testing.B) {
		for b.Loop() {
			_ = validateSecretName("../etc/passwd")
		}
	})
}

func TestExitCode_RoundTrip(t *testing.T) {
	tests := []struct {
		name         string
		execErr      error
		wantExitCode int32
		wantErrMsg   string
	}{
		{
			name:         "ExitError with ActionFailed code",
			execErr:      exitcode.WithCode(fmt.Errorf("deploy failed"), exitcode.ActionFailed),
			wantExitCode: int32(exitcode.ActionFailed),
			wantErrMsg:   "deploy failed",
		},
		{
			name:         "ExitError with ValidationFailed code",
			execErr:      exitcode.WithCode(fmt.Errorf("bad input"), exitcode.ValidationFailed),
			wantExitCode: int32(exitcode.ValidationFailed),
			wantErrMsg:   "bad input",
		},
		{
			name:         "plain error gets GeneralError code",
			execErr:      fmt.Errorf("something went wrong"),
			wantExitCode: int32(exitcode.GeneralError),
			wantErrMsg:   "something went wrong",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockProviderPlugin{
				execFunc: func(_ context.Context, _ string, _ map[string]any) (*provider.Output, error) {
					return nil, tt.execErr
				},
			}
			server := &GRPCServer{Impl: mock}

			inputBytes, err := json.Marshal(map[string]any{"key": "value"})
			require.NoError(t, err)

			resp, err := server.ExecuteProvider(context.Background(), &proto.ExecuteProviderRequest{
				ProviderName: "test-provider",
				Input:        inputBytes,
			})
			require.NoError(t, err, "gRPC-level error should be nil")
			assert.NotEmpty(t, resp.Error)
			assert.Equal(t, tt.wantExitCode, resp.ExitCode)
			assert.Contains(t, resp.Error, tt.wantErrMsg)
			assert.NotEmpty(t, resp.Diagnostics, "diagnostics should be populated")
			assert.Equal(t, proto.Diagnostic_ERROR, resp.Diagnostics[0].Severity)
		})
	}
}

func TestExitCode_ClientReconstruction(t *testing.T) {
	// Test that the client reconstructs ExitError from the response
	mock := &MockProviderPlugin{
		execFunc: func(_ context.Context, _ string, _ map[string]any) (*provider.Output, error) {
			return nil, exitcode.WithCode(fmt.Errorf("action failed"), exitcode.ActionFailed)
		},
	}
	server := &GRPCServer{Impl: mock}

	inputBytes, err := json.Marshal(map[string]any{"key": "value"})
	require.NoError(t, err)

	resp, err := server.ExecuteProvider(context.Background(), &proto.ExecuteProviderRequest{
		ProviderName: "test-provider",
		Input:        inputBytes,
	})
	require.NoError(t, err)

	// Simulate client-side reconstruction
	assert.NotEmpty(t, resp.Error)
	assert.Equal(t, int32(exitcode.ActionFailed), resp.ExitCode)

	// Verify the client would reconstruct an ExitError
	clientErr := fmt.Errorf("provider execution failed: %s", resp.Error)
	if resp.ExitCode != 0 {
		clientErr = exitcode.WithCode(clientErr, int(resp.ExitCode))
	}
	assert.Equal(t, exitcode.ActionFailed, exitcode.GetCode(clientErr))
}

func TestDiagnostics_ErrorConversion(t *testing.T) {
	tests := []struct {
		name  string
		diags []*proto.Diagnostic
		want  string
	}{
		{
			name:  "nil diagnostics",
			diags: nil,
			want:  "",
		},
		{
			name: "error with detail",
			diags: []*proto.Diagnostic{
				{
					Severity: proto.Diagnostic_ERROR,
					Summary:  "validation failed",
					Detail:   "field 'url' is required",
				},
			},
			want: "validation failed: field 'url' is required",
		},
		{
			name: "error with attribute and detail",
			diags: []*proto.Diagnostic{
				{
					Severity:  proto.Diagnostic_ERROR,
					Summary:   "invalid value",
					Detail:    "must be a valid URL",
					Attribute: "config.url",
				},
			},
			want: "invalid value: must be a valid URL (attribute: config.url)",
		},
		{
			name: "error with attribute but no detail",
			diags: []*proto.Diagnostic{
				{
					Severity:  proto.Diagnostic_ERROR,
					Summary:   "missing field",
					Attribute: "config.url",
				},
			},
			want: "missing field (attribute: config.url)",
		},
		{
			name: "warnings only produce no error",
			diags: []*proto.Diagnostic{
				{
					Severity: proto.Diagnostic_WARNING,
					Summary:  "deprecated field",
				},
			},
			want: "",
		},
		{
			name: "multiple errors joined",
			diags: []*proto.Diagnostic{
				{
					Severity: proto.Diagnostic_ERROR,
					Summary:  "error one",
				},
				{
					Severity: proto.Diagnostic_ERROR,
					Summary:  "error two",
				},
			},
			want: "error one\nerror two",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := diagnosticsToError(context.Background(), tt.diags)
			if tt.want == "" {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Equal(t, tt.want, err.Error())
			}
		})
	}
}

func TestDiagnostics_ErrorToDiagnostics(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		assert.Nil(t, errorToDiagnostics(nil))
	})

	t.Run("plain error", func(t *testing.T) {
		diags := errorToDiagnostics(fmt.Errorf("something failed"))
		require.Len(t, diags, 1)
		assert.Equal(t, proto.Diagnostic_ERROR, diags[0].Severity)
		assert.Equal(t, "something failed", diags[0].Summary)
		assert.Empty(t, diags[0].Detail)
	})

	t.Run("ExitError includes code description", func(t *testing.T) {
		diags := errorToDiagnostics(exitcode.WithCode(fmt.Errorf("timeout"), exitcode.TimeoutError))
		require.Len(t, diags, 1)
		assert.Equal(t, proto.Diagnostic_ERROR, diags[0].Severity)
		assert.Contains(t, diags[0].Detail, "exit code 9")
		assert.Contains(t, diags[0].Detail, "timeout")
	})
}

func TestListAuthHandlers_Filtered(t *testing.T) {
	tests := []struct {
		name            string
		allHandlers     []string
		defaultHandler  string
		allowedHandlers []string
		wantHandlers    []string
		wantDefault     string
	}{
		{
			name:            "no restrictions returns all",
			allHandlers:     []string{"entra", "gcp", "github"},
			defaultHandler:  "entra",
			allowedHandlers: nil,
			wantHandlers:    []string{"entra", "gcp", "github"},
			wantDefault:     "entra",
		},
		{
			name:            "filters to allowed set",
			allHandlers:     []string{"entra", "gcp", "github"},
			defaultHandler:  "entra",
			allowedHandlers: []string{"entra", "github"},
			wantHandlers:    []string{"entra", "github"},
			wantDefault:     "entra",
		},
		{
			name:            "redacts default when not allowed",
			allHandlers:     []string{"entra", "gcp", "github"},
			defaultHandler:  "gcp",
			allowedHandlers: []string{"entra"},
			wantHandlers:    []string{"entra"},
			wantDefault:     "",
		},
		{
			name:            "empty allowed with restrictions returns none",
			allHandlers:     []string{"entra", "gcp"},
			defaultHandler:  "entra",
			allowedHandlers: []string{"nonexistent"},
			wantHandlers:    []string{},
			wantDefault:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := &HostServiceServer{
				Deps: HostServiceDeps{
					AllowedAuthHandlers: tt.allowedHandlers,
					AuthHandlersFunc: func(_ context.Context) ([]string, string, error) {
						return tt.allHandlers, tt.defaultHandler, nil
					},
				},
			}

			resp, err := server.ListAuthHandlers(context.Background(), &proto.ListAuthHandlersRequest{})
			require.NoError(t, err)

			if len(tt.wantHandlers) == 0 {
				assert.Empty(t, resp.HandlerNames)
			} else {
				assert.Equal(t, tt.wantHandlers, resp.HandlerNames)
			}
			assert.Equal(t, tt.wantDefault, resp.DefaultHandler)
		})
	}
}

func TestDescriptorCache(t *testing.T) {
	callCount := 0
	mock := &MockProviderPlugin{
		descriptors: map[string]*provider.Descriptor{
			"cached-provider": {
				Name:        "cached-provider",
				DisplayName: "Cached",
				Description: "Test descriptor caching",
				APIVersion:  "v1",
			},
		},
	}
	// Wrap mock to count calls
	originalGetDesc := mock.GetProviderDescriptor
	countingPlugin := &countingDescriptorPlugin{
		ProviderPlugin: mock,
		getDescFn:      originalGetDesc,
		callCount:      &callCount,
	}

	client := &Client{
		plugin: countingPlugin,
		name:   "test",
	}

	// First call should hit the plugin
	desc1, err := client.GetProviderDescriptor(context.Background(), "cached-provider")
	require.NoError(t, err)
	assert.Equal(t, "cached-provider", desc1.Name)
	assert.Equal(t, 1, callCount)

	// Second call should return cached result
	desc2, err := client.GetProviderDescriptor(context.Background(), "cached-provider")
	require.NoError(t, err)
	assert.Equal(t, "cached-provider", desc2.Name)
	assert.Equal(t, 1, callCount, "second call should use cache")

	// Different provider should still call plugin
	_, _ = client.GetProviderDescriptor(context.Background(), "other-provider")
	assert.Equal(t, 2, callCount, "different provider should hit plugin")
}

// countingDescriptorPlugin wraps ProviderPlugin to count GetProviderDescriptor calls.
type countingDescriptorPlugin struct {
	ProviderPlugin
	getDescFn func(ctx context.Context, name string) (*provider.Descriptor, error)
	callCount *int
}

func (c *countingDescriptorPlugin) GetProviderDescriptor(ctx context.Context, name string) (*provider.Descriptor, error) {
	*c.callCount++
	return c.getDescFn(ctx, name)
}
