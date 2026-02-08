// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package validate provides validation for key-value flag values based on URI scheme prefixes.
//
//nolint:revive // Package name validate with Validate* functions is intentional for clarity
package validate

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ValidateValue validates a value based on its scheme prefix.
// The scheme prefix is preserved in the returned value.
// Returns the original value and any validation error.
func ValidateValue(key, value string) (string, error) {
	if strings.HasPrefix(value, "json://") {
		content := value[7:] // Strip prefix for validation
		if err := ValidateJSON(content); err != nil {
			return "", fmt.Errorf("invalid JSON for key %q: %w", key, err)
		}
		return value, nil // Return with prefix
	}

	if strings.HasPrefix(value, "yaml://") {
		content := value[7:]
		if err := ValidateYAML(content); err != nil {
			return "", fmt.Errorf("invalid YAML for key %q: %w", key, err)
		}
		return value, nil
	}

	if strings.HasPrefix(value, "base64://") {
		content := value[9:]
		if err := ValidateBase64(content); err != nil {
			return "", fmt.Errorf("invalid base64 for key %q: %w", key, err)
		}
		return value, nil
	}

	if strings.HasPrefix(value, "file://") {
		path := value[7:]
		if err := ValidateFile(path); err != nil {
			return "", fmt.Errorf("invalid file path for key %q: %w", key, err)
		}
		return value, nil
	}

	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		if err := ValidateURL(value); err != nil {
			return "", fmt.Errorf("invalid URL for key %q: %w", key, err)
		}
		return value, nil
	}

	// No scheme or unknown scheme - no validation
	return value, nil
}

// ValidateAll validates all values in a parsed key-value map.
// Returns a new map with validated values (schemes preserved) and any validation errors.
func ValidateAll(parsed map[string][]string) (map[string][]string, error) {
	result := make(map[string][]string)

	for key, values := range parsed {
		validatedValues := make([]string, 0, len(values))
		for _, val := range values {
			validated, err := ValidateValue(key, val)
			if err != nil {
				return nil, err
			}
			validatedValues = append(validatedValues, validated)
		}
		result[key] = validatedValues
	}

	return result, nil
}

// ValidateJSON validates that the string is valid JSON.
func ValidateJSON(data string) error {
	var js any
	if err := json.Unmarshal([]byte(data), &js); err != nil {
		return fmt.Errorf("malformed JSON: %w", err)
	}
	return nil
}

// ValidateYAML validates that the string is valid YAML.
func ValidateYAML(data string) error {
	var y any
	if err := yaml.Unmarshal([]byte(data), &y); err != nil {
		return fmt.Errorf("malformed YAML: %w", err)
	}
	return nil
}

// ValidateBase64 validates that the string is valid base64.
func ValidateBase64(data string) error {
	_, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return fmt.Errorf("malformed base64: %w", err)
	}
	return nil
}

// ValidateFile validates that the file path is valid and the file exists.
func ValidateFile(path string) error {
	if path == "" {
		return fmt.Errorf("empty file path")
	}

	// Check if file exists
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file does not exist: %s", path)
		}
		return fmt.Errorf("cannot access file: %w", err)
	}

	// Ensure it's a file, not a directory
	if info.IsDir() {
		return fmt.Errorf("path is a directory, not a file: %s", path)
	}

	return nil
}

// ValidateURL validates that the string is a valid HTTP/HTTPS URL.
func ValidateURL(urlStr string) error {
	u, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("malformed URL: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL must use http or https scheme")
	}

	if u.Host == "" {
		return fmt.Errorf("URL must have a host")
	}

	return nil
}
