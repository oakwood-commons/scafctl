// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/adrg/xdg"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	provdetail "github.com/oakwood-commons/scafctl/pkg/provider/detail"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pluginBinaryName returns the platform-appropriate binary filename.
// On Windows, plugin binaries have an .exe suffix to match Cache.binaryPath.
func pluginBinaryName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

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
		{
			name: "has_operation_flag_and_keeps_positional_provider_name",
			validate: func(t *testing.T) {
				ioStreams, _, _ := terminal.NewTestIOStreams()
				cliParams := &settings.Run{BinaryName: "mycli"}
				cmd := CommandProvider(cliParams, ioStreams, "get")

				opFlag := cmd.Flags().Lookup("operation")
				require.NotNil(t, opFlag)
				assert.Empty(t, opFlag.Shorthand)
				assert.Nil(t, cmd.Flags().Lookup("operations"))
				assert.Nil(t, cmd.Flags().ShorthandLookup("n"))
				assert.Contains(t, cmd.Long, "mycli get provider github --operation create_issue")
			},
		},
		{
			name: "rejects_operation_flag_without_provider_name_argument",
			validate: func(t *testing.T) {
				var outBuf bytes.Buffer
				ioStreams := &terminal.IOStreams{Out: &outBuf, ErrOut: &outBuf}
				cliParams := &settings.Run{BinaryName: "mycli"}
				w := writer.New(ioStreams, cliParams)
				ctx := writer.WithWriter(context.Background(), w)

				cmd := CommandProvider(cliParams, ioStreams, "get")
				cmd.SetContext(ctx)
				cmd.SetArgs([]string{"--operation", "create_issue"})
				err := cmd.Execute()
				require.Error(t, err)
				assert.Contains(t, err.Error(), "--operation requires a provider name argument")
				assert.Contains(t, err.Error(), "mycli get provider <name> --operation create_issue")
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

func TestOptions_RunGetProvider_OfficialProviderWithoutCache(t *testing.T) {
	t.Run("returns_hint_in_structured_output_when_plugin_not_cached", func(t *testing.T) {
		// Isolate from real plugin cache.
		t.Setenv("XDG_CACHE_HOME", t.TempDir())
		xdg.Reload()

		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{
			Out:    &outBuf,
			ErrOut: &outBuf,
		}
		cliParams := &settings.Run{BinaryName: "scafctl"}
		reg := provider.NewRegistry(provider.WithAllowOverwrite(true))

		options := &Options{
			IOStreams:      ioStreams,
			CliParams:      cliParams,
			BinaryName:     "scafctl",
			registry:       reg,
			KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
		}

		officialReg := official.NewRegistryFrom([]official.Provider{
			{Name: "github", CatalogRef: "gh-plugin", DefaultVersion: "latest", Description: "GitHub provider"},
		})

		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)
		ctx = official.WithRegistry(ctx, officialReg)

		err := options.RunGetProvider(ctx, "github")
		require.NoError(t, err)

		output := outBuf.String()
		assert.Contains(t, output, "github")
		assert.Contains(t, output, "hint")
		assert.Contains(t, output, "plugins install")
		assert.Contains(t, output, "gh-plugin", "hint should reference CatalogRef, not provider name")
	})

	t.Run("returns_human_text_in_default_output_when_plugin_not_cached", func(t *testing.T) {
		// Isolate from real plugin cache.
		t.Setenv("XDG_CACHE_HOME", t.TempDir())
		xdg.Reload()

		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{
			Out:    &outBuf,
			ErrOut: &outBuf,
		}
		cliParams := &settings.Run{BinaryName: "scafctl"}
		reg := provider.NewRegistry(provider.WithAllowOverwrite(true))

		options := &Options{
			IOStreams:      ioStreams,
			CliParams:      cliParams,
			BinaryName:     "scafctl",
			registry:       reg,
			KvxOutputFlags: flags.KvxOutputFlags{Output: ""},
		}

		officialReg := official.NewRegistryFrom([]official.Provider{
			{Name: "github", CatalogRef: "gh-plugin", DefaultVersion: "latest", Description: "GitHub provider"},
		})

		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)
		ctx = official.WithRegistry(ctx, officialReg)

		err := options.RunGetProvider(ctx, "github")
		require.NoError(t, err)

		output := outBuf.String()
		assert.Contains(t, output, "github")
		assert.Contains(t, output, "official plugin provider")
		assert.Contains(t, output, "plugins install")
		assert.Contains(t, output, "gh-plugin", "install hint should reference CatalogRef, not provider name")
	})
}

