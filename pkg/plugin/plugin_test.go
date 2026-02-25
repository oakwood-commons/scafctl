// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/plugin/proto"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockProviderPlugin implements ProviderPlugin for testing
type MockProviderPlugin struct {
	providers   []string
	descriptors map[string]*provider.Descriptor
	execFunc    func(ctx context.Context, name string, input map[string]any) (*provider.Output, error)
}

func (m *MockProviderPlugin) GetProviders(ctx context.Context) ([]string, error) {
	if m.providers == nil {
		return []string{"test-provider"}, nil
	}
	return m.providers, nil
}

func (m *MockProviderPlugin) GetProviderDescriptor(ctx context.Context, providerName string) (*provider.Descriptor, error) {
	if m.descriptors != nil {
		if desc, ok := m.descriptors[providerName]; ok {
			return desc, nil
		}
	}

	if providerName == "test-provider" {
		return &provider.Descriptor{
			Name:        "test-provider",
			DisplayName: "Test Provider",
			Description: "A test provider",
			APIVersion:  "v1",
			Version:     semver.MustParse("1.0.0"),
			Category:    "test",
			Schema: schemahelper.ObjectSchema([]string{"input"}, map[string]*jsonschema.Schema{
				"input": schemahelper.StringProp("Test input"),
			}),
		}, nil
	}
	return nil, fmt.Errorf("unknown provider: %s", providerName)
}

func (m *MockProviderPlugin) ExecuteProvider(ctx context.Context, providerName string, input map[string]any) (*provider.Output, error) {
	if m.execFunc != nil {
		return m.execFunc(ctx, providerName, input)
	}

	return &provider.Output{
		Data: map[string]any{
			"result": input,
		},
	}, nil
}

func TestGRPCPlugin_ServerClient(t *testing.T) {
	mock := &MockProviderPlugin{}

	// Test GetProviders
	providers, err := mock.GetProviders(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"test-provider"}, providers)

	// Test GetProviderDescriptor
	desc, err := mock.GetProviderDescriptor(context.Background(), "test-provider")
	require.NoError(t, err)
	assert.Equal(t, "test-provider", desc.Name)
	assert.Equal(t, "Test Provider", desc.DisplayName)

	// Test ExecuteProvider
	output, err := mock.ExecuteProvider(context.Background(), "test-provider", map[string]any{
		"input": "test",
	})
	require.NoError(t, err)
	assert.NotNil(t, output)
	assert.NotNil(t, output.Data)
}

