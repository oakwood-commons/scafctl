// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fingerprint

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/state"
)

// sourcesKey returns the state entry key for an action's source fingerprint.
func sourcesKey(actionName string) string {
	return fmt.Sprintf("__fingerprint:%s:sources", actionName)
}

// generatesKey returns the state entry key for an action's generates fingerprint.
func generatesKey(actionName string) string {
	return fmt.Sprintf("__fingerprint:%s:generates", actionName)
}

// inputsKey returns the state entry key for an action's inputs fingerprint.
func inputsKey(actionName string) string {
	return fmt.Sprintf("__fingerprint:%s:inputs", actionName)
}

// LoadSourcesHash retrieves the previously stored sources hash from state.
// Returns empty string if not found.
func LoadSourcesHash(data *state.Data, actionName string) string {
	if data == nil || data.Fingerprints == nil {
		return ""
	}
	entry, ok := data.Fingerprints[sourcesKey(actionName)]
	if !ok || entry == nil {
		return ""
	}
	return entry.Value
}

// LoadGeneratesHash retrieves the previously stored generates hash from state.
// Returns empty string if not found.
func LoadGeneratesHash(data *state.Data, actionName string) string {
	if data == nil || data.Fingerprints == nil {
		return ""
	}
	entry, ok := data.Fingerprints[generatesKey(actionName)]
	if !ok || entry == nil {
		return ""
	}
	return entry.Value
}

// LoadInputsHash retrieves the previously stored inputs hash from state.
// Returns empty string if not found.
func LoadInputsHash(data *state.Data, actionName string) string {
	if data == nil || data.Fingerprints == nil {
		return ""
	}
	entry, ok := data.Fingerprints[inputsKey(actionName)]
	if !ok || entry == nil {
		return ""
	}
	return entry.Value
}

// SaveHashes stores the current fingerprint hashes into state data.
// Sources hash is always stored. Generates and inputs hashes are stored
// only when non-empty.
func SaveHashes(data *state.Data, actionName, sourcesHash, generatesHash, inputsHash string) {
	if data == nil {
		return
	}
	if data.Fingerprints == nil {
		data.Fingerprints = make(map[string]*state.FingerprintEntry)
	}

	now := time.Now().UTC()

	data.Fingerprints[sourcesKey(actionName)] = &state.FingerprintEntry{
		Value:     sourcesHash,
		UpdatedAt: now,
	}

	if generatesHash != "" {
		data.Fingerprints[generatesKey(actionName)] = &state.FingerprintEntry{
			Value:     generatesHash,
			UpdatedAt: now,
		}
	} else {
		delete(data.Fingerprints, generatesKey(actionName))
	}

	if inputsHash != "" {
		data.Fingerprints[inputsKey(actionName)] = &state.FingerprintEntry{
			Value:     inputsHash,
			UpdatedAt: now,
		}
	} else {
		delete(data.Fingerprints, inputsKey(actionName))
	}
}

// KeyPrefix is the shared prefix for all fingerprint state keys.
const KeyPrefix = "__fingerprint:"

// ParseActionName extracts the action name from a fingerprint state key.
// Returns the action name and true if the key is a valid fingerprint key,
// or empty string and false otherwise.
//
// Keys have format: __fingerprint:<actionName>:<type>
func ParseActionName(key string) (string, bool) {
	if !strings.HasPrefix(key, KeyPrefix) {
		return "", false
	}
	rest := key[len(KeyPrefix):]
	lastColon := strings.LastIndex(rest, ":")
	if lastColon <= 0 || lastColon == len(rest)-1 {
		return "", false
	}
	return rest[:lastColon], true
}

// SplitKey parses a fingerprint state key into the action name and type
// (sources, generates, or inputs). Returns empty strings and false if the
// key is not a valid fingerprint key.
func SplitKey(key string) (name, typ string, ok bool) {
	name, ok = ParseActionName(key)
	if !ok {
		return "", "", false
	}
	// Key format: __fingerprint:<name>:<type>
	typ = key[len(KeyPrefix)+len(name)+1:]
	return name, typ, true
}

// ClearAction removes all fingerprint entries (sources, generates, inputs) for the
// given action name from state data. Returns the number of entries removed.
func ClearAction(data *state.Data, actionName string) int {
	if data == nil || data.Fingerprints == nil {
		return 0
	}
	removed := 0
	for _, key := range []string{sourcesKey(actionName), generatesKey(actionName), inputsKey(actionName)} {
		if _, ok := data.Fingerprints[key]; ok {
			delete(data.Fingerprints, key)
			removed++
		}
	}
	return removed
}

// ListActions returns a deduplicated, sorted list of action names that have
// fingerprint entries in the given state data.
func ListActions(data *state.Data) []string {
	if data == nil || len(data.Fingerprints) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	for key := range data.Fingerprints {
		if name, ok := ParseActionName(key); ok {
			seen[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