func TestOptions_RunGetProvider_OfficialProviderWithCachedBinary(t *testing.T) {
	t.Run("falls_back_to_hint_when_probe_fails_structured", func(t *testing.T) {
		// Create a fake cached binary that exists but isn't a valid plugin.
		cacheDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", cacheDir)
		xdg.Reload()

		// Set up fake binary at the expected cache path.
		platform := runtime.GOOS + "-" + runtime.GOARCH
		binDir := filepath.Join(cacheDir, "scafctl", "plugins", "gh-plugin", "1.0.0", platform)
		require.NoError(t, os.MkdirAll(binDir, 0o755))
		binPath := filepath.Join(binDir, pluginBinaryName("gh-plugin"))
		require.NoError(t, os.WriteFile(binPath, []byte("#!/bin/sh\nexit 1\n"), 0o755))

		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{Out: &outBuf, ErrOut: &outBuf}
		cliParams := &settings.Run{BinaryName: "scafctl"}
		reg := provider.NewRegistry(provider.WithAllowOverwrite(true))

		options := &Options{
			IOStreams:      ioStreams,
			CliParams:      cliParams,
			BinaryName:     "scafctl",
			registry:       reg,
			KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
		}

		officialReg := official.NewRegistryFrom([]official.Provider{
			{Name: "github", CatalogRef: "gh-plugin", DefaultVersion: "latest", Description: "GitHub provider"},
		})

		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)
		ctx = official.WithRegistry(ctx, officialReg)

		err := options.RunGetProvider(ctx, "github")
		require.NoError(t, err)

		output := outBuf.String()
		// Should fall back to hint since probe fails on a fake binary.
		assert.Contains(t, output, "hint")
		assert.Contains(t, output, "gh-plugin")
	})

	t.Run("falls_back_to_human_text_when_probe_fails_default", func(t *testing.T) {
		cacheDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", cacheDir)
		xdg.Reload()

		platform := runtime.GOOS + "-" + runtime.GOARCH
		binDir := filepath.Join(cacheDir, "scafctl", "plugins", "gh-plugin", "1.0.0", platform)
		require.NoError(t, os.MkdirAll(binDir, 0o755))
		binPath := filepath.Join(binDir, pluginBinaryName("gh-plugin"))
		require.NoError(t, os.WriteFile(binPath, []byte("#!/bin/sh\nexit 1\n"), 0o755))

		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{Out: &outBuf, ErrOut: &outBuf}
		cliParams := &settings.Run{BinaryName: "scafctl"}
		reg := provider.NewRegistry(provider.WithAllowOverwrite(true))

		options := &Options{
			IOStreams:      ioStreams,
			CliParams:      cliParams,
			BinaryName:     "scafctl",
			registry:       reg,
			KvxOutputFlags: flags.KvxOutputFlags{Output: ""},
		}

		officialReg := official.NewRegistryFrom([]official.Provider{
			{Name: "github", CatalogRef: "gh-plugin", DefaultVersion: "latest", Description: "GitHub provider"},
		})

		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)
		ctx = official.WithRegistry(ctx, officialReg)

		err := options.RunGetProvider(ctx, "github")
		require.NoError(t, err)

		output := outBuf.String()
		assert.Contains(t, output, "official plugin provider")
		assert.Contains(t, output, "gh-plugin")
	})
}

