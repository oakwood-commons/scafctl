// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package secretprovider implements a resolver provider for accessing encrypted secrets.
// Secrets are retrieved from the scafctl secrets store which uses AES-256-GCM encryption
// with OS keychain integration for master key management.
package secretprovider

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
)

const (
	// Operation types
	OpGet  = "get"
	OpList = "list"

	// Field names
	FieldOperation = "operation"
	FieldName      = "name"
	FieldPattern   = "pattern"
	FieldRequired  = "required"
	FieldFallback  = "fallback"

	// Provider metadata
	ProviderName        = "secret"
	ProviderDisplayName = "Secret"
	ProviderAPIVersion  = "scafctl.dev/v1alpha1"
	ProviderDescription = "Retrieves encrypted secrets from the scafctl secrets store"
)

// SecretOps defines operations for secret access (used for testing)
type SecretOps interface {
	Get(ctx context.Context, name string) ([]byte, error)
	List(ctx context.Context) ([]string, error)
	Exists(ctx context.Context, name string) (bool, error)
}

// storeOpsAdapter adapts secrets.Store to SecretOps interface
type storeOpsAdapter struct {
	store secrets.Store
}

func (a *storeOpsAdapter) Get(ctx context.Context, name string) ([]byte, error) {
	return a.store.Get(ctx, name)
}

func (a *storeOpsAdapter) List(ctx context.Context) ([]string, error) {
	return a.store.List(ctx)
}

func (a *storeOpsAdapter) Exists(ctx context.Context, name string) (bool, error) {
	return a.store.Exists(ctx, name)
}

// SecretProvider implements the Provider interface for secret access
type SecretProvider struct {
	descriptor *provider.Descriptor
	ops        SecretOps
}

// Option configures the SecretProvider
type Option func(*SecretProvider)

// WithSecretStore sets the secrets.Store for the provider
func WithSecretStore(store secrets.Store) Option {
	return func(p *SecretProvider) {
		p.ops = &storeOpsAdapter{store: store}
	}
}

// WithSecretOps sets a custom SecretOps implementation (for testing)
func WithSecretOps(ops SecretOps) Option {
	return func(p *SecretProvider) {
		p.ops = ops
	}
}

// NewSecretProvider creates a new secret provider instance.
// If no store is provided via options, the provider will attempt to get
// the store from the execution context.
func NewSecretProvider(opts ...Option) *SecretProvider {
	p := &SecretProvider{}

	// Apply options
	for _, opt := range opts {
		opt(p)
	}

	// Initialize descriptor
	p.descriptor = &provider.Descriptor{
		Name:         ProviderName,
		DisplayName:  ProviderDisplayName,
		APIVersion:   ProviderAPIVersion,
		Version:      semver.MustParse("1.0.0"),
		Description:  ProviderDescription,
		MockBehavior: "Returns mock secret value without accessing actual encrypted storage",
		Capabilities: []provider.Capability{
			provider.CapabilityFrom,
		},
		Schema:        p.buildSchema(),
		OutputSchemas: p.buildOutputSchemas(),
		Examples:      p.buildExamples(),
	}

	return p
}

// Descriptor returns the provider descriptor
func (p *SecretProvider) Descriptor() *provider.Descriptor {
	return p.descriptor
}

// buildSchema constructs the input schema for the secret provider
func (p *SecretProvider) buildSchema() *jsonschema.Schema {
	return schemahelper.ObjectSchema([]string{FieldOperation}, map[string]*jsonschema.Schema{
		FieldOperation: schemahelper.StringProp("Operation to perform: 'get' (retrieve single secret) or 'list' (list all secrets)",
			schemahelper.WithEnum(OpGet, OpList),
			schemahelper.WithExample(OpGet)),
		FieldName: schemahelper.StringProp("Secret name (for 'get' operation). Required if pattern not specified.",
			schemahelper.WithExample("api-token"),
			schemahelper.WithPattern("^[a-zA-Z0-9._-]+$")),
		FieldPattern: schemahelper.StringProp("Regular expression pattern to match secret names (for 'get' operation). Returns first match.",
			schemahelper.WithExample("^prod-.+$")),
		FieldRequired: schemahelper.BoolProp("If true, error when secret not found. If false, return fallback or empty string.",
			schemahelper.WithExample(true)),
		FieldFallback: schemahelper.StringProp("Value to return when secret not found and required=false",
			schemahelper.WithExample("default-value")),
	})
}

// buildOutputSchemas documents the output format for each operation
func (p *SecretProvider) buildOutputSchemas() map[provider.Capability]*jsonschema.Schema {
	return map[provider.Capability]*jsonschema.Schema{
		provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
			"_result": schemahelper.AnyProp("For 'get' operation: the secret value as a string. For 'list' operation: array of secret names.",
				schemahelper.WithExample("my-secret-value")),
		}),
	}
}

