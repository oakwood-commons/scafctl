// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import "time"

// NewMockData creates a StateData populated with test data.
// Use this in tests that need a pre-populated state.
func NewMockData(solution, version string, values map[string]*Entry) *Data {
	now := time.Now().UTC()
	data := NewData()
	data.Metadata = Metadata{
		Solution:       solution,
		Version:        version,
		CreatedAt:      now,
		LastUpdatedAt:  now,
		ScafctlVersion: "test",
	}
	if values != nil {
		data.Values = values
	}
	return data
}
