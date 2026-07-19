// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package settings

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRuntimeProvenance_Resolved(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		prov        RuntimeProvenance
		wantCLIName string
		wantCLIVer  string
	}{
		{
			name:        "cli mirrors engine when empty",
			prov:        RuntimeProvenance{EngineName: "scafctl", EngineVersion: "1.2.3"},
			wantCLIName: "scafctl",
			wantCLIVer:  "1.2.3",
		},
		{
			name:        "cli overrides both",
			prov:        RuntimeProvenance{EngineName: "scafctl", EngineVersion: "1.2.3", CLIName: "mycli", CLIVersion: "9.9.9"},
			wantCLIName: "mycli",
			wantCLIVer:  "9.9.9",
		},
		{
			name:        "cli name only, version falls back",
			prov:        RuntimeProvenance{EngineName: "scafctl", EngineVersion: "1.2.3", CLIName: "mycli"},
			wantCLIName: "mycli",
			wantCLIVer:  "1.2.3",
		},
		{
			name:        "zero value yields empty",
			prov:        RuntimeProvenance{},
			wantCLIName: "",
			wantCLIVer:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.wantCLIName, tt.prov.ResolvedCLIName())
			assert.Equal(t, tt.wantCLIVer, tt.prov.ResolvedCLIVersion())
		})
	}
}

func TestRuntimeProvenanceFromContext(t *testing.T) {
	t.Parallel()

	t.Run("embedder identity from settings", func(t *testing.T) {
		t.Parallel()
		ctx := IntoContext(context.Background(), &Run{BinaryName: "mycli", EmbedderVersion: "4.5.6"})
		p := RuntimeProvenanceFromContext(ctx, "0.1.0")

		assert.Equal(t, CliBinaryName, p.EngineName)
		assert.Equal(t, "0.1.0", p.EngineVersion)
		assert.Equal(t, "mycli", p.ResolvedCLIName())
		assert.Equal(t, "4.5.6", p.ResolvedCLIVersion())
	})

	t.Run("direct use mirrors engine", func(t *testing.T) {
		t.Parallel()
		p := RuntimeProvenanceFromContext(context.Background(), "0.1.0")

		assert.Equal(t, CliBinaryName, p.EngineName)
		assert.Equal(t, "0.1.0", p.EngineVersion)
		assert.Equal(t, CliBinaryName, p.ResolvedCLIName())
		assert.Equal(t, "0.1.0", p.ResolvedCLIVersion())
	})

	t.Run("empty engine version falls back to build version", func(t *testing.T) {
		t.Parallel()
		p := RuntimeProvenanceFromContext(context.Background(), "")
		assert.Equal(t, VersionInformation.BuildVersion, p.EngineVersion)
	})
}
