// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package serve

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/api/endpoints"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// CommandOpenAPI creates the `scafctl serve openapi` subcommand.
func CommandOpenAPI(_ *settings.Run, _ *terminal.IOStreams) *cobra.Command {
	var (
		format string
		output string
	)

	cmd := &cobra.Command{
		Use:   "openapi",
		Short: "Export OpenAPI specification",
		Long: heredoc.Doc(`
			Generate the full OpenAPI specification for the scafctl REST API
			without starting the server.

			The specification includes all endpoints, request/response schemas,
			authentication schemes, and documentation.
		`),
		Example: heredoc.Doc(`
			# Export as JSON to stdout
			scafctl serve openapi

			# Export as YAML to a file
			scafctl serve openapi --format yaml --output openapi.yaml

			# Export as JSON to a file
			scafctl serve openapi --format json --output openapi.json
		`),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOpenAPI(cmd, format, output)
		},
	}

	cmd.Flags().StringVar(&format, "format", "json", "Output format (json, yaml)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file path (default: stdout)")

	return cmd
}

func runOpenAPI(cmd *cobra.Command, format, output string) error {
	w := writer.FromContext(cmd.Context())

	// Create a minimal router + Huma API for spec generation.
	// BuildHumaConfig mirrors the live server config including security schemes.
	router := chi.NewRouter()

	// Derive API version from loaded config so the export matches the live server.
	cfg := config.FromContext(cmd.Context())
	apiVersion := settings.DefaultAPIVersion
	if cfg != nil && cfg.APIServer.APIVersion != "" {
		apiVersion = cfg.APIServer.APIVersion
	}
	humaConfig := api.BuildHumaConfig(apiVersion, cfg)

	humaAPI := humachi.New(router, humaConfig)

	// Register all endpoints for export
	endpoints.RegisterAllForExport(humaAPI, apiVersion, cfg)

	// Get the OpenAPI spec
	spec := humaAPI.OpenAPI()

	var data []byte
	var err error

	switch format {
	case "yaml", "yml":
		data, err = yaml.Marshal(spec)
	case "json":
		data, err = json.MarshalIndent(spec, "", "  ")
	default:
		return fmt.Errorf("unsupported format %q: use json or yaml", format)
	}

	if err != nil {
		return fmt.Errorf("marshaling OpenAPI spec: %w", err)
	}

	if output != "" {
		if err := os.WriteFile(output, data, 0o600); err != nil {
			return fmt.Errorf("writing OpenAPI spec to %s: %w", output, err)
		}
		w.Successf("OpenAPI spec written to %s\n", output)
		return nil
	}

	w.Plainln(string(data))
	return nil
}
