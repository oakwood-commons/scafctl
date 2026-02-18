// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package explain

import (
	"bytes"
	"context"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
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
	return &provider.Output{}, nil
}

func TestCommandProvider(t *testing.T) {
	t.Run("creates provider command with correct usage", func(t *testing.T) {
		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			Out:    outBuf,
			ErrOut: errBuf,
		}
		cliParams := &settings.Run{}

		cmd := CommandProvider(cliParams, ioStreams, "scafctl/explain")

		assert.Equal(t, "provider <name>", cmd.Use)
		assert.Contains(t, cmd.Aliases, "providers")
		assert.Contains(t, cmd.Aliases, "prov")
		assert.Contains(t, cmd.Aliases, "p")
		assert.NotEmpty(t, cmd.Short)
		assert.NotEmpty(t, cmd.Long)
	})

	t.Run("requires provider name argument", func(t *testing.T) {
		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			Out:    outBuf,
			ErrOut: errBuf,
		}
		cliParams := &settings.Run{}

		cmd := CommandProvider(cliParams, ioStreams, "scafctl/explain")
		cmd.SetOut(outBuf)
		cmd.SetErr(errBuf)
		cmd.SetArgs([]string{})

		err := cmd.Execute()
		assert.Error(t, err)
	})
}

func TestProviderOptions_Run(t *testing.T) {
	version := semver.MustParse("1.0.0")

	t.Run("explains existing provider", func(t *testing.T) {
		reg := provider.NewRegistry(provider.WithAllowOverwrite(true))
		mp := &mockProvider{
			descriptor: &provider.Descriptor{
				Name:        "test-provider",
				DisplayName: "Test Provider",
				Description: "A test provider for testing",
				APIVersion:  "scafctl.io/v1",
				Version:     version,
				Category:    "testing",
				Capabilities: []provider.Capability{
					provider.CapabilityFrom,
					provider.CapabilityTransform,
				},
				Tags:         []string{"test", "mock"},
				MockBehavior: "Returns test data",
				Schema: schemahelper.ObjectSchema([]string{"input"}, map[string]*jsonschema.Schema{
					"input": schemahelper.StringProp("Input value",
						schemahelper.WithExample("example-value")),
				}),
				OutputSchemas: map[provider.Capability]*jsonschema.Schema{
					provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
						"data": schemahelper.AnyProp(""),
					}),
					provider.CapabilityTransform: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
						"data": schemahelper.AnyProp(""),
					}),
				},
				Examples: []provider.Example{
					{
						Name:        "Basic Usage",
						Description: "Simple example",
						YAML:        "provider: test-provider\nwith:\n  input: value",
					},
				},
				Links: []provider.Link{
					{Name: "Docs", URL: "https://example.com/docs"},
				},
				Maintainers: []provider.Contact{
					{Name: "Test Author", Email: "test@example.com"},
				},
			},
		}
		err := reg.Register(mp)
		require.NoError(t, err)

		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			Out:    outBuf,
			ErrOut: errBuf,
		}

		options := &ProviderOptions{
			IOStreams: ioStreams,
			CliParams: &settings.Run{NoColor: true},
			registry:  reg,
		}

		err = options.Run(context.Background(), "test-provider")
		require.NoError(t, err)

		output := outBuf.String()
		assert.Contains(t, output, "Test Provider")
		assert.Contains(t, output, "test-provider")
		assert.Contains(t, output, "A test provider for testing")
		assert.Contains(t, output, "1.0.0")
		assert.Contains(t, output, "testing")
		assert.Contains(t, output, "from")
		assert.Contains(t, output, "transform")
		assert.Contains(t, output, "Returns test data")
		assert.Contains(t, output, "input")
		assert.Contains(t, output, "Basic Usage")
		assert.Contains(t, output, "test, mock")
		assert.Contains(t, output, "Docs")
		assert.Contains(t, output, "Test Author")
	})

	t.Run("returns error for non-existent provider", func(t *testing.T) {
		reg := provider.NewRegistry()

		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			Out:    outBuf,
			ErrOut: errBuf,
		}

		options := &ProviderOptions{
			IOStreams: ioStreams,
			CliParams: &settings.Run{NoColor: true},
			registry:  reg,
		}

		err := options.Run(context.Background(), "non-existent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("shows deprecated warning", func(t *testing.T) {
		reg := provider.NewRegistry(provider.WithAllowOverwrite(true))
		mp := &mockProvider{
			descriptor: &provider.Descriptor{
				Name:         "deprecated-provider",
				DisplayName:  "Deprecated Provider",
				Description:  "An old provider",
				APIVersion:   "scafctl.io/v1",
				Version:      version,
				Capabilities: []provider.Capability{provider.CapabilityFrom},
				MockBehavior: "Returns nothing",
				Schema:       &jsonschema.Schema{Type: "object"},
				OutputSchemas: map[provider.Capability]*jsonschema.Schema{
					provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
						"data": schemahelper.AnyProp(""),
					}),
				},
				Deprecated: true,
			},
		}
		err := reg.Register(mp)
		require.NoError(t, err)

		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			Out:    outBuf,
			ErrOut: errBuf,
		}

		options := &ProviderOptions{
			IOStreams: ioStreams,
			CliParams: &settings.Run{NoColor: true},
			registry:  reg,
		}

		err = options.Run(context.Background(), "deprecated-provider")
		require.NoError(t, err)

		output := outBuf.String()
		assert.Contains(t, output, "DEPRECATED")
	})

	t.Run("shows beta status", func(t *testing.T) {
		reg := provider.NewRegistry(provider.WithAllowOverwrite(true))
		mp := &mockProvider{
			descriptor: &provider.Descriptor{
				Name:         "beta-provider",
				DisplayName:  "Beta Provider",
				Description:  "A beta provider",
				APIVersion:   "scafctl.io/v1",
				Version:      version,
				Capabilities: []provider.Capability{provider.CapabilityFrom},
				MockBehavior: "Returns nothing",
				Schema:       &jsonschema.Schema{Type: "object"},
				OutputSchemas: map[provider.Capability]*jsonschema.Schema{
					provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
						"data": schemahelper.AnyProp(""),
					}),
				},
				Beta: true,
			},
		}
		err := reg.Register(mp)
		require.NoError(t, err)

		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			Out:    outBuf,
			ErrOut: errBuf,
		}

		options := &ProviderOptions{
			IOStreams: ioStreams,
			CliParams: &settings.Run{NoColor: true},
			registry:  reg,
		}

		err = options.Run(context.Background(), "beta-provider")
		require.NoError(t, err)

		output := outBuf.String()
		assert.Contains(t, output, "Beta")
	})
}

