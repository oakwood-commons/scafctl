// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package stateprovider implements the read-only "state" provider. It lets a
// resolver read a previously persisted resolver value out of the state snapshot
// loaded at the start of the run -- i.e. the value from the PRIOR run, before
// the current run overwrites it at save time.
package stateprovider

import (
	"context"
	"fmt"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/ptrs"
	"github.com/oakwood-commons/scafctl/pkg/state"
)

const (
	// ProviderName is the name of the state provider.
	ProviderName = "state"
	// Version is the version of the state provider.
	Version = "1.0.0"
	// OperationGet reads a persisted value from the loaded state snapshot.
	OperationGet = "get"
)

// keyPattern matches a persisted resolver key. It mirrors the resolver name
// grammar so authors cannot pass expressions or path traversals as a key.
const keyPattern = `^[A-Za-z_][A-Za-z0-9_.\-]*$`

// StateProvider reads previously persisted resolver values from the state
// snapshot carried in the execution context. It performs no I/O and never
// re-reads the backend from disk, which guarantees it observes the prior run's
// value rather than the current run's not-yet-written overwrite.
type StateProvider struct {
	descriptor *provider.Descriptor
}

// New creates a new state provider instance.
func New() *StateProvider {
	version, _ := semver.NewVersion(Version)

	return &StateProvider{
		descriptor: &provider.Descriptor{
			Name:        ProviderName,
			DisplayName: "Persisted State",
			Description: "Reads a previously persisted resolver value from the state snapshot loaded at the start of the run (the prior run's value).",
			Version:     version,
			APIVersion:  "v1",
			Category:    "system",
			Tags:        []string{"state", "persist", "read"},
			Capabilities: []provider.Capability{
				provider.CapabilityFrom,
			},
			Schema: schemahelper.ObjectSchema([]string{"key"}, map[string]*jsonschema.Schema{
				"operation": schemahelper.StringProp("The state operation to perform. Only \"get\" is supported; it reads a persisted value from the loaded snapshot.",
					schemahelper.WithEnum(OperationGet),
					schemahelper.WithExample(OperationGet)),
				"key": schemahelper.StringProp("Name of the persisted resolver whose prior-run value to read. Treated as an opaque string; it never creates a dependency edge in the resolver graph.",
					schemahelper.WithMaxLength(*ptrs.IntPtr(256)),
					schemahelper.WithPattern(keyPattern),
					schemahelper.WithExample("db_password")),
				"default": schemahelper.AnyProp("Value to return when the key has no persisted entry (e.g. on the first run). When omitted, a missing key returns null.",
					schemahelper.WithExample("")),
			}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"value":  schemahelper.AnyProp("The persisted value from the prior run, or the default (or null) when absent", schemahelper.WithExample("prior-value")),
					"exists": schemahelper.BoolProp("Whether a persisted entry existed for the key", schemahelper.WithExample(true)),
				}),
			},
			WhatIf: func(_ context.Context, input any) (string, error) {
				inputs, ok := input.(map[string]any)
				if !ok {
					return "", nil
				}
				if key, ok := inputs["key"].(string); ok {
					return fmt.Sprintf("Would read persisted state value for key %q", key), nil
				}
				return "Would read a persisted state value", nil
			},
			Examples: []provider.Example{
				{
					Name:        "Read a prior persisted value",
					Description: "Read the value db_password persisted on the previous run, falling back to an empty string on the first run",
					YAML: `provider: state
operation: get
key: db_password
default: ""`,
				},
			},
		},
	}
}

// Descriptor returns the provider's descriptor.
func (p *StateProvider) Descriptor() *provider.Descriptor {
	return p.descriptor
}

// Execute reads a persisted value from the state snapshot in the context.
func (p *StateProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	inputs, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected map[string]any, got %T", ProviderName, input)
	}

	lgr.V(1).Info("executing provider", "provider", ProviderName)

	operation := OperationGet
	if raw, ok := inputs["operation"]; ok {
		op, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("%s: operation must be a string, got %T", ProviderName, raw)
		}
		if op != "" {
			operation = op
		}
	}
	if operation != OperationGet {
		return nil, fmt.Errorf("%s: unsupported operation %q (only %q is supported)", ProviderName, operation, OperationGet)
	}

	key, ok := inputs["key"].(string)
	if !ok || key == "" {
		return nil, fmt.Errorf("%s: missing required input: key", ProviderName)
	}

	def, hasDefault := inputs["default"]

	// Read the value from the loaded snapshot only. When state is disabled or a
	// persisted entry does not exist, fall back to the default (or null).
	stateData, ok := state.FromContext(ctx)
	if ok && stateData != nil {
		if entry, exists := stateData.Resolvers[key]; exists && entry != nil {
			lgr.V(1).Info("provider completed", "provider", ProviderName, "key", key, "exists", true)
			return &provider.Output{
				Data:     entry.Value,
				Metadata: map[string]any{"exists": true},
			}, nil
		}
	}

	var value any
	if hasDefault {
		value = def
	}
	lgr.V(1).Info("provider completed", "provider", ProviderName, "key", key, "exists", false)
	return &provider.Output{
		Data:     value,
		Metadata: map[string]any{"exists": false},
	}, nil
}
