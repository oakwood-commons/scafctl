// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fingerprint

import (
	"fmt"
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