func TestDescriptorConversion(t *testing.T) {
	maxLen := 100
	tests := []struct {
		name       string
		descriptor *provider.Descriptor
	}{
		{
			name: "basic descriptor",
			descriptor: &provider.Descriptor{
				Name:        "test",
				DisplayName: "Test Provider",
				Description: "Test description",
				APIVersion:  "v1",
				Version:     semver.MustParse("1.0.0"),
				Category:    "test",
				Capabilities: []provider.Capability{
					provider.CapabilityTransform,
					provider.CapabilityValidation,
				},
			},
		},
		{
			name: "descriptor with schema",
			descriptor: &provider.Descriptor{
				Name:       "test",
				APIVersion: "v1",
				Version:    semver.MustParse("1.0.0"),
				Schema: schemahelper.ObjectSchema([]string{"param1"}, map[string]*jsonschema.Schema{
					"param1": schemahelper.StringProp("Parameter 1",
						schemahelper.WithExample("example"),
						schemahelper.WithMaxLength(maxLen)),
					"param2": schemahelper.IntProp("Parameter 2",
						schemahelper.WithDefault(42)),
				}),
			},
		},
		{
			name: "descriptor with output schemas",
			descriptor: &provider.Descriptor{
				Name:         "test",
				APIVersion:   "v1",
				Version:      semver.MustParse("1.0.0"),
				Capabilities: []provider.Capability{provider.CapabilityFrom, provider.CapabilityAction},
				OutputSchemas: map[provider.Capability]*jsonschema.Schema{
					provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
						"result": schemahelper.StringProp("Result"),
					}),
					provider.CapabilityAction: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
						"success": schemahelper.BoolProp("Whether action succeeded"),
					}),
				},
			},
		},
		{
			name: "descriptor with all metadata fields",
			descriptor: &provider.Descriptor{
				Name:            "full-provider",
				DisplayName:     "Full Provider",
				APIVersion:      "v1",
				Version:         semver.MustParse("2.3.4"),
				Description:     "A fully-populated provider descriptor",
				Category:        "network",
				MockBehavior:    "Returns mock data without making real requests",
				SensitiveFields: []string{"apiKey", "token"},
				Tags:            []string{"http", "api", "network"},
				Icon:            "https://example.com/icon.png",
				Deprecated:      true,
				Beta:            false,
				Capabilities:    []provider.Capability{provider.CapabilityFrom, provider.CapabilityTransform},
				Links: []provider.Link{
					{Name: "Docs", URL: "https://example.com/docs"},
					{Name: "Source", URL: "https://github.com/example/provider"},
				},
				Examples: []provider.Example{
					{
						Name:        "Basic usage",
						Description: "Fetch data from an API",
						YAML:        "provider: full-provider\ninputs:\n  url: https://api.example.com",
					},
				},
				Maintainers: []provider.Contact{
					{Name: "Jane Doe", Email: "jane@example.com"},
					{Name: "Team", Email: "team@example.com"},
				},
				Schema: schemahelper.ObjectSchema([]string{"url"}, map[string]*jsonschema.Schema{
					"url": schemahelper.StringProp("URL to fetch"),
				}),
				OutputSchemas: map[provider.Capability]*jsonschema.Schema{
					provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
						"data": schemahelper.StringProp("Response data"),
					}),
					provider.CapabilityTransform: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
						"result": schemahelper.StringProp("Transformed result"),
					}),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert to proto
			protoDesc := descriptorToProto(tt.descriptor)
			require.NotNil(t, protoDesc)

			// Convert back
			converted := protoToDescriptor(protoDesc)
			require.NotNil(t, converted)

			// Compare
			assert.Equal(t, tt.descriptor.Name, converted.Name)
			assert.Equal(t, tt.descriptor.DisplayName, converted.DisplayName)
			assert.Equal(t, tt.descriptor.Description, converted.Description)
			if tt.descriptor.Version != nil && converted.Version != nil {
				assert.Equal(t, tt.descriptor.Version.String(), converted.Version.String())
			}
			assert.Equal(t, tt.descriptor.Category, converted.Category)
			assert.Equal(t, len(tt.descriptor.Capabilities), len(converted.Capabilities))

			// Compare new metadata fields
			assert.Equal(t, tt.descriptor.APIVersion, converted.APIVersion)
			assert.Equal(t, tt.descriptor.MockBehavior, converted.MockBehavior)
			assert.Equal(t, tt.descriptor.SensitiveFields, converted.SensitiveFields)
			assert.Equal(t, tt.descriptor.Tags, converted.Tags)
			assert.Equal(t, tt.descriptor.Icon, converted.Icon)
			assert.Equal(t, tt.descriptor.Deprecated, converted.Deprecated) //nolint:staticcheck // field is intentionally tested for gRPC roundtrip
			assert.Equal(t, tt.descriptor.Beta, converted.Beta)
			assert.Equal(t, len(tt.descriptor.Links), len(converted.Links))
			for i, link := range tt.descriptor.Links {
				assert.Equal(t, link.Name, converted.Links[i].Name)
				assert.Equal(t, link.URL, converted.Links[i].URL)
			}
			assert.Equal(t, len(tt.descriptor.Examples), len(converted.Examples))
			for i, ex := range tt.descriptor.Examples {
				assert.Equal(t, ex.Name, converted.Examples[i].Name)
				assert.Equal(t, ex.Description, converted.Examples[i].Description)
				assert.Equal(t, ex.YAML, converted.Examples[i].YAML)
			}
			assert.Equal(t, len(tt.descriptor.Maintainers), len(converted.Maintainers))
			for i, m := range tt.descriptor.Maintainers {
				assert.Equal(t, m.Name, converted.Maintainers[i].Name)
				assert.Equal(t, m.Email, converted.Maintainers[i].Email)
			}

			// Compare schema
			if tt.descriptor.Schema != nil && len(tt.descriptor.Schema.Properties) > 0 {
				require.NotNil(t, converted.Schema)
				assert.Equal(t, len(tt.descriptor.Schema.Properties), len(converted.Schema.Properties))
				for name, prop := range tt.descriptor.Schema.Properties {
					convertedProp, ok := converted.Schema.Properties[name]
					require.True(t, ok, "property %s not found", name)
					assert.Equal(t, prop.Type, convertedProp.Type)
					assert.Equal(t, prop.Description, convertedProp.Description)
				}
			}

			// Compare output schemas
			if len(tt.descriptor.OutputSchemas) > 0 {
				assert.Equal(t, len(tt.descriptor.OutputSchemas), len(converted.OutputSchemas))
				for cap, schema := range tt.descriptor.OutputSchemas {
					convertedSchema, ok := converted.OutputSchemas[cap]
					require.True(t, ok, "output schema for capability %s not found", cap)
					if schema != nil && convertedSchema != nil {
						assert.Equal(t, len(schema.Properties), len(convertedSchema.Properties))
					}
				}
			}
		})
	}
}

