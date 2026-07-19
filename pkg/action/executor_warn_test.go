// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

func TestExecutor_warnEmptySources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		emptyPatterns []string
		allEmpty      bool
		wantStdout    string
		wantStderr    string
	}{
		{
			name:          "no empty patterns is silent",
			emptyPatterns: nil,
		},
		{
			name:          "total no-match warns on stderr",
			emptyPatterns: []string{"*.go", "missing.txt"},
			allEmpty:      true,
			wantStderr:    "fingerprint disabled",
		},
		{
			name:          "partial no-match hints on stderr",
			emptyPatterns: []string{"missing.txt"},
			allEmpty:      false,
			wantStderr:    "matched no files (ignored)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ioStreams, out, errOut := terminal.NewTestIOStreams()
			w := writer.New(ioStreams, &settings.Run{})
			ctx := writer.WithWriter(context.Background(), w)

			e := NewExecutor()
			e.warnEmptySources(ctx, "build", tt.emptyPatterns, tt.allEmpty)

			// Warnings must never pollute stdout (structured output safety).
			assert.Empty(t, out.String())
			if tt.wantStderr == "" {
				assert.Empty(t, errOut.String())
			} else {
				assert.Contains(t, errOut.String(), tt.wantStderr)
				assert.Contains(t, errOut.String(), "build")
			}
		})
	}
}

// Embedder-safe: when no writer is attached to the context, warnEmptySources
// must be a no-op and never panic.
func TestExecutor_warnEmptySources_NoWriter(t *testing.T) {
	t.Parallel()
	e := NewExecutor()
	assert.NotPanics(t, func() {
		e.warnEmptySources(context.Background(), "build", []string{"missing.txt"}, true)
	})
}
