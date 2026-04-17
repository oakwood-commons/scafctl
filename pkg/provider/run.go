// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// RunOptions configures a direct provider execution.
type RunOptions struct {
	// Provider is the resolved provider to execute.  Required.
	Provider Provider `json:"-" yaml:"-"`

	// Inputs are the key-value pairs passed to the provider.
	Inputs map[string]any `json:"inputs" yaml:"inputs" doc:"Provider input parameters"`

	// Capability is the capability to execute.
	// When empty, the first declared capability is used.
	Capability string `json:"capability,omitempty" yaml:"capability,omitempty" doc:"Capability to execute (from, transform, validation, authentication, action)" maxLength:"64"`

	// DryRun shows what would be executed without running the provider.
	DryRun bool `json:"dryRun,omitempty" yaml:"dryRun,omitempty" doc:"Preview execution without side effects"`
}

// RunResult is the structured output of a direct provider execution.
type RunResult struct {
	// Provider is the name of the provider that was executed.
	Provider string `json:"provider" yaml:"provider" doc:"Provider name"`

	// Capability is the capability that was executed.
	Capability string `json:"capability" yaml:"capability" doc:"Capability that was executed"`

	// Data is the provider's output data.
	Data any `json:"data" yaml:"data" doc:"Provider output data"`

	// Warnings from the provider execution.
	Warnings []string `json:"warnings,omitempty" yaml:"warnings,omitempty" doc:"Provider warnings"`

	// Metadata from the provider execution.
	Metadata map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty" doc:"Provider metadata"`

	// DryRun indicates this was a dry-run (no side effects).
	DryRun bool `json:"dryRun,omitempty" yaml:"dryRun,omitempty" doc:"Whether this was a dry-run"`

	// Duration is the execution time.
	Duration time.Duration `json:"duration" yaml:"duration" doc:"Execution duration"`
}

// ResolveCapability determines which capability to use for execution.
// If capabilityName is non-empty it is validated against the provider's
// declared capabilities.  Otherwise the first declared capability is used.
func ResolveCapability(desc *Descriptor, capabilityName string) (Capability, error) {
	if capabilityName != "" {
		requested := Capability(capabilityName)
		if !requested.IsValid() {
			return "", fmt.Errorf("invalid capability %q (valid: from, transform, validation, authentication, action)", capabilityName)
		}
		for _, c := range desc.Capabilities {
			if c == requested {
				return requested, nil
			}
		}
		capStrs := make([]string, len(desc.Capabilities))
		for i, c := range desc.Capabilities {
			capStrs[i] = string(c)
		}
		return "", fmt.Errorf("provider %q does not support capability %q (supported: %v)", desc.Name, capabilityName, capStrs)
	}

	if len(desc.Capabilities) == 0 {
		return "", fmt.Errorf("provider %q declares no capabilities", desc.Name)
	}
	return desc.Capabilities[0], nil
}

// ValidateInputKeys checks that all keys in inputs exist in the provider
// schema's declared properties.  Returns an error listing unrecognized keys
// alongside the full list of valid keys.
func ValidateInputKeys(inputs map[string]any, desc *Descriptor) error {
	if desc.Schema == nil || len(desc.Schema.Properties) == 0 {
		return nil
	}

	validKeys := make(map[string]struct{}, len(desc.Schema.Properties))
	for k := range desc.Schema.Properties {
		validKeys[k] = struct{}{}
	}

	var unknown []string
	for k := range inputs {
		if _, ok := validKeys[k]; !ok {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)

	valid := make([]string, 0, len(validKeys))
	for k := range validKeys {
		valid = append(valid, k)
	}
	sort.Strings(valid)

	return fmt.Errorf("unknown input keys for provider %q: %v (valid keys: %v)", desc.Name, unknown, valid)
}

// RunProvider executes a provider with the given options.  It resolves the
// capability, validates inputs, sets up the execution context, and calls
// Execute.  This is the shared domain entry point used by both the CLI
// command and the MCP tool.
func RunProvider(ctx context.Context, opts RunOptions) (*RunResult, error) {
	if opts.Provider == nil {
		return nil, fmt.Errorf("provider is required")
	}

	desc := opts.Provider.Descriptor()
	if desc == nil {
		return nil, fmt.Errorf("provider returned a nil descriptor")
	}

	// Resolve capability
	capability, err := ResolveCapability(desc, opts.Capability)
	if err != nil {
		return nil, err
	}

	// Validate input keys against schema
	if err := ValidateInputKeys(opts.Inputs, desc); err != nil {
		return nil, err
	}

	// Set execution context
	ctx = WithExecutionMode(ctx, capability)
	ctx = WithDryRun(ctx, opts.DryRun)

	// Execute
	start := time.Now()
	result, err := Execute(ctx, opts.Provider, opts.Inputs)
	elapsed := time.Since(start)
	if err != nil {
		return nil, fmt.Errorf("provider execution failed: %w", err)
	}

	return &RunResult{
		Provider:   desc.Name,
		Capability: string(capability),
		Data:       result.Output.Data,
		Warnings:   result.Output.Warnings,
		Metadata:   result.Output.Metadata,
		DryRun:     result.DryRun,
		Duration:   elapsed,
	}, nil
}
