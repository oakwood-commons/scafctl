package envprovider

import (
	"context"
	"fmt"
	"os"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/ptrs"
)

const (
	// ProviderName is the name of the environment provider
	ProviderName = "env"
	// Version is the version of the environment provider
	Version = "1.0.0"
)

// EnvOps defines the interface for environment variable operations
type EnvOps interface {
	LookupEnv(key string) (string, bool)
	Setenv(key, value string) error
	Unsetenv(key string) error
	Environ() []string
}

// DefaultEnvOps provides real OS environment operations
type DefaultEnvOps struct{}

// LookupEnv looks up an environment variable
func (d *DefaultEnvOps) LookupEnv(key string) (string, bool) {
	return os.LookupEnv(key)
}

// Setenv sets an environment variable
func (d *DefaultEnvOps) Setenv(key, value string) error {
	return os.Setenv(key, value)
}

// Unsetenv unsets an environment variable
func (d *DefaultEnvOps) Unsetenv(key string) error {
	return os.Unsetenv(key)
}

// Environ returns all environment variables
func (d *DefaultEnvOps) Environ() []string {
	return os.Environ()
}

// EnvProvider provides environment variable operations
type EnvProvider struct {
	descriptor *provider.Descriptor
	envOps     EnvOps
}

// Option is a functional option for configuring EnvProvider
type Option func(*EnvProvider)

// WithEnvOps sets custom environment operations for the provider
func WithEnvOps(ops EnvOps) Option {
	return func(p *EnvProvider) {
		p.envOps = ops
	}
}

// NewEnvProvider creates a new environment variable provider
func NewEnvProvider(opts ...Option) *EnvProvider {
	version, _ := semver.NewVersion(Version)

	p := &EnvProvider{
		descriptor: &provider.Descriptor{
			Name:         ProviderName,
			DisplayName:  "Environment Variables",
			APIVersion:   "v1",
			Description:  "Provider for reading and setting environment variables",
			Version:      version,
			Category:     "system",
			MockBehavior: "Returns mock environment variable value without accessing actual environment",
			Capabilities: []provider.Capability{
				provider.CapabilityFrom,
			},
			Schema: provider.SchemaDefinition{
				Properties: map[string]provider.PropertyDefinition{
					"operation": {
						Type:        provider.PropertyTypeString,
						Description: "Operation to perform: 'get' to read a variable, 'set' to set a variable, 'list' to list all variables, 'unset' to remove a variable",
						Required:    true,
						Enum:        []any{"get", "set", "list", "unset"},
						Example:     "get",
						MaxLength:   ptrs.IntPtr(10),
					},
					"name": {
						Type:        provider.PropertyTypeString,
						Description: "Name of the environment variable (required for get, set, unset operations)",
						MaxLength:   ptrs.IntPtr(256),
						Pattern:     `^[A-Za-z_][A-Za-z0-9_]*$`,
						Example:     "HOME",
					},
					"value": {
						Type:        provider.PropertyTypeString,
						Description: "Value to set (required for set operation)",
						MaxLength:   ptrs.IntPtr(4096),
						Example:     "/home/user",
					},
					"default": {
						Type:        provider.PropertyTypeString,
						Description: "Default value to return if variable is not set (only for get operation)",
						MaxLength:   ptrs.IntPtr(4096),
						Example:     "default-value",
					},
					"prefix": {
						Type:        provider.PropertyTypeString,
						Description: "Filter environment variables by prefix (only for list operation)",
						MaxLength:   ptrs.IntPtr(256),
						Example:     "AWS_",
					},
				},
			},
			OutputSchemas: map[provider.Capability]provider.SchemaDefinition{
				provider.CapabilityFrom: {
					Properties: map[string]provider.PropertyDefinition{
						"operation": {
							Type:        provider.PropertyTypeString,
							Description: "Operation that was performed",
							Example:     "get",
						},
						"name": {
							Type:        provider.PropertyTypeString,
							Description: "Name of the environment variable (for get, set, unset operations)",
							Example:     "HOME",
						},
						"value": {
							Type:        provider.PropertyTypeString,
							Description: "Value of the environment variable (for get operation)",
							Example:     "/home/user",
						},
						"exists": {
							Type:        provider.PropertyTypeBool,
							Description: "Whether the variable exists (for get operation)",
							Example:     true,
						},
						"variables": {
							Type:        provider.PropertyTypeAny,
							Description: "Map of environment variables (for list operation)",
							Example:     map[string]string{"HOME": "/home/user", "PATH": "/usr/bin"},
						},
						"count": {
							Type:        provider.PropertyTypeInt,
							Description: "Number of variables (for list operation)",
							Example:     10,
						},
					},
				},
			},
			Examples: []provider.Example{
				{
					Name:        "Get environment variable",
					Description: "Read an environment variable with a default value fallback",
					YAML: `name: get-home
provider: env
inputs:
  operation: get
  name: HOME
  default: "/home/default"`,
				},
				{
					Name:        "Set environment variable",
					Description: "Set an environment variable for the current process",
					YAML: `name: set-api-key
provider: env
inputs:
  operation: set
  name: API_KEY
  value: "secret-key-value"`,
				},
				{
					Name:        "List environment variables",
					Description: "List all environment variables with a specific prefix",
					YAML: `name: list-aws-vars
provider: env
inputs:
  operation: list
  prefix: "AWS_"`,
				},
				{
					Name:        "Unset environment variable",
					Description: "Remove an environment variable from the current process",
					YAML: `name: unset-temp-var
provider: env
inputs:
  operation: unset
  name: TEMP_VAR`,
				},
			},
		},
		envOps: &DefaultEnvOps{},
	}

	// Apply options
	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Descriptor returns the provider's descriptor
func (p *EnvProvider) Descriptor() *provider.Descriptor {
	return p.descriptor
}

// Execute performs the environment variable operation
func (p *EnvProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	inputs, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected map[string]any, got %T", ProviderName, input)
	}
	operation, ok := inputs["operation"].(string)
	if !ok {
		return nil, fmt.Errorf("%s: operation is required and must be a string", ProviderName)
	}

	lgr.V(1).Info("executing provider", "provider", ProviderName, "operation", operation)

	// Check for dry-run mode
	if dryRun := provider.DryRunFromContext(ctx); dryRun {
		return p.executeDryRun(operation, inputs)
	}

	var result *provider.Output
	var err error

	switch operation {
	case "get":
		result, err = p.executeGet(inputs)
	case "set":
		result, err = p.executeSet(inputs)
	case "list":
		result, err = p.executeList(inputs)
	case "unset":
		result, err = p.executeUnset(inputs)
	default:
		return nil, fmt.Errorf("%s: unsupported operation: %s", ProviderName, operation)
	}

	if err != nil {
		return nil, fmt.Errorf("%s: %w", ProviderName, err)
	}

	lgr.V(1).Info("provider execution completed", "provider", ProviderName, "operation", operation)

	return result, nil
}

