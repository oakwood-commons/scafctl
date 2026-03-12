// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package build

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/solution/builder"
	"github.com/oakwood-commons/scafctl/pkg/terminal/format"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseByteSize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		{"100B", 100, false},
		{"1KB", 1024, false},
		{"50MB", 50 * 1024 * 1024, false},
		{"1GB", 1024 * 1024 * 1024, false},
		{"100", 100, false},
		{"50mb", 50 * 1024 * 1024, false},
		{"invalid", 0, true},
		{"MB", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := builder.ParseByteSize(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestFormatByteSize(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{100, "100 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{50 * 1024 * 1024, "50.0 MB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, format.Bytes(tt.input))
		})
	}
}
