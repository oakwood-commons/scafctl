// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package flags

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// maxRawReadSize is the maximum number of bytes that readRawReader and
// readRawFile will consume. This prevents accidental OOM when a user
// points key=@- at an unbounded stream or key=@/dev/zero at a device.
const maxRawReadSize = 1 << 20 // 1 MiB

// parseYAMLOrJSON tries to unmarshal data as YAML first, then JSON.
// The source parameter is used in error messages to identify the input origin.
func parseYAMLOrJSON(data []byte, source string) (map[string]any, error) {
	result := make(map[string]any)
	if yamlErr := yaml.Unmarshal(data, &result); yamlErr != nil {
		if jsonErr := json.Unmarshal(data, &result); jsonErr != nil {
			return nil, fmt.Errorf("failed to parse %s as YAML or JSON: %w", source, errors.Join(yamlErr, jsonErr))
		}
	}
	return result, nil
}

// LoadParameterFile loads parameters from a YAML or JSON file.
// The file format is auto-detected based on extension, or by trying
// YAML first then JSON if the extension is not recognized.
func LoadParameterFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read parameter file %q: %w", path, err)
	}

	ext := strings.ToLower(filepath.Ext(path))
	result := make(map[string]any)

	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("failed to parse YAML parameter file %q: %w", path, err)
		}
	case ".json":
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("failed to parse JSON parameter file %q: %w", path, err)
		}
	default:
		return parseYAMLOrJSON(data, fmt.Sprintf("parameter file %q", path))
	}

	return result, nil
}

// LoadParameterReader loads parameters from an io.Reader containing YAML or JSON.
// It tries YAML first, then JSON, matching the behavior of LoadParameterFile
// for files with unknown extensions. The source parameter is used in error
// messages to identify the input origin (e.g., "stdin").
func LoadParameterReader(r io.Reader, source string) (map[string]any, error) {
	// Protect against unbounded streams (e.g., piping from a long-running process)
	// by limiting the maximum number of bytes we will read.
	limited := io.LimitReader(r, maxRawReadSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("failed to read parameters from %s: %w", source, err)
	}

	if int64(len(data)) > maxRawReadSize {
		return nil, fmt.Errorf("parameters from %s exceed maximum allowed size (%d bytes)", source, maxRawReadSize)
	}

	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("no data received from %s", source)
	}

	return parseYAMLOrJSON(data, source)
}

// parseValueRef checks if a key=value string has an @-reference in the value
// position (e.g. "key=@-" or "key=@file.txt"). Returns the key, the reference
// (without the @ prefix), and true if it matches. Returns false for standalone
// @-references or plain key=value pairs.
func parseValueRef(s string) (key, ref string, ok bool) {
	idx := strings.Index(s, "=")
	if idx < 0 {
		return "", "", false
	}
	key = s[:idx]
	// Enforce non-empty key with no whitespace, matching other parsers.
	if key == "" || strings.ContainsAny(key, " \t\r\n") {
		return "", "", false
	}
	val := s[idx+1:]
	if !strings.HasPrefix(val, "@") || len(val) < 2 {
		return "", "", false
	}
	return key, val[1:], true
}

// readRawReader reads all content from an io.Reader as a raw string, trimming
// one trailing newline (to match shell pipe behavior like echo).
// Reads at most maxRawReadSize bytes to prevent unbounded memory growth.
func readRawReader(r io.Reader, source string) (string, error) {
	limited := io.LimitReader(r, maxRawReadSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("failed to read from %s: %w", source, err)
	}
	if int64(len(data)) > maxRawReadSize {
		return "", fmt.Errorf("%s exceeds maximum raw read size (%d bytes)", source, maxRawReadSize)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return "", fmt.Errorf("no data received from %s", source)
	}
	// Trim a single trailing \r\n or \n for shell usability (echo adds a newline).
	s := strings.TrimSuffix(string(data), "\n")
	s = strings.TrimSuffix(s, "\r")
	return s, nil
}

// readRawFile reads a file's content as a raw string, trimming one trailing newline.
// Returns an error if the file is empty or exceeds maxRawReadSize.
func readRawFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file %q: %w", path, err)
	}
	// For regular files, fail fast if the declared size already exceeds the limit.
	if info.Mode().IsRegular() && info.Size() > maxRawReadSize {
		return "", fmt.Errorf("file %q exceeds maximum raw read size (%d bytes)", path, maxRawReadSize)
	}

	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file %q: %w", path, err)
	}
	defer f.Close()

	// Delegate to readRawReader to enforce a hard read limit and consistent trimming.
	return readRawReader(f, fmt.Sprintf("file %q", path))
}

// ParseResolverFlags parses -r flag values, handling both key=value syntax
// and @file.yaml syntax for loading parameters from files.
//
// Supported formats:
//   - key=value: Simple key-value pair
//   - key=value1,value2: Multiple values (becomes an array)
//   - @file.yaml: Load all parameters from a YAML file
//   - @file.json: Load all parameters from a JSON file
//
// Multiple values for the same key are automatically combined into an array.
func ParseResolverFlags(values []string) (map[string]any, error) {
	return ParseResolverFlagsWithStdin(values, nil)
}