func TestOptions_RunGetProvider_PluginCacheWithProbeFallback(t *testing.T) {
	t.Run("falls_back_to_minimal_info_when_probe_fails_structured", func(t *testing.T) {
		cacheDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", cacheDir)
		xdg.Reload()

		// Create a fake cached plugin binary (not a valid plugin process).
		platform := runtime.GOOS + "-" + runtime.GOARCH
		binDir := filepath.Join(cacheDir, "scafctl", "plugins", "my-plugin", "2.0.0", platform)
		require.NoError(t, os.MkdirAll(binDir, 0o755))
		binPath := filepath.Join(binDir, pluginBinaryName("my-plugin"))
		require.NoError(t, os.WriteFile(binPath, []byte("#!/bin/sh\nexit 1\n"), 0o755))

		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{Out: &outBuf, ErrOut: &outBuf}
		cliParams := &settings.Run{BinaryName: "scafctl"}
		reg := provider.NewRegistry(provider.WithAllowOverwrite(true))

		options := &Options{
			IOStreams:      ioStreams,
			CliParams:      cliParams,
			BinaryName:     "scafctl",
			registry:       reg,
			KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
		}

		// No official registry — so it goes to the plugin cache fallback.
		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)

		err := options.RunGetProvider(ctx, "my-plugin")
		// The probe fails and the ProbePluginDescription also fails — provider not found.
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "my-plugin")
	})

	t.Run("falls_back_to_human_text_when_probe_fails_default", func(t *testing.T) {
		cacheDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", cacheDir)
		xdg.Reload()

		platform := runtime.GOOS + "-" + runtime.GOARCH
		binDir := filepath.Join(cacheDir, "scafctl", "plugins", "my-plugin", "2.0.0", platform)
		require.NoError(t, os.MkdirAll(binDir, 0o755))
		binPath := filepath.Join(binDir, pluginBinaryName("my-plugin"))
		require.NoError(t, os.WriteFile(binPath, []byte("#!/bin/sh\nexit 1\n"), 0o755))

		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{Out: &outBuf, ErrOut: &outBuf}
		cliParams := &settings.Run{BinaryName: "scafctl"}
		reg := provider.NewRegistry(provider.WithAllowOverwrite(true))

		options := &Options{
			IOStreams:      ioStreams,
			CliParams:      cliParams,
			BinaryName:     "scafctl",
			registry:       reg,
			KvxOutputFlags: flags.KvxOutputFlags{Output: ""},
		}

		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)

		err := options.RunGetProvider(ctx, "my-plugin")
		// Same as above — probe fails, description probe also fails.
		assert.Error(t, err)
	})
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
		// Isolate from real plugin cache so the probe doesn't find a cached binary.
		t.Setenv("XDG_CACHE_HOME", t.TempDir())
		xdg.Reload()

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

var ansiEscapeRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func newMockOperationsCatalogProvider() *mockProvider {
	return &mockProvider{
		descriptor: &provider.Descriptor{
			Name:         "scm",
			DisplayName:  "SCM Provider",
			Description:  "Source control operations provider",
			APIVersion:   "v1",
			Version:      semver.MustParse("1.4.0"),
			Capabilities: []provider.Capability{provider.CapabilityFrom, provider.CapabilityAction},
			Operations: []provider.OperationDescriptor{
				{
					Name:         "get_repo",
					DisplayName:  "Get Repository",
					Description:  "Get repository metadata and settings",
					Capabilities: []provider.Capability{provider.CapabilityFrom},
					IsWrite:      false,
					Tags:         []string{"repos", "read"},
					InputSchema: &jsonschema.Schema{
						Type:     "object",
						Required: []string{"repo"},
						Properties: map[string]*jsonschema.Schema{
							"repo": {Type: "string", Description: "Repository name"},
						},
					},
					OutputSchema: &jsonschema.Schema{
						Type: "object",
						Properties: map[string]*jsonschema.Schema{
							"defaultBranch": {Type: "string", Description: "Default branch name"},
						},
					},
				},
				{
					Name:               "create_issue",
					DisplayName:        "Create Issue",
					Description:        "Create a new issue in the repository",
					Capabilities:       []provider.Capability{provider.CapabilityAction},
					IsWrite:            true,
					IsDeprecated:       true,
					DeprecationMessage: "Use create_work_item instead",
					InputSchema: &jsonschema.Schema{
						Type:     "object",
						Required: []string{"title"},
						Properties: map[string]*jsonschema.Schema{
							"title":  {Type: "string", Description: "Issue title"},
							"state":  {Type: "string", Default: []byte(`"open"`), Enum: []any{"open", "closed"}},
							"labels": {Type: "array", Description: "Issue labels"},
						},
					},
					OutputSchema: &jsonschema.Schema{
						Type: "object",
						Properties: map[string]*jsonschema.Schema{
							"number": {Type: "integer", Description: "Created issue number"},
						},
					},
					Examples: []provider.Example{
						{
							Name:        "Open bug",
							Description: "Create a bug report",
							YAML:        "provider: scm\ninputs:\n  operation: create_issue\n  title: Broken build",
						},
					},
				},
				{
					Name:         "merge_pull_request",
					Description:  "Merge a pull request",
					Capabilities: []provider.Capability{provider.CapabilityAction},
					IsWrite:      true,
				},
			},
			Schema: schemahelper.ObjectSchema([]string{"operation"}, map[string]*jsonschema.Schema{
				"operation": schemahelper.StringProp("Operation name"),
			}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom:   schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{"result": schemahelper.AnyProp("")}),
				provider.CapabilityAction: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{"success": schemahelper.BoolProp("")}),
			},
		},
	}
}

