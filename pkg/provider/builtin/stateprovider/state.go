// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package stateprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/state"
)

const (
	// ProviderName is the name of this provider.
	ProviderName = "state"

	// Version is the provider version.
	Version = "1.0.0"
)

// StateProvider gives resolvers and actions explicit read/write access to
// individual state entries. It reads/writes the in-memory StateData loaded
// during the pre-execution phase (via state.FromContext).
type StateProvider struct{}

// New creates a new state provider.
func New() *StateProvider {
	return &StateProvider{}
}

// Descriptor returns the provider's metadata and schema.
func (p *StateProvider) Descriptor() *provider.Descriptor {
	return &provider.Descriptor{
		Name:        ProviderName,
		DisplayName: "State Provider",
		Description: "Provides read/write access to individual state entries for resolvers and actions. Reads from and writes to the in-memory state loaded during pre-execution.",
		APIVersion:  "v1",
		Version:     semver.MustParse(Version),
		Category:    "state",
		Tags:        []string{"state", "persistence", "resolver"},
		Capabilities: []provider.Capability{
			provider.CapabilityFrom,
			provider.CapabilityAction,
		},
		Schema: schemahelper.ObjectSchema([]string{"key"}, map[string]*jsonschema.Schema{
			"key": schemahelper.StringProp("State entry key (typically a resolver name)",
				schemahelper.WithMaxLength(253),
				schemahelper.WithExample("auth_token"),
				schemahelper.WithPattern(`^[a-zA-Z_][a-zA-Z0-9_-]*$`)),
			"required": schemahelper.BoolProp("If true, error when key is not found",
				schemahelper.WithDefault(false)),
			"fallback": schemahelper.AnyProp("Value returned when key is not found and required is false"),
			"value":    schemahelper.AnyProp("Value to store (for write/action mode)"),
			"type": schemahelper.StringProp("Resolver type for the entry (for write/action mode)",
				schemahelper.WithExample("string"),
				schemahelper.WithMaxLength(30)),
			"immutable": schemahelper.BoolProp("Lock value permanently (future enhancement)",
				schemahelper.WithDefault(false)),
		}),
		OutputSchemas: map[provider.Capability]*jsonschema.Schema{
			provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"value": schemahelper.AnyProp("The state entry value"),
			}),
			provider.CapabilityAction: schemahelper.ObjectSchema([]string{"success"}, map[string]*jsonschema.Schema{
				"success": schemahelper.BoolProp("Whether the write succeeded"),
			}),
		},
		// ExtractDependencies is nil -- the state provider's key input is a literal string,
		// not a resolver reference. The DAG builder should not extract dependencies from it.
		WhatIf: func(_ context.Context, input any) (string, error) {
			inputs, ok := input.(map[string]any)
			if !ok {
				return "", nil
			}
			key, _ := inputs["key"].(string)
			if _, hasValue := inputs["value"]; hasValue {
				return fmt.Sprintf("Would write state key %q", key), nil
			}
			return fmt.Sprintf("Would read state key %q", key), nil
		},
	}
}

// Execute runs the state provider.
// In CapabilityFrom mode (resolve phase): reads a key from state.
// In CapabilityAction mode (action phase): writes a key to state.
func (p *StateProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	inputs, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid input type: %T", input)
	}

	key, _ := inputs["key"].(string)
	if key == "" {
		return nil, fmt.Errorf("key is required")
	}

	// Check if this is a write operation (action mode)
	if _, hasValue := inputs["value"]; hasValue {
		return p.write(ctx, key, inputs)
	}

	// Read operation (from mode)
	return p.read(ctx, key, inputs)
}

func (p *StateProvider) read(ctx context.Context, key string, inputs map[string]any) (*provider.Output, error) {
	stateData, ok := state.FromContext(ctx)
	if !ok || stateData == nil {
		// No state in context -- behave as if empty state (first run)
		return p.handleMissing(key, inputs)
	}

	entry, exists := stateData.Values[key]
	if !exists || entry == nil {
		return p.handleMissing(key, inputs)
	}

	return &provider.Output{
		Data: entry.Value,
	}, nil
}

func (p *StateProvider) handleMissing(key string, inputs map[string]any) (*provider.Output, error) {
	required, _ := inputs["required"].(bool)
	if required {
		return nil, fmt.Errorf("state key %q not found: %w", key, state.ErrKeyNotFound)
	}

	// Return fallback value (nil/null if not specified)
	fallback := inputs["fallback"]
	return &provider.Output{
		Data: fallback,
	}, nil
}

func (p *StateProvider) write(ctx context.Context, key string, inputs map[string]any) (*provider.Output, error) {
	stateData, ok := state.FromContext(ctx)
	if !ok || stateData == nil {
		return nil, fmt.Errorf("state not available in context: cannot write key %q", key)
	}

	// Check immutability of existing entry (future enforcement)
	if existing, exists := stateData.Values[key]; exists && existing.Immutable {
		return nil, fmt.Errorf("state key %q: %w", key, state.ErrImmutableEntry)
	}

	value := inputs["value"]
	entryType, _ := inputs["type"].(string)
	immutable, _ := inputs["immutable"].(bool)

	stateData.Values[key] = &state.Entry{
		Value:     value,
		Type:      entryType,
		UpdatedAt: time.Now().UTC(),
		Immutable: immutable,
	}

	return &provider.Output{
		Data: map[string]any{
			"success": true,
		},
	}, nil
}
