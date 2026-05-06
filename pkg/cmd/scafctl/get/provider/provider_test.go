// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/adrg/xdg"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
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
				KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
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
				KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
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
				KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
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
				KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
			},
			wantErr: false,
			checkOutput: func(t *testing.T, output string) {
				// Empty registry produces an empty JSON array
				assert.Contains(t, output, "[]")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Isolate from real plugin cache on the host.
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			xdg.Reload()

			var outBuf bytes.Buffer
			ioStreams := &terminal.IOStreams{
				Out:    &outBuf,
				ErrOut: &outBuf,
			}
			tc.options.IOStreams = ioStreams
			tc.options.CliParams = &settings.Run{}
			tc.options.registry = tc.setupReg()

			w := writer.New(ioStreams, tc.options.CliParams)
			ctx := writer.WithWriter(context.Background(), w)
			err := tc.options.RunListProviders(ctx)

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
				KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
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
				KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
			},
			wantErr: false,
			checkOutput: func(t *testing.T, output string) {
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

			w := writer.New(ioStreams, tc.options.CliParams)
			ctx := writer.WithWriter(context.Background(), w)
			err := tc.options.RunGetProvider(ctx, tc.providerName)

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
			},
			checkKeys: []string{"name", "displayName", "apiVersion", "version", "description", "capabilities"},
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
				IsDeprecated: true,
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

func TestOptions_RunListProviders_IncludesOfficialProviders(t *testing.T) {
	t.Run("includes_official_providers_when_registry_in_context", func(t *testing.T) {
		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{
			Out:    &outBuf,
			ErrOut: &outBuf,
		}
		cliParams := &settings.Run{}
		reg := provider.NewRegistry(provider.WithAllowOverwrite(true))
		mp := newMockProvider("builtin-prov", "A built-in provider", []provider.Capability{provider.CapabilityFrom})
		_ = reg.Register(mp)

		options := &Options{
			IOStreams:      ioStreams,
			CliParams:      cliParams,
			registry:       reg,
			KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
		}

		officialReg := official.NewRegistryFrom([]official.Provider{
			{Name: "exec", CatalogRef: "exec", DefaultVersion: "latest"},
			{Name: "git", CatalogRef: "git", DefaultVersion: "latest"},
		})

		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)
		ctx = official.WithRegistry(ctx, officialReg)

		err := options.RunListProviders(ctx)
		require.NoError(t, err)

		output := outBuf.String()
		assert.Contains(t, output, "builtin-prov")
		assert.Contains(t, output, "exec")
		assert.Contains(t, output, "git")

		var items []map[string]any
		require.NoError(t, json.Unmarshal([]byte(output), &items))
		sources := make(map[string]bool)
		for _, item := range items {
			if s, ok := item["source"].(string); ok {
				sources[s] = true
			}
		}
		assert.True(t, sources["builtin"], "expected at least one builtin provider")
		assert.True(t, sources["official"], "expected at least one official provider")
	})

	t.Run("shows_only_builtins_when_official_registry_not_in_context", func(t *testing.T) {
		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{
			Out:    &outBuf,
			ErrOut: &outBuf,
		}
		cliParams := &settings.Run{}
		reg := provider.NewRegistry(provider.WithAllowOverwrite(true))
		mp := newMockProvider("builtin-prov", "A built-in provider", []provider.Capability{provider.CapabilityFrom})
		_ = reg.Register(mp)

		options := &Options{
			IOStreams:      ioStreams,
			CliParams:      cliParams,
			registry:       reg,
			KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
		}

		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)

		err := options.RunListProviders(ctx)
		require.NoError(t, err)

		output := outBuf.String()
		assert.Contains(t, output, "builtin-prov")

		var items []map[string]any
		require.NoError(t, json.Unmarshal([]byte(output), &items))
		for _, item := range items {
			assert.NotEqual(t, "official", item["source"], "should not contain official providers without registry")
		}
	})

	t.Run("excludes_official_providers_when_capability_filter_set", func(t *testing.T) {
		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{
			Out:    &outBuf,
			ErrOut: &outBuf,
		}
		cliParams := &settings.Run{}
		reg := provider.NewRegistry(provider.WithAllowOverwrite(true))
		mp := newMockProvider("builtin-prov", "A built-in provider", []provider.Capability{provider.CapabilityFrom})
		_ = reg.Register(mp)

		options := &Options{
			IOStreams:      ioStreams,
			CliParams:      cliParams,
			registry:       reg,
			Capability:     "from",
			KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
		}

		officialReg := official.NewRegistryFrom([]official.Provider{
			{Name: "exec", CatalogRef: "exec", DefaultVersion: "latest"},
		})

		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)
		ctx = official.WithRegistry(ctx, officialReg)

		err := options.RunListProviders(ctx)
		require.NoError(t, err)

		output := outBuf.String()
		assert.Contains(t, output, "builtin-prov")
		assert.NotContains(t, output, "exec")

		var items []map[string]any
		require.NoError(t, json.Unmarshal([]byte(output), &items))
		for _, item := range items {
			assert.NotEqual(t, "official", item["source"], "should not contain official providers when capability filter set")
		}
	})

	t.Run("excludes_official_providers_when_category_filter_set", func(t *testing.T) {
		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{
			Out:    &outBuf,
			ErrOut: &outBuf,
		}
		cliParams := &settings.Run{}
		reg := provider.NewRegistry(provider.WithAllowOverwrite(true))
		mp := newMockProvider("builtin-prov", "A built-in provider", []provider.Capability{provider.CapabilityFrom})
		mp.descriptor.Category = "network"
		_ = reg.Register(mp)

		options := &Options{
			IOStreams:      ioStreams,
			CliParams:      cliParams,
			registry:       reg,
			Category:       "network",
			KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
		}

		officialReg := official.NewRegistryFrom([]official.Provider{
			{Name: "exec", CatalogRef: "exec", DefaultVersion: "latest"},
		})

		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)
		ctx = official.WithRegistry(ctx, officialReg)

		err := options.RunListProviders(ctx)
		require.NoError(t, err)

		output := outBuf.String()
		assert.Contains(t, output, "builtin-prov")
		assert.NotContains(t, output, "exec")

		var items []map[string]any
		require.NoError(t, json.Unmarshal([]byte(output), &items))
		for _, item := range items {
			assert.NotEqual(t, "official", item["source"], "should not contain official providers when category filter set")
		}
	})
}

