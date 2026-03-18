// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/logger"
)

// Provider is the core interface that all providers must implement.
// Providers are stateless execution primitives that perform single, well-defined operations.
type Provider interface {
	// Descriptor returns the provider's metadata, schema, and capabilities.
	Descriptor() *Descriptor

	// Execute runs the provider logic with resolved inputs.
	// The input parameter is either:
	//   - map[string]any if Descriptor().Decode is nil
	//   - The decoded type if Descriptor().Decode is set and returns a typed struct
	// Resolver values can be accessed via ResolverContextFromContext(ctx).
	// Execution mode and dry-run flag are available via ExecutionModeFromContext(ctx) and DryRunFromContext(ctx).
	Execute(ctx context.Context, input any) (*Output, error)
}

// Descriptor contains provider identity, versioning, schemas, capabilities, and catalog metadata.
type Descriptor struct {
	// Name is the unique identifier for this provider. Must be lowercase with hyphens only.
	// Used to reference the provider in configurations and the registry.
	Name string `json:"name" yaml:"name" doc:"Unique provider identifier" minLength:"2" maxLength:"100" example:"http" pattern:"^[a-z][a-z0-9-]*$" required:"true"`

	// DisplayName is the human-readable name shown in UIs and documentation.
	// Optional - defaults to Name if not specified.
	DisplayName string `json:"displayName,omitempty" yaml:"displayName,omitempty" doc:"Human-readable display name" maxLength:"100" example:"HTTP Client"`

	// APIVersion indicates the provider API contract version (e.g., "v1").
	// Used for compatibility checking and migration support.
	APIVersion string `json:"apiVersion" yaml:"apiVersion" doc:"Provider API version" maxLength:"16" example:"v1" pattern:"^v[0-9]+$" required:"true"`

	// Version is the semantic version of this provider implementation.
	// Follows semver conventions for versioning provider releases.
	Version *semver.Version `json:"version" yaml:"version" doc:"Semantic version" required:"true"`

	// Description provides a concise explanation of what the provider does.
	// Displayed in catalogs, help text, and documentation.
	Description string `json:"description" yaml:"description" doc:"Provider description" minLength:"10" maxLength:"500" example:"Fetches data over HTTP" required:"true"`

	// Schema defines the structure and validation rules for provider inputs using JSON Schema.
	// Used for input validation, documentation generation, and UI form building.
	Schema *jsonschema.Schema `json:"schema" yaml:"schema" doc:"Input schema (JSON Schema)" required:"true"`

	// OutputSchemas defines the output structure for each supported capability using JSON Schema.
	// Each capability can produce different output shapes. Required for all declared capabilities.
	// Certain capabilities have required minimum fields:
	//   - validation: must include "valid" (boolean) and "errors" (array)
	//   - authentication: must include "authenticated" (boolean) and "token" (string)
	//   - action: must include "success" (boolean)
	//   - from: no required fields
	//   - transform: no required fields
	OutputSchemas map[Capability]*jsonschema.Schema `json:"outputSchemas" yaml:"outputSchemas" doc:"Output schemas per capability (JSON Schema)" required:"true"`

	// SensitiveFields lists property names that contain sensitive data and should be redacted
	// in logs, errors, and snapshot output. Replaces the old per-property IsSecret flag.
	SensitiveFields []string `json:"sensitiveFields,omitempty" yaml:"sensitiveFields,omitempty" doc:"Property names containing sensitive data" maxItems:"50"`

	// Decode converts validated map[string]any inputs into strongly-typed structs for internal use.
	// Called after schema validation but before Execute(). Optional - providers can work with map[string]any directly.
	// When Decode is set, the Executor calls it and passes the result directly to Execute().
	Decode func(map[string]any) (any, error) `json:"-" yaml:"-"`

	// ExtractDependencies extracts resolver dependencies from the provider's inputs.
	// Called during dependency graph building to determine execution order.
	// Optional - if nil, the generic extraction logic is used (which handles common patterns like
	// CEL expressions with _.resolverName and Go templates with {{.resolverName}}).
	// Providers should implement this when they have custom input formats or need special handling
	// (e.g., go-template provider with custom delimiters).
	// The function receives the raw inputs map and returns a slice of resolver names that are referenced.
	ExtractDependencies func(inputs map[string]any) []string `json:"-" yaml:"-"`

	// WhatIf generates a human-readable description of what the provider would do
	// with the given inputs, without executing. Optional — if nil, falls back to
	// a generic message. In solution dry-run, receives the materialized inputs
	// map (map[string]any), not the decoded struct that Execute may receive.
	WhatIf func(ctx context.Context, input any) (string, error) `json:"-" yaml:"-"`

	// Capabilities declares the execution contexts this provider supports.
	// Determines where the provider can be used (from, transform, validation, etc.).
	Capabilities []Capability `json:"capabilities" yaml:"capabilities" doc:"Supported execution contexts" minItems:"1" maxItems:"10" required:"true"`

	// Category classifies the provider for organization in catalogs and documentation.
	// Examples: "network", "storage", "security", "utility".
	Category string `json:"category,omitempty" yaml:"category,omitempty" doc:"Classification category" maxLength:"50" example:"network"`

	// Tags are searchable keywords for discovery and filtering.
	// Used in catalog searches and provider listings.
	Tags []string `json:"tags,omitempty" yaml:"tags,omitempty" doc:"Searchable keywords" maxItems:"20"`

	// Icon is a URL to an image representing the provider.
	// Displayed in UIs and documentation alongside the provider name.
	Icon string `json:"icon,omitempty" yaml:"icon,omitempty" doc:"Icon URL" format:"uri" maxLength:"500" example:"https://example.com/icon.svg"`

	// Links provides related resources such as documentation, source code, or tutorials.
	Links []Link `json:"links,omitempty" yaml:"links,omitempty" doc:"Related links" maxItems:"10"`

	// Examples contains sample configurations demonstrating provider usage.
	// Shown in documentation and can be used for testing.
	Examples []Example `json:"examples,omitempty" yaml:"examples,omitempty" doc:"Usage examples" maxItems:"10"`

	// IsDeprecated indicates the provider should no longer be used.
	// Deprecated providers may be removed in future versions.
	IsDeprecated bool `json:"deprecated,omitempty" yaml:"deprecated,omitempty" doc:"Deprecation status"`

	// Beta indicates the provider is experimental and may have breaking changes.
	// Beta providers are not recommended for production use.
	Beta bool `json:"beta,omitempty" yaml:"beta,omitempty" doc:"Beta status"`

	// Maintainers lists the people or teams responsible for this provider.
	// Used for contact and support information.
	Maintainers []Contact `json:"maintainers,omitempty" yaml:"maintainers,omitempty" doc:"Maintainer contacts" maxItems:"10"`
}

