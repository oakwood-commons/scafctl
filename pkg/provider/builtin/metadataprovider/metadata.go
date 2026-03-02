// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package metadataprovider

import (
	"context"
	"fmt"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
)

// ProviderName is the name of this provider.
const ProviderName = "metadata"

// MetadataProvider returns solution metadata fields as resolved data.
// This lets resolvers expose metadata (name, version, description, etc.)
// for use in templates and downstream providers without hard-coding values.
type MetadataProvider struct{}

// New creates a new metadata provider instance.
func New() *MetadataProvider {
	return &MetadataProvider{}
}

// Descriptor returns the provider's metadata and schema.
func (p *MetadataProvider) Descriptor() *provider.Descriptor {
	return &provider.Descriptor{
		Name:        ProviderName,
		DisplayName: "Metadata Provider",
		APIVersion:  "v1",
		Version:     semver.MustParse("1.0.0"),
		Description: "Returns structured metadata about a solution. Accepts arbitrary key-value pairs and returns them as a map, optionally returning a single field by key. Useful for exposing solution name, version, description, tags, and custom attributes to templates and downstream resolvers.",
		Schema: func() *jsonschema.Schema {
			s := schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"field": schemahelper.StringProp("Optional: return only this single field from the metadata map instead of the full map", schemahelper.WithMaxLength(200), schemahelper.WithExample("name")),
			})
			s.AdditionalProperties = &jsonschema.Schema{} // allow arbitrary metadata keys
			return s
		}(),
		OutputSchemas: map[provider.Capability]*jsonschema.Schema{
			provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"metadata": schemahelper.AnyProp("The full metadata map or a single field value"),
			}),
		},
		Capabilities: []provider.Capability{
			provider.CapabilityFrom,
		},
		Category:     "Core",
		Tags:         []string{"metadata", "solution", "introspection"},
		MockBehavior: "Returns the provided metadata map unchanged",
		Examples: []provider.Example{
			{
				Name:        "Full metadata",
				Description: "Return all metadata fields as a map",
				YAML: `name: sol-meta
type: metadata
from:
  name: my-solution
  version: 2.1.0
  description: Scaffolds a GCP project
  category: infrastructure
  tags:
    - gcp
    - terraform`,
			},
			{
				Name:        "Single field",
				Description: "Return only the solution name",
				YAML: `name: sol-name
type: metadata
from:
  field: name
  name: my-solution
  version: 2.1.0`,
			},
		},
	}
}

// Execute returns the metadata map or a single field from it.
func (p *MetadataProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	inputs, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected map[string]any, got %T", ProviderName, input)
	}

	lgr.V(1).Info("executing provider", "provider", ProviderName)

	// Check if a specific field was requested.
	fieldName, _ := inputs["field"].(string)

	if fieldName != "" {
		value, exists := inputs[fieldName]
		if !exists {
			return nil, fmt.Errorf("%s: requested field %q not found in metadata inputs", ProviderName, fieldName)
		}
		lgr.V(1).Info("provider completed", "provider", ProviderName, "field", fieldName)
		return &provider.Output{Data: value}, nil
	}

	// Return the full metadata map (excluding the "field" key itself).
	result := make(map[string]any, len(inputs))
	for k, v := range inputs {
		if k == "field" {
			continue
		}
		result[k] = v
	}

	lgr.V(1).Info("provider completed", "provider", ProviderName, "keys", len(result))
	return &provider.Output{Data: result}, nil
}
