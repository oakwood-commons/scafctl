// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"bytes"
	"context"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider implements provider.Provider for testing
type mockProvider struct {
	descriptor *provider.Descriptor
}

func (m *mockProvider) Descriptor() *provider.Descriptor {
	return m.descriptor
}

func (m *mockProvider) Execute(_ context.Context, _ any) (*provider.Output, error) {
	return &provider.Output{Data: map[string]any{"result": "mock"}}, nil
}

// newMockProvider creates a mock provider with minimal required fields
func newMockProvider(name, description string, caps []provider.Capability) *mockProvider {
	return &mockProvider{
		descriptor: &provider.Descriptor{
			Name:         name,
			DisplayName:  name,
			Description:  description,
			APIVersion:   "v1",
			Version:      semver.MustParse("1.0.0"),
			Capabilities: caps,
			MockBehavior: "Returns mock data for testing",
			Schema: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"input": schemahelper.StringProp("Test input"),
			}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				caps[0]: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"output": schemahelper.StringProp("Test output"),
				}),
			},
		},
	}
}

// newMockProviderFull creates a mock provider with all fields populated
func newMockProviderFull() *mockProvider {
	return &mockProvider{
		descriptor: &provider.Descriptor{
			Name:         "full-provider",
			DisplayName:  "Full Provider",
			Description:  "A provider with all fields populated",
			APIVersion:   "v1",
			Version:      semver.MustParse("2.1.0"),
			Capabilities: []provider.Capability{provider.CapabilityFrom, provider.CapabilityTransform},
			Category:     "network",
			Tags:         []string{"http", "api", "rest"},
			Icon:         "🌐",
			MockBehavior: "Returns mock network data",
			Beta:         true,
			Schema: schemahelper.ObjectSchema([]string{"url"}, map[string]*jsonschema.Schema{
				"url": schemahelper.StringProp("The URL to fetch",
					schemahelper.WithExample("https://api.example.com")),
				"timeout": schemahelper.IntProp("Timeout in seconds",
					schemahelper.WithDefault(30)),
			}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"body": schemahelper.StringProp("Response body"),
				}),
				provider.CapabilityTransform: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"data": schemahelper.AnyProp("Transformed data"),
				}),
			},
			Links: []provider.Link{
				{Name: "Documentation", URL: "https://docs.example.com"},
			},
			Examples: []provider.Example{
				{
					Name:        "Basic GET",
					Description: "Simple HTTP GET request",
					YAML:        "provider: full-provider\nurl: https://api.example.com",
				},
			},
			Maintainers: []provider.Contact{
				{Name: "Test User", Email: "test@example.com"},
			},
		},
	}
}