func TestPrintProviderDetail_OperationsAndAlignment(t *testing.T) {
	t.Parallel()

	t.Run("renders_aligned_operations_table_with_and_without_ansi_colors", func(t *testing.T) {
		t.Parallel()
		mp := newMockOperationsCatalogProvider()

		for _, noColor := range []bool{true, false} {
			var outBuf bytes.Buffer
			ioStreams := &terminal.IOStreams{Out: &outBuf, ErrOut: &outBuf}
			cliParams := &settings.Run{NoColor: noColor}
			w := writer.New(ioStreams, cliParams)
			ctx := writer.WithWriter(context.Background(), w)

			err := (&Options{IOStreams: ioStreams, CliParams: cliParams}).printProviderDetail(ctx, mp.Descriptor())
			require.NoError(t, err)

			plain := ansiEscapeRE.ReplaceAllString(outBuf.String(), "")
			assert.Contains(t, plain, "Operations:")
			assert.Contains(t, plain, "get_repo")
			assert.Contains(t, plain, "create_issue")
			assert.Contains(t, plain, "merge_pull_request")
			assert.Contains(t, plain, "(write)")
			assert.Contains(t, plain, "(deprecated)")
			assert.Contains(t, plain, "Get repository metadata and settings")

			// Verify every operation description starts at the exact same visual column.
			descCols := make([]int, 0, 3)
			for _, line := range strings.Split(plain, "\n") {
				for _, descText := range []string{
					"Get repository metadata and settings",
					"Create a new issue in the repository",
					"Merge a pull request",
				} {
					if idx := strings.Index(line, descText); idx >= 0 {
						descCols = append(descCols, idx)
					}
				}
			}
			require.Len(t, descCols, 3, "expected all three operation descriptions in output:\n%s", plain)
			assert.Equal(t, descCols[0], descCols[1], "columns must align regardless of ANSI styling (noColor=%v):\n%s", noColor, plain)
			assert.Equal(t, descCols[1], descCols[2], "columns must align regardless of ANSI styling (noColor=%v):\n%s", noColor, plain)
		}
	})

	t.Run("falls_back_to_write_operations_when_operations_empty", func(t *testing.T) {
		t.Parallel()
		mp := newMockProvider("legacy-ops", "Legacy classifier provider", []provider.Capability{provider.CapabilityFrom, provider.CapabilityAction})
		mp.descriptor.WriteOperations = []string{"write_file", "delete_file"}

		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{Out: &outBuf, ErrOut: &outBuf}
		cliParams := &settings.Run{NoColor: true}
		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)

		err := (&Options{IOStreams: ioStreams, CliParams: cliParams}).printProviderDetail(ctx, mp.Descriptor())
		require.NoError(t, err)

		output := outBuf.String()
		assert.NotContains(t, output, "\nOperations:\n")
		assert.Contains(t, output, "Write Operations:")
		assert.Contains(t, output, "  write_file")
		assert.Contains(t, output, "  delete_file")
	})
}

