// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"time"
)

// BuildOutputData transforms an ExecutionResult and optional execution metadata
// into a structured map suitable for JSON/YAML output or programmatic consumption.
//
// The returned map contains:
//   - "status": the final execution status string
//   - "startTime", "endTime": RFC3339 timestamps
//   - "duration": human-readable duration
//   - "actions": map of action name → {status, results?, error?, skipReason?}
//   - "failedActions", "skippedActions": lists (if non-empty)
//   - "__execution": execution metadata (if provided)
func BuildOutputData(result *ExecutionResult, executionData map[string]any) map[string]any {
	output := map[string]any{
		"status":    string(result.FinalStatus),
		"startTime": result.StartTime.Format(time.RFC3339),
		"endTime":   result.EndTime.Format(time.RFC3339),
		"duration":  result.Duration().String(),
	}

	actions := make(map[string]any)
	for name, ar := range result.Actions {
		actionOutput := map[string]any{
			"status": string(ar.Status),
		}
		if ar.Results != nil {
			actionOutput["results"] = ar.Results
		}
		if ar.Error != "" {
			actionOutput["error"] = ar.Error
		}
		if ar.SkipReason != "" {
			actionOutput["skipReason"] = string(ar.SkipReason)
		}
		actions[name] = actionOutput
	}
	output["actions"] = actions

	if len(result.FailedActions) > 0 {
		output["failedActions"] = result.FailedActions
	}
	if len(result.SkippedActions) > 0 {
		output["skippedActions"] = result.SkippedActions
	}

	if executionData != nil {
		output["__execution"] = executionData
	}

	return output
}