func TestCommandProvider(t *testing.T) {
	tests := []struct {
		name     string
		validate func(t *testing.T)
	}{
		{
			name: "creates_provider_command_with_correct_usage",
			validate: func(t *testing.T) {
				ioStreams, _, _ := terminal.NewTestIOStreams()
				cliParams := &settings.Run{}
				cmd := CommandProvider(cliParams, ioStreams, "get")

				assert.Equal(t, "provider [name]", cmd.Use)
				assert.Contains(t, cmd.Aliases, "providers")
				assert.Contains(t, cmd.Aliases, "prov")
				assert.Contains(t, cmd.Aliases, "p")
				assert.Contains(t, cmd.Short, "List or get provider information")
			},
		},
		{
			name: "has_output_flag",
			validate: func(t *testing.T) {
				ioStreams, _, _ := terminal.NewTestIOStreams()
				cliParams := &settings.Run{}
				cmd := CommandProvider(cliParams, ioStreams, "get")

				flag := cmd.Flags().Lookup("output")
				assert.NotNil(t, flag)
				assert.Equal(t, "o", flag.Shorthand)
			},
		},
		{
			name: "has_capability_filter_flag",
			validate: func(t *testing.T) {
				ioStreams, _, _ := terminal.NewTestIOStreams()
				cliParams := &settings.Run{}
				cmd := CommandProvider(cliParams, ioStreams, "get")

				flag := cmd.Flags().Lookup("capability")
				assert.NotNil(t, flag)
			},
		},
		{
			name: "has_category_filter_flag",
			validate: func(t *testing.T) {
				ioStreams, _, _ := terminal.NewTestIOStreams()
				cliParams := &settings.Run{}
				cmd := CommandProvider(cliParams, ioStreams, "get")

				flag := cmd.Flags().Lookup("category")
				assert.NotNil(t, flag)
			},
		},
		{
			name: "has_interactive_flag",
			validate: func(t *testing.T) {
				ioStreams, _, _ := terminal.NewTestIOStreams()
				cliParams := &settings.Run{}
				cmd := CommandProvider(cliParams, ioStreams, "get")

				flag := cmd.Flags().Lookup("interactive")
				assert.NotNil(t, flag)
				assert.Equal(t, "i", flag.Shorthand)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, tc.validate)
	}
}

func TestOptions_RunListProviders(t *testing.T) {
	tests := []struct {
		name        string
		setupReg    func() *provider.Registry
		options     *Options
		wantErr     bool
		checkOutput func(t *testing.T, output string)
	}{
		{
			name: "lists_all_providers",
			setupReg: func() *provider.Registry {
				reg := provider.NewRegistry(provider.WithAllowOverwrite(true))
				mp1 := newMockProvider("test-provider-1", "First test provider", []provider.Capability{provider.CapabilityFrom})
				mp2 := newMockProvider("test-provider-2", "Second test provider", []provider.Capability{provider.CapabilityTransform})
				_ = reg.Register(mp1)
				_ = reg.Register(mp2)
				return reg
			},
			options: &Options{
				KvxOutputFlags: flags.KvxOutputFlags{Output: "quiet"},
			},
			wantErr: false,
			checkOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "test-provider-1")
				assert.Contains(t, output, "test-provider-2")
			},
		},
		{
			name: "filters_by_capability",
			setupReg: func() *provider.Registry {
				reg := provider.NewRegistry(provider.WithAllowOverwrite(true))
				mp1 := newMockProvider("from-provider", "Provider with from capability", []provider.Capability{provider.CapabilityFrom})
				mp2 := newMockProvider("transform-provider", "Provider with transform capability", []provider.Capability{provider.CapabilityTransform})
				_ = reg.Register(mp1)
				_ = reg.Register(mp2)
				return reg
			},
			options: &Options{
				Capability:     "from",
				KvxOutputFlags: flags.KvxOutputFlags{Output: "quiet"},
			},
			wantErr: false,
			checkOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "from-provider")
				assert.NotContains(t, output, "transform-provider")
			},
		},
		{
			name: "filters_by_category",
			setupReg: func() *provider.Registry {
				reg := provider.NewRegistry(provider.WithAllowOverwrite(true))
				mp1 := newMockProvider("network-provider", "Network provider", []provider.Capability{provider.CapabilityFrom})
				mp1.descriptor.Category = "network"
				mp2 := newMockProvider("storage-provider", "Storage provider", []provider.Capability{provider.CapabilityFrom})
				mp2.descriptor.Category = "storage"
				_ = reg.Register(mp1)
				_ = reg.Register(mp2)
				return reg
			},
			options: &Options{
				Category:       "network",
				KvxOutputFlags: flags.KvxOutputFlags{Output: "quiet"},
			},
			wantErr: false,
			checkOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "network-provider")
				assert.NotContains(t, output, "storage-provider")
			},
		},
		{
			name: "handles_empty_registry",
			setupReg: func() *provider.Registry {
				return provider.NewRegistry(provider.WithAllowOverwrite(true))
			},
			options: &Options{
				KvxOutputFlags: flags.KvxOutputFlags{Output: "quiet"},
			},
			wantErr: false,
			checkOutput: func(t *testing.T, output string) {
				assert.Empty(t, output)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var outBuf bytes.Buffer
			ioStreams := &terminal.IOStreams{
				Out:    &outBuf,
				ErrOut: &outBuf,
			}
			tc.options.IOStreams = ioStreams
			tc.options.CliParams = &settings.Run{}
			tc.options.registry = tc.setupReg()

			err := tc.options.RunListProviders(context.Background())

			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tc.checkOutput != nil {
				tc.checkOutput(t, outBuf.String())
			}
		})
	}
}