func TestOptions_RunGetProvider_SingleOperation(t *testing.T) {
	t.Parallel()

	setupReg := func() *provider.Registry {
		reg := provider.NewRegistry(provider.WithAllowOverwrite(true))
		_ = reg.Register(newMockOperationsCatalogProvider())
		_ = reg.Register(newMockProvider("plain-prov", "No operations", []provider.Capability{provider.CapabilityFrom}))
		return reg
	}

	t.Run("renders_human_detail_for_write_operation", func(t *testing.T) {
		t.Parallel()
		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{Out: &outBuf, ErrOut: &outBuf}
		cliParams := &settings.Run{NoColor: true}
		opts := &Options{
			IOStreams: ioStreams,
			CliParams: cliParams,
			Operation: "create_issue",
			registry:  setupReg(),
		}
		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)

		require.NoError(t, opts.RunGetProvider(ctx, "scm"))
		out := outBuf.String()
		assert.Contains(t, out, "Provider: scm")
		assert.Contains(t, out, "Operation: create_issue")
		assert.Contains(t, out, "Display Name: Create Issue")
		assert.Contains(t, out, "Write: yes")
		assert.Contains(t, out, "[action]")
		assert.Contains(t, out, "DEPRECATED: Use create_work_item instead")
		assert.Contains(t, out, "Input Schema:")
		assert.Contains(t, out, "title (string) *")
		assert.Contains(t, out, "Output Schema:")
		assert.Contains(t, out, "number (integer)")
		assert.Contains(t, out, "Examples:")
		assert.Contains(t, out, "Open bug")
	})

	t.Run("renders_json_for_single_operation_and_cel_filter", func(t *testing.T) {
		t.Parallel()
		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{Out: &outBuf, ErrOut: &outBuf}
		cliParams := &settings.Run{NoColor: true}
		opts := &Options{
			IOStreams:      ioStreams,
			CliParams:      cliParams,
			Operation:      "get_repo",
			KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
			registry:       setupReg(),
		}
		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)

		require.NoError(t, opts.RunGetProvider(ctx, "scm"))
		var got map[string]any
		require.NoError(t, json.Unmarshal(outBuf.Bytes(), &got))
		assert.Equal(t, "scm", got["provider"])
		assert.Equal(t, "builtin", got["source"])
		assert.Equal(t, "get_repo", got["name"])
		assert.Equal(t, false, got["isWrite"])

		// Verify CEL expression filtering over operations works without -o and with -o json.
		outBuf.Reset()
		defaultExprOpts := &Options{
			IOStreams:      ioStreams,
			CliParams:      cliParams,
			KvxOutputFlags: flags.KvxOutputFlags{Output: "", Expression: "size(_.operations.filter(o, o.isWrite))"},
			registry:       setupReg(),
		}
		require.NoError(t, defaultExprOpts.RunGetProvider(ctx, "scm"))
		assert.Equal(t, "2", strings.TrimSpace(outBuf.String()))

		outBuf.Reset()
		exprOpts := &Options{
			IOStreams:      ioStreams,
			CliParams:      cliParams,
			KvxOutputFlags: flags.KvxOutputFlags{Output: "json", Expression: "_.operations.filter(o, o.isWrite).map(o, o.name)"},
			registry:       setupReg(),
		}
		require.NoError(t, exprOpts.RunGetProvider(ctx, "scm"))
		var writeNames []string
		require.NoError(t, json.Unmarshal(outBuf.Bytes(), &writeNames))
		assert.Equal(t, []string{"create_issue", "merge_pull_request"}, writeNames)
	})

	t.Run("errors_when_provider_has_no_operations_or_operation_missing", func(t *testing.T) {
		t.Parallel()
		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{Out: &outBuf, ErrOut: &outBuf}
		cliParams := &settings.Run{NoColor: true}
		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)

		noOps := &Options{IOStreams: ioStreams, CliParams: cliParams, Operation: "read", registry: setupReg()}
		err := noOps.RunGetProvider(ctx, "plain-prov")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `provider "plain-prov" does not declare per-operation metadata`)

		missingOp := &Options{IOStreams: ioStreams, CliParams: cliParams, Operation: "nonexistent", registry: setupReg()}
		err = missingOp.RunGetProvider(ctx, "scm")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `operation "nonexistent" not found on provider "scm"`)
		assert.Contains(t, err.Error(), "get_repo, create_issue, merge_pull_request")

		// Escape sequences and newlines in available operation names are sanitized on stderr and in error strings.
		outBuf.Reset()
		dirtyReg := provider.NewRegistry()
		dirtyProv := newMockProvider("dirty-prov", "Untrusted ops", []provider.Capability{provider.CapabilityFrom})
		dirtyProv.descriptor.Operations = []provider.OperationDescriptor{
			{Name: "safe_op", Capabilities: []provider.Capability{provider.CapabilityFrom}},
			{Name: "\x1b[31minjected_op\x1b[0m\r\n::error::spoofed", Capabilities: []provider.Capability{provider.CapabilityFrom}},
		}
		require.NoError(t, dirtyReg.Register(dirtyProv))

		dirtyMissing := &Options{IOStreams: ioStreams, CliParams: cliParams, Operation: "missing", registry: dirtyReg}
		dirtyErr := dirtyMissing.RunGetProvider(ctx, "dirty-prov")
		require.Error(t, dirtyErr)
		assert.NotContains(t, dirtyErr.Error(), "\x1b")
		assert.NotContains(t, dirtyErr.Error(), "\n")
		assert.NotContains(t, outBuf.String(), "\x1b")
		assert.Contains(t, dirtyErr.Error(), "safe_op, injected_op ::error::spoofed")
	})

	t.Run("renders_nested_array_and_dynamic_key_operation_schemas_in_human_and_json", func(t *testing.T) {
		t.Parallel()
		reg := provider.NewRegistry()
		complexProv := newMockProvider("complex-ops", "Complex schemas", []provider.Capability{provider.CapabilityAction})
		complexProv.descriptor.OutputSchemas = map[provider.Capability]*jsonschema.Schema{
			provider.CapabilityAction: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"success": schemahelper.BoolProp("Success"),
			}),
		}
		complexProv.descriptor.Operations = []provider.OperationDescriptor{
			{
				Name:         "apply_config",
				Capabilities: []provider.Capability{provider.CapabilityAction},
				IsWrite:      true,
				InputSchema: &jsonschema.Schema{
					Type:                 "object",
					AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}}, // "additionalProperties": false
					Required:             []string{"spec"},
					Properties: map[string]*jsonschema.Schema{
						"spec": {
							Type:                 "object",
							AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
							Required:             []string{"endpoint"},
							Properties: map[string]*jsonschema.Schema{
								"endpoint": {Type: "string", Description: "Target URL"},
							},
						},
						"targets": {
							Type:  "array",
							Items: &jsonschema.Schema{Type: "string"},
						},
						"matrix": {
							Type: "array",
							Items: &jsonschema.Schema{
								Type: "array",
								Items: &jsonschema.Schema{
									Type:     "object",
									Required: []string{"cellValue"},
									Properties: map[string]*jsonschema.Schema{
										"cellValue": {Type: "string"},
									},
								},
							},
						},
						"snowflakeId": {
							Type:    "integer",
							Default: json.RawMessage(`9007199254740993`),
							Enum:    []any{int64(9007199254740993)},
						},
					},
					PatternProperties: map[string]*jsonschema.Schema{
						"^env_": {
							Type: "array",
							Items: &jsonschema.Schema{
								Type:     "object",
								Required: []string{"varName"},
								Properties: map[string]*jsonschema.Schema{
									"varName": {Type: "string"},
								},
							},
						},
					},
				},
				OutputSchema: &jsonschema.Schema{
					Type: "object",
					AdditionalProperties: &jsonschema.Schema{
						Type:        "array",
						Description: "Applied rules per environment",
						Items: &jsonschema.Schema{
							Type:     "object",
							Required: []string{"ruleId"},
							Properties: map[string]*jsonschema.Schema{
								"ruleId": {Type: "string"},
							},
						},
					},
				},
			},
			{
				Name:         "strict_empty",
				Capabilities: []provider.Capability{provider.CapabilityAction},
				InputSchema: &jsonschema.Schema{
					Type:                 "object",
					AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}}, // empty strict object
				},
				OutputSchema: &jsonschema.Schema{
					Type: "string",
					Enum: []any{"queued", "applied"},
				},
			},
		}
		require.NoError(t, reg.Register(complexProv))

		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{Out: &outBuf, ErrOut: &outBuf}
		cliParams := &settings.Run{NoColor: true}
		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)

		humanOpts := &Options{IOStreams: ioStreams, CliParams: cliParams, Operation: "apply_config", registry: reg}
		require.NoError(t, humanOpts.RunGetProvider(ctx, "complex-ops"))
		humanOut := outBuf.String()
		assert.Contains(t, humanOut, "spec (object) *")
		assert.Contains(t, humanOut, "endpoint (string) *")
		assert.Contains(t, humanOut, "Items: (string)")
		assert.Contains(t, humanOut, "matrix (array)")
		assert.Contains(t, humanOut, "Items: (array)")
		assert.Contains(t, humanOut, "cellValue (string) *")
		assert.Contains(t, humanOut, "^env_ (array)")
		assert.Contains(t, humanOut, "varName (string) *")
		assert.Contains(t, humanOut, "<key> (array)")
		assert.Contains(t, humanOut, "ruleId (string)")
		assert.NotContains(t, humanOut, "ruleId (string) *")
		assert.NotContains(t, humanOut, "<key> (any)", "additionalProperties: false must not render as <key> (any)")

		// Strict empty object omits Input Schema heading; scalar OutputSchema renders top-level Enum.
		outBuf.Reset()
		strictOpts := &Options{IOStreams: ioStreams, CliParams: cliParams, Operation: "strict_empty", registry: reg}
		require.NoError(t, strictOpts.RunGetProvider(ctx, "complex-ops"))
		strictOut := outBuf.String()
		assert.NotContains(t, strictOut, "Input Schema:")
		assert.NotContains(t, strictOut, "<key>")
		assert.Contains(t, strictOut, "Output Schema:")
		assert.Contains(t, strictOut, "Enum: queued, applied")

		outBuf.Reset()
		jsonOpts := &Options{
			IOStreams:      ioStreams,
			CliParams:      cliParams,
			Operation:      "apply_config",
			KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
			registry:       reg,
		}
		require.NoError(t, jsonOpts.RunGetProvider(ctx, "complex-ops"))
		jsonStr := outBuf.String()
		assert.Contains(t, jsonStr, "9007199254740993")
		assert.NotContains(t, jsonStr, "9007199254740992")
		var got map[string]any
		require.NoError(t, json.Unmarshal(outBuf.Bytes(), &got))
		inSchema := got["inputSchema"].(map[string]any)
		specProp := inSchema["properties"].(map[string]any)["spec"].(map[string]any)
		assert.Equal(t, []any{"endpoint"}, specProp["required"])
		outSchema := got["outputSchema"].(map[string]any)
		assert.Equal(t, "array", outSchema["additionalProperties"].(map[string]any)["type"])

		// YAML and CEL also retain exact numeric type/precision above 2^53.
		outBuf.Reset()
		yamlOpts := &Options{
			IOStreams:      ioStreams,
			CliParams:      cliParams,
			Operation:      "apply_config",
			KvxOutputFlags: flags.KvxOutputFlags{Output: "yaml"},
			registry:       reg,
		}
		require.NoError(t, yamlOpts.RunGetProvider(ctx, "complex-ops"))
		assert.Contains(t, outBuf.String(), "default: 9007199254740993\n")
		assert.NotContains(t, outBuf.String(), `"9007199254740993"`)

		outBuf.Reset()
		celOpts := &Options{
			IOStreams: ioStreams,
			CliParams: cliParams,
			Operation: "apply_config",
			KvxOutputFlags: flags.KvxOutputFlags{
				Expression: "_.inputSchema.properties.snowflakeId.default == 9007199254740993",
			},
			registry: reg,
		}
		require.NoError(t, celOpts.RunGetProvider(ctx, "complex-ops"))
		assert.Equal(t, "true", strings.TrimSpace(outBuf.String()))
	})
}

