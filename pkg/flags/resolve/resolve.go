// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package resolve provides resolution and fetching of key-value flag values based on URI scheme prefixes.
// It validates schemes, fetches data, and returns parsed/decoded content.
//
//nolint:revive // Package name resolve with Resolve* functions is intentional for clarity
package resolve

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/flags/validate"
	"gopkg.in/yaml.v3"
)

// ResolveValue validates and resolves a value based on its scheme prefix.
// The scheme prefix is stripped from the result, and the data is fetched/parsed.
// Returns the resolved data as any (parsed JSON/YAML, decoded base64, raw bytes for files/http).
func ResolveValue(ctx context.Context, key, value string) (any, error) {
	// json:// - Parse and return as Go types
	if strings.HasPrefix(value, "json://") {
		content := value[7:]
		var result any
		if err := json.Unmarshal([]byte(content), &result); err != nil {
			return nil, fmt.Errorf("invalid JSON for key %q: %w", key, err)
		}
		return result, nil
	}

	// yaml:// - Parse and return as Go types
	if strings.HasPrefix(value, "yaml://") {
		content := value[7:]
		var result any
		if err := yaml.Unmarshal([]byte(content), &result); err != nil {
			return nil, fmt.Errorf("invalid YAML for key %q: %w", key, err)
		}
		return result, nil
	}

	// base64:// - Decode and return as bytes
	if strings.HasPrefix(value, "base64://") {
		content := value[9:]
		decoded, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			return nil, fmt.Errorf("invalid base64 for key %q: %w", key, err)
		}
		return decoded, nil
	}

	// file:// - Read file and return as bytes
	if strings.HasPrefix(value, "file://") {
		path := value[7:]
		if err := validate.ValidateFile(path); err != nil {
			return nil, fmt.Errorf("invalid file path for key %q: %w", key, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read file for key %q: %w", key, err)
		}
		return data, nil
	}

	// http://, https:// - Fetch URL and return as bytes
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		if err := validate.ValidateURL(value); err != nil {
			return nil, fmt.Errorf("invalid URL for key %q: %w", key, err)
		}
		data, err := fetchURL(ctx, value)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch URL for key %q: %w", key, err)
		}
		return data, nil
	}

	// No scheme or unknown scheme - return as-is (string)
	return value, nil
}

// ResolveAll validates and resolves all values in a parsed key-value map.
// Returns a new map with resolved values (schemes stripped, data fetched/parsed).
func ResolveAll(ctx context.Context, parsed map[string][]string) (map[string][]any, error) {
	result := make(map[string][]any)

	for key, values := range parsed {
		resolvedValues := make([]any, 0, len(values))
		for _, val := range values {
			resolved, err := ResolveValue(ctx, key, val)
			if err != nil {
				return nil, err
			}
			resolvedValues = append(resolvedValues, resolved)
		}
		result[key] = resolvedValues
	}

	return result, nil
}

// fetchURL fetches content from an HTTP/HTTPS URL using the standard HTTP client.
func fetchURL(ctx context.Context, urlStr string) ([]byte, error) {
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "scafctl-flags-resolver/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error: status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return data, nil
}

// GetFirst returns the first resolved value for a key as any, or nil if not found.
func GetFirst(m map[string][]any, key string) any {
	values, ok := m[key]
	if !ok || len(values) == 0 {
		return nil
	}
	return values[0]
}

// GetAll returns all resolved values for a key as []any, or empty slice if not found.
func GetAll(m map[string][]any, key string) []any {
	values, ok := m[key]
	if !ok {
		return []any{}
	}
	return values
}

// Has returns true if the key exists in the map.
func Has(m map[string][]any, key string) bool {
	_, ok := m[key]
	return ok
}