func TestOptions_RunGetProvider(t *testing.T) {
	tests := []struct {
		name         string
		providerName string
		setupReg     func() *provider.Registry
		options      *Options
		wantErr      bool
		errContains  string
		checkOutput  func(t *testing.T, output string)
	}{
		{
			name:         "gets_existing_provider",
			providerName: "test-provider",
			setupReg: func() *provider.Registry {
				reg := provider.NewRegistry(provider.WithAllowOverwrite(true))
				mp := newMockProvider("test-provider", "Test provider description", []provider.Capability{provider.CapabilityFrom})
				_ = reg.Register(mp)
				return reg
			},
			options: &Options{
				KvxOutputFlags: flags.KvxOutputFlags{Output: "quiet"},
			},
			wantErr: false,
			checkOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "test-provider")
			},
		},
		{
			name:         "returns_error_for_non_existent_provider",
			providerName: "non-existent",
			setupReg: func() *provider.Registry {
				return provider.NewRegistry(provider.WithAllowOverwrite(true))
			},
			options: &Options{
				KvxOutputFlags: flags.KvxOutputFlags{Output: "quiet"},
			},
			wantErr:     true,
			errContains: "not found",
		},
		{
			name:         "gets_provider_with_full_details",
			providerName: "full-provider",
			setupReg: func() *provider.Registry {
				reg := provider.NewRegistry(provider.WithAllowOverwrite(true))
				mp := newMockProviderFull()
				_ = reg.Register(mp)
				return reg
			},
			options: &Options{
				KvxOutputFlags: flags.KvxOutputFlags{Output: "quiet"},
			},
			wantErr: false,
			checkOutput: func(t *testing.T, output string) {
				// In quiet mode with a single provider that has a name, only the name is printed
				assert.Contains(t, output, "full-provider")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var outBuf bytes.Buffer
			ioStreams := &terminal.IOStreams{
				Out:    &outBuf,
				ErrOut: &outBuf,
			}
			tc.options.IOStreams = ioStreams
			tc.options.CliParams = &settings.Run{}
			tc.options.registry = tc.setupReg()

			err := tc.options.RunGetProvider(context.Background(), tc.providerName)

			if tc.wantErr {
				require.Error(t, err)
				if tc.errContains != "" {
					assert.Contains(t, err.Error(), tc.errContains)
				}
			} else {
				require.NoError(t, err)
			}
			if tc.checkOutput != nil {
				tc.checkOutput(t, outBuf.String())
			}
		})
	}
}

func TestOptions_filterProviders(t *testing.T) {
	fromProvider := newMockProvider("from-only", "From capability only", []provider.Capability{provider.CapabilityFrom})
	transformProvider := newMockProvider("transform-only", "Transform capability only", []provider.Capability{provider.CapabilityTransform})
	multiCapProvider := newMockProvider("multi-cap", "Multiple capabilities", []provider.Capability{provider.CapabilityFrom, provider.CapabilityTransform})
	multiCapProvider.descriptor.Category = "network"

	networkProvider := newMockProvider("network-prov", "Network category", []provider.Capability{provider.CapabilityFrom})
	networkProvider.descriptor.Category = "network"

	storageProvider := newMockProvider("storage-prov", "Storage category", []provider.Capability{provider.CapabilityFrom})
	storageProvider.descriptor.Category = "storage"

	tests := []struct {
		name       string
		providers  []provider.Provider
		capability string
		category   string
		wantCount  int
		wantNames  []string
	}{
		{
			name:       "no_filters_returns_all",
			providers:  []provider.Provider{fromProvider, transformProvider},
			capability: "",
			category:   "",
			wantCount:  2,
			wantNames:  []string{"from-only", "transform-only"},
		},
		{
			name:       "filter_by_from_capability",
			providers:  []provider.Provider{fromProvider, transformProvider, multiCapProvider},
			capability: "from",
			category:   "",
			wantCount:  2,
			wantNames:  []string{"from-only", "multi-cap"},
		},
		{
			name:       "filter_by_transform_capability",
			providers:  []provider.Provider{fromProvider, transformProvider, multiCapProvider},
			capability: "transform",
			category:   "",
			wantCount:  2,
			wantNames:  []string{"transform-only", "multi-cap"},
		},
		{
			name:       "filter_by_category",
			providers:  []provider.Provider{networkProvider, storageProvider},
			capability: "",
			category:   "network",
			wantCount:  1,
			wantNames:  []string{"network-prov"},
		},
		{
			name:       "filter_by_both_capability_and_category",
			providers:  []provider.Provider{fromProvider, multiCapProvider, networkProvider},
			capability: "from",
			category:   "network",
			wantCount:  2,
			wantNames:  []string{"multi-cap", "network-prov"},
		},
		{
			name:       "capability_filter_case_insensitive",
			providers:  []provider.Provider{fromProvider, transformProvider},
			capability: "FROM",
			category:   "",
			wantCount:  1,
			wantNames:  []string{"from-only"},
		},
		{
			name:       "category_filter_case_insensitive",
			providers:  []provider.Provider{networkProvider, storageProvider},
			capability: "",
			category:   "NETWORK",
			wantCount:  1,
			wantNames:  []string{"network-prov"},
		},
		{
			name:       "no_matches_returns_empty",
			providers:  []provider.Provider{fromProvider, transformProvider},
			capability: "validation",
			category:   "",
			wantCount:  0,
			wantNames:  []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			options := &Options{
				Capability: tc.capability,
				Category:   tc.category,
			}

			result := options.filterProviders(tc.providers)

			assert.Len(t, result, tc.wantCount)
			for _, wantName := range tc.wantNames {
				found := false
				for _, p := range result {
					if p.Descriptor().Name == wantName {
						found = true
						break
					}
				}
				assert.True(t, found, "expected provider %q in result", wantName)
			}
		})
	}
}

