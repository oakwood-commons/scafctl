// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package duration_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/duration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestNew(t *testing.T) {
	d := duration.New(5 * time.Second)
	assert.Equal(t, 5*time.Second, d.Duration)
}

func TestNewPtr(t *testing.T) {
	d := duration.NewPtr(5 * time.Second)
	require.NotNil(t, d)
	assert.Equal(t, 5*time.Second, d.Duration)
}

func TestDuration_String(t *testing.T) {
	d := duration.New(5 * time.Second)
	assert.Equal(t, "5s", d.String())
}

func TestDuration_JSONMarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		duration duration.Duration
		json     string
	}{
		{"1 second", duration.New(time.Second), `"1s"`},
		{"500ms", duration.New(500 * time.Millisecond), `"500ms"`},
		{"1h30m", duration.New(90 * time.Minute), `"1h30m0s"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.duration)
			require.NoError(t, err)
			assert.Equal(t, tt.json, string(data))

			var d duration.Duration
			err = json.Unmarshal(data, &d)
			require.NoError(t, err)
			assert.Equal(t, tt.duration, d)
		})
	}
}

func TestDuration_JSONUnmarshalNumeric(t *testing.T) {
	var d duration.Duration
	err := json.Unmarshal([]byte("1000000000"), &d) // 1 second in nanoseconds
	require.NoError(t, err)
	assert.Equal(t, duration.New(time.Second), d)
}

func TestDuration_JSONUnmarshalInvalid(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{"invalid string", `"invalid"`},
		{"boolean", `true`},
		{"null", `null`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d duration.Duration
			err := json.Unmarshal([]byte(tt.json), &d)
			assert.Error(t, err)
		})
	}
}

func TestDuration_YAMLMarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		duration duration.Duration
		yaml     string
	}{
		{"1 second", duration.New(time.Second), "1s\n"},
		{"500ms", duration.New(500 * time.Millisecond), "500ms\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := yaml.Marshal(tt.duration)
			require.NoError(t, err)
			assert.Equal(t, tt.yaml, string(data))

			var d duration.Duration
			err = yaml.Unmarshal(data, &d)
			require.NoError(t, err)
			assert.Equal(t, tt.duration, d)
		})
	}
}

func TestDuration_YAMLUnmarshalNumeric(t *testing.T) {
	var d duration.Duration
	err := yaml.Unmarshal([]byte("1000000000"), &d) // 1 second in nanoseconds
	require.NoError(t, err)
	assert.Equal(t, duration.New(time.Second), d)
}

func TestDuration_YAMLRoundTrip(t *testing.T) {
	type config struct {
		Timeout *duration.Duration `yaml:"timeout,omitempty"`
	}
	original := config{Timeout: duration.NewPtr(30 * time.Second)}

	data, err := yaml.Marshal(original)
	require.NoError(t, err)

	var decoded config
	err = yaml.Unmarshal(data, &decoded)
	require.NoError(t, err)
	require.NotNil(t, decoded.Timeout)
	assert.Equal(t, 30*time.Second, decoded.Timeout.Duration)
}