// Output is the standardized return structure for all provider executions.
type Output struct {
	Data     any            `json:"data" yaml:"data" doc:"Provider output data" required:"true"`
	Warnings []string       `json:"warnings,omitempty" yaml:"warnings,omitempty" doc:"Non-fatal warning messages" maxItems:"50"`
	Metadata map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty" doc:"Execution metadata"`
	// Streamed indicates that the provider already wrote its primary output
	// (e.g., stdout/stderr) directly to the terminal via IOStreams from context.
	// When true, the CLI output layer should not re-print the streamed content.
	Streamed bool `json:"streamed,omitempty" yaml:"streamed,omitempty" doc:"Whether output was already streamed to terminal"`
}

// DescribeWhatIf returns a human-readable description of what the provider would do.
// Calls WhatIf if set, falls back to a generic message.
func (d *Descriptor) DescribeWhatIf(ctx context.Context, input any) string {
	if d.WhatIf != nil {
		msg, err := d.WhatIf(ctx, input)
		if err != nil {
			lgr := logger.FromContext(ctx)
			lgr.V(1).Info("WhatIf function returned error, falling back to generic message",
				"provider", d.Name, "error", err)
		} else if msg != "" {
			return msg
		}
		// On error or empty result, fall through to generic message
	}
	return fmt.Sprintf("Would execute %s provider", d.Name)
}

