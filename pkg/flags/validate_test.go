// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package flags

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateInputKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		inputs      map[string]any
		validKeys   []string
		contextName string
		wantErr     bool
		errContains string
	}{
		{
			name:        "all keys valid",
			inputs:      map[string]any{"url": "https://example.com", "method": "GET"},
			validKeys:   []string{"url", "method", "headers", "body", "timeout"},
			contextName: `provider "http"`,
		},
		{
			name:        "empty inputs",
			inputs:      map[string]any{},
			validKeys:   []string{"url", "method"},
			contextName: `provider "http"`,
		},
		{
			name:        "nil inputs",
			inputs:      nil,
			validKeys:   []string{"url", "method"},
			contextName: `provider "http"`,
		},
		{
			name:        "empty valid keys",
			inputs:      map[string]any{"url": "https://example.com"},
			validKeys:   []string{},
			contextName: `provider "http"`,
		},
		{
			name:        "single unknown key with suggestion",
			inputs:      map[string]any{"urll": "https://example.com"},
			validKeys:   []string{"url", "method", "headers", "body", "timeout"},
			contextName: `provider "http"`,
			wantErr:     true,
			errContains: `did you mean "url"`,
		},
		{
			name:        "single unknown key no suggestion",
			inputs:      map[string]any{"zzzzzzzzz": "value"},
			validKeys:   []string{"url", "method"},
			contextName: `provider "http"`,
			wantErr:     true,
			errContains: `does not accept input "zzzzzzzzz"`,
		},
		{
			name:        "multiple unknown keys",
			inputs:      map[string]any{"urll": "v", "mehod": "GET"},
			validKeys:   []string{"url", "method", "headers"},
			contextName: `provider "http"`,
			wantErr:     true,
			errContains: "unknown inputs",
		},
		{
			name:        "mixed valid and unknown keys",
			inputs:      map[string]any{"url": "https://example.com", "boddy": "data"},
			validKeys:   []string{"url", "method", "headers", "body", "timeout"},
			contextName: `provider "http"`,
			wantErr:     true,
			errContains: `did you mean "body"`,
		},
		{
			name:        "suggestion for resolver parameter typo",
			inputs:      map[string]any{"envrionment": "prod"}, //nolint:misspell // intentional typo for testing
			validKeys:   []string{"environment", "region", "cluster"},
			contextName: "solution",
			wantErr:     true,
			errContains: `did you mean "environment"`,
		},
		{
			name:        "exact match is not flagged",
			inputs:      map[string]any{"environment": "prod"},
			validKeys:   []string{"environment", "region"},
			contextName: "solution",
		},
		{
			name:        "error message includes valid inputs list",
			inputs:      map[string]any{"unknown": "x"},
			validKeys:   []string{"alpha", "beta"},
			contextName: "solution",
			wantErr:     true,
			errContains: "valid inputs: alpha, beta",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateInputKeys(tt.inputs, tt.validKeys, tt.contextName)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			assert.NoError(t, err)
		})
	}
}

func TestClosestKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		key       string
		validKeys []string
		want      string
	}{
		{
			name:      "single character typo",
			key:       "urll",
			validKeys: []string{"url", "method", "headers"},
			want:      "url",
		},
		{
			name:      "transposition",
			key:       "mehod",
			validKeys: []string{"url", "method", "headers"},
			want:      "method",
		},
		{
			name:      "missing character",
			key:       "headrs",
			validKeys: []string{"url", "method", "headers"},
			want:      "headers",
		},
		{
			name:      "extra character",
			key:       "bodyy",
			validKeys: []string{"url", "method", "body"},
			want:      "body",
		},
		{
			name:      "too distant - no suggestion",
			key:       "zzzzzzz",
			validKeys: []string{"url", "method", "headers"},
			want:      "",
		},
		{
			name:      "empty valid keys",
			key:       "url",
			validKeys: []string{},
			want:      "",
		},
		{
			name:      "exact match returns it",
			key:       "url",
			validKeys: []string{"url", "method"},
			want:      "url",
		},
		{
			name:      "picks closest among multiple candidates",
			key:       "timeot",
			validKeys: []string{"timeout", "time", "timer"},
			want:      "timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := closestKey(tt.key, tt.validKeys)
			assert.Equal(t, tt.want, got)
		})
	}
}

func BenchmarkValidateInputKeys(b *testing.B) {
	inputs := map[string]any{
		"urll":    "https://example.com",
		"method":  "GET",
		"boddy":   "data",
		"timeout": 30,
	}
	validKeys := []string{"url", "method", "headers", "body", "timeout", "retries", "follow_redirects"}

	b.ResetTimer()
	for b.Loop() {
		_ = ValidateInputKeys(inputs, validKeys, `provider "http"`)
	}
}

func BenchmarkClosestKey(b *testing.B) {
	validKeys := []string{"url", "method", "headers", "body", "timeout", "retries", "follow_redirects", "max_redirects", "verify_ssl", "proxy"}

	b.ResetTimer()
	for b.Loop() {
		closestKey("headrs", validKeys)
	}
}

func BenchmarkClosestKeyLargeKeySet(b *testing.B) {
	// Simulate a provider with many input keys
	validKeys := make([]string, 50)
	for i := range validKeys {
		validKeys[i] = "property_" + string(rune('a'+i%26)) + string(rune('0'+i/26))
	}

	b.ResetTimer()
	for b.Loop() {
		closestKey("property_z9", validKeys)
	}
}
