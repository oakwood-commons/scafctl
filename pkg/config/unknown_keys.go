package config

import (
	"context"
	"reflect"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/logger"
)

// WarnUnknownKeys logs warnings for any configuration keys that are not
// recognized by the config schema. This helps users identify typos or
// deprecated settings in their config files.
func (m *Manager) WarnUnknownKeys(ctx context.Context) {
	if m.v == nil {
		return
	}

	knownKeys := getKnownConfigKeys()
	allKeys := m.v.AllKeys()

	for _, key := range allKeys {
		if !isKnownKey(key, knownKeys) {
			logger.FromContext(ctx).Info("unknown config key (may be a typo)", "key", key)
		}
	}
}

// GetUnknownKeys returns a list of configuration keys that are not recognized
// by the config schema. Useful for programmatic validation.
func (m *Manager) GetUnknownKeys() []string {
	if m.v == nil {
		return nil
	}

	knownKeys := getKnownConfigKeys()
	allKeys := m.v.AllKeys()

	var unknown []string
	for _, key := range allKeys {
		if !isKnownKey(key, knownKeys) {
			unknown = append(unknown, key)
		}
	}
	return unknown
}

// isKnownKey checks if a key matches any known config path.
// Handles array notation (e.g., "catalogs.0.name" matches "catalogs.*.name").
func isKnownKey(key string, knownKeys map[string]bool) bool {
	// Direct match
	if knownKeys[key] {
		return true
	}

	// Check for array element paths (e.g., catalogs.0.name -> catalogs.*.name)
	normalizedKey := normalizeArrayKey(key)
	if normalizedKey != key && knownKeys[normalizedKey] {
		return true
	}

	// Check if it's a prefix of a known key (for nested objects)
	// This handles cases like "catalogs" when we have "catalogs.*.name"
	for known := range knownKeys {
		if strings.HasPrefix(known, key+".") {
			return true
		}
	}

	return false
}

// normalizeArrayKey replaces numeric indices with wildcards.
// Example: "catalogs.0.name" -> "catalogs.*.name"
func normalizeArrayKey(key string) string {
	parts := strings.Split(key, ".")
	for i, part := range parts {
		if isNumeric(part) {
			parts[i] = "*"
		}
	}
	return strings.Join(parts, ".")
}

// isNumeric returns true if the string consists only of digits.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// getKnownConfigKeys returns a set of all valid configuration key paths
// derived from the Config struct using reflection.
// All keys are lowercased to match Viper's behavior.
func getKnownConfigKeys() map[string]bool {
	keys := make(map[string]bool)
	extractKeys(reflect.TypeOf(Config{}), "", keys)
	return keys
}

// extractKeys recursively extracts mapstructure tag paths from a struct type.
// Keys are lowercased to match Viper's behavior.
func extractKeys(t reflect.Type, prefix string, keys map[string]bool) {
	// Handle pointer types
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// Only process structs
	if t.Kind() != reflect.Struct {
		return
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Get the mapstructure tag (or json tag as fallback)
		tag := field.Tag.Get("mapstructure")
		if tag == "" {
			tag = field.Tag.Get("json")
		}
		if tag == "" {
			continue
		}

		// Handle tag options like "omitempty"
		tagName := strings.Split(tag, ",")[0]
		if tagName == "" || tagName == "-" {
			continue
		}

		// Lowercase to match Viper's key normalization
		tagName = strings.ToLower(tagName)

		// Build the full key path
		var fullKey string
		if prefix == "" {
			fullKey = tagName
		} else {
			fullKey = prefix + "." + tagName
		}

		// Add this key
		keys[fullKey] = true

		// Recurse into nested types
		fieldType := field.Type

		// Handle slices/arrays
		if fieldType.Kind() == reflect.Slice || fieldType.Kind() == reflect.Array {
			elemType := fieldType.Elem()
			// For slice of structs, add wildcard path
			if elemType.Kind() == reflect.Struct {
				extractKeys(elemType, fullKey+".*", keys)
			} else if elemType.Kind() == reflect.Ptr && elemType.Elem().Kind() == reflect.Struct {
				extractKeys(elemType.Elem(), fullKey+".*", keys)
			}
			continue
		}

		// Handle maps (only add the key itself, not nested paths for arbitrary keys)
		if fieldType.Kind() == reflect.Map {
			// For maps, we just register the map key itself
			// Users can put arbitrary keys in maps like "metadata"
			continue
		}

		// Handle pointers
		if fieldType.Kind() == reflect.Ptr {
			fieldType = fieldType.Elem()
		}

		// Recurse into nested structs
		if fieldType.Kind() == reflect.Struct {
			extractKeys(fieldType, fullKey, keys)
		}
	}
}
