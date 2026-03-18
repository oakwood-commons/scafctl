// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package sleepprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/ptrs"
)

// ProviderName is the name of this provider used in error messages and logging.
const ProviderName = "sleep"

// SleepProvider provides sleep/delay functionality for workflow control.
type SleepProvider struct {
	descriptor *provider.Descriptor
}

// NewSleepProvider creates a new sleep provider instance.
func NewSleepProvider() *SleepProvider {
	version, _ := semver.NewVersion("1.0.0")

	// Common output schema for capabilities without required fields
	commonOutputSchema := schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
		"duration": schemahelper.StringProp("The duration that was slept"),
		"elapsed":  schemahelper.StringProp("The actual elapsed time (should match duration in most cases)"),
	})

	return &SleepProvider{
		descriptor: &provider.Descriptor{
			Name:        "sleep",
			DisplayName: "Sleep Provider",
			APIVersion:  "v1",
			Version:     version,
			Description: "Provides sleep/delay functionality for workflow control. Useful for rate limiting, waiting for external systems, or pacing workflow execution.",
			WhatIf: func(_ context.Context, input any) (string, error) {
				inputs, ok := input.(map[string]any)
				if !ok {
					return "", nil
				}
				duration, _ := inputs["duration"].(string)
				if duration != "" {
					return fmt.Sprintf("Would sleep for %s", duration), nil
				}
				return "Would sleep for configured duration", nil
			},
			Capabilities: []provider.Capability{
				provider.CapabilityFrom,
				provider.CapabilityTransform,
				provider.CapabilityValidation,
				provider.CapabilityAuthentication,
				provider.CapabilityAction,
			},
			Schema: schemahelper.ObjectSchema([]string{"duration"}, map[string]*jsonschema.Schema{
				"duration": schemahelper.StringProp("Duration to sleep. Accepts Go duration format (e.g., '1s', '500ms', '2m', '1h30m'). Valid time units are 'ns', 'us' (or 'µs'), 'ms', 's', 'm', 'h'.",
					schemahelper.WithExample("5s"),
					schemahelper.WithMaxLength(*ptrs.IntPtr(50)),
					schemahelper.WithPattern(`^(\d+(\.\d+)?(ns|us|µs|ms|s|m|h))+$`)),
			}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom:      commonOutputSchema,
				provider.CapabilityTransform: commonOutputSchema,
				provider.CapabilityValidation: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"valid":    schemahelper.BoolProp("Whether the sleep operation completed successfully (always true if no error)"),
					"errors":   schemahelper.ArrayProp("Validation errors (empty if valid)"),
					"duration": schemahelper.StringProp("The duration that was slept"),
					"elapsed":  schemahelper.StringProp("The actual elapsed time"),
				}),
				provider.CapabilityAuthentication: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"authenticated": schemahelper.BoolProp("Whether authentication succeeded (always true for sleep)"),
					"token":         schemahelper.StringProp("The authentication token (empty for sleep provider)"),
					"duration":      schemahelper.StringProp("The duration that was slept"),
					"elapsed":       schemahelper.StringProp("The actual elapsed time"),
				}),
				provider.CapabilityAction: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"success":  schemahelper.BoolProp("Whether the sleep operation completed successfully"),
					"duration": schemahelper.StringProp("The duration that was slept"),
					"elapsed":  schemahelper.StringProp("The actual elapsed time"),
				}),
			},
			Examples: []provider.Example{
				{
					Name:        "Short delay",
					Description: "Pause workflow execution for 5 seconds",
					YAML: `name: wait-5-seconds
provider: sleep
inputs:
  duration: "5s"`,
				},
				{
					Name:        "Millisecond precision delay",
					Description: "Pause for 500 milliseconds for precise timing control",
					YAML: `name: half-second-delay
provider: sleep
inputs:
  duration: "500ms"`,
				},
				{
					Name:        "Rate limiting delay",
					Description: "Wait 2 minutes between API calls to respect rate limits",
					YAML: `name: rate-limit-pause
provider: sleep
inputs:
  duration: "2m"`,
				},
				{
					Name:        "Combined duration units",
					Description: "Use multiple time units for complex durations (1 hour 30 minutes)",
					YAML: `name: long-wait
provider: sleep
inputs:
  duration: "1h30m"`,
				},
			},
		},
	}
}

// Descriptor returns the provider descriptor.
func (p *SleepProvider) Descriptor() *provider.Descriptor {
	return p.descriptor
}

// Execute performs the sleep operation.
func (p *SleepProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	inputs, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected map[string]any, got %T", ProviderName, input)
	}
	lgr := logger.FromContext(ctx)

	// Check for dry-run mode
	if dryRun := provider.DryRunFromContext(ctx); dryRun {
		return p.executeDryRun(inputs)
	}

	// Validate and parse duration
	durationStr, ok := inputs["duration"].(string)
	if !ok || durationStr == "" {
		return nil, fmt.Errorf("%s: duration is required and must be a string", ProviderName)
	}

	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid duration format: %w (expected format like '1s', '500ms', '2m')", ProviderName, err)
	}

	if duration < 0 {
		return nil, fmt.Errorf("%s: duration cannot be negative: %s", ProviderName, durationStr)
	}

	lgr.V(1).Info("Starting sleep", "duration", durationStr)

	// Perform the sleep
	start := time.Now()

	// Use a timer with context cancellation support
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-timer.C:
		// Sleep completed normally
		elapsed := time.Since(start)
		lgr.V(1).Info("Sleep completed", "duration", durationStr, "elapsed", elapsed)

		return &provider.Output{
			Data: map[string]any{
				"success":  true,
				"duration": durationStr,
				"elapsed":  elapsed.String(),
			},
		}, nil

	case <-ctx.Done():
		// Context was cancelled
		elapsed := time.Since(start)
		lgr.V(1).Info("Sleep interrupted by context cancellation", "duration", durationStr, "elapsed", elapsed)
		return nil, fmt.Errorf("%s: sleep interrupted: %w", ProviderName, ctx.Err())
	}
}

// executeDryRun handles dry-run mode.
func (p *SleepProvider) executeDryRun(inputs map[string]any) (*provider.Output, error) {
	durationStr, _ := inputs["duration"].(string)

	// Validate duration format even in dry-run
	if durationStr != "" {
		if _, err := time.ParseDuration(durationStr); err != nil {
			return nil, fmt.Errorf("%s: invalid duration format: %w (expected format like '1s', '500ms', '2m')", ProviderName, err)
		}
	}

	message := fmt.Sprintf("Would sleep for %s", durationStr)

	return &provider.Output{
		Data: map[string]any{
			"success":  true,
			"duration": durationStr,
			"elapsed":  "0s",
			"_dryRun":  true,
			"_message": message,
		},
	}, nil
}