// ParseResolverFlagsWithStdin parses -r flag values like ParseResolverFlags,
// but additionally supports @- to read parameters from stdin as YAML or JSON.
//
// Supported formats:
//   - key=value: Simple key-value pair
//   - key=value1,value2: Multiple values (becomes an array)
//   - key=@-: Read raw stdin content as the value for key
//   - key=@file: Read raw file content as the value for key
//   - @file.yaml: Load all parameters from a YAML file
//   - @file.json: Load all parameters from a JSON file
//   - @-: Read all parameters from stdin (YAML or JSON)
//
// The @- token (both standalone and in key=@-) may only appear once.
// If stdin is nil and @- is used, an error is returned.
// Multiple values for the same key are automatically combined into an array.
func ParseResolverFlagsWithStdin(values []string, stdin io.Reader) (map[string]any, error) {
	result := make(map[string]any)
	stdinUsed := false

	for _, v := range values {
		if strings.HasPrefix(v, "@") {
			ref := strings.TrimPrefix(v, "@")

			if ref == "-" {
				// Read from stdin
				if stdinUsed {
					return nil, fmt.Errorf("@- can only be specified once (stdin can only be read once)")
				}
				if stdin == nil {
					return nil, fmt.Errorf("@- requires stdin but no stdin is available")
				}
				stdinUsed = true
				stdinParams, err := LoadParameterReader(stdin, "stdin")
				if err != nil {
					return nil, err
				}
				for k, val := range stdinParams {
					result[k] = MergeValue(result[k], val)
				}
			} else {
				// Load from file
				fileParams, err := LoadParameterFile(ref)
				if err != nil {
					return nil, err
				}
				for k, val := range fileParams {
					result[k] = MergeValue(result[k], val)
				}
			}
		} else if key, ref, ok := parseValueRef(v); ok {
			// key=@- or key=@file — read raw content for a single key
			if ref == "-" {
				if stdinUsed {
					return nil, fmt.Errorf("%s=@-: stdin has already been consumed (stdin can only be read once)", key)
				}
				if stdin == nil {
					return nil, fmt.Errorf("%s=@- requires stdin but no stdin is available", key)
				}
				stdinUsed = true
				raw, err := readRawReader(stdin, "stdin")
				if err != nil {
					return nil, err
				}
				result[key] = MergeValue(result[key], raw)
			} else {
				raw, err := readRawFile(ref)
				if err != nil {
					return nil, err
				}
				result[key] = MergeValue(result[key], raw)
			}
		} else {
			// Parse key=value using ParseKeyValueCSV
			parsed, err := ParseKeyValueCSV([]string{v})
			if err != nil {
				return nil, fmt.Errorf("failed to parse parameter %q: %w", v, err)
			}
			// Merge parsed values
			for k, vals := range parsed {
				// Convert []string to appropriate type
				if len(vals) == 1 {
					result[k] = MergeValue(result[k], vals[0])
				} else {
					// Multiple values - convert to []any
					anyVals := make([]any, len(vals))
					for i, s := range vals {
						anyVals[i] = s
					}
					result[k] = MergeValue(result[k], anyVals)
				}
			}
		}
	}

	return result, nil
}

// ParseDynamicInputArgs normalises raw CLI arguments into key=value strings
// suitable for ParseResolverFlags.
//
// Three forms are recognised:
//
//	--key=value    → strip the leading "--" → "key=value"
//	key=value      → passed through unchanged
//	@file.yaml     → passed through unchanged (file reference)
//
// A bare "--key" (no "=") is rejected because we cannot distinguish a
// boolean flag from a flag that expects the next token as a value.
// Single-dash forms ("-k=v") are also rejected to avoid collisions with
// existing short flags.
func ParseDynamicInputArgs(args []string) ([]string, error) {
	out := make([]string, 0, len(args))
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--") && strings.Contains(a, "="):
			// --key=value → key=value
			out = append(out, strings.TrimPrefix(a, "--"))

		case strings.HasPrefix(a, "--"):
			// --key without "=" — ambiguous
			return nil, fmt.Errorf("dynamic flag %q must use --key=value syntax (= is required)", a)

		case strings.HasPrefix(a, "-"):
			// -k or -k=v — reject to avoid short-flag collisions
			return nil, fmt.Errorf("single-dash flag %q is not supported for dynamic inputs; use --key=value or key=value", a)

		case strings.HasPrefix(a, "@"):
			// @file.yaml — file reference, pass through
			out = append(out, a)

		case strings.Contains(a, "="):
			// key=value positional — pass through
			out = append(out, a)

		default:
			return nil, fmt.Errorf("unexpected argument %q: use key=value or --key=value for provider inputs", a)
		}
	}
	return out, nil
}

// ContainsStdinRef returns true if any of the values contain the @- token
// (either standalone or as key=@-), indicating that stdin will be consumed.
func ContainsStdinRef(values []string) bool {
	for _, v := range values {
		if v == "@-" {
			return true
		}
		// Check for key=@- in value position
		if _, ref, ok := parseValueRef(v); ok && ref == "-" {
			return true
		}
	}
	return false
}

// MergeValue merges a new value with an existing value, creating arrays as needed.
// If existing is nil, returns newVal. If both are slices, concatenates them.
// If existing is a scalar and newVal is provided, creates a slice.
func MergeValue(existing, newVal any) any {
	if existing == nil {
		return newVal
	}

	// Handle existing slice
	switch e := existing.(type) {
	case []any:
		switch n := newVal.(type) {
		case []any:
			return append(e, n...)
		default:
			return append(e, n)
		}
	case []string:
		// Convert to []any first
		anySlice := make([]any, 0, len(e))
		for _, s := range e {
			anySlice = append(anySlice, s)
		}
		switch n := newVal.(type) {
		case []any:
			return append(anySlice, n...)
		case []string:
			for _, s := range n {
				anySlice = append(anySlice, s)
			}
			return anySlice
		default:
			return append(anySlice, n)
		}
	default:
		// Existing is a scalar
		switch n := newVal.(type) {
		case []any:
			return append([]any{e}, n...)
		default:
			return []any{e, n}
		}
	}
}
