// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpprovider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAcceptableStatusCodes_Unconfigured(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		inputs map[string]any
	}{
		{name: "absent", inputs: map[string]any{}},
		{name: "nil value", inputs: map[string]any{fieldAcceptableStatusCodes: nil}},
		{name: "empty list", inputs: map[string]any{fieldAcceptableStatusCodes: []any{}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			acc, err := parseAcceptableStatusCodes(tt.inputs)
			require.NoError(t, err)
			assert.False(t, acc.configured)
			// Unconfigured: only 2xx is successful.
			assert.True(t, acc.isSuccess(200))
			assert.True(t, acc.isSuccess(204))
			assert.False(t, acc.isSuccess(404))
			assert.False(t, acc.isSuccess(500))
		})
	}
}

func TestParseAcceptableStatusCodes_Entries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		entries  []any
		success  []int
		notMatch []int
	}{
		{
			name:     "exact ints",
			entries:  []any{200, 404},
			success:  []int{200, 404},
			notMatch: []int{201, 500},
		},
		{
			name:     "float codes from json",
			entries:  []any{float64(200), float64(202)},
			success:  []int{200, 202},
			notMatch: []int{201, 404},
		},
		{
			name:     "string code",
			entries:  []any{"404"},
			success:  []int{404},
			notMatch: []int{200},
		},
		{
			name:     "class shorthand",
			entries:  []any{"2xx", "4xx"},
			success:  []int{200, 204, 299, 400, 404, 499},
			notMatch: []int{300, 500},
		},
		{
			name:     "inclusive range",
			entries:  []any{"200-204"},
			success:  []int{200, 202, 204},
			notMatch: []int{199, 205},
		},
		{
			name:     "mixed",
			entries:  []any{200, "2xx", "400-404"},
			success:  []int{200, 250, 400, 404},
			notMatch: []int{405, 500},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			acc, err := parseAcceptableStatusCodes(map[string]any{fieldAcceptableStatusCodes: tt.entries})
			require.NoError(t, err)
			assert.True(t, acc.configured)
			for _, code := range tt.success {
				assert.Truef(t, acc.isSuccess(code), "expected %d to be successful", code)
			}
			for _, code := range tt.notMatch {
				assert.Falsef(t, acc.isSuccess(code), "expected %d to not be successful", code)
			}
		})
	}
}

func TestParseAcceptableStatusCodes_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		inputs map[string]any
	}{
		{name: "not an array", inputs: map[string]any{fieldAcceptableStatusCodes: "2xx"}},
		{name: "unsupported entry type", inputs: map[string]any{fieldAcceptableStatusCodes: []any{true}}},
		{name: "non-integer float", inputs: map[string]any{fieldAcceptableStatusCodes: []any{float64(200.5)}}},
		{name: "empty string entry", inputs: map[string]any{fieldAcceptableStatusCodes: []any{""}}},
		{name: "invalid range", inputs: map[string]any{fieldAcceptableStatusCodes: []any{"200-abc"}}},
		{name: "reversed range", inputs: map[string]any{fieldAcceptableStatusCodes: []any{"500-200"}}},
		{name: "invalid token", inputs: map[string]any{fieldAcceptableStatusCodes: []any{"okay"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseAcceptableStatusCodes(tt.inputs)
			assert.Error(t, err)
		})
	}
}

func TestStatusAcceptance_Describe(t *testing.T) {
	t.Parallel()

	acc, err := parseAcceptableStatusCodes(map[string]any{fieldAcceptableStatusCodes: []any{200}})
	require.NoError(t, err)
	assert.Equal(t, "200", acc.describe())
}

func TestStatusAcceptance_DescribeDeterministic(t *testing.T) {
	t.Parallel()

	// Entries are intentionally provided out of order across exact codes,
	// classes, and ranges to verify the output is sorted and stable.
	inputs := map[string]any{
		fieldAcceptableStatusCodes: []any{404, 200, "5xx", "2xx", "500-504", "200-204"},
	}
	acc, err := parseAcceptableStatusCodes(inputs)
	require.NoError(t, err)

	want := "200, 404, 2xx, 5xx, 200-204, 500-504"
	// Call repeatedly to confirm map iteration order does not affect output.
	for range 5 {
		assert.Equal(t, want, acc.describe())
	}
}
