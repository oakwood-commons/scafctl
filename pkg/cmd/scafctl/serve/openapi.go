// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package serve

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

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
func CommandOpenAPI(cliParams *settings.Run, ioStreams *terminal.IOStreams) *cobra.Command {
	var (
		outputFormat string
		outputFile   string
	)

	binaryName := settings.CliBinaryName
	if cliParams != nil && cliParams.BinaryName != "" {
		binaryName = cliParams.BinaryName
	}

	cmd := &cobra.Command{
		Use:   "openapi",
		Short: "Export OpenAPI specification",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Generate the full OpenAPI specification for the scafctl REST API
			without starting the server.

			The specification includes all endpoints, request/response schemas,
			authentication schemes, and documentation.
		`), settings.CliBinaryName, binaryName),
		Example: strings.ReplaceAll(heredoc.Doc(`
			# Export as JSON to stdout
			scafctl serve openapi

			# Export as YAML to a file
			scafctl serve openapi -o yaml --output-file openapi.yaml

			# Export as JSON to a file
			scafctl serve openapi -o json --output-file openapi.json
		`), settings.CliBinaryName, binaryName),
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOpenAPI(cmd, cliParams, ioStreams, outputFormat, outputFile)
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "json", "Output format (json, yaml)")
	cmd.Flags().StringVar(&outputFile, "output-file", "", "Output file path (default: stdout)")

	return cmd
}

func runOpenAPI(cmd *cobra.Command, cliParams *settings.Run, ioStreams *terminal.IOStreams, outputFormat, outputFile string) error {
	w := writer.FromContext(cmd.Context())
	if w == nil && ioStreams != nil {
		w = writer.New(ioStreams, cliParams)
	}
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

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

	switch outputFormat {
	case "yaml", "yml":
		data, err = yaml.Marshal(spec)
	case "json":
		data, err = json.MarshalIndent(spec, "", "  ")
	default:
		return fmt.Errorf("unsupported output format %q: use json or yaml", outputFormat)
	}

	if err != nil {
		return fmt.Errorf("marshaling OpenAPI spec: %w", err)
	}

	if outputFile != "" {
		if err := os.WriteFile(outputFile, data, 0o600); err != nil {
			return fmt.Errorf("writing OpenAPI spec to %s: %w", outputFile, err)
		}
		w.Successf("OpenAPI spec written to %s\n", outputFile)
		return nil
	}

	w.Plainln(string(data))
	return nil
}
