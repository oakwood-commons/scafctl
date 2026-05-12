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
	if data == nil || data.Values == nil {
		return ""
	}
	entry, ok := data.Values[sourcesKey(actionName)]
	if !ok || entry == nil {
		return ""
	}
	s, _ := entry.Value.(string)
	return s
}

// LoadGeneratesHash retrieves the previously stored generates hash from state.
// Returns empty string if not found.
func LoadGeneratesHash(data *state.Data, actionName string) string {
	if data == nil || data.Values == nil {
		return ""
	}
	entry, ok := data.Values[generatesKey(actionName)]
	if !ok || entry == nil {
		return ""
	}
	s, _ := entry.Value.(string)
	return s
}

// LoadInputsHash retrieves the previously stored inputs hash from state.
// Returns empty string if not found.
func LoadInputsHash(data *state.Data, actionName string) string {
	if data == nil || data.Values == nil {
		return ""
	}
	entry, ok := data.Values[inputsKey(actionName)]
	if !ok || entry == nil {
		return ""
	}
	s, _ := entry.Value.(string)
	return s
}

// SaveHashes stores the current fingerprint hashes into state data.
// Sources hash is always stored. Generates and inputs hashes are stored
// only when non-empty.
func SaveHashes(data *state.Data, actionName, sourcesHash, generatesHash, inputsHash string) {
	if data == nil {
		return
	}
	if data.Values == nil {
		data.Values = make(map[string]*state.Entry)
	}

	data.Values[sourcesKey(actionName)] = &state.Entry{
		Value:     sourcesHash,
		Type:      "string",
		UpdatedAt: time.Now().UTC(),
	}

	if generatesHash != "" {
		data.Values[generatesKey(actionName)] = &state.Entry{
			Value:     generatesHash,
			Type:      "string",
			UpdatedAt: time.Now().UTC(),
		}
	}

	if inputsHash != "" {
		data.Values[inputsKey(actionName)] = &state.Entry{
			Value:     inputsHash,
			Type:      "string",
			UpdatedAt: time.Now().UTC(),
		}
	}
}
