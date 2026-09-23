// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Solutions are decoded leniently (unknown keys are dropped), so a state key
// removed by the load/save split would otherwise be ignored silently -- and an
// ignored state.backend turns state off with no error at all. The decode hooks
// below reject the removed keys while the solution is being decoded, so every
// entrypoint (run, render, lint, embedders) fails loudly with a migration hint.

// removedKey is a state configuration key that no longer exists, with the
// migration hint shown when a solution still uses it.
type removedKey struct {
	key  string
	hint string
}

// removedKeys is one configuration level's set of removed keys.
type removedKeys []removedKey

func removedConfigKeys() removedKeys {
	return removedKeys{
		{"backend", "state.backend was split into state.load (read only) and state.save (the only write mechanism): rename backend to load, then add `save: [{extends: load}]` to keep writing to the same place"},
		{"emit", "state.emit was renamed: move each emit entry into the state.save list"},
	}
}

func removedLoadKeys() removedKeys {
	return removedKeys{
		{"saveOverrides", "saveOverrides was removed: add a state.save target with `extends: load` and put these keys in its inputs"},
		{"format", "format applies to save targets only: move it onto a state.save entry"},
		{"parameters", "parameter narrowing applies to save targets only: move it onto a state.save entry"},
	}
}

func removedSaveTargetKeys() removedKeys {
	return removedKeys{
		{"saveOverrides", "saveOverrides was removed: a save target's inputs are already resolved at save time, so put these keys in inputs"},
	}
}

// lookup finds key in the set. foldCase matches case-insensitively, as
// encoding/json matches object keys to struct fields.
func (r removedKeys) lookup(key string, foldCase bool) (removedKey, bool) {
	for _, rk := range r {
		if rk.key == key || (foldCase && strings.EqualFold(rk.key, key)) {
			return rk, true
		}
	}
	return removedKey{}, false
}

// checkYAML returns an error naming the first removed key (in document order)
// present in a YAML mapping node, including keys pulled in through a merge key
// (<<). where is the config path used in the message.
func (r removedKeys) checkYAML(node *yaml.Node, where string) error {
	return r.checkYAMLMapping(node, where, map[*yaml.Node]bool{})
}

func (r removedKeys) checkYAMLMapping(node *yaml.Node, where string, seen map[*yaml.Node]bool) error {
	if node == nil || node.Kind != yaml.MappingNode || seen[node] {
		return nil
	}
	seen[node] = true
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		if key.Tag == "!!merge" {
			for _, merged := range mergedMappings(node.Content[i+1], seen) {
				if err := r.checkYAMLMapping(merged, where, seen); err != nil {
					return err
				}
			}
			continue
		}
		if rk, ok := r.lookup(key.Value, false); ok {
			return fmt.Errorf("%w: %s.%s (line %d): %s", ErrLegacyStateConfig, where, key.Value, key.Line, rk.hint)
		}
	}
	return nil
}

// mergedMappings returns the mappings a merge key's value pulls in: an alias,
// a mapping, or a sequence of either. seen guards against alias cycles.
func mergedMappings(node *yaml.Node, seen map[*yaml.Node]bool) []*yaml.Node {
	if node == nil || seen[node] {
		return nil
	}
	switch node.Kind {
	case yaml.AliasNode:
		return mergedMappings(node.Alias, seen)
	case yaml.MappingNode:
		return []*yaml.Node{node}
	case yaml.SequenceNode:
		var out []*yaml.Node
		for _, item := range node.Content {
			out = append(out, mergedMappings(item, seen)...)
		}
		return out
	case yaml.DocumentNode, yaml.ScalarNode:
		// Not mergeable: the regular decode reports an invalid merge value.
		return nil
	}
	return nil
}

// checkJSON is the JSON counterpart of checkYAML. JSON objects are unordered, so
// the first removed key in sorted order is reported, for a deterministic
// message. Keys match case-insensitively, as encoding/json matched them to the
// removed fields.
func (r removedKeys) checkJSON(data []byte, where string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil //nolint:nilerr // not an object: the regular decode reports the real problem
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		if _, ok := r.lookup(key, true); ok {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)
	rk, _ := r.lookup(keys[0], true)
	return fmt.Errorf("%w: %s.%s: %s", ErrLegacyStateConfig, where, keys[0], rk.hint)
}

// UnmarshalYAML rejects removed state keys, then decodes the config normally.
func (c *Config) UnmarshalYAML(node *yaml.Node) error {
	if err := removedConfigKeys().checkYAML(node, "state"); err != nil {
		return err
	}
	type plain Config
	return node.Decode((*plain)(c))
}

// UnmarshalJSON rejects removed state keys, then decodes the config normally.
func (c *Config) UnmarshalJSON(data []byte) error {
	if err := removedConfigKeys().checkJSON(data, "state"); err != nil {
		return err
	}
	type plain Config
	return json.Unmarshal(data, (*plain)(c))
}

// UnmarshalYAML rejects removed load keys, then decodes the load block normally.
func (l *LoadConfig) UnmarshalYAML(node *yaml.Node) error {
	if err := removedLoadKeys().checkYAML(node, "state.load"); err != nil {
		return err
	}
	type plain LoadConfig
	return node.Decode((*plain)(l))
}

// UnmarshalJSON rejects removed load keys, then decodes the load block normally.
func (l *LoadConfig) UnmarshalJSON(data []byte) error {
	if err := removedLoadKeys().checkJSON(data, "state.load"); err != nil {
		return err
	}
	type plain LoadConfig
	return json.Unmarshal(data, (*plain)(l))
}

// UnmarshalYAML rejects removed save-target keys, then decodes the target normally.
func (t *SaveTarget) UnmarshalYAML(node *yaml.Node) error {
	if err := removedSaveTargetKeys().checkYAML(node, "state.save[]"); err != nil {
		return err
	}
	type plain SaveTarget
	return node.Decode((*plain)(t))
}

// UnmarshalJSON rejects removed save-target keys, then decodes the target normally.
func (t *SaveTarget) UnmarshalJSON(data []byte) error {
	if err := removedSaveTargetKeys().checkJSON(data, "state.save[]"); err != nil {
		return err
	}
	type plain SaveTarget
	return json.Unmarshal(data, (*plain)(t))
}