// IsSensitiveField checks whether a field name is marked as sensitive in the descriptor.
func (d *Descriptor) IsSensitiveField(name string) bool {
	for _, f := range d.SensitiveFields {
		if f == name {
			return true
		}
	}
	return false
}

// Capability represents the types of operations a provider can perform.
type Capability string

const (
	CapabilityFrom           Capability = "from"
	CapabilityTransform      Capability = "transform"
	CapabilityValidation     Capability = "validation"
	CapabilityAuthentication Capability = "authentication"
	CapabilityAction         Capability = "action"
)

// IsValid checks if the capability is valid.
func (c Capability) IsValid() bool {
	switch c {
	case CapabilityFrom, CapabilityTransform, CapabilityValidation, CapabilityAuthentication, CapabilityAction:
		return true
	default:
		return false
	}
}

// String returns the string representation.
func (c Capability) String() string {
	return string(c)
}

// Contact represents maintainer contact information.
type Contact struct {
	Name  string `json:"name,omitempty" yaml:"name,omitempty" doc:"Maintainer name" maxLength:"60" example:"Jane Doe"`
	Email string `json:"email,omitempty" yaml:"email,omitempty" doc:"Maintainer email" format:"email" maxLength:"100"`
}

// Link represents a named hyperlink.
type Link struct {
	Name string `json:"name,omitempty" yaml:"name,omitempty" doc:"Link name" maxLength:"30" example:"Documentation"`
	URL  string `json:"url,omitempty" yaml:"url,omitempty" doc:"Link URL" format:"uri" maxLength:"500"`
}

// Example represents a usage example for a provider.
type Example struct {
	Name        string `json:"name,omitempty" yaml:"name,omitempty" doc:"Example name" maxLength:"50" example:"Basic usage"`
	Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"Example description" maxLength:"300" example:"Basic HTTP GET request"`
	YAML        string `json:"yaml" yaml:"yaml" doc:"YAML example" minLength:"10" maxLength:"2000" required:"true"`
}

// capabilityRequiredFields maps capabilities to their required output fields and expected JSON Schema types.
var capabilityRequiredFields = map[Capability]map[string]string{
	CapabilityFrom:      {},
	CapabilityTransform: {},
	CapabilityValidation: {
		"valid":  "boolean",
		"errors": "array",
	},
	CapabilityAuthentication: {
		"authenticated": "boolean",
		"token":         "string",
	},
	CapabilityAction: {
		"success": "boolean",
	},
}

// ValidateDescriptor validates that a Descriptor meets all requirements.
// Returns an error if:
//   - OutputSchemas is missing for any declared capability
//   - Required fields are missing for capabilities that mandate them
//   - Field types don't match the expected JSON Schema types
func ValidateDescriptor(desc *Descriptor) error {
	if desc == nil {
		return errors.New("descriptor is nil")
	}

	if len(desc.Capabilities) == 0 {
		return errors.New("descriptor must declare at least one capability")
	}

	if desc.OutputSchemas == nil {
		return errors.New("descriptor must define OutputSchemas")
	}

	var errs []error

	for _, cap := range desc.Capabilities {
		schema, exists := desc.OutputSchemas[cap]
		if !exists {
			errs = append(errs, fmt.Errorf("missing output schema for capability %q", cap))
			continue
		}

		requiredFields := capabilityRequiredFields[cap]
		for fieldName, expectedType := range requiredFields {
			if schema == nil || schema.Properties == nil {
				errs = append(errs, fmt.Errorf("capability %q requires output field %q", cap, fieldName))
				continue
			}
			prop, found := schema.Properties[fieldName]
			if !found {
				errs = append(errs, fmt.Errorf("capability %q requires output field %q", cap, fieldName))
				continue
			}
			if prop.Type != expectedType {
				errs = append(errs, fmt.Errorf("capability %q field %q must be type %q, got %q", cap, fieldName, expectedType, prop.Type))
			}
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}
