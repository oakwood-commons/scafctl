// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	sdkprovider "github.com/oakwood-commons/scafctl-plugin-sdk/provider"
)

// --- SDK type aliases ---

// Provider is the core interface that all providers must implement.
type Provider = sdkprovider.Provider

// Descriptor contains provider identity, versioning, schemas, capabilities, and catalog metadata.
type Descriptor = sdkprovider.Descriptor

// Output is the standardized return structure for all provider executions.
type Output = sdkprovider.Output

// Capability represents the types of operations a provider can perform.
type Capability = sdkprovider.Capability

const (
	CapabilityFrom           = sdkprovider.CapabilityFrom
	CapabilityTransform      = sdkprovider.CapabilityTransform
	CapabilityValidation     = sdkprovider.CapabilityValidation
	CapabilityAuthentication = sdkprovider.CapabilityAuthentication
	CapabilityAction         = sdkprovider.CapabilityAction

	// CapabilityState signals that a provider can act as a state persistence backend.
	// Providers with this capability handle load/save/delete operations for solution state.
	CapabilityState = sdkprovider.CapabilityState

	// CapabilityKubeconfig signals that a provider can perform kubeconfig and cluster
	// operations (write/remove kubeconfig entries, detect auth type, check reachability,
	// whoami). Providers with this capability dispatch on an "operation" input field.
	CapabilityKubeconfig = sdkprovider.CapabilityKubeconfig
)

// Contact represents maintainer contact information.
type Contact = sdkprovider.Contact

// Link represents a named hyperlink.
type Link = sdkprovider.Link

// Example represents a usage example for a provider.
type Example = sdkprovider.Example

// OperationDescriptor is re-exported from the SDK so host code can describe and
// round-trip per-operation metadata (including write classification).
type OperationDescriptor = sdkprovider.OperationDescriptor

// ValidateDescriptor validates that a Descriptor meets all requirements.
// It delegates directly to the SDK validator, which understands all capabilities
// including scafctl-host capabilities (CapabilityState, CapabilityKubeconfig).
// A shallow copy is passed so the caller's top-level fields are never reassigned;
// note this does not deep-copy nested maps/slices (e.g. OutputSchemas), which
// remain shared. The SDK validator is read-only and does not mutate them.
func ValidateDescriptor(desc *Descriptor) error {
	if desc == nil {
		return fmt.Errorf("descriptor is nil")
	}
	sdkDesc := *desc
	return sdkprovider.ValidateDescriptor(&sdkDesc)
}

// IsCapabilityValid reports whether the capability is recognized by the SDK.
func IsCapabilityValid(c Capability) bool {
	return c.IsValid()
}

// builtinProviderNames is an O(1) lookup set of provider names that are
// compiled into the scafctl binary. Built-in providers are always
// pre-registered and must not be fetched from catalogs.
var builtinProviderNames = map[string]bool{
	"http":        true,
	"cel":         true,
	"file":        true,
	"validation":  true,
	"debug":       true,
	"go-template": true,
	"message":     true,
	"static":      true,
	"parameter":   true,
	"solution":    true,
	"state":       true,
}

// IsBuiltinProvider reports whether name is a built-in provider shipped with
// the scafctl binary. Built-in providers are always pre-registered and must
// not be fetched from a catalog.
func IsBuiltinProvider(name string) bool {
	return builtinProviderNames[name]
}

type MockProvider struct {
	ExecuteFunc    func() (*Output, error)
	DescriptorFunc func() *Descriptor
	Name           string
}

func (m *MockProvider) Execute(_ context.Context, _ any) (*Output, error) {
	if m.ExecuteFunc == nil {
		return &Output{}, nil
	}
	return m.ExecuteFunc()
}

func (m *MockProvider) Descriptor() *Descriptor {
	if m.DescriptorFunc == nil {
		return &Descriptor{
			Name:         m.Name,
			APIVersion:   "v1",
			Version:      semver.MustParse("1.0.0"),
			Description:  "A test provider",
			Capabilities: []Capability{CapabilityFrom},
			Schema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"test": {Type: "string"},
				},
			},
			OutputSchemas: map[Capability]*jsonschema.Schema{
				CapabilityFrom: {
					Type: "object",
					Properties: map[string]*jsonschema.Schema{
						"result": {Type: "string"},
					},
				},
			},
		}
	}
	return m.DescriptorFunc()
}