func TestOptions_RunGetProvider_FallsBackToOfficialRegistry(t *testing.T) {
	t.Run("shows_info_for_official_provider_not_in_builtin_registry", func(t *testing.T) {
		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{
			Out:    &outBuf,
			ErrOut: &outBuf,
		}
		cliParams := &settings.Run{}
		reg := provider.NewRegistry(provider.WithAllowOverwrite(true))

		options := &Options{
			IOStreams:      ioStreams,
			CliParams:      cliParams,
			registry:       reg,
			KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
		}

		officialReg := official.NewRegistryFrom([]official.Provider{
			{Name: "exec", CatalogRef: "exec", DefaultVersion: "latest"},
		})

		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)
		ctx = official.WithRegistry(ctx, officialReg)

		err := options.RunGetProvider(ctx, "exec")
		require.NoError(t, err)

		output := outBuf.String()
		assert.Contains(t, output, "exec")
		assert.Contains(t, output, "Official plugin provider")
	})

	t.Run("returns_error_when_provider_not_in_any_registry", func(t *testing.T) {
		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{
			Out:    &outBuf,
			ErrOut: &outBuf,
		}
		cliParams := &settings.Run{}
		reg := provider.NewRegistry(provider.WithAllowOverwrite(true))

		options := &Options{
			IOStreams:      ioStreams,
			CliParams:      cliParams,
			registry:       reg,
			KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
		}

		officialReg := official.NewRegistryFrom([]official.Provider{
			{Name: "exec", CatalogRef: "exec", DefaultVersion: "latest"},
		})

		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)
		ctx = official.WithRegistry(ctx, officialReg)

		err := options.RunGetProvider(ctx, "nonexistent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("shows_plain_text_for_official_provider_with_default_output", func(t *testing.T) {
		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{
			Out:    &outBuf,
			ErrOut: &outBuf,
		}
		cliParams := &settings.Run{}
		reg := provider.NewRegistry(provider.WithAllowOverwrite(true))

		options := &Options{
			IOStreams: ioStreams,
			CliParams: cliParams,
			registry:  reg,
		}

		officialReg := official.NewRegistryFrom([]official.Provider{
			{Name: "exec", CatalogRef: "oci://catalog/exec", DefaultVersion: "v1.2.0"},
		})

		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)
		ctx = official.WithRegistry(ctx, officialReg)

		err := options.RunGetProvider(ctx, "exec")
		require.NoError(t, err)

		output := outBuf.String()
		assert.Contains(t, output, "official plugin provider")
		assert.Contains(t, output, "oci://catalog/exec")
		assert.Contains(t, output, "v1.2.0")
	})

	t.Run("shows_only_name_for_official_provider_with_quiet_output", func(t *testing.T) {
		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{
			Out:    &outBuf,
			ErrOut: &outBuf,
		}
		cliParams := &settings.Run{}
		reg := provider.NewRegistry(provider.WithAllowOverwrite(true))

		options := &Options{
			IOStreams:      ioStreams,
			CliParams:      cliParams,
			registry:       reg,
			KvxOutputFlags: flags.KvxOutputFlags{Output: "quiet"},
		}

		officialReg := official.NewRegistryFrom([]official.Provider{
			{Name: "exec", CatalogRef: "oci://catalog/exec", DefaultVersion: "v1.2.0"},
		})

		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)
		ctx = official.WithRegistry(ctx, officialReg)

		err := options.RunGetProvider(ctx, "exec")
		require.NoError(t, err)

		output := outBuf.String()
		assert.Empty(t, output, "quiet mode should produce no output for official providers, consistent with built-ins")
	})
}

