// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	sdkplugin "github.com/oakwood-commons/scafctl-plugin-sdk/plugin"
	sdkprovider "github.com/oakwood-commons/scafctl-plugin-sdk/provider"
	sdkhelper "github.com/oakwood-commons/scafctl-plugin-sdk/provider/schemahelper"
)

// EchoPlugin implements a simple echo plugin that returns its input
type EchoPlugin struct{}

// GetProviders returns the list of providers exposed by this plugin
//
//nolint:revive // ctx required by interface
func (p *EchoPlugin) GetProviders(ctx context.Context) ([]string, error) {
	return []string{"echo"}, nil
}

// GetProviderDescriptor returns the descriptor for the echo provider
//
//nolint:revive // ctx required by interface
func (p *EchoPlugin) GetProviderDescriptor(ctx context.Context, providerName string) (*sdkprovider.Descriptor, error) {
	if providerName != "echo" {
		return nil, fmt.Errorf("unknown provider: %s", providerName)
	}

	maxLen := 1000
	return &sdkprovider.Descriptor{
		Name:        "echo",
		DisplayName: "Echo Provider",
		Description: "A simple provider that echoes its input",
		APIVersion:  "v1",
		Version:     semver.MustParse("1.0.0"),
		Category:    "utility",
		Capabilities: []sdkprovider.Capability{
			sdkprovider.CapabilityTransform,
		},
		Schema: sdkhelper.ObjectSchema(
			[]string{"message"},
			map[string]*jsonschema.Schema{
				"message": sdkhelper.StringProp(
					"The message to echo",
					sdkhelper.WithExample("Hello, World!"),
					sdkhelper.WithMaxLength(maxLen),
				),
				"uppercase": sdkhelper.BoolProp(
					"Whether to convert the message to uppercase",
					sdkhelper.WithDefault(json.RawMessage("false")),
				),
			},
		),
		OutputSchemas: map[sdkprovider.Capability]*jsonschema.Schema{
			sdkprovider.CapabilityTransform: sdkhelper.ObjectSchema(
				nil,
				map[string]*jsonschema.Schema{
					"echoed": sdkhelper.StringProp("The echoed message"),
				},
			),
		},
	}, nil
}

// ExecuteProvider executes the echo provider
//
//nolint:revive // ctx required by interface
func (p *EchoPlugin) ExecuteProvider(ctx context.Context, providerName string, input map[string]any) (*sdkprovider.Output, error) {
	if providerName != "echo" {
		return nil, fmt.Errorf("unknown provider: %s", providerName)
	}

	message, ok := input["message"].(string)
	if !ok {
		return nil, fmt.Errorf("message must be a string")
	}

	uppercase, _ := input["uppercase"].(bool)
	result := message
	if uppercase {
		result = strings.ToUpper(result)
	}

	return &sdkprovider.Output{
		Data: map[string]any{
			"echoed": result,
		},
	}, nil
}

// DescribeWhatIf returns a description of what the provider would do.
func (p *EchoPlugin) DescribeWhatIf(_ context.Context, providerName string, input map[string]any) (string, error) {
	if providerName != "echo" {
		return "", fmt.Errorf("unknown provider: %s", providerName)
	}
	message, _ := input["message"].(string)
	if message != "" {
		return fmt.Sprintf("Would echo %q", message), nil
	}
	return "Would echo message", nil
}

// ConfigureProvider stores host-side configuration. The echo plugin does not
// require any configuration, so this is a no-op.
//
//nolint:revive // ctx and cfg required by interface
func (p *EchoPlugin) ConfigureProvider(_ context.Context, _ string, _ sdkplugin.ProviderConfig) error {
	return nil
}

// ExecuteProviderStream is not supported by the echo plugin.
//
//nolint:revive // all params required by interface
func (p *EchoPlugin) ExecuteProviderStream(_ context.Context, _ string, _ map[string]any, _ func(sdkplugin.StreamChunk)) error {
	return sdkplugin.ErrStreamingNotSupported
}

// ExtractDependencies is not implemented by the echo plugin (generic fallback is used).
//
//nolint:revive // all params required by interface
func (p *EchoPlugin) ExtractDependencies(_ context.Context, _ string, _ map[string]any) ([]string, error) {
	return nil, nil
}

// StopProvider is a no-op for the echo plugin.
//
//nolint:revive // all params required by interface
func (p *EchoPlugin) StopProvider(_ context.Context, _ string) error {
	return nil
}

func main() {
	sdkplugin.Serve(&EchoPlugin{})
}
