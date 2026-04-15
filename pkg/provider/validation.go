// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import sdkprovider "github.com/oakwood-commons/scafctl-plugin-sdk/provider"

// SchemaValidator provides validation for provider inputs and outputs against JSON Schema definitions.
type SchemaValidator = sdkprovider.SchemaValidator

// NewSchemaValidator creates a new schema validator.
func NewSchemaValidator() *SchemaValidator { return sdkprovider.NewSchemaValidator() }

// ValidationError represents a single field validation error with contextual information.
type ValidationError = sdkprovider.ValidationError

// ValidationErrors is a collection of validation errors.
type ValidationErrors = sdkprovider.ValidationErrors
