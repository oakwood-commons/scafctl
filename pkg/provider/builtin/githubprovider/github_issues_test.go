// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package githubprovider

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ── mapIssueStateForMutation tests ────────────────────────────────────────────

func TestMapIssueStateForMutation_Coverage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"lowercase open", "open", "OPEN"},
		{"uppercase OPEN", "OPEN", "OPEN"},
		{"lowercase closed", "closed", "CLOSED"},
		{"uppercase CLOSED", "CLOSED", "CLOSED"},
		{"passthrough unknown", "pending", "pending"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := mapIssueStateForMutation(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ── Input validation tests for issue operations ───────────────────────────────

func TestExecuteCreateIssue_MissingTitle(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeCreateIssue(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{
		// no title
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "title")
}

func TestExecuteUpdateIssue_MissingNumber(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeUpdateIssue(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{
		"title": "Updated Title",
		// no number
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "number")
}

func TestExecuteCreateIssueComment_MissingNumber(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeCreateIssueComment(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{
		"body": "comment text",
		// no number
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "number")
}

func TestExecuteCreateIssueComment_MissingBody(t *testing.T) {
	t.Parallel()

	p := NewGitHubProvider()
	_, err := p.executeCreateIssueComment(t.Context(), nil, "https://api.github.com", "owner", "repo", map[string]any{
		"number": float64(1),
		// no body
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "body")
}

// ── Benchmark tests ───────────────────────────────────────────────────────────

func BenchmarkMapIssueStateForMutation(b *testing.B) {
	states := []string{"open", "OPEN", "closed", "CLOSED", "unknown"}
	b.ReportAllocs()
	b.ResetTimer()
	idx := 0
	for b.Loop() {
		mapIssueStateForMutation(states[idx%len(states)])
		idx++
	}
}