func TestOptions_appendCachedPlugins(t *testing.T) {
	t.Run("skips_cached_plugins_that_fail_probing", func(t *testing.T) {
		cacheDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", cacheDir)
		xdg.Reload()

		// Create a fake cached plugin binary (not a real plugin, probe will fail).
		cache := plugin.NewCache(filepath.Join(cacheDir, "scafctl", "plugins"))
		_, err := cache.Put("myplugin", "1.0.0", plugin.CurrentPlatform(), []byte("binary"))
		require.NoError(t, err)

		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{Out: &outBuf, ErrOut: &outBuf}
		cliParams := &settings.Run{}
		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)

		options := &Options{IOStreams: ioStreams, CliParams: cliParams}
		output := options.appendCachedPlugins(ctx, nil)

		assert.Empty(t, output, "fake binaries that fail probing should be excluded")
	})

	t.Run("skips_already_listed_names", func(t *testing.T) {
		cacheDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", cacheDir)
		xdg.Reload()

		cache := plugin.NewCache(filepath.Join(cacheDir, "scafctl", "plugins"))
		_, err := cache.Put("duplicate", "1.0.0", plugin.CurrentPlatform(), []byte("binary"))
		require.NoError(t, err)

		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{Out: &outBuf, ErrOut: &outBuf}
		cliParams := &settings.Run{}
		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)

		options := &Options{IOStreams: ioStreams, CliParams: cliParams}
		existing := []Summary{{Name: "duplicate", Source: "builtin"}}
		output := options.appendCachedPlugins(ctx, existing)

		assert.Len(t, output, 1, "should not add duplicate")
		assert.Equal(t, "builtin", output[0].Source)
	})

	t.Run("handles_empty_cache", func(t *testing.T) {
		t.Setenv("XDG_CACHE_HOME", t.TempDir())
		xdg.Reload()

		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{Out: &outBuf, ErrOut: &outBuf}
		cliParams := &settings.Run{}
		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)

		options := &Options{IOStreams: ioStreams, CliParams: cliParams}
		output := options.appendCachedPlugins(ctx, nil)

		assert.Empty(t, output)
	})
}

