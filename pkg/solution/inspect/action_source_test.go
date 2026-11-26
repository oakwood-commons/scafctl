// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package inspect

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildActionSourceFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		sourceVal     string
		selectorFlags string
		want          string
	}{
		{
			name: "empty source omits flag",
			want: "",
		},
		{
			name:      "relative local path uses -f",
			sourceVal: "./solution.yaml",
			want:      " -f ./solution.yaml",
		},
		{
			name:      "absolute local path with spaces uses quoted -f",
			sourceVal: "/tmp/my app/solution.yaml",
			want:      " -f '/tmp/my app/solution.yaml'",
		},
		{
			name:      "stdin uses -f -",
			sourceVal: "-",
			want:      " -f -",
		},
		{
			name:          "bare catalog name uses -s and preserves selector flags",
			sourceVal:     "scoped-tasks",
			selectorFlags: " --catalog local --version '^1.0.0'",
			want:          " -s scoped-tasks --catalog local --version '^1.0.0'",
		},
		{
			name:      "versioned catalog name uses -s",
			sourceVal: "scoped-tasks@1.2.3",
			want:      " -s scoped-tasks@1.2.3",
		},
		{
			name:      "remote registry ref uses -s",
			sourceVal: "ghcr.io/org/scoped-tasks@1.2.3",
			want:      " -s ghcr.io/org/scoped-tasks@1.2.3",
		},
		{
			name:      "oci scheme ref uses -s",
			sourceVal: "oci://ghcr.io/org/scoped-tasks:1.2.3",
			want:      " -s oci://ghcr.io/org/scoped-tasks:1.2.3",
		},
		{
			name:      "HTTPS URL uses -s",
			sourceVal: "https://example.com/solution.yaml",
			want:      " -s https://example.com/solution.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, buildActionSourceFlags(tt.sourceVal, tt.selectorFlags))
		})
	}
}

func TestBuildActionList_EmbedderBinaryUsesSolutionFlagForCatalogRef(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{
		Metadata: solution.Metadata{Name: "deploy-suite"},
		Spec: solution.Spec{
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"release": {Description: "Explicit release", Explicit: true},
				},
			},
		},
	}
	sol.SetPath("catalog:deploy-suite@2.0.0")

	entries := BuildActionList(context.Background(), sol, "deploy-suite@2.0.0", "mycli")
	require.Len(t, entries, 1)
	assert.Equal(t, "mycli run action release -s deploy-suite@2.0.0", entries[0].Command)
}

func BenchmarkBuildActionSourceFlags(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = buildActionSourceFlags("scoped-tasks@1.0.0", " --catalog local")
	}
}
