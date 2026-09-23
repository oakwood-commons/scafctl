// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// solutionDoc mirrors how a solution embeds its state block.
type solutionDoc struct {
	State *Config `yaml:"state" json:"state"`
}

func TestLegacyStateKeys_YAML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		doc      string
		wantPath string
		wantHint string
	}{
		{
			name:     "state.backend",
			doc:      "state:\n  backend:\n    provider: file\n",
			wantPath: "state.backend (line 2)",
			wantHint: "save: [{extends: load}]",
		},
		{
			name:     "state.emit",
			doc:      "state:\n  emit:\n    - provider: file\n",
			wantPath: "state.emit (line 2)",
			wantHint: "move each emit entry into the state.save list",
		},
		{
			name:     "state.load.saveOverrides",
			doc:      "state:\n  load:\n    provider: file\n    saveOverrides:\n      branch: feature\n",
			wantPath: "state.load.saveOverrides (line 4)",
			wantHint: "extends: load",
		},
		{
			name:     "state.load.format",
			doc:      "state:\n  load:\n    provider: file\n    format: intent\n",
			wantPath: "state.load.format (line 4)",
			wantHint: "move it onto a state.save entry",
		},
		{
			name:     "state.load.parameters",
			doc:      "state:\n  load:\n    provider: file\n    parameters:\n      include: [a]\n",
			wantPath: "state.load.parameters (line 4)",
			wantHint: "move it onto a state.save entry",
		},
		{
			name:     "state.save[].saveOverrides",
			doc:      "state:\n  save:\n    - extends: load\n      saveOverrides:\n        branch: feature\n",
			wantPath: "state.save[].saveOverrides (line 4)",
			wantHint: "put these keys in inputs",
		},
		{
			name:     "the first removed key in document order is reported",
			doc:      "state:\n  emit: []\n  backend:\n    provider: file\n",
			wantPath: "state.emit (line 2)",
		},
		{
			name:     "a removed key pulled in through a merge key",
			doc:      "defaults: &d\n  backend:\n    provider: file\nstate:\n  <<: *d\n",
			wantPath: "state.backend (line 2)",
		},
		{
			name:     "a removed key pulled in through a sequence of merges",
			doc:      "a: &a\n  enabled: true\nb: &b\n  emit: []\nstate:\n  <<: [*a, *b]\n",
			wantPath: "state.emit (line 4)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var doc solutionDoc
			err := yaml.Unmarshal([]byte(tt.doc), &doc)
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrLegacyStateConfig), "got %v", err)
			assert.Contains(t, err.Error(), tt.wantPath)
			assert.Contains(t, err.Error(), tt.wantHint)
		})
	}

	t.Run("a non-mapping state reports the regular decode error", func(t *testing.T) {
		t.Parallel()
		var doc solutionDoc
		err := yaml.Unmarshal([]byte("state: file\n"), &doc)
		require.Error(t, err)
		assert.False(t, errors.Is(err, ErrLegacyStateConfig))
	})
}

func TestLegacyStateKeys_JSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		doc      string
		wantPath string
	}{
		{name: "state.backend", doc: `{"state":{"backend":{"provider":"file"}}}`, wantPath: "state.backend:"},
		{name: "state.emit", doc: `{"state":{"emit":[]}}`, wantPath: "state.emit:"},
		{name: "state.load.format", doc: `{"state":{"load":{"provider":"file","format":"intent"}}}`, wantPath: "state.load.format:"},
		{name: "state.save[].saveOverrides", doc: `{"state":{"save":[{"provider":"file","saveOverrides":{}}]}}`, wantPath: "state.save[].saveOverrides:"},
		{name: "several removed keys report the first in sorted order", doc: `{"state":{"emit":[],"backend":{}}}`, wantPath: "state.backend:"},
		{name: "keys match case-insensitively, as encoding/json did", doc: `{"state":{"Backend":{"provider":"file"}}}`, wantPath: "state.Backend:"},
		{name: "a case-folded save target key", doc: `{"state":{"save":[{"provider":"file","SAVEOVERRIDES":{}}]}}`, wantPath: "state.save[].SAVEOVERRIDES:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var doc solutionDoc
			err := json.Unmarshal([]byte(tt.doc), &doc)
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrLegacyStateConfig), "got %v", err)
			assert.Contains(t, err.Error(), tt.wantPath)
		})
	}

	t.Run("a non-object state reports the regular decode error", func(t *testing.T) {
		t.Parallel()
		var doc solutionDoc
		err := json.Unmarshal([]byte(`{"state":"file"}`), &doc)
		require.Error(t, err)
		assert.False(t, errors.Is(err, ErrLegacyStateConfig))
	})
}