func TestBuildProviderDetail(t *testing.T) {
	tests := []struct {
		name       string
		descriptor provider.Descriptor
		checkKeys  []string
		checkVals  map[string]any
	}{
		{
			name: "includes_basic_fields",
			descriptor: provider.Descriptor{
				Name:         "test",
				DisplayName:  "Test Provider",
				APIVersion:   "v1",
				Version:      semver.MustParse("1.0.0"),
				Description:  "Test description",
				Capabilities: []provider.Capability{provider.CapabilityFrom},
				MockBehavior: "Test mock behavior",
			},
			checkKeys: []string{"name", "displayName", "apiVersion", "version", "description", "capabilities", "mockBehavior"},
			checkVals: map[string]any{
				"name":        "test",
				"displayName": "Test Provider",
				"apiVersion":  "v1",
				"version":     "1.0.0",
			},
		},
		{
			name: "includes_optional_fields_when_present",
			descriptor: provider.Descriptor{
				Name:         "full",
				DisplayName:  "Full Provider",
				APIVersion:   "v1",
				Version:      semver.MustParse("1.0.0"),
				Description:  "Full description",
				Capabilities: []provider.Capability{provider.CapabilityFrom},
				Category:     "network",
				Tags:         []string{"tag1", "tag2"},
				Icon:         "🔧",
				MockBehavior: "Test mock behavior",
				Beta:         true,
			},
			checkKeys: []string{"category", "tags", "icon", "beta"},
			checkVals: map[string]any{
				"category": "network",
				"icon":     "🔧",
				"beta":     true,
			},
		},
		{
			name: "includes_deprecated_when_true",
			descriptor: provider.Descriptor{
				Name:         "deprecated",
				DisplayName:  "Deprecated Provider",
				APIVersion:   "v1",
				Version:      semver.MustParse("1.0.0"),
				Description:  "Deprecated description",
				Capabilities: []provider.Capability{provider.CapabilityFrom},
				MockBehavior: "Test mock behavior",
				Deprecated:   true,
			},
			checkKeys: []string{"deprecated"},
			checkVals: map[string]any{
				"deprecated": true,
			},
		},
		{
			name: "includes_schema_when_present",
			descriptor: provider.Descriptor{
				Name:         "with-schema",
				DisplayName:  "With Schema",
				APIVersion:   "v1",
				Version:      semver.MustParse("1.0.0"),
				Description:  "Has schema",
				Capabilities: []provider.Capability{provider.CapabilityFrom},
				MockBehavior: "Test mock behavior",
				Schema: schemahelper.ObjectSchema([]string{"url"}, map[string]*jsonschema.Schema{
					"url": schemahelper.StringProp("The URL"),
				}),
			},
			checkKeys: []string{"schema"},
		},
		{
			name: "includes_output_schemas_when_present",
			descriptor: provider.Descriptor{
				Name:         "with-output",
				DisplayName:  "With Output",
				APIVersion:   "v1",
				Version:      semver.MustParse("1.0.0"),
				Description:  "Has output schemas",
				Capabilities: []provider.Capability{provider.CapabilityFrom},
				MockBehavior: "Test mock behavior",
				OutputSchemas: map[provider.Capability]*jsonschema.Schema{
					provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
						"result": schemahelper.StringProp(""),
					}),
				},
			},
			checkKeys: []string{"outputSchemas"},
		},
		{
			name: "includes_links_when_present",
			descriptor: provider.Descriptor{
				Name:         "with-links",
				DisplayName:  "With Links",
				APIVersion:   "v1",
				Version:      semver.MustParse("1.0.0"),
				Description:  "Has links",
				Capabilities: []provider.Capability{provider.CapabilityFrom},
				MockBehavior: "Test mock behavior",
				Links: []provider.Link{
					{Name: "Docs", URL: "https://docs.example.com"},
				},
			},
			checkKeys: []string{"links"},
		},
		{
			name: "includes_examples_when_present",
			descriptor: provider.Descriptor{
				Name:         "with-examples",
				DisplayName:  "With Examples",
				APIVersion:   "v1",
				Version:      semver.MustParse("1.0.0"),
				Description:  "Has examples",
				Capabilities: []provider.Capability{provider.CapabilityFrom},
				MockBehavior: "Test mock behavior",
				Examples: []provider.Example{
					{Name: "Basic", Description: "Basic example", YAML: "key: value"},
				},
			},
			checkKeys: []string{"examples"},
		},
		{
			name: "includes_maintainers_when_present",
			descriptor: provider.Descriptor{
				Name:         "with-maintainers",
				DisplayName:  "With Maintainers",
				APIVersion:   "v1",
				Version:      semver.MustParse("1.0.0"),
				Description:  "Has maintainers",
				Capabilities: []provider.Capability{provider.CapabilityFrom},
				MockBehavior: "Test mock behavior",
				Maintainers: []provider.Contact{
					{Name: "John Doe", Email: "john@example.com"},
				},
			},
			checkKeys: []string{"maintainers"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := BuildProviderDetail(tc.descriptor)

			for _, key := range tc.checkKeys {
				assert.Contains(t, result, key, "expected key %q in result", key)
			}
			for key, val := range tc.checkVals {
				assert.Equal(t, val, result[key], "expected %q to be %v", key, val)
			}
		})
	}
}

