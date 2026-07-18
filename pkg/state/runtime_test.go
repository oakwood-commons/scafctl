// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/stretchr/testify/assert"
)

func TestRuntimeMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		prov        settings.RuntimeProvenance
		wantEngine  RuntimeComponent
		wantCLIName string
		wantCLIVer  string
	}{
		{
			name:        "cli fields mirror engine when empty",
			prov:        settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "1.2.3"},
			wantEngine:  RuntimeComponent{Name: "scafctl", Version: "1.2.3"},
			wantCLIName: "scafctl",
			wantCLIVer:  "1.2.3",
		},
		{
			name: "embedder cli distinct from engine",
			prov: settings.RuntimeProvenance{
				EngineName:    "scafctl",
				EngineVersion: "1.2.3",
				CLIName:       "mycli",
				CLIVersion:    "9.9.9",
			},
			wantEngine:  RuntimeComponent{Name: "scafctl", Version: "1.2.3"},
			wantCLIName: "mycli",
			wantCLIVer:  "9.9.9",
		},
		{
			name: "embedder name but no version falls back to engine version",
			prov: settings.RuntimeProvenance{
				EngineName:    "scafctl",
				EngineVersion: "1.2.3",
				CLIName:       "mycli",
			},
			wantEngine:  RuntimeComponent{Name: "scafctl", Version: "1.2.3"},
			wantCLIName: "mycli",
			wantCLIVer:  "1.2.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := runtimeMetadata(tt.prov)
			assert.Equal(t, tt.wantEngine, got.Engine)
			assert.Equal(t, tt.wantCLIName, got.CLI.Name)
			assert.Equal(t, tt.wantCLIVer, got.CLI.Version)
		})
	}
}

func TestRuntimeProvenanceFromContext_Embedder(t *testing.T) {
	t.Parallel()

	ctx := settings.IntoContext(context.Background(), &settings.Run{
		BinaryName:      "mycli",
		EmbedderVersion: "4.5.6",
	})

	rt := runtimeMetadata(RuntimeProvenanceFromContext(ctx))

	// Engine is always scafctl at its build version.
	assert.Equal(t, settings.CliBinaryName, rt.Engine.Name)
	assert.Equal(t, settings.VersionInformation.BuildVersion, rt.Engine.Version)

	// CLI reflects the embedder identity, distinct from the engine.
	assert.Equal(t, "mycli", rt.CLI.Name)
	assert.Equal(t, "4.5.6", rt.CLI.Version)
	assert.NotEqual(t, rt.Engine.Name, rt.CLI.Name)
}

func TestRuntimeProvenanceFromContext_DirectUse(t *testing.T) {
	t.Parallel()

	// No settings in context: CLI mirrors the engine.
	rt := runtimeMetadata(RuntimeProvenanceFromContext(context.Background()))

	assert.Equal(t, settings.CliBinaryName, rt.Engine.Name)
	assert.Equal(t, rt.Engine.Name, rt.CLI.Name)
	assert.Equal(t, rt.Engine.Version, rt.CLI.Version)
}