// buildExamples provides configuration examples
func (p *SecretProvider) buildExamples() []provider.Example {
	return []provider.Example{
		{
			Name:        "Get secret by name",
			Description: "Retrieve a specific secret",
			YAML: `name: get-api-token
provider: secret
inputs:
  operation: get
  name: api-token
  required: true`,
		},
		{
			Name:        "Get secret with fallback",
			Description: "Retrieve secret or use default value",
			YAML: `name: get-optional-token
provider: secret
inputs:
  operation: get
  name: optional-token
  required: false
  fallback: default-token`,
		},
		{
			Name:        "Get secret by pattern",
			Description: "Find and retrieve first secret matching regex pattern",
			YAML: `name: get-prod-secret
provider: secret
inputs:
  operation: get
  pattern: ^prod-.+$
  required: true`,
		},
		{
			Name:        "List all secrets",
			Description: "Get names of all stored secrets",
			YAML: `name: list-all-secrets
provider: secret
inputs:
  operation: list`,
		},
	}
}

// Execute performs the secret operation
func (p *SecretProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	// Convert input to map
	inputs, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected map[string]any, got %T", input)
	}

	// Get secrets store from options or fail
	ops := p.ops
	if ops == nil {
		return nil, fmt.Errorf("secret store not configured: use WithSecretStore option")
	}

	// Handle dry-run mode
	if dryRun, ok := inputs["__dry_run__"].(bool); ok && dryRun {
		return &provider.Output{
			Data: map[string]any{
				"_dry_run": true,
				"message":  "dry-run: secret operation would be executed",
			},
		}, nil
	}

	// Get operation type
	operation, ok := inputs[FieldOperation].(string)
	if !ok || operation == "" {
		return nil, fmt.Errorf("operation field is required and must be a string")
	}

	// Route to operation handler
	var data any
	var err error
	switch operation {
	case OpGet:
		data, err = p.executeGet(ctx, ops, inputs)
	case OpList:
		data, err = p.executeList(ctx, ops, inputs)
	default:
		return nil, fmt.Errorf("unsupported operation: %s (must be 'get' or 'list')", operation)
	}

	if err != nil {
		return nil, err
	}

	return &provider.Output{Data: data}, nil
}

// executeGet handles the 'get' operation
func (p *SecretProvider) executeGet(ctx context.Context, ops SecretOps, input map[string]any) (any, error) {
	name, hasName := input[FieldName].(string)
	pattern, hasPattern := input[FieldPattern].(string)

	// Validate: need either name or pattern
	if !hasName && !hasPattern {
		return nil, fmt.Errorf("either 'name' or 'pattern' field is required for get operation")
	}

	// Get required flag (default: true)
	required := true
	if reqVal, ok := input[FieldRequired].(bool); ok {
		required = reqVal
	}

	// Get fallback value
	fallback, _ := input[FieldFallback].(string)

	// Determine secret name to retrieve
	var secretName string
	if hasName {
		secretName = name
	} else {
		// Pattern matching: find first secret that matches
		matched, err := p.findMatchingSecret(ctx, ops, pattern)
		if err != nil {
			return nil, fmt.Errorf("failed to find secret matching pattern %q: %w", pattern, err)
		}
		if matched == "" {
			if required {
				return nil, fmt.Errorf("no secret found matching pattern %q", pattern)
			}
			return fallback, nil
		}
		secretName = matched
	}

	// Retrieve the secret
	value, err := ops.Get(ctx, secretName)
	if err != nil {
		if errors.Is(err, secrets.ErrNotFound) {
			if required {
				return nil, fmt.Errorf("secret %q not found", secretName)
			}
			return fallback, nil
		}
		return nil, fmt.Errorf("failed to get secret %q: %w", secretName, err)
	}

	return string(value), nil
}

// executeList handles the 'list' operation
func (p *SecretProvider) executeList(ctx context.Context, ops SecretOps, _ map[string]any) (any, error) {
	names, err := ops.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list secrets: %w", err)
	}

	return names, nil
}

// findMatchingSecret finds the first secret name matching the given regex pattern
func (p *SecretProvider) findMatchingSecret(ctx context.Context, ops SecretOps, pattern string) (string, error) {
	// Compile regex
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex pattern: %w", err)
	}

	// List all secrets
	names, err := ops.List(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to list secrets: %w", err)
	}

	// Find first match
	for _, name := range names {
		if re.MatchString(name) {
			return name, nil
		}
	}

	return "", nil
}
