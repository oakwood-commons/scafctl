// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import sdkprovider "github.com/oakwood-commons/scafctl-plugin-sdk/provider"

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
)

// Contact represents maintainer contact information.
type Contact = sdkprovider.Contact

// Link represents a named hyperlink.
type Link = sdkprovider.Link

// Example represents a usage example for a provider.
type Example = sdkprovider.Example

// ValidateDescriptor validates that a Descriptor meets all requirements.
func ValidateDescriptor(desc *Descriptor) error { return sdkprovider.ValidateDescriptor(desc) }
