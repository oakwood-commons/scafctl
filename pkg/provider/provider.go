// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"fmt"

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
	// This capability is defined in scafctl (not the SDK) because state is an application-level
	// concern that external plugin providers can also implement.
	CapabilityState Capability = "state"
)

// Contact represents maintainer contact information.
type Contact = sdkprovider.Contact

// Link represents a named hyperlink.
type Link = sdkprovider.Link

// Example represents a usage example for a provider.
type Example = sdkprovider.Example

// ValidateDescriptor validates that a Descriptor meets all requirements.
// It extends the SDK validation with scafctl-specific capabilities (e.g., CapabilityState).
func ValidateDescriptor(desc *Descriptor) error {
	// Temporarily strip scafctl-specific capabilities before calling the SDK validator,
	// then validate them locally. This avoids "unknown capability" errors from the SDK.
	var localCaps []Capability
	var sdkCaps []Capability
	for _, cap := range desc.Capabilities {
		if cap == CapabilityState {
			localCaps = append(localCaps, cap)
		} else {
			sdkCaps = append(sdkCaps, cap)
		}
	}

	// Validate SDK-known capabilities via the SDK validator using a shallow
	// copy so we never mutate the caller's descriptor (avoids data races if
	// registration ever runs concurrently).
	if len(sdkCaps) > 0 {
		sdkDesc := *desc
		sdkDesc.Capabilities = sdkCaps
		if err := sdkprovider.ValidateDescriptor(&sdkDesc); err != nil {
			return err
		}
	}

	// Validate scafctl-specific capabilities locally
	for _, cap := range localCaps {
		schema, exists := desc.OutputSchemas[cap]
		if !exists {
			return fmt.Errorf("missing output schema for capability %q", cap)
		}
		requiredFields := stateCapabilityRequiredFields()
		for fieldName, expectedType := range requiredFields {
			if schema == nil || schema.Properties == nil {
				return fmt.Errorf("capability %q requires output field %q", cap, fieldName)
			}
			prop, found := schema.Properties[fieldName]
			if !found || prop == nil {
				return fmt.Errorf("capability %q requires output field %q", cap, fieldName)
			}
			if prop.Type != expectedType {
				return fmt.Errorf("capability %q field %q must be type %q, got %q", cap, fieldName, expectedType, prop.Type)
			}
		}
	}

	return nil
}

// stateCapabilityRequiredFields returns the required output fields for CapabilityState.
func stateCapabilityRequiredFields() map[string]string {
	return map[string]string{
		"success": "boolean",
	}
}

// IsCapabilityValid checks if the capability is valid, including scafctl-specific capabilities.
func IsCapabilityValid(c Capability) bool {
	if c == CapabilityState {
		return true
	}
	return c.IsValid()
}
