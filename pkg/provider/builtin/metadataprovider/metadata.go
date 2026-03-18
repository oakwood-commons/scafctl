// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package metadataprovider

import (
	"context"
	"os"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// ProviderName is the name of this provider.
const ProviderName = "metadata"

// MetadataProvider returns runtime metadata about the scafctl process and
// the currently-executing solution. It requires no inputs — all data is
// gathered from the execution context and process environment.
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
		Version:     semver.MustParse("2.0.0"),
		Description: "Returns runtime metadata about the scafctl process and the currently-executing solution. Provides the scafctl version, CLI arguments, working directory, entrypoint type (cli/api), command path, and solution metadata. Requires no inputs.",
		Schema:      schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{}),
		OutputSchemas: map[provider.Capability]*jsonschema.Schema{
			provider.CapabilityFrom: schemahelper.ObjectSchema(
				[]string{"version", "args", "cwd", "entrypoint", "command", "solution"},
				map[string]*jsonschema.Schema{
					"version": schemahelper.ObjectProp("Build version information", nil, map[string]*jsonschema.Schema{
						"buildVersion": schemahelper.StringProp("Semantic version of the scafctl build"),
						"commit":       schemahelper.StringProp("Git commit hash of the build"),
						"buildTime":    schemahelper.StringProp("Timestamp of the build"),
					}),
					"args":       schemahelper.ArrayProp("Command-line arguments passed to scafctl", schemahelper.WithItems(schemahelper.StringProp("A CLI argument"))),
					"cwd":        schemahelper.StringProp("Current working directory"),
					"entrypoint": schemahelper.StringProp("How scafctl was invoked", schemahelper.WithEnum("cli", "api", "unknown")),
					"command":    schemahelper.StringProp("The command path (e.g. scafctl/run/solution)"),
					"solution": schemahelper.ObjectProp("Metadata about the currently-running solution", nil, map[string]*jsonschema.Schema{
						"name":        schemahelper.StringProp("Solution name"),
						"version":     schemahelper.StringProp("Solution version"),
						"displayName": schemahelper.StringProp("Solution display name"),
						"description": schemahelper.StringProp("Solution description"),
						"category":    schemahelper.StringProp("Solution category"),
						"tags":        schemahelper.ArrayProp("Solution tags", schemahelper.WithItems(schemahelper.StringProp("A tag"))),
					}),
				},
			),
		},
		Capabilities: []provider.Capability{
			provider.CapabilityFrom,
		},
		Category: "Core",
		Tags:     []string{"metadata", "solution", "introspection", "runtime"},
		Examples: []provider.Example{
			{
				Name:        "Runtime metadata",
				Description: "Return all runtime metadata about the scafctl process and current solution",
				YAML: `name: runtime-meta
type: metadata
from:
  inputs: {}`,
			},
		},
	}
}

// Execute gathers runtime metadata from the process environment and context.
func (p *MetadataProvider) Execute(ctx context.Context, _ any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("executing provider", "provider", ProviderName)

	// Build version info from the global settings.
	versionInfo := settings.VersionInformation
	version := map[string]any{
		"buildVersion": versionInfo.BuildVersion,
		"commit":       versionInfo.Commit,
		"buildTime":    versionInfo.BuildTime,
	}

	// CLI arguments.
	args := os.Args

	// Current working directory (context-aware).
	cwd, _ := provider.GetWorkingDirectory(ctx)

	// Entrypoint and command path from settings context.
	entrypoint := "unknown"
	command := ""
	if run, ok := settings.FromContext(ctx); ok && run != nil {
		ep := run.EntryPointSettings
		switch {
		case ep.FromCli:
			entrypoint = "cli"
		case ep.FromAPI:
			entrypoint = "api"
		}
		command = ep.Path
	}

	// Solution metadata from provider context.
	var solData map[string]any
	if meta, ok := provider.SolutionMetadataFromContext(ctx); ok && meta != nil {
		solData = map[string]any{
			"name":        meta.Name,
			"version":     meta.Version,
			"displayName": meta.DisplayName,
			"description": meta.Description,
			"category":    meta.Category,
			"tags":        meta.Tags,
		}
	} else {
		solData = map[string]any{}
	}

	result := map[string]any{
		"version":    version,
		"args":       args,
		"cwd":        cwd,
		"entrypoint": entrypoint,
		"command":    command,
		"solution":   solData,
	}

	lgr.V(1).Info("provider completed", "provider", ProviderName)
	return &provider.Output{Data: result}, nil
}
