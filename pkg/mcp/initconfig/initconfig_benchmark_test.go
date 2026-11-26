// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package initconfig

import (
	"testing"
)

func BenchmarkUpsertJSONC(b *testing.B) {
	seed := []byte(`{
  // Workspace configuration
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "github": {
      "type": "local",
      "command": ["npx", "-y", "@modelcontextprotocol/server-github"],
      "enabled": true,
    } // keep inline note
  }
}
`)
	spec := serverEntrySpec(TargetOpenCode, "scafctl")

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := upsertJSONC(seed, true, "scafctl", spec); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRenderSkill(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = RenderSkill("scafctl", "## Extra\n\nCustom guidance.")
	}
}
