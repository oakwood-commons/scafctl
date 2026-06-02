// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewStateData(t *testing.T) {
	data := NewData()
	assert.Equal(t, SchemaVersionCurrent, data.SchemaVersion)
	assert.NotNil(t, data.Parameters)
	assert.Empty(t, data.Parameters)
	assert.NotNil(t, data.Immutables)
	assert.Empty(t, data.Immutables)
	assert.NotNil(t, data.Fingerprints)
	assert.Empty(t, data.Fingerprints)
	assert.NotNil(t, data.Command.Parameters)
	assert.Empty(t, data.Command.Parameters)
}

func TestStateData_JSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC)

	original := &Data{
		SchemaVersion: 1,
		Metadata: Metadata{
			Solution:       "deploy-app",
			Version:        "1.0.0",
			CreatedAt:      now,
			LastUpdatedAt:  now,
			ScafctlVersion: "0.9.0",
		},
		Command: CommandInfo{
			Subcommand: "run solution",
			Parameters: map[string]string{
				"project": "foo",
			},
		},
		Parameters: map[string]any{
			"env":    "prod",
			"region": "us-east-1",
		},
		Immutables: map[string]*ImmutableEntry{
			"cluster_id": {
				Value:     "uuid-1234",
				Type:      "string",
				CreatedAt: now,
			},
		},
		Fingerprints: map[string]*FingerprintEntry{
			"__fingerprint:deploy:sources": {
				Value:     "abc123",
				UpdatedAt: now,
			},
		},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored Data
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, original.SchemaVersion, restored.SchemaVersion)
	assert.Equal(t, original.Metadata.Solution, restored.Metadata.Solution)
	assert.Equal(t, original.Metadata.Version, restored.Metadata.Version)
	assert.Equal(t, original.Metadata.ScafctlVersion, restored.Metadata.ScafctlVersion)
	assert.Equal(t, original.Command.Subcommand, restored.Command.Subcommand)
	assert.Equal(t, original.Command.Parameters, restored.Command.Parameters)
	assert.Len(t, restored.Parameters, 2)
	assert.Equal(t, "prod", restored.Parameters["env"])
	assert.Equal(t, "us-east-1", restored.Parameters["region"])
	assert.Len(t, restored.Immutables, 1)
	assert.Equal(t, "uuid-1234", restored.Immutables["cluster_id"].Value)
	assert.Equal(t, "string", restored.Immutables["cluster_id"].Type)
	assert.Len(t, restored.Fingerprints, 1)
	assert.Equal(t, "abc123", restored.Fingerprints["__fingerprint:deploy:sources"].Value)
}

func TestStateData_EmptyJSONRoundTrip(t *testing.T) {
	original := NewData()

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored Data
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, SchemaVersionCurrent, restored.SchemaVersion)
}

func TestNewMockStateData(t *testing.T) {
	params := map[string]any{
		"env": "prod",
	}

	data := NewMockData("test-sol", "2.0.0", params)

	assert.Equal(t, SchemaVersionCurrent, data.SchemaVersion)
	assert.Equal(t, "test-sol", data.Metadata.Solution)
	assert.Equal(t, "2.0.0", data.Metadata.Version)
	assert.Equal(t, "test", data.Metadata.ScafctlVersion)
	assert.False(t, data.Metadata.CreatedAt.IsZero())
	assert.Len(t, data.Parameters, 1)
	assert.Equal(t, "prod", data.Parameters["env"])
}

func TestNewMockStateData_NilParams(t *testing.T) {
	data := NewMockData("test-sol", "1.0.0", nil)

	assert.NotNil(t, data.Parameters)
	assert.Empty(t, data.Parameters)
}

func TestImmutableEntry_JSONRoundTrip(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name  string
		entry ImmutableEntry
	}{
		{
			name:  "string value",
			entry: ImmutableEntry{Value: "hello", Type: "string", CreatedAt: now},
		},
		{
			name:  "number value",
			entry: ImmutableEntry{Value: float64(42), Type: "int", CreatedAt: now},
		},
		{
			name:  "bool value",
			entry: ImmutableEntry{Value: true, Type: "bool", CreatedAt: now},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.entry)
			require.NoError(t, err)

			var restored ImmutableEntry
			err = json.Unmarshal(data, &restored)
			require.NoError(t, err)

			assert.Equal(t, tt.entry.Type, restored.Type)
		})
	}
}
