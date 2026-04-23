// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

//go:embed defaults.yaml
var defaultsYAML []byte

// DefaultsYAML returns a copy of the embedded defaults.yaml content.
// Embedders can inspect or forward this to WithBaseConfig.
func DefaultsYAML() []byte {
	cp := make([]byte, len(defaultsYAML))
	copy(cp, defaultsYAML)
	return cp
}

// EmbeddedGitHubDefaults parses the embedded defaults.yaml and returns the
// GitHub auth section. Returns nil if the section is absent or unparseable.
func EmbeddedGitHubDefaults() *GitHubAuthConfig {
	var cfg Config
	if err := yaml.Unmarshal(defaultsYAML, &cfg); err != nil {
		return nil
	}
	return cfg.Auth.GitHub
}

// EmbeddedEntraDefaults parses the embedded defaults.yaml and returns the
// Entra auth section. Returns nil if the section is absent or unparseable.
func EmbeddedEntraDefaults() *EntraAuthConfig {
	var cfg Config
	if err := yaml.Unmarshal(defaultsYAML, &cfg); err != nil {
		return nil
	}
	return cfg.Auth.Entra
}

// EmbeddedCatalogDefaults parses the embedded defaults.yaml and returns the
// catalog entries. Returns nil if the section is absent or unparseable.
func EmbeddedCatalogDefaults() []CatalogConfig {
	var cfg Config
	if err := yaml.Unmarshal(defaultsYAML, &cfg); err != nil {
		return nil
	}
	return cfg.Catalogs
}

// EnsureDefaults writes the embedded default config to configPath when no
// config file exists. When a config file already exists, it merges in any
// missing catalog entries without overwriting values the user has customised.
func EnsureDefaults(configPath string) error {
	if _, err := os.Stat(configPath); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("checking config file: %w", err)
		}
		// No config file -- write the full embedded defaults.
		if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
			return fmt.Errorf("creating config directory: %w", err)
		}
		return writeDefaultsFile(configPath, defaultsYAML)
	}

	// Config exists -- merge missing defaults into it.
	return mergeDefaults(configPath)
}

// writeDefaultsFile writes data with 0600 permissions.
func writeDefaultsFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

// mergeDefaults reads the existing config and the embedded defaults, then adds
// any missing catalog entries (by name) from the defaults. Existing entries
// are never modified.
func mergeDefaults(configPath string) error {
	existingData, err := os.ReadFile(configPath) //nolint:gosec // config path from trusted source
	if err != nil {
		return fmt.Errorf("reading existing config: %w", err)
	}

	var existing, defs map[string]any
	if err := yaml.Unmarshal(existingData, &existing); err != nil {
		return fmt.Errorf("parsing existing config: %w", err)
	}
	if err := yaml.Unmarshal(defaultsYAML, &defs); err != nil {
		return fmt.Errorf("parsing embedded defaults: %w", err)
	}
	if existing == nil {
		existing = make(map[string]any)
	}

	changed := false
	changed = mergeCatalogDefaults(existing, defs) || changed
	changed = mergeDefaultCatalogSetting(existing, defs) || changed

	if !changed {
		return nil
	}

	out, err := yaml.Marshal(existing)
	if err != nil {
		return fmt.Errorf("marshaling merged config: %w", err)
	}
	return writeDefaultsFile(configPath, out)
}

// mergeCatalogDefaults adds any catalog entries from defaults that are not
// already present (matched by name) in the existing config.
//
// Reserved catalog names ("local", "official") have all their fields
// overwritten from the defaults so they cannot be redirected by user config.
// Non-reserved entries that already exist get missing fields backfilled
// without overwriting user-customised values.
func mergeCatalogDefaults(existing, defs map[string]any) bool {
	defaultCatalogs := toSlice(defs["catalogs"])
	if len(defaultCatalogs) == 0 {
		return false
	}

	existingCatalogs := toSlice(existing["catalogs"])
	nameIndex := make(map[string]int, len(existingCatalogs))
	for i, c := range existingCatalogs {
		if m, ok := c.(map[string]any); ok {
			if name, ok := m["name"].(string); ok {
				nameIndex[name] = i
			}
		}
	}

	changed := false
	for _, c := range defaultCatalogs {
		dm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		name, ok := dm["name"].(string)
		if !ok {
			continue
		}
		if idx, exists := nameIndex[name]; exists {
			if IsReservedCatalogName(name) {
				// Reserved: replace the entire entry with the default.
				existingCatalogs[idx] = c
				changed = true
			} else {
				// Non-reserved: backfill missing fields only.
				em, _ := existingCatalogs[idx].(map[string]any)
				if em == nil {
					continue
				}
				for k, v := range dm {
					if _, has := em[k]; !has {
						em[k] = v
						changed = true
					}
				}
			}
		} else {
			existingCatalogs = append(existingCatalogs, c)
			changed = true
		}
	}
	if changed {
		existing["catalogs"] = existingCatalogs
	}
	return changed
}

// mergeDefaultCatalogSetting sets settings.defaultCatalog from defaults when
// the existing config does not already specify one.
func mergeDefaultCatalogSetting(existing, defs map[string]any) bool {
	defaultSettings, _ := defs["settings"].(map[string]any)
	if defaultSettings == nil {
		return false
	}
	defaultCatalog, _ := defaultSettings["defaultCatalog"].(string)
	if defaultCatalog == "" {
		return false
	}

	if existing["settings"] == nil {
		existing["settings"] = make(map[string]any)
	}
	existingSettings, _ := existing["settings"].(map[string]any)
	if existingSettings == nil {
		return false
	}

	if v, _ := existingSettings["defaultCatalog"].(string); v != "" {
		return false // user already set a default catalog
	}
	existingSettings["defaultCatalog"] = defaultCatalog
	return true
}

// toSlice converts an any that is expected to be []any (from YAML
// unmarshalling). Returns nil if the conversion fails.
func toSlice(v any) []any {
	if v == nil {
		return nil
	}
	s, _ := v.([]any)
	return s
}
