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
	"regexp"
	"sort"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
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

// keyRe is the compiled form of keyPattern. The schema enforces the pattern on
// the single-key input; map mode ("keys" list) validates each entry at runtime.
var keyRe = regexp.MustCompile(keyPattern)

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
			Schema: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"operation": schemahelper.StringProp("The state operation to perform. Only \"get\" is supported; it reads persisted value(s) from the loaded snapshot.",
					schemahelper.WithEnum(OperationGet),
					schemahelper.WithExample(OperationGet)),
				"key": schemahelper.StringProp("Name of a single persisted resolver whose prior-run value to read. Returns the value directly. Treated as an opaque string; it never creates a dependency edge in the resolver graph. Mutually exclusive with \"keys\" and \"all\".",
					schemahelper.WithMaxLength(*ptrs.IntPtr(256)),
					schemahelper.WithPattern(keyPattern),
					schemahelper.WithExample("db_password")),
				"keys": schemahelper.ArrayProp("Explicit set of persisted keys to read as a map. Absent keys are OMITTED from the returned map (not defaulted) so has() and optional chaining stay faithful. Mutually exclusive with \"key\" and \"all\".",
					schemahelper.WithItems(schemahelper.StringProp("A persisted resolver key",
						schemahelper.WithMaxLength(*ptrs.IntPtr(256)),
						schemahelper.WithPattern(keyPattern))),
					schemahelper.WithExample([]any{"keyA", "keyB"})),
				"all": schemahelper.BoolProp("When true, read the entire persisted snapshot as a map (every persisted resolver's prior-run value). Mutually exclusive with \"key\" and \"keys\".",
					schemahelper.WithExample(true)),
				"default": schemahelper.AnyProp("Value to return when the key has no persisted entry (e.g. on the first run). Only valid with \"key\"; in map mode absent keys are omitted instead. When omitted, a missing key returns null.",
					schemahelper.WithExample("")),
			}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom: schemahelper.AnyProp("In single-key mode the persisted value (or the default/null when absent). In map mode (\"keys\"/\"all\") a map of present keys to their persisted values, with absent keys omitted."),
			},
			WhatIf: func(_ context.Context, input any) (string, error) {
				inputs, ok := input.(map[string]any)
				if !ok {
					return "", nil
				}
				if all, ok := inputs["all"].(bool); ok && all {
					return "Would read the entire persisted state snapshot as a map", nil
				}
				if _, ok := inputs["keys"]; ok {
					return "Would read the requested persisted state keys as a map", nil
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
inputs:
  operation: get
  key: db_password
  default: ""`,
				},
				{
					Name:        "Read a set of persisted keys as a map",
					Description: "Read several persisted keys at once. Absent keys are omitted, so has(_.myState.keyB) stays faithful.",
					YAML: `provider: state
inputs:
  operation: get
  keys: [keyA, keyB, keyC]`,
				},
				{
					Name:        "Read the whole persisted snapshot",
					Description: "Read every persisted resolver value as a single map.",
					YAML: `provider: state
inputs:
  operation: get
  all: true`,
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

	// Determine the read mode. Exactly one of key / keys / all selects a mode.
	_, hasKey := inputs["key"]
	rawKeys, hasKeys := inputs["keys"]

	allMode := false
	if raw, ok := inputs["all"]; ok {
		b, ok := raw.(bool)
		if !ok {
			return nil, fmt.Errorf("%s: all must be a boolean, got %T", ProviderName, raw)
		}
		// Only all:true selects the whole-snapshot mode; all:false is inert.
		allMode = b
	}

	selectors := 0
	if hasKey {
		selectors++
	}
	if hasKeys {
		selectors++
	}
	if allMode {
		selectors++
	}
	if selectors != 1 {
		return nil, fmt.Errorf(`%s: specify exactly one of "key", "keys", or "all"`, ProviderName)
	}

	def, hasDefault := inputs["default"]

	// Map mode: read a set of keys (or the whole snapshot) into a map, omitting
	// absent keys so has()/optional chaining remain faithful.
	if hasKeys || allMode {
		if hasDefault {
			return nil, fmt.Errorf(`%s: "default" is only valid with a single "key"; in map mode absent keys are omitted (use has()/optional chaining)`, ProviderName)
		}
		return p.executeMapGet(ctx, lgr, rawKeys, allMode)
	}

	// Single-key mode.
	key, ok := inputs["key"].(string)
	if !ok || key == "" {
		return nil, fmt.Errorf("%s: missing required input: key", ProviderName)
	}

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

// executeMapGet reads either an explicit set of keys or the entire persisted
// snapshot into a map. Absent keys are omitted from the returned map (rather
// than defaulted) so that has() and optional chaining against the map stay
// faithful. The present-key list (and, in keys mode, the requested-but-absent
// list) is reported in metadata; the resolver value is the bare map.
func (p *StateProvider) executeMapGet(ctx context.Context, lgr *logr.Logger, rawKeys any, allMode bool) (*provider.Output, error) {
	stateData, _ := state.FromContext(ctx)

	result := make(map[string]any)
	presentKeys := []string{}
	missing := []string{}

	if allMode {
		if stateData != nil {
			for k, entry := range stateData.Resolvers {
				if entry == nil {
					continue
				}
				result[k] = entry.Value
				presentKeys = append(presentKeys, k)
			}
		}
		sort.Strings(presentKeys)
	} else {
		requested, err := toStringKeys(rawKeys)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ProviderName, err)
		}
		for i, k := range requested {
			if !keyRe.MatchString(k) {
				return nil, fmt.Errorf("%s: invalid key %q at keys[%d]", ProviderName, k, i)
			}
		}
		for _, k := range requested {
			if stateData != nil {
				if entry, exists := stateData.Resolvers[k]; exists && entry != nil {
					result[k] = entry.Value
					presentKeys = append(presentKeys, k)
					continue
				}
			}
			missing = append(missing, k)
		}
	}

	metadata := map[string]any{
		"mode": "map",
		"keys": presentKeys,
	}
	if !allMode {
		metadata["missing"] = missing
	}
	lgr.V(1).Info("provider completed", "provider", ProviderName, "mode", "map", "present", len(presentKeys), "missing", len(missing))
	return &provider.Output{Data: result, Metadata: metadata}, nil
}

// toStringKeys normalizes the "keys" input into a []string. It accepts both
// []string and []any (the shape a CEL list or YAML sequence produces).
func toStringKeys(raw any) ([]string, error) {
	switch v := raw.(type) {
	case []string:
		return v, nil
	case []any:
		out := make([]string, 0, len(v))
		for i, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("keys[%d] must be a string, got %T", i, item)
			}
			out = append(out, s)
		}
		return out, nil
	case nil:
		return nil, fmt.Errorf(`"keys" must be an array of strings`)
	default:
		return nil, fmt.Errorf(`"keys" must be an array of strings, got %T`, raw)
	}
}