func (p *EnvProvider) executeGet(inputs map[string]any) (*provider.Output, error) {
	name, ok := inputs["name"].(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("name is required for get operation")
	}

	value, exists := p.envOps.LookupEnv(name)
	if !exists {
		// Use default value if provided
		if defaultValue, ok := inputs["default"].(string); ok {
			value = defaultValue
		}
	}

	return &provider.Output{
		Data: map[string]any{
			"operation": "get",
			"name":      name,
			"value":     value,
			"exists":    exists,
		},
	}, nil
}

func (p *EnvProvider) executeSet(inputs map[string]any) (*provider.Output, error) {
	name, ok := inputs["name"].(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("name is required for set operation")
	}

	value, ok := inputs["value"].(string)
	if !ok {
		return nil, fmt.Errorf("value is required for set operation")
	}

	if err := p.envOps.Setenv(name, value); err != nil {
		return nil, fmt.Errorf("failed to set environment variable: %w", err)
	}

	return &provider.Output{
		Data: map[string]any{
			"operation": "set",
			"name":      name,
			"value":     value,
		},
		Metadata: map[string]any{
			"warning": "Environment variable changes only affect the current process",
		},
	}, nil
}

//nolint:unparam // Error return kept for consistent interface - may return errors in future
func (p *EnvProvider) executeList(inputs map[string]any) (*provider.Output, error) {
	envVars := make(map[string]string)
	prefix, _ := inputs["prefix"].(string)

	// Get all environment variables
	for _, env := range p.envOps.Environ() {
		// Parse "KEY=VALUE" format
		for i := 0; i < len(env); i++ {
			if env[i] == '=' {
				key := env[:i]
				value := env[i+1:]

				// Apply prefix filter if specified
				if prefix == "" || len(key) >= len(prefix) && key[:len(prefix)] == prefix {
					envVars[key] = value
				}
				break
			}
		}
	}

	return &provider.Output{
		Data: map[string]any{
			"operation": "list",
			"variables": envVars,
			"count":     len(envVars),
		},
	}, nil
}

func (p *EnvProvider) executeUnset(inputs map[string]any) (*provider.Output, error) {
	name, ok := inputs["name"].(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("name is required for unset operation")
	}

	if err := p.envOps.Unsetenv(name); err != nil {
		return nil, fmt.Errorf("failed to unset environment variable: %w", err)
	}

	return &provider.Output{
		Data: map[string]any{
			"operation": "unset",
			"name":      name,
		},
		Metadata: map[string]any{
			"warning": "Environment variable changes only affect the current process",
		},
	}, nil
}

func (p *EnvProvider) executeDryRun(operation string, inputs map[string]any) (*provider.Output, error) {
	result := map[string]any{
		"operation": operation,
	}

	// Include operation-specific fields in dry-run output
	switch operation {
	case "get":
		if name, ok := inputs["name"].(string); ok {
			result["name"] = name
			result["value"] = "[DRY-RUN] Value not retrieved"
			result["exists"] = false
		}
	case "set":
		if name, ok := inputs["name"].(string); ok {
			result["name"] = name
		}
		if value, ok := inputs["value"].(string); ok {
			result["value"] = value
		}
	case "list":
		result["variables"] = map[string]string{}
		result["count"] = 0
	case "unset":
		if name, ok := inputs["name"].(string); ok {
			result["name"] = name
		}
	}

	return &provider.Output{
		Data: result,
		Metadata: map[string]any{
			"dryRun": true,
		},
	}, nil
}
