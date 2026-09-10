// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	sdkplugin "github.com/oakwood-commons/scafctl-plugin-sdk/plugin"
	sdkprovider "github.com/oakwood-commons/scafctl-plugin-sdk/provider"
	sdkhelper "github.com/oakwood-commons/scafctl-plugin-sdk/provider/schemahelper"
)

type MyPlugin struct {
	cfg sdkplugin.ProviderConfig
}

func (p *MyPlugin) GetProviders(ctx context.Context) ([]string, error) {
	return []string{"test-provider"}, nil
}

func (p *MyPlugin) GetProviderDescriptor(ctx context.Context, name string) (*sdkprovider.Descriptor, error) {
	switch name {
	case "test-provider":
		return &sdkprovider.Descriptor{
			Name:        "test-provider",
			DisplayName: "Test Provider",
			APIVersion:  "v1",
			Version:     semver.MustParse("1.0.0"),
			Description: "A custom provider that does something useful",
			Capabilities: []sdkprovider.Capability{
				sdkprovider.CapabilityFrom,
			},
			Schema: sdkhelper.ObjectSchema([]string{"input"}, map[string]*jsonschema.Schema{
				"input": sdkhelper.StringProp("The input value to process"),
			}),
			OutputSchemas: map[sdkprovider.Capability]*jsonschema.Schema{
				sdkprovider.CapabilityFrom: sdkhelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"output": sdkhelper.StringProp("The processed output"),
				}),
			},
			Category: "custom",
			Tags:     []string{"custom", "example"},
		}, nil
	default:
		return nil, fmt.Errorf("unknown provider: %s", name)
	}
}

func (p *MyPlugin) ConfigureProvider(_ context.Context, _ string, cfg sdkplugin.ProviderConfig) error {
	p.cfg = cfg
	return nil
}

func (p *MyPlugin) ExecuteProvider(ctx context.Context, name string, input map[string]any) (*sdkprovider.Output, error) {
	switch name {
	case "test-provider":
		value, _ := input["input"].(string)
		if sdkprovider.DryRunFromContext(ctx) {
			return &sdkprovider.Output{
				Data: map[string]any{"output": "[DRY-RUN] Would process: " + value},
			}, nil
		}
		return &sdkprovider.Output{
			Data: map[string]any{"output": "Processed: " + value},
		}, nil
	default:
		return nil, fmt.Errorf("unknown provider: %s", name)
	}
}

func (p *MyPlugin) ExecuteProviderStream(_ context.Context, _ string, _ map[string]any, _ func(sdkplugin.StreamChunk)) error {
	return sdkplugin.ErrStreamingNotSupported
}

func (p *MyPlugin) DescribeWhatIf(_ context.Context, name string, input map[string]any) (string, error) {
	switch name {
	case "test-provider":
		value, _ := input["input"].(string)
		if value != "" {
			return fmt.Sprintf("Would process %q", value), nil
		}
		return "Would process input", nil
	default:
		return "", fmt.Errorf("unknown provider: %s", name)
	}
}

func (p *MyPlugin) ExtractDependencies(_ context.Context, _ string, _ map[string]any) ([]string, error) {
	return nil, nil
}

func (p *MyPlugin) StopProvider(_ context.Context, _ string) error {
	return nil
}

func main() {
	sdkplugin.Serve(&MyPlugin{})
}
