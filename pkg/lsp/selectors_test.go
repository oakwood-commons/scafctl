// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/stretchr/testify/assert"
)

func TestRecognizedFilesFor_DefaultBinary(t *testing.T) {
	rf := RecognizedFilesFor(settings.CliBinaryName)

	assert.Equal(t, settings.CliBinaryName, rf.BinaryName)

	// The standard set the extension previously missed must be present.
	assert.Contains(t, rf.YAMLNames, "solution.yaml")
	assert.Contains(t, rf.YAMLNames, "solution.yml")
	assert.Contains(t, rf.YAMLNames, "taskfile.yaml")
	assert.Contains(t, rf.YAMLNames, "taskfile.yml")
	assert.Contains(t, rf.YAMLNames, "actions.yaml")
	assert.Contains(t, rf.YAMLNames, "actions.yml")

	// JSON solutions must be routed to JSONNames, never YAMLNames -- a JSON
	// document does not match a YAML-scoped editor selector.
	assert.Contains(t, rf.JSONNames, "solution.json")
	assert.NotContains(t, rf.YAMLNames, "solution.json")
	for _, name := range rf.YAMLNames {
		assert.NotContains(t, name, ".json", "YAMLNames must not contain JSON files")
	}
}

func TestRecognizedFilesFor_EmbedderBinary(t *testing.T) {
	rf := RecognizedFilesFor("mycli")

	assert.Equal(t, "mycli", rf.BinaryName)
	// Embedder binary name flows into both YAML and JSON variants.
	assert.Contains(t, rf.YAMLNames, "mycli.yaml")
	assert.Contains(t, rf.YAMLNames, "mycli.yml")
	assert.Contains(t, rf.JSONNames, "mycli.json")
	// Standard names are still recognized alongside the embedder name.
	assert.Contains(t, rf.YAMLNames, "solution.yaml")
	assert.Contains(t, rf.JSONNames, "solution.json")
}

func TestRecognizedFilesFor_EmptyBinaryFallsBack(t *testing.T) {
	rf := RecognizedFilesFor("")
	assert.Equal(t, settings.CliBinaryName, rf.BinaryName)
	assert.Contains(t, rf.YAMLNames, "solution.yaml")
}

func TestRecognizedFilesFor_SanitizesUnsafeInput(t *testing.T) {
	// A path or extension must be normalized to a bare binary name so the
	// recognized filenames are valid (matching the CLI/embedder contract).
	for _, raw := range []string{"/opt/tools/mycli", "mycli.exe", "./mycli"} {
		rf := RecognizedFilesFor(raw)
		assert.Equalf(t, "mycli", rf.BinaryName, "input %q", raw)
		assert.Containsf(t, rf.YAMLNames, "mycli.yaml", "input %q", raw)
		assert.Containsf(t, rf.JSONNames, "mycli.json", "input %q", raw)
		// No path/extension debris should leak into any recognized name.
		for _, name := range append(append([]string{}, rf.YAMLNames...), rf.JSONNames...) {
			assert.NotContainsf(t, name, "/", "input %q produced %q", raw, name)
			assert.NotContainsf(t, name, ".exe", "input %q produced %q", raw, name)
		}
	}
}

func TestRecognizedFilesFor_Deduplicates(t *testing.T) {
	rf := RecognizedFilesFor(settings.CliBinaryName)

	// solution.yaml appears in both SolutionFileNamesFor and ActionFileNamesFor;
	// the union must not duplicate it (nor any other name).
	seen := make(map[string]int)
	for _, n := range rf.YAMLNames {
		seen[n]++
	}
	for _, n := range rf.JSONNames {
		seen[n]++
	}
	for name, count := range seen {
		assert.Equalf(t, 1, count, "%q appears %d times; expected exactly one", name, count)
	}
}

func TestRecognizedFilesFor_IsSupersetOfCLIDiscovery(t *testing.T) {
	// Every name the CLI would discover (solution + action modes) must be
	// represented, so editor targeting never lags CLI auto-discovery.
	rf := RecognizedFilesFor(settings.CliBinaryName)
	all := make(map[string]struct{})
	for _, n := range rf.YAMLNames {
		all[n] = struct{}{}
	}
	for _, n := range rf.JSONNames {
		all[n] = struct{}{}
	}

	for _, name := range settings.SolutionFileNamesFor(settings.CliBinaryName) {
		_, ok := all[name]
		assert.Truef(t, ok, "solution name %q missing from RecognizedFiles", name)
	}
	for _, name := range settings.ActionFileNamesFor(settings.CliBinaryName) {
		_, ok := all[name]
		assert.Truef(t, ok, "action name %q missing from RecognizedFiles", name)
	}
}

func BenchmarkRecognizedFilesFor(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = RecognizedFilesFor(settings.CliBinaryName)
	}
}