func TestLoadSaveConfig_Decodes(t *testing.T) {
	t.Parallel()

	const yamlDoc = `
state:
  enabled: true
  load:
    provider: github
    inputs:
      path: state/app.json
      ref: main
  save:
    - extends: load
      checkpoint: true
      inputs:
        branch:
          rslvr: featureBranch
    - provider: file
      format: intent
      parameters:
        exclude: [mode]
      enabled:
        expr: "_.publish"
      inputs:
        path: intent.json
`
	var fromYAML solutionDoc
	require.NoError(t, yaml.Unmarshal([]byte(yamlDoc), &fromYAML))
	assertDecodedConfig(t, fromYAML.State)

	// The same document through the JSON path (JSON is YAML-compatible).
	var generic map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(yamlDoc), &generic))
	raw, err := json.Marshal(generic)
	require.NoError(t, err)
	var fromJSON solutionDoc
	require.NoError(t, json.Unmarshal(raw, &fromJSON))
	assertDecodedConfig(t, fromJSON.State)
}

func assertDecodedConfig(t *testing.T, cfg *Config) {
	t.Helper()
	require.NotNil(t, cfg)
	require.NotNil(t, cfg.Enabled)
	assert.Equal(t, true, cfg.Enabled.Literal)

	require.NotNil(t, cfg.Load)
	assert.Equal(t, "github", cfg.Load.Provider)
	assert.Equal(t, "state/app.json", cfg.Load.Inputs["path"].Literal)
	assert.Equal(t, "main", cfg.Load.Inputs["ref"].Literal)

	require.Len(t, cfg.Save, 2)
	first := cfg.Save[0]
	assert.Equal(t, ExtendsLoad, first.Extends)
	assert.True(t, first.Checkpoint)
	require.NotNil(t, first.Inputs["branch"].Resolver)
	assert.Equal(t, "featureBranch", *first.Inputs["branch"].Resolver)

	second := cfg.Save[1]
	assert.Equal(t, "file", second.Provider)
	assert.Equal(t, FormatIntent, second.Format)
	require.NotNil(t, second.Parameters)
	assert.Equal(t, []string{"mode"}, second.Parameters.Exclude)
	require.NotNil(t, second.Enabled)
	require.NotNil(t, second.Enabled.Expr)
	assert.Equal(t, "intent.json", second.Inputs["path"].Literal)
}

// benchStateYAML is a representative state block; the decode hooks run on
// every solution load (including each LSP re-parse).
const benchStateYAML = `
state:
  enabled: true
  load:
    provider: file
    inputs:
      path: .state/app.json
  save:
    - extends: load
      checkpoint: true
    - provider: file
      format: intent
      parameters:
        exclude: [mode]
      inputs:
        path: intent.json
`

func BenchmarkConfigUnmarshalYAML(b *testing.B) {
	data := []byte(benchStateYAML)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var doc solutionDoc
		if err := yaml.Unmarshal(data, &doc); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConfigUnmarshalJSON(b *testing.B) {
	var generic map[string]any
	if err := yaml.Unmarshal([]byte(benchStateYAML), &generic); err != nil {
		b.Fatal(err)
	}
	data, err := json.Marshal(generic)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var doc solutionDoc
		if err := json.Unmarshal(data, &doc); err != nil {
			b.Fatal(err)
		}
	}
}