func TestOptions_RunGetProvider_FallsBackToCachedPlugin(t *testing.T) {
	t.Run("returns_not_found_for_non_provider_cached_binary", func(t *testing.T) {
		cacheDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", cacheDir)
		xdg.Reload()

		// Put a fake binary (not a real provider plugin) in cache.
		cache := plugin.NewCache(filepath.Join(cacheDir, "scafctl", "plugins"))
		_, err := cache.Put("myplugin", "2.0.0", plugin.CurrentPlatform(), []byte("binary"))
		require.NoError(t, err)

		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{Out: &outBuf, ErrOut: &outBuf}
		cliParams := &settings.Run{}
		reg := provider.NewRegistry(provider.WithAllowOverwrite(true))

		options := &Options{
			IOStreams:      ioStreams,
			CliParams:      cliParams,
			BinaryName:     "scafctl",
			registry:       reg,
			KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
		}

		officialReg := official.NewRegistryFrom(nil)
		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)
		ctx = official.WithRegistry(ctx, officialReg)

		err = options.RunGetProvider(ctx, "myplugin")
		require.Error(t, err, "fake binary should fail validation")
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestPrintProviderDetail_RendersAllSections(t *testing.T) {
	t.Parallel()

	desc := &provider.Descriptor{
		Name:         "test-provider",
		DisplayName:  "Test Provider",
		Description:  "A test provider for unit tests",
		APIVersion:   "v1",
		Version:      semver.MustParse("2.3.0"),
		Category:     "testing",
		Tags:         []string{"unit", "ci"},
		Beta:         true,
		IsDeprecated: true,
		Capabilities: []provider.Capability{
			provider.CapabilityFrom,
			provider.CapabilityTransform,
		},
		Schema: schemahelper.ObjectSchema([]string{"url"}, map[string]*jsonschema.Schema{
			"url":    schemahelper.StringProp("Target URL"),
			"method": {Type: "string", Default: []byte(`"GET"`), Enum: []any{"GET", "POST"}},
		}),
		OutputSchemas: map[provider.Capability]*jsonschema.Schema{
			provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"body": schemahelper.StringProp("Response body"),
			}),
		},
		SensitiveFields: []string{"token"},
		Links: []provider.Link{
			{Name: "Docs", URL: "https://example.com/docs"},
		},
		Maintainers: []provider.Contact{
			{Name: "Test Author", Email: "test@example.com"},
		},
		Examples: []provider.Example{
			{
				Name:        "basic",
				Description: "Basic GET request",
				YAML:        "url: https://example.com\nmethod: GET",
			},
		},
	}

	var outBuf bytes.Buffer
	ioStreams := &terminal.IOStreams{Out: &outBuf, ErrOut: &outBuf}
	cliParams := &settings.Run{NoColor: true}
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	options := &Options{IOStreams: ioStreams, CliParams: cliParams}
	err := options.printProviderDetail(ctx, desc)
	require.NoError(t, err)

	output := outBuf.String()
	assert.Contains(t, output, "test-provider")
	assert.Contains(t, output, "Test Provider")
	assert.Contains(t, output, "2.3.0")
	assert.Contains(t, output, "A test provider for unit tests")
	assert.Contains(t, output, "testing")
	assert.Contains(t, output, "unit, ci")
	assert.Contains(t, output, "BETA")
	assert.Contains(t, output, "DEPRECATED")
	assert.Contains(t, output, "[from]")
	assert.Contains(t, output, "[transform]")
	assert.Contains(t, output, "url")
	assert.Contains(t, output, "(string)")
	assert.Contains(t, output, "GET")
	assert.Contains(t, output, "body")
	assert.Contains(t, output, "Docs")
	assert.Contains(t, output, "https://example.com/docs")
	assert.Contains(t, output, "Test Author")
	assert.Contains(t, output, "basic")
	assert.Contains(t, output, "Basic GET request")
}

func TestPrintProviderDetail_NilWriter(t *testing.T) {
	t.Parallel()

	desc := &provider.Descriptor{
		Name:       "minimal",
		APIVersion: "v1",
		Version:    semver.MustParse("1.0.0"),
	}

	// Context without a writer should return nil
	err := (&Options{}).printProviderDetail(context.Background(), desc)
	require.NoError(t, err)
}

func TestGenerateCLIExamples_Delegate(t *testing.T) {
	t.Parallel()

	desc := &provider.Descriptor{
		Name:       "static",
		APIVersion: "v1",
		Version:    semver.MustParse("1.0.0"),
		Capabilities: []provider.Capability{
			provider.CapabilityFrom,
		},
		Schema: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
			"value": schemahelper.StringProp("Static value"),
		}),
	}
	examples := GenerateCLIExamples(desc)
	assert.NotEmpty(t, examples)
}

func TestSchemaPlaceholder_Delegate(t *testing.T) {
	t.Parallel()

	prop := &jsonschema.Schema{Type: "string"}
	result := SchemaPlaceholder("myfield", prop)
	assert.Contains(t, result, "myfield")
}

func TestBuildProviderDetail_Delegate(t *testing.T) {
	t.Parallel()

	desc := provider.Descriptor{
		Name:       "test",
		APIVersion: "v1",
		Version:    semver.MustParse("1.0.0"),
	}
	detail := BuildProviderDetail(desc)
	assert.Equal(t, "test", detail["name"])
}
