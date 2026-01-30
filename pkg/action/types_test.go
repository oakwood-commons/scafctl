package action

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestBackoffType_IsValid(t *testing.T) {
	tests := []struct {
		name     string
		backoff  BackoffType
		expected bool
	}{
		{"fixed", BackoffFixed, true},
		{"linear", BackoffLinear, true},
		{"exponential", BackoffExponential, true},
		{"empty", "", true},
		{"invalid", "invalid", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.backoff.IsValid())
		})
	}
}

func TestBackoffType_OrDefault(t *testing.T) {
	tests := []struct {
		name     string
		backoff  BackoffType
		expected BackoffType
	}{
		{"fixed returns fixed", BackoffFixed, BackoffFixed},
		{"linear returns linear", BackoffLinear, BackoffLinear},
		{"exponential returns exponential", BackoffExponential, BackoffExponential},
		{"empty returns fixed", "", BackoffFixed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.backoff.OrDefault())
		})
	}
}

func TestActionStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		name     string
		status   ActionStatus
		expected bool
	}{
		{"pending", StatusPending, false},
		{"running", StatusRunning, false},
		{"succeeded", StatusSucceeded, true},
		{"failed", StatusFailed, true},
		{"skipped", StatusSkipped, true},
		{"timeout", StatusTimeout, true},
		{"cancelled", StatusCancelled, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.status.IsTerminal())
		})
	}
}

func TestActionStatus_IsSuccess(t *testing.T) {
	tests := []struct {
		name     string
		status   ActionStatus
		expected bool
	}{
		{"succeeded", StatusSucceeded, true},
		{"pending", StatusPending, false},
		{"running", StatusRunning, false},
		{"failed", StatusFailed, false},
		{"skipped", StatusSkipped, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.status.IsSuccess())
		})
	}
}

func TestActionResult_Duration(t *testing.T) {
	now := time.Now()
	later := now.Add(5 * time.Second)

	tests := []struct {
		name     string
		result   ActionResult
		expected time.Duration
	}{
		{
			name: "with start and end",
			result: ActionResult{
				StartTime: &now,
				EndTime:   &later,
			},
			expected: 5 * time.Second,
		},
		{
			name:     "no start time",
			result:   ActionResult{EndTime: &later},
			expected: 0,
		},
		{
			name:     "no end time",
			result:   ActionResult{StartTime: &now},
			expected: 0,
		},
		{
			name:     "no times",
			result:   ActionResult{},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.result.Duration())
		})
	}
}

func TestForEachIterationResult_Duration(t *testing.T) {
	now := time.Now()
	later := now.Add(3 * time.Second)

	tests := []struct {
		name     string
		result   ForEachIterationResult
		expected time.Duration
	}{
		{
			name: "with start and end",
			result: ForEachIterationResult{
				StartTime: &now,
				EndTime:   &later,
			},
			expected: 3 * time.Second,
		},
		{
			name:     "no times",
			result:   ForEachIterationResult{},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.result.Duration())
		})
	}
}

