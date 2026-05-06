// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package official

import "context"

// ListItem is a structured representation of an official provider for
// listing purposes. Both CLI and MCP surfaces use this to avoid
// duplicating the display/description logic.
type ListItem struct {
	Name         string   `json:"name" yaml:"name"`
	DisplayName  string   `json:"displayName" yaml:"displayName"`
	Description  string   `json:"description" yaml:"description"`
	Source       string   `json:"source" yaml:"source"`
	Version      string   `json:"version,omitempty" yaml:"version,omitempty"`
	Capabilities []string `json:"capabilities" yaml:"capabilities"`
}

// ListItems returns the listing items for all official providers in the
// registry found in ctx. Returns nil if no registry is available.
func ListItems(ctx context.Context) []ListItem {
	reg := RegistryFromContext(ctx)
	if reg == nil {
		return nil
	}

	names := reg.Names()
	items := make([]ListItem, 0, len(names))
	for _, name := range names {
		p, _ := reg.Get(name)
		items = append(items, ListItem{
			Name:         p.Name,
			DisplayName:  p.Name,
			Description:  "Official plugin provider (auto-fetched from catalog: " + p.CatalogRef + ")",
			Source:       "official",
			Version:      p.DefaultVersion,
			Capabilities: []string{},
		})
	}
	return items
}

// Detail returns a map with the structured detail payload for a single
// official provider. Used by both CLI structured output and MCP
// get_provider_schema fallback.
func Detail(p Provider) map[string]any {
	return map[string]any{
		"name":        p.Name,
		"source":      "official",
		"catalogRef":  p.CatalogRef,
		"version":     p.DefaultVersion,
		"description": "Official plugin provider. Auto-fetched from catalog on first use. Use 'plugins install' to pre-fetch.",
	}
}
