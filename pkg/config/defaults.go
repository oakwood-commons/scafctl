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
	return EnsureDefaultsWith(configPath, defaultsYAML)
}

// EnsureDefaultsWith is like EnsureDefaults but uses caller-supplied defaults
// bytes instead of the embedded defaults.yaml. Embedders use this to bootstrap
// the on-disk config from their own product-specific defaults so the config
// file matches their runtime ConfigDefaults.
//
// Behavior:
//   - If config does not exist, write the supplied defaults verbatim.
//   - If config exists, merge missing catalog entries and settings according
//     to the same rules as EnsureDefaults (reserved-catalog protection, etc.).
func EnsureDefaultsWith(configPath string, customDefaults []byte) error {
	if _, err := os.Stat(configPath); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("checking config file: %w", err)
		}
		// No config file -- write the full defaults.
		if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
			return fmt.Errorf("creating config directory: %w", err)
		}
		return writeDefaultsFile(configPath, customDefaults)
	}

	// Config exists -- merge missing defaults into it.
	return mergeDefaults(configPath, customDefaults)
}

// writeDefaultsFile writes data with 0600 permissions.
func writeDefaultsFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

// mergeDefaults reads the existing config and the provided defaults, then adds
// any missing catalog entries (by name) from the defaults. Existing entries
// are never modified.
func mergeDefaults(configPath string, defsYAML []byte) error {
	existingData, err := os.ReadFile(configPath) //nolint:gosec // config path from trusted source
	if err != nil {
		return fmt.Errorf("reading existing config: %w", err)
	}

	var existing, defs map[string]any
	if err := yaml.Unmarshal(existingData, &existing); err != nil {
		return fmt.Errorf("parsing existing config: %w", err)
	}
	if err := yaml.Unmarshal(defsYAML, &defs); err != nil {
		return fmt.Errorf("parsing provided defaults: %w", err)
	}
	if existing == nil {
		existing = make(map[string]any)
	}

	changed := false
	changed = mergeCatalogDefaults(existing, defs) || changed
	changed = mergeDefaultCatalogSetting(existing, defs) || changed
	changed = mergeAuthDefaults(existing, defs) || changed

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

// mergeAuthDefaults merges auth handler defaults from the provided defaults
// into the existing config. For each known auth handler (github, entra, gcp),
// it backfills missing fields (of any type except defaultScopes, which is
// merged additively) and appends new scope entries from defaults that are not
// already present. This ensures updated embedded defaults (e.g., new OAuth
// scopes or new configuration keys) propagate to existing user config files.
func mergeAuthDefaults(existing, defs map[string]any) bool {
	defaultAuth, _ := defs["auth"].(map[string]any)
	if defaultAuth == nil {
		return false
	}

	if existing["auth"] == nil {
		existing["auth"] = make(map[string]any)
	}
	existingAuth, _ := existing["auth"].(map[string]any)
	if existingAuth == nil {
		return false
	}

	changed := false
	// Merge each known auth handler section.
	for _, handler := range []string{"github", "entra", "gcp"} {
		defHandler, _ := defaultAuth[handler].(map[string]any)
		if defHandler == nil {
			continue
		}

		if existingAuth[handler] == nil {
			// Handler not in user config — add entire section from defaults.
			existingAuth[handler] = defHandler
			changed = true
			continue
		}

		existingHandler, _ := existingAuth[handler].(map[string]any)
		if existingHandler == nil {
			continue
		}

		// Backfill missing scalar fields.
		for k, v := range defHandler {
			if k == "defaultScopes" {
				continue // handled separately below
			}
			if _, has := existingHandler[k]; !has {
				existingHandler[k] = v
				changed = true
			}
		}

		// Additively merge defaultScopes.
		if mergeDefaultScopes(existingHandler, defHandler) {
			changed = true
		}
	}

	// Merge customOAuth2 entries by name (backfill missing entries only).
	if mergeCustomOAuth2Defaults(existingAuth, defaultAuth) {
		changed = true
	}

	return changed
}

// mergeDefaultScopes additively merges defaultScopes from defHandler into
// existingHandler. New scope entries from defaults that are not already
// present in the existing list are appended.
func mergeDefaultScopes(existingHandler, defHandler map[string]any) bool {
	defScopes := toStringSlice(defHandler["defaultScopes"])
	if len(defScopes) == 0 {
		return false
	}

	existingScopes := toStringSlice(existingHandler["defaultScopes"])
	scopeSet := make(map[string]struct{}, len(existingScopes))
	for _, s := range existingScopes {
		scopeSet[s] = struct{}{}
	}

	changed := false
	for _, s := range defScopes {
		if _, has := scopeSet[s]; !has {
			existingScopes = append(existingScopes, s)
			scopeSet[s] = struct{}{}
			changed = true
		}
	}

	if changed {
		// Store as []any to match YAML unmarshal format.
		result := make([]any, len(existingScopes))
		for i, s := range existingScopes {
			result[i] = s
		}
		existingHandler["defaultScopes"] = result
	}
	return changed
}

// mergeCustomOAuth2Defaults adds any customOAuth2 entries from defaults that
// are not already present (matched by name) in the existing auth config.
// Existing entries are intentionally never modified -- customOAuth2 handlers
// are user-defined configurations and their fields should not be overwritten
// by defaults. Only entirely new entries (by name) are appended.
func mergeCustomOAuth2Defaults(existingAuth, defaultAuth map[string]any) bool {
	defaultCustom := toSlice(defaultAuth["customOAuth2"])
	if len(defaultCustom) == 0 {
		return false
	}

	existingCustom := toSlice(existingAuth["customOAuth2"])
	nameSet := make(map[string]struct{}, len(existingCustom))
	for _, c := range existingCustom {
		if m, ok := c.(map[string]any); ok {
			if name, ok := m["name"].(string); ok {
				nameSet[name] = struct{}{}
			}
		}
	}

	changed := false
	for _, c := range defaultCustom {
		dm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		name, _ := dm["name"].(string)
		if name == "" {
			continue
		}
		if _, exists := nameSet[name]; !exists {
			existingCustom = append(existingCustom, c)
			changed = true
		}
	}

	if changed {
		existingAuth["customOAuth2"] = existingCustom
	}
	return changed
}

// toStringSlice converts an any that is expected to be []any of strings (from
// YAML unmarshalling) into a []string. Returns nil if the underlying value is
// not a slice (via toSlice). Non-string items within the slice are silently
// skipped, so the result may be shorter than the input.
func toStringSlice(v any) []string {
	items := toSlice(v)
	if items == nil {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
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