func TestBuildSchemaOutput(t *testing.T) {
	tests := []struct {
		name   string
		schema *jsonschema.Schema
		want   map[string]any
	}{
		{
			name:   "nil_schema_returns_nil",
			schema: nil,
			want:   nil,
		},
		{
			name: "empty_schema_returns_nil",
			schema: &jsonschema.Schema{
				Properties: map[string]*jsonschema.Schema{},
			},
			want: nil,
		},
		{
			name: "basic_property",
			schema: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"url": schemahelper.StringProp("The URL"),
			}),
			want: map[string]any{
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "The URL",
					},
				},
			},
		},
		{
			name: "property_with_all_fields",
			schema: schemahelper.ObjectSchema([]string{"count"}, map[string]*jsonschema.Schema{
				"count": schemahelper.IntProp("Count value",
					schemahelper.WithDefault(10),
					schemahelper.WithExample(5),
					schemahelper.WithEnum(1, 5, 10)),
			}),
			want: map[string]any{
				"properties": map[string]any{
					"count": map[string]any{
						"type":        "integer",
						"description": "Count value",
						"required":    true,
						"default":     float64(10),
						"example":     5,
						"enum":        []any{1, 5, 10},
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := BuildSchemaOutput(tc.schema)
			assert.Equal(t, tc.want, result)
		})
	}
}

func TestCapabilitiesToStrings(t *testing.T) {
	tests := []struct {
		name string
		caps []provider.Capability
		want []string
	}{
		{
			name: "empty",
			caps: []provider.Capability{},
			want: []string{},
		},
		{
			name: "single_capability",
			caps: []provider.Capability{provider.CapabilityFrom},
			want: []string{"from"},
		},
		{
			name: "multiple_capabilities",
			caps: []provider.Capability{provider.CapabilityFrom, provider.CapabilityTransform, provider.CapabilityValidation},
			want: []string{"from", "transform", "validation"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := CapabilitiesToStrings(tc.caps)
			assert.Equal(t, tc.want, result)
		})
	}
}

func TestOptions_getRegistry(t *testing.T) {
	t.Run("returns_injected_registry_when_set", func(t *testing.T) {
		injectedReg := provider.NewRegistry()
		options := &Options{
			registry: injectedReg,
		}

		result := options.getRegistry(context.Background())
		assert.Same(t, injectedReg, result)
	})

	t.Run("returns_registry_when_not_set", func(t *testing.T) {
		options := &Options{}

		result := options.getRegistry(context.Background())
		assert.NotNil(t, result)
	})
}

func TestOptions_writeQuietOutput(t *testing.T) {
	tests := []struct {
		name        string
		data        any
		checkOutput func(t *testing.T, output string)
	}{
		{
			name: "list_of_providers",
			data: []map[string]any{
				{"name": "provider-1"},
				{"name": "provider-2"},
			},
			checkOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "provider-1")
				assert.Contains(t, output, "provider-2")
			},
		},
		{
			name: "single_provider_with_name",
			data: map[string]any{
				"name":        "single-provider",
				"description": "A single provider",
			},
			checkOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "single-provider")
			},
		},
		{
			name: "single_provider_without_name",
			data: map[string]any{
				"description": "No name field",
			},
			checkOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "description")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var outBuf bytes.Buffer
			options := &Options{
				IOStreams: &terminal.IOStreams{
					Out: &outBuf,
				},
			}

			err := options.writeQuietOutput(tc.data)
			require.NoError(t, err)
			tc.checkOutput(t, outBuf.String())
		})
	}
}
