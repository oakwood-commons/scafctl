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
	assert.NotNil(t, data.Values)
	assert.Empty(t, data.Values)
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
		Values: map[string]*Entry{
			"api_key": {
				Value:     "sk-abc123",
				Type:      "string",
				UpdatedAt: now,
				Immutable: false,
			},
			"count": {
				Value:     float64(42),
				Type:      "int",
				UpdatedAt: now,
				Immutable: true,
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
	assert.Len(t, restored.Values, 2)
	assert.Equal(t, "sk-abc123", restored.Values["api_key"].Value)
	assert.Equal(t, "string", restored.Values["api_key"].Type)
	assert.False(t, restored.Values["api_key"].Immutable)
	assert.Equal(t, float64(42), restored.Values["count"].Value)
	assert.True(t, restored.Values["count"].Immutable)
}

func TestStateData_EmptyJSONRoundTrip(t *testing.T) {
	original := NewData()

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored Data
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, SchemaVersionCurrent, restored.SchemaVersion)
	assert.Empty(t, restored.Values)
}

func TestNewMockStateData(t *testing.T) {
	values := map[string]*Entry{
		"key1": {Value: "val1", Type: "string"},
	}

	data := NewMockData("test-sol", "2.0.0", values)

	assert.Equal(t, SchemaVersionCurrent, data.SchemaVersion)
	assert.Equal(t, "test-sol", data.Metadata.Solution)
	assert.Equal(t, "2.0.0", data.Metadata.Version)
	assert.Equal(t, "test", data.Metadata.ScafctlVersion)
	assert.False(t, data.Metadata.CreatedAt.IsZero())
	assert.Len(t, data.Values, 1)
	assert.Equal(t, "val1", data.Values["key1"].Value)
}

func TestNewMockStateData_NilValues(t *testing.T) {
	data := NewMockData("test-sol", "1.0.0", nil)

	assert.NotNil(t, data.Values)
	assert.Empty(t, data.Values)
}

func TestStateEntry_JSONRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		entry Entry
	}{
		{
			name:  "string value",
			entry: Entry{Value: "hello", Type: "string", UpdatedAt: time.Now().UTC()},
		},
		{
			name:  "number value",
			entry: Entry{Value: float64(42), Type: "int", UpdatedAt: time.Now().UTC()},
		},
		{
			name:  "bool value",
			entry: Entry{Value: true, Type: "bool", UpdatedAt: time.Now().UTC()},
		},
		{
			name:  "array value",
			entry: Entry{Value: []any{"a", "b"}, Type: "array", UpdatedAt: time.Now().UTC()},
		},
		{
			name:  "immutable",
			entry: Entry{Value: "locked", Type: "string", Immutable: true, UpdatedAt: time.Now().UTC()},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.entry)
			require.NoError(t, err)

			var restored Entry
			err = json.Unmarshal(data, &restored)
			require.NoError(t, err)

			assert.Equal(t, tt.entry.Type, restored.Type)
			assert.Equal(t, tt.entry.Immutable, restored.Immutable)
		})
	}
}