func TestLookupProvider(t *testing.T) {
	t.Run("returns descriptor from injected registry", func(t *testing.T) {
		customReg := provider.NewRegistry()
		err := customReg.Register(&mockProvider{
			descriptor: &provider.Descriptor{
				Name:         "test-prov",
				DisplayName:  "Test Provider",
				Description:  "A test provider",
				APIVersion:   "scafctl.io/v1",
				Version:      semver.MustParse("1.0.0"),
				Capabilities: []provider.Capability{provider.CapabilityFrom},
				MockBehavior: "Returns test data",
				Schema:       &jsonschema.Schema{Type: "object"},
				OutputSchemas: map[provider.Capability]*jsonschema.Schema{
					provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
						"data": schemahelper.AnyProp(""),
					}),
				},
			},
		})
		require.NoError(t, err)

		desc, err := LookupProvider(context.Background(), "test-prov", customReg)
		require.NoError(t, err)
		assert.Equal(t, "test-prov", desc.Name)
	})

	t.Run("returns error for unknown provider", func(t *testing.T) {
		customReg := provider.NewRegistry()

		_, err := LookupProvider(context.Background(), "nonexistent", customReg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("uses default registry when nil", func(t *testing.T) {
		// Should not panic even when nil is passed
		_, err := LookupProvider(context.Background(), "nonexistent-provider-xyz", nil)
		assert.Error(t, err)
	})
}
