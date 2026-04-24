// Package <% .pkg_name %> implements the <% .provider_name %> provider plugin.
package <% .pkg_name %>

import (
	"context"
	"fmt"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	sdkplugin "github.com/oakwood-commons/scafctl-plugin-sdk/plugin"
	sdkprovider "github.com/oakwood-commons/scafctl-plugin-sdk/provider"
	sdkhelper "github.com/oakwood-commons/scafctl-plugin-sdk/provider/schemahelper"
)

const (
	// ProviderName is the unique identifier for this provider.
	ProviderName = "<% .provider_name %>"

	// Version is the provider version.
	Version = "0.1.0"
)

// Plugin implements the scafctl ProviderPlugin interface.
type Plugin struct{}

// GetProviders returns the list of providers exposed by this plugin.
//
//nolint:revive // ctx required by interface
func (p *Plugin) GetProviders(_ context.Context) ([]string, error) {
	return []string{ProviderName}, nil
}

// GetProviderDescriptor returns the descriptor for the named provider.
//
//nolint:revive // ctx required by interface
func (p *Plugin) GetProviderDescriptor(_ context.Context, providerName string) (*sdkprovider.Descriptor, error) {
	if providerName != ProviderName {
		return nil, fmt.Errorf("unknown provider: %s", providerName)
	}

	return &sdkprovider.Descriptor{
		Name:        ProviderName,
		DisplayName: "<% .display_name %>",
		Description: "<% .description %>",
		APIVersion:  "v1",
		Version:     semver.MustParse(Version),
		Category:    "custom",
		Capabilities: []sdkprovider.Capability{
<%- range .capability_consts %>
			<% . %>,
<%- end %>
		},
		Schema: sdkhelper.ObjectSchema(
			[]string{"value"},
			map[string]*jsonschema.Schema{
				"value": sdkhelper.StringProp(
					"The input value",
					sdkhelper.WithExample("hello"),
				),
			},
		),
	}, nil
}

// ExecuteProvider executes the named provider with the given input.
//
//nolint:revive // ctx required by interface
func (p *Plugin) ExecuteProvider(_ context.Context, providerName string, input map[string]any) (*sdkprovider.Output, error) {
	if providerName != ProviderName {
		return nil, fmt.Errorf("unknown provider: %s", providerName)
	}

	value, _ := input["value"].(string)

	// TODO: implement your provider logic here
	return &sdkprovider.Output{
		Data: map[string]any{
			"result": value,
		},
	}, nil
}

// DescribeWhatIf returns a description of what the provider would do.
//
//nolint:revive // ctx required by interface
func (p *Plugin) DescribeWhatIf(_ context.Context, providerName string, input map[string]any) (string, error) {
	if providerName != ProviderName {
		return "", fmt.Errorf("unknown provider: %s", providerName)
	}

	value, _ := input["value"].(string)
	if value != "" {
		return fmt.Sprintf("Would process %q", value), nil
	}
	return "Would process input value", nil
}

// ConfigureProvider stores host-side configuration.
//
//nolint:revive // ctx and cfg required by interface
func (p *Plugin) ConfigureProvider(_ context.Context, _ string, _ sdkplugin.ProviderConfig) error {
	return nil
}

// ExecuteProviderStream is not supported.
//
//nolint:revive // all params required by interface
func (p *Plugin) ExecuteProviderStream(_ context.Context, _ string, _ map[string]any, _ func(sdkplugin.StreamChunk)) error {
	return sdkplugin.ErrStreamingNotSupported
}

// ExtractDependencies returns resolver keys this input depends on.
//
//nolint:revive // all params required by interface
func (p *Plugin) ExtractDependencies(_ context.Context, _ string, _ map[string]any) ([]string, error) {
	return nil, nil
}

// StopProvider performs cleanup for the named provider.
//
//nolint:revive // all params required by interface
func (p *Plugin) StopProvider(_ context.Context, _ string) error {
	return nil
}
