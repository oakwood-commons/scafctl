// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package main provides an example of using pkg/flags for key-value parsing with validation.
//
//nolint:forbidigo,revive,unparam // Example code demonstrates usage patterns
package main

import (
	"fmt"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/flags"
	"github.com/oakwood-commons/scafctl/pkg/flags/validate"
	"github.com/spf13/cobra"
)

// Example command demonstrating key-value flag parsing with validation
func main() {
	var resourceFlags []string

	cmd := &cobra.Command{
		Use:   "example",
		Short: "Example command showing key-value flag parsing with validation",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Step 1: Parse the flags (handles CSV, quotes, escapes, URI schemes)
			parsed, err := flags.ParseKeyValueCSV(resourceFlags)
			if err != nil {
				return fmt.Errorf("failed to parse flags: %w", err)
			}

			// Step 2: Validate all values (checks JSON, YAML, base64, file existence, URLs)
			validated, err := validate.ValidateAll(parsed)
			if err != nil {
				return fmt.Errorf("validation failed: %w", err)
			}

			// Step 3: Process the validated values
			for key, values := range validated {
				fmt.Printf("Key: %s\n", key)
				for _, val := range values {
					processValue(key, val)
				}
				fmt.Println()
			}

			return nil
		},
	}

	// IMPORTANT: Use StringArrayVarP, not StringSliceVarP
	// StringSliceVarP causes issues with special characters
	cmd.Flags().StringArrayVarP(&resourceFlags, "resource", "r", nil,
		"Resource configuration in key=value format (repeatable, supports CSV)")

	// Example usage would be:
	// example -r "config=json://{\"db\":\"postgres\",\"port\":5432}"
	// example -r "data=yaml://items: [a, b, c]"
	// example -r "env=prod,region=us-east1,config=json://{\"timeout\":30}"
	// example -r "token=base64://SGVsbG8sIFdvcmxkIQ=="
	// example -r "path=file:///etc/config.json"

	if err := cmd.Execute(); err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}

// processValue demonstrates how to handle different scheme types
func processValue(key, value string) {
	// Check for JSON scheme
	if strings.HasPrefix(value, "json://") {
		jsonContent := value[7:] // Strip scheme prefix
		fmt.Printf("  - JSON value: %s\n", jsonContent)
		// Parse JSON here with encoding/json
		return
	}

	// Check for YAML scheme
	if strings.HasPrefix(value, "yaml://") {
		yamlContent := value[7:]
		fmt.Printf("  - YAML value: %s\n", yamlContent)
		// Parse YAML here with gopkg.in/yaml.v3
		return
	}

	// Check for Base64 scheme
	if strings.HasPrefix(value, "base64://") {
		base64Content := value[9:]
		fmt.Printf("  - Base64 value: %s\n", base64Content)
		// Decode base64 here with encoding/base64
		return
	}

	// Check for file scheme
	if strings.HasPrefix(value, "file://") {
		filePath := value[7:]
		fmt.Printf("  - File path: %s\n", filePath)
		// Read file here with os.ReadFile
		return
	}

	// Check for HTTP/HTTPS scheme
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		fmt.Printf("  - URL: %s\n", value)
		// Fetch URL here with http.Get
		return
	}

	// No scheme - plain value
	fmt.Printf("  - Plain value: %s\n", value)
}
