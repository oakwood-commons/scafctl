package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
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
func (p *EchoPlugin) GetProviderDescriptor(ctx context.Context, providerName string) (*provider.Descriptor, error) {
	if providerName != "echo" {
		return nil, fmt.Errorf("unknown provider: %s", providerName)
	}

	maxLen := 1000
	return &provider.Descriptor{
		Name:        "echo",
		DisplayName: "Echo Provider",
		Description: "A simple provider that echoes its input",
		APIVersion:  "v1",
		Version:     semver.MustParse("1.0.0"),
		Category:    "utility",
		Capabilities: []provider.Capability{
			provider.CapabilityTransform,
		},
		Schema: provider.SchemaDefinition{
			Properties: map[string]provider.PropertyDefinition{
				"message": {
					Type:        provider.PropertyTypeString,
					Required:    true,
					Description: "The message to echo",
					Example:     "Hello, World!",
					MaxLength:   &maxLen,
				},
				"uppercase": {
					Type:        provider.PropertyTypeBool,
					Required:    false,
					Description: "Whether to convert the message to uppercase",
					Default:     false,
				},
			},
		},
		OutputSchemas: map[provider.Capability]provider.SchemaDefinition{
			provider.CapabilityTransform: {
				Properties: map[string]provider.PropertyDefinition{
					"echoed": {
						Type:        provider.PropertyTypeString,
						Description: "The echoed message",
					},
				},
			},
		},
	}, nil
}

// ExecuteProvider executes the echo provider
//
//nolint:revive // ctx required by interface
func (p *EchoPlugin) ExecuteProvider(ctx context.Context, providerName string, input map[string]any) (*provider.Output, error) {
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

	return &provider.Output{
		Data: map[string]any{
			"echoed": result,
		},
	}, nil
}

func main() {
	plugin.Serve(&EchoPlugin{})
}