func TestOptions_RunGetProvider_SingleOperation_UncachedOfficial(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheDir)
	xdg.Reload()
	t.Cleanup(xdg.Reload)

	names := make([]string, 20)
	for i := range names {
		names[i] = fmt.Sprintf("op_%02d", i)
	}
	formatted := provdetail.FormatAvailableOperations(names)
	assert.Contains(t, formatted, "... (20 total)")

	var outBuf bytes.Buffer
	ioStreams := &terminal.IOStreams{Out: &outBuf, ErrOut: &outBuf}
	cliParams := &settings.Run{BinaryName: "mycli", NoColor: true}
	w := writer.New(ioStreams, cliParams)
	ctx := official.WithRegistry(writer.WithWriter(context.Background(), w), official.NewRegistryFrom([]official.Provider{
		{Name: "github", CatalogRef: "github-plugin", DefaultVersion: "latest"},
	}))

	opts := &Options{
		IOStreams:  ioStreams,
		CliParams:  cliParams,
		BinaryName: "mycli",
		Operation:  "create_issue",
		registry:   provider.NewRegistry(),
	}
	err := opts.RunGetProvider(ctx, "github")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mycli plugins install github-plugin")

	// When a cached binary exists for the official plugin but descriptor probing fails,
	// --operation reports the probe failure rather than claiming the plugin is uncached.
	cache := plugin.NewCache(settings.PluginCacheDirFor("mycli"))
	_, putErr := cache.Put("github-plugin", "1.0.0", plugin.CurrentPlatform(), []byte("invalid-plugin"))
	require.NoError(t, putErr)

	errCachedBroken := opts.RunGetProvider(ctx, "github")
	require.Error(t, errCachedBroken)
	assert.Contains(t, errCachedBroken.Error(), "failed to load cached plugin descriptor")
	assert.NotContains(t, errCachedBroken.Error(), "plugin is not cached locally")
}