func TestDuration_JSONMarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		duration Duration
		json     string
	}{
		{"1 second", Duration(time.Second), `"1s"`},
		{"500ms", Duration(500 * time.Millisecond), `"500ms"`},
		{"1h30m", Duration(90 * time.Minute), `"1h30m0s"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal
			data, err := json.Marshal(tt.duration)
			require.NoError(t, err)
			assert.Equal(t, tt.json, string(data))

			// Unmarshal
			var d Duration
			err = json.Unmarshal(data, &d)
			require.NoError(t, err)
			assert.Equal(t, tt.duration, d)
		})
	}
}

func TestDuration_JSONUnmarshalNumeric(t *testing.T) {
	// JSON can represent durations as numbers (nanoseconds)
	var d Duration
	err := json.Unmarshal([]byte("1000000000"), &d) // 1 second in nanoseconds
	require.NoError(t, err)
	assert.Equal(t, Duration(time.Second), d)
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
			var d Duration
			err := json.Unmarshal([]byte(tt.json), &d)
			assert.Error(t, err)
		})
	}
}

func TestDuration_YAMLMarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		duration Duration
		yaml     string
	}{
		{"1 second", Duration(time.Second), "1s\n"},
		{"500ms", Duration(500 * time.Millisecond), "500ms\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal
			data, err := yaml.Marshal(tt.duration)
			require.NoError(t, err)
			assert.Equal(t, tt.yaml, string(data))

			// Unmarshal
			var d Duration
			err = yaml.Unmarshal(data, &d)
			require.NoError(t, err)
			assert.Equal(t, tt.duration, d)
		})
	}
}

func TestDuration_YAMLUnmarshalNumeric(t *testing.T) {
	// YAML can represent durations as integer nanoseconds
	var d Duration
	err := yaml.Unmarshal([]byte("1000000000"), &d) // 1 second in nanoseconds
	require.NoError(t, err)
	assert.Equal(t, Duration(time.Second), d)
}

func TestDuration_String(t *testing.T) {
	d := Duration(5 * time.Second)
	assert.Equal(t, "5s", d.String())
}

func TestDuration_AsDuration(t *testing.T) {
	d := Duration(5 * time.Second)
	assert.Equal(t, 5*time.Second, d.AsDuration())
}

func TestWorkflow_YAMLUnmarshal(t *testing.T) {
	yamlData := `
actions:
  build:
    provider: shell
    inputs:
      command:
        literal: "go build"
  deploy:
    provider: kubernetes
    dependsOn:
      - build
finally:
  cleanup:
    provider: shell
    inputs:
      command:
        literal: "rm -rf tmp"
`
	var w Workflow
	err := yaml.Unmarshal([]byte(yamlData), &w)
	require.NoError(t, err)

	assert.Len(t, w.Actions, 2)
	assert.Len(t, w.Finally, 1)

	assert.Equal(t, "shell", w.Actions["build"].Provider)
	assert.Equal(t, "kubernetes", w.Actions["deploy"].Provider)
	assert.Equal(t, []string{"build"}, w.Actions["deploy"].DependsOn)
	assert.Equal(t, "shell", w.Finally["cleanup"].Provider)
}

func TestAction_YAMLUnmarshal(t *testing.T) {
	yamlData := `
name: test-action
description: Test action description
provider: shell
timeout: 30s
onError: continue
retry:
  maxAttempts: 3
  backoff: exponential
  initialDelay: 1s
  maxDelay: 30s
dependsOn:
  - other-action
`
	var a Action
	err := yaml.Unmarshal([]byte(yamlData), &a)
	require.NoError(t, err)

	assert.Equal(t, "test-action", a.Name)
	assert.Equal(t, "Test action description", a.Description)
	assert.Equal(t, "shell", a.Provider)
	assert.NotNil(t, a.Timeout)
	assert.Equal(t, Duration(30*time.Second), *a.Timeout)
	assert.Equal(t, "continue", string(a.OnError))
	assert.NotNil(t, a.Retry)
	assert.Equal(t, 3, a.Retry.MaxAttempts)
	assert.Equal(t, BackoffExponential, a.Retry.Backoff)
	assert.Equal(t, []string{"other-action"}, a.DependsOn)
}

func TestRetryConfig_YAMLUnmarshal(t *testing.T) {
	yamlData := `
maxAttempts: 5
backoff: linear
initialDelay: 2s
maxDelay: 1m
`
	var r RetryConfig
	err := yaml.Unmarshal([]byte(yamlData), &r)
	require.NoError(t, err)

	assert.Equal(t, 5, r.MaxAttempts)
	assert.Equal(t, BackoffLinear, r.Backoff)
	assert.NotNil(t, r.InitialDelay)
	assert.Equal(t, Duration(2*time.Second), *r.InitialDelay)
	assert.NotNil(t, r.MaxDelay)
	assert.Equal(t, Duration(time.Minute), *r.MaxDelay)
}

func TestActionResult_JSONRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	later := now.Add(5 * time.Second)

	original := ActionResult{
		Inputs: map[string]any{
			"command": "echo hello",
		},
		Results:   map[string]any{"stdout": "hello"},
		Status:    StatusSucceeded,
		StartTime: &now,
		EndTime:   &later,
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored ActionResult
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, original.Status, restored.Status)
	assert.Equal(t, original.Inputs["command"], restored.Inputs["command"])
}
