// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"sort"
	"strings"
)

// Source identifies where a configuration key's effective value came from.
// Values are ordered from lowest to highest precedence, matching Load().
type Source string

const (
	// SourceDefault means the value came from built-in defaults or the
	// embedder base layer (nothing user-authored overrode it).
	SourceDefault Source = "default"

	// SourceDropIn means the value came from a config.d fragment.
	SourceDropIn Source = "dropin"

	// SourceFile means the value came from the user's main config file.
	SourceFile Source = "file"

	// SourceEnv means the value came from an environment variable
	// (highest precedence).
	SourceEnv Source = "env"
)

// AllSources enumerates every Source in precedence order.
var AllSources = []Source{SourceDefault, SourceDropIn, SourceFile, SourceEnv} //nolint:gochecknoglobals // catalog of enum values

// ValidSource reports whether s is one of the known Source values.
func ValidSource(s Source) bool {
	for _, known := range AllSources {
		if s == known {
			return true
		}
	}
	return false
}

// EnvOverride is a single environment variable that matches the configured
// env prefix. Value is already redacted if the key looks sensitive.
type EnvOverride struct {
	Key   string `json:"key" yaml:"key" doc:"Environment variable name"`
	Value string `json:"value" yaml:"value" doc:"Environment variable value (redacted if sensitive)"`
}

// sensitiveEnvKeywords lists case-insensitive substrings that mark an env var
// as sensitive. Values whose key contains any of these words are redacted.
// This is a redaction policy tied to config presentation, so it lives here
// rather than in pkg/settings (which houses runtime tuning constants).
var sensitiveEnvKeywords = []string{ //nolint:gochecknoglobals // shared allowlist
	"secret", "password", "token", "credential", "apikey", "api_key",
	"private_key", "privatekey", "bearer", "session", "cookie", "passphrase",
}

// RedactSensitiveEnv returns RedactedValue when key contains a sensitive
// keyword (case-insensitive), otherwise returns value unchanged.
func RedactSensitiveEnv(key, value string) string {
	lower := strings.ToLower(key)
	for _, kw := range sensitiveEnvKeywords {
		if strings.Contains(lower, kw) {
			return RedactedValue
		}
	}
	return value
}

// EnvOverrides returns every environment variable whose name starts with the
// configured env prefix (e.g. SCAFCTL_) in stable sorted order. Values are
// redacted via RedactSensitiveEnv. Load need not have been called first. The
// returned slice is always non-nil so JSON callers see `[]` rather than `null`.
func (m *Manager) EnvOverrides() []EnvOverride {
	prefix := m.envPrefix + "_"
	out := []EnvOverride{}
	for _, env := range os.Environ() {
		key, value, ok := strings.Cut(env, "=")
		if !ok {
			continue
		}
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		out = append(out, EnvOverride{Key: key, Value: RedactSensitiveEnv(key, value)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Sources returns a flat, dot-keyed map identifying the effective source of
// every configuration key currently loaded in the Manager. Load must have
// been called first; on an unloaded Manager the returned map is empty.
//
// Precedence mirrors Load: env > file > dropin > default. A key is reported
// as SourceEnv if a matching env variable is currently set, as SourceFile if
// it appears in the user's config file, as SourceDropIn if it comes from a
// config.d fragment, and otherwise as SourceDefault.
//
// Slice-valued keys (notably "catalogs") are classified by their top-level
// origin only. Because viper replaces slices wholesale during merge and
// reserved default entries are re-added after Load, the reported source for
// "catalogs" reflects whichever layer supplied the outermost list, not the
// origin of individual entries.
//
// Manager is not safe for concurrent use; Sources must not be called while
// Load is running on another goroutine.
func (m *Manager) Sources() map[string]Source {
	keys := m.v.AllKeys()
	out := make(map[string]Source, len(keys))

	envKeys := m.envKeysToDotted()
	fileKeys := flattenKeys(m.userSettings)
	dropInKeys := flattenKeys(m.dropIn)

	for _, k := range keys {
		lk := strings.ToLower(k)
		switch {
		case envKeys[lk]:
			out[k] = SourceEnv
		case fileKeys[lk]:
			out[k] = SourceFile
		case dropInKeys[lk]:
			out[k] = SourceDropIn
		default:
			out[k] = SourceDefault
		}
	}
	return out
}

// envKeysToDotted returns the set of dotted config keys currently supplied by
// the process environment under the Manager's env prefix. Keys are lowercased
// to match viper's key normalization.
func (m *Manager) envKeysToDotted() map[string]bool {
	prefix := m.envPrefix + "_"
	out := make(map[string]bool)
	for _, env := range os.Environ() {
		key, _, ok := strings.Cut(env, "=")
		if !ok || !strings.HasPrefix(key, prefix) {
			continue
		}
		trimmed := strings.TrimPrefix(key, prefix)
		if trimmed == "" {
			continue
		}
		out[strings.ToLower(strings.ReplaceAll(trimmed, "_", "."))] = true
	}
	return out
}

// flattenKeys returns the set of dotted leaf keys present in a nested map,
// with each path lowercased so lookups match viper's normalization. Only
// leaves are included; intermediate map nodes are traversed but not recorded.
func flattenKeys(m map[string]any) map[string]bool {
	out := make(map[string]bool)
	flattenKeysInto(out, "", m)
	return out
}

func flattenKeysInto(out map[string]bool, prefix string, m map[string]any) {
	for k, v := range m {
		key := strings.ToLower(k)
		if prefix != "" {
			key = prefix + "." + key
		}
		if nested, ok := v.(map[string]any); ok {
			flattenKeysInto(out, key, nested)
			continue
		}
		out[key] = true
	}
}

// FilterMapBySource walks data (a StructToMap-shaped tree of nested maps) and
// returns a copy containing only leaves whose dotted-path source matches only.
// Non-matching branches are pruned; empty maps are dropped. Slice- and
// scalar-valued leaves are kept only when their own key's source matches -
// individual slice entries are not addressable through viper's dotted keys.
//
// prefix is the dotted key of data within the overall config (empty for the
// root of a top-level section like "settings" or "catalogs").
func FilterMapBySource(data map[string]any, sources map[string]Source, only Source, prefix string) map[string]any {
	out := make(map[string]any, len(data))
	for k, v := range data {
		childKey := strings.ToLower(k)
		if prefix != "" {
			childKey = prefix + "." + childKey
		}
		// Recurse into nested maps first; the child may contain matching leaves
		// even if the parent key itself has a different source.
		if nested, ok := v.(map[string]any); ok {
			if pruned := FilterMapBySource(nested, sources, only, childKey); len(pruned) > 0 {
				out[k] = pruned
			}
			continue
		}
		if sources[childKey] == only {
			out[k] = v
		}
	}
	return out
}
