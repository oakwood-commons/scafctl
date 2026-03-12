// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/errexplain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleExplainError(t *testing.T) {
	s := &Server{}

	tests := []struct {
		name            string
		errorText       string
		wantCategory    string
		wantSummaryPart string
		wantSuggestions int // minimum number of suggestions
		wantIsError     bool
	}{
		{
			name:            "execution error with http provider",
			errorText:       `resolver "api-data" failed in resolve phase (step 0, provider http): connection refused`,
			wantCategory:    "resolver_execution",
			wantSummaryPart: "api-data",
			wantSuggestions: 3,
		},
		{
			name:            "execution error with cel provider",
			errorText:       `resolver "transform-x" failed in transform phase (step 1, provider cel): found no matching overload`,
			wantCategory:    "resolver_execution",
			wantSummaryPart: "transform-x",
			wantSuggestions: 3,
		},
		{
			name:            "type coercion error",
			errorText:       `resolver "age-value": type coercion from string to int failed after resolve phase: invalid syntax`,
			wantCategory:    "type_coercion",
			wantSummaryPart: "string to int",
			wantSuggestions: 3,
		},
		{
			name:            "validation failure",
			errorText:       `resolver "email" validation failed: value must be a valid email address`,
			wantCategory:    "validation",
			wantSummaryPart: "email",
			wantSuggestions: 3,
		},
		{
			name:            "circular dependency",
			errorText:       `circular dependency detected: a → b → a`,
			wantCategory:    "dependency",
			wantSummaryPart: "Circular dependency",
			wantSuggestions: 3,
		},
		{
			name:            "CEL undeclared reference",
			errorText:       `undeclared reference to 'foobar' in expression`,
			wantCategory:    "cel_expression",
			wantSummaryPart: "foobar",
			wantSuggestions: 3,
		},
		{
			name:            "CEL no matching overload",
			errorText:       `found no matching overload for 'size'`,
			wantCategory:    "cel_expression",
			wantSummaryPart: "size",
			wantSuggestions: 2,
		},
		{
			name:            "no such key",
			errorText:       `no such key: items`,
			wantCategory:    "data_access",
			wantSummaryPart: "items",
			wantSuggestions: 3,
		},
		{
			name:            "no such key with http context",
			errorText:       `no such key: data — http statusCode was 200`,
			wantCategory:    "data_access",
			wantSummaryPart: "data",
			wantSuggestions: 4,
		},
		{
			name:            "phase timeout",
			errorText:       `phase 2 timed out with 3 resolvers still waiting`,
			wantCategory:    "timeout",
			wantSummaryPart: "Phase 2",
			wantSuggestions: 3,
		},
		{
			name:            "value size exceeded",
			errorText:       `resolver "big-data" value size 10485760 bytes exceeds maximum 1048576 bytes`,
			wantCategory:    "value_size",
			wantSummaryPart: "big-data",
			wantSuggestions: 3,
		},
		{
			name:            "forEach type mismatch",
			errorText:       `resolver "items" transform step 0: forEach requires array input, got string`,
			wantCategory:    "type_mismatch",
			wantSummaryPart: "items",
			wantSuggestions: 3,
		},
		{
			name:            "aggregated failures",
			errorText:       `3 resolver(s) failed, 2 skipped due to failed dependencies`,
			wantCategory:    "multiple_failures",
			wantSummaryPart: "3 resolvers",
			wantSuggestions: 3,
		},
		{
			name:            "unknown error",
			errorText:       `something completely unexpected happened`,
			wantCategory:    "unknown",
			wantSummaryPart: "Could not categorize",
			wantSuggestions: 3,
		},
		{
			name:        "empty error",
			errorText:   "",
			wantIsError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Arguments = map[string]any{
				"error": tt.errorText,
			}

			result, err := s.handleExplainError(context.TODO(), request)
			require.NoError(t, err)
			require.NotNil(t, result)

			if tt.wantIsError {
				assert.True(t, result.IsError)
				return
			}

			assert.False(t, result.IsError)
			require.Len(t, result.Content, 1)

			text := result.Content[0].(mcp.TextContent).Text
			var exp errexplain.Explanation
			require.NoError(t, json.Unmarshal([]byte(text), &exp))

			assert.Equal(t, tt.wantCategory, exp.Category)
			assert.Contains(t, exp.Summary, tt.wantSummaryPart)
			assert.GreaterOrEqual(t, len(exp.Suggestions), tt.wantSuggestions, "expected at least %d suggestions, got %d", tt.wantSuggestions, len(exp.Suggestions))
			assert.NotEmpty(t, exp.RootCause)
		})
	}
}