func TestProviderWrapper(t *testing.T) {
	// Create a mock client with mock plugin
	mock := &MockProviderPlugin{
		providers: []string{"test-provider"},
		descriptors: map[string]*provider.Descriptor{
			"test-provider": {
				Name:        "test-provider",
				DisplayName: "Test Provider",
				APIVersion:  "v1",
				Version:     semver.MustParse("1.0.0"),
			},
		},
		execFunc: func(ctx context.Context, name string, input map[string]any) (*provider.Output, error) {
			return &provider.Output{
				Data: map[string]any{
					"input": input,
				},
			}, nil
		},
	}

	// Create a fake client (we can't easily test the real client without a real plugin process)
	// Instead we'll test the wrapper logic directly
	desc, err := mock.GetProviderDescriptor(context.Background(), "test-provider")
	require.NoError(t, err)

	wrapper := &ProviderWrapper{
		providerName: "test-provider",
		descriptor:   desc,
	}

	// Test Descriptor
	gotDesc := wrapper.Descriptor()
	assert.Equal(t, "test-provider", gotDesc.Name)
	assert.Equal(t, "Test Provider", gotDesc.DisplayName)
}

func TestDiscover(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Create some test files
	execFile := filepath.Join(tmpDir, "plugin-exec")
	nonExecFile := filepath.Join(tmpDir, "plugin-nonexec")
	dir := filepath.Join(tmpDir, "subdir")

	// Create executable file
	err := os.WriteFile(execFile, []byte("#!/bin/sh\necho test"), 0o755)
	require.NoError(t, err)

	// Create non-executable file
	err = os.WriteFile(nonExecFile, []byte("not executable"), 0o644)
	require.NoError(t, err)

	// Create directory
	err = os.Mkdir(dir, 0o755)
	require.NoError(t, err)

	// Test discovery with non-existent directory
	clients, err := Discover([]string{filepath.Join(tmpDir, "nonexistent")})
	require.NoError(t, err)
	assert.Empty(t, clients)

	// Test discovery with real directory (will fail to connect but should not error)
	// The Discover function skips plugins that fail to load
	clients, err = Discover([]string{tmpDir})
	require.NoError(t, err)
	// No real plugins, so should be empty
	assert.Empty(t, clients)
}

func TestHandshakeConfig(t *testing.T) {
	assert.NotNil(t, HandshakeConfig)
	assert.Equal(t, uint(1), HandshakeConfig.ProtocolVersion)
	assert.Equal(t, "SCAFCTL_PLUGIN", HandshakeConfig.MagicCookieKey)
	assert.Equal(t, "scafctl_provider_plugin", HandshakeConfig.MagicCookieValue)
}

func TestGRPCServer_GetProviders(t *testing.T) {
	mock := &MockProviderPlugin{
		providers: []string{"provider1", "provider2"},
	}

	server := &GRPCServer{Impl: mock}

	resp, err := server.GetProviders(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"provider1", "provider2"}, resp.ProviderNames)
}

func TestGRPCServer_GetProviderDescriptor(t *testing.T) {
	mock := &MockProviderPlugin{
		descriptors: map[string]*provider.Descriptor{
			"test": {
				Name:        "test",
				DisplayName: "Test",
				APIVersion:  "v1",
				Version:     semver.MustParse("1.0.0"),
			},
		},
	}

	server := &GRPCServer{Impl: mock}

	resp, err := server.GetProviderDescriptor(context.Background(), &proto.GetProviderDescriptorRequest{
		ProviderName: "test",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "test", resp.GetDescriptor_().Name)
}

func TestGRPCServer_ExecuteProvider(t *testing.T) {
	tests := []struct {
		name        string
		input       map[string]any
		execFunc    func(ctx context.Context, name string, input map[string]any) (*provider.Output, error)
		expectError bool
	}{
		{
			name: "success",
			input: map[string]any{
				"key": "value",
			},
			execFunc: func(ctx context.Context, name string, input map[string]any) (*provider.Output, error) {
				return &provider.Output{
					Data: input,
				}, nil
			},
			expectError: false,
		},
		{
			name: "error",
			input: map[string]any{
				"key": "value",
			},
			execFunc: func(ctx context.Context, name string, input map[string]any) (*provider.Output, error) {
				return nil, fmt.Errorf("execution failed")
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockProviderPlugin{
				execFunc: tt.execFunc,
			}

			server := &GRPCServer{Impl: mock}

			inputBytes, err := json.Marshal(tt.input)
			require.NoError(t, err)

			resp, err := server.ExecuteProvider(context.Background(), &proto.ExecuteProviderRequest{
				ProviderName: "test",
				Input:        inputBytes,
			})
			require.NoError(t, err) // gRPC call should succeed

			if tt.expectError {
				assert.NotEmpty(t, resp.Error)
			} else {
				assert.Empty(t, resp.Error)
				assert.NotEmpty(t, resp.Output)
			}
		})
	}
}
