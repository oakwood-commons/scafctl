// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildOutputData(t *testing.T) {
	now := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	t.Run("basic succeeded result", func(t *testing.T) {
		result := &ExecutionResult{
			FinalStatus: ExecutionSucceeded,
			StartTime:   now,
			EndTime:     now.Add(5 * time.Second),
			Actions: map[string]*ActionResult{
				"deploy": {
					Status:  StatusSucceeded,
					Results: map[string]any{"url": "https://example.com"},
				},
			},
		}

		output := BuildOutputData(result, nil)

		assert.Equal(t, "succeeded", output["status"])
		assert.Equal(t, "2025-01-15T10:00:00Z", output["startTime"])
		assert.Equal(t, "2025-01-15T10:00:05Z", output["endTime"])
		assert.Equal(t, "5s", output["duration"])
		assert.Nil(t, output["failedActions"])
		assert.Nil(t, output["skippedActions"])
		assert.Nil(t, output["__execution"])

		actions, ok := output["actions"].(map[string]any)
		require.True(t, ok)
		deploy, ok := actions["deploy"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "succeeded", deploy["status"])
		results, ok := deploy["results"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "https://example.com", results["url"])
	})

	t.Run("failed actions included", func(t *testing.T) {
		result := &ExecutionResult{
			FinalStatus: ExecutionFailed,
			StartTime:   now,
			EndTime:     now.Add(1 * time.Second),
			Actions: map[string]*ActionResult{
				"build": {
					Status: StatusFailed,
					Error:  "compilation failed",
				},
			},
			FailedActions: []string{"build"},
		}

		output := BuildOutputData(result, nil)

		assert.Equal(t, "failed", output["status"])
		assert.Equal(t, []string{"build"}, output["failedActions"])

		actions := output["actions"].(map[string]any)
		build := actions["build"].(map[string]any)
		assert.Equal(t, "failed", build["status"])
		assert.Equal(t, "compilation failed", build["error"])
	})

	t.Run("skipped actions included", func(t *testing.T) {
		result := &ExecutionResult{
			FinalStatus: ExecutionSucceeded,
			StartTime:   now,
			EndTime:     now.Add(1 * time.Second),
			Actions: map[string]*ActionResult{
				"optional": {
					Status:     StatusSkipped,
					SkipReason: SkipReasonCondition,
				},
			},
			SkippedActions: []string{"optional"},
		}

		output := BuildOutputData(result, nil)

		assert.Equal(t, []string{"optional"}, output["skippedActions"])
		actions := output["actions"].(map[string]any)
		optional := actions["optional"].(map[string]any)
		assert.Equal(t, "skipped", optional["status"])
		assert.Equal(t, string(SkipReasonCondition), optional["skipReason"])
	})

	t.Run("execution data included", func(t *testing.T) {
		result := &ExecutionResult{
			FinalStatus: ExecutionSucceeded,
			StartTime:   now,
			EndTime:     now.Add(1 * time.Second),
			Actions:     map[string]*ActionResult{},
		}
		execData := map[string]any{
			"resolvers": map[string]any{"env": "prod"},
		}

		output := BuildOutputData(result, execData)

		assert.NotNil(t, output["__execution"])
		exec := output["__execution"].(map[string]any)
		assert.Equal(t, map[string]any{"env": "prod"}, exec["resolvers"])
	})

	t.Run("nil execution data excluded", func(t *testing.T) {
		result := &ExecutionResult{
			FinalStatus: ExecutionSucceeded,
			StartTime:   now,
			EndTime:     now.Add(1 * time.Second),
			Actions:     map[string]*ActionResult{},
		}

		output := BuildOutputData(result, nil)

		_, exists := output["__execution"]
		assert.False(t, exists)
	})

	t.Run("nil results not included in action", func(t *testing.T) {
		result := &ExecutionResult{
			FinalStatus: ExecutionSucceeded,
			StartTime:   now,
			EndTime:     now.Add(1 * time.Second),
			Actions: map[string]*ActionResult{
				"validate": {
					Status: StatusSucceeded,
				},
			},
		}

		output := BuildOutputData(result, nil)

		actions := output["actions"].(map[string]any)
		validate := actions["validate"].(map[string]any)
		_, hasResults := validate["results"]
		assert.False(t, hasResults)
	})
}

func BenchmarkBuildOutputData(b *testing.B) {
	now := time.Now()
	result := &ExecutionResult{
		FinalStatus: ExecutionSucceeded,
		StartTime:   now,
		EndTime:     now.Add(5 * time.Second),
		Actions: map[string]*ActionResult{
			"build":  {Status: StatusSucceeded, Results: map[string]any{"artifact": "v1.0"}},
			"deploy": {Status: StatusSucceeded, Results: map[string]any{"url": "https://example.com"}},
			"test":   {Status: StatusSkipped, SkipReason: SkipReasonCondition},
		},
		SkippedActions: []string{"test"},
	}
	execData := map[string]any{"resolvers": map[string]any{"env": "prod"}}

	for b.Loop() {
		BuildOutputData(result, execData)
	}
}
