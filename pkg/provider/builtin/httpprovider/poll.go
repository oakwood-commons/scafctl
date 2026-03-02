// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// pollConfig holds polling configuration for HTTP requests that need to
// retry until a response condition is met (e.g., waiting for deployment status).
type pollConfig struct {
	Until       string        // CEL expression that must evaluate to true to stop polling (required)
	FailWhen    string        // CEL expression — if true, stop with error (optional)
	Interval    time.Duration // Duration between poll attempts (default: 10s)
	MaxAttempts int           // Maximum poll attempts before giving up (default: 30)
}

// defaultPollConfig returns default polling configuration.
func defaultPollConfig() pollConfig {
	return pollConfig{
		Interval:    10 * time.Second,
		MaxAttempts: 30,
	}
}

// parsePollConfig parses poll configuration from inputs.
// Returns nil if no poll configuration is present.
func parsePollConfig(inputs map[string]any) (*pollConfig, error) {
	pollInput, ok := inputs["poll"]
	if !ok || pollInput == nil {
		return nil, nil
	}

	pollMap, ok := pollInput.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("poll must be an object")
	}

	cfg := defaultPollConfig()

	until, _ := pollMap["until"].(string)
	if until == "" {
		return nil, fmt.Errorf("poll.until is required (CEL expression)")
	}
	cfg.Until = until

	if failWhen, ok := pollMap["failWhen"].(string); ok {
		cfg.FailWhen = failWhen
	}

	if interval, ok := pollMap["interval"].(string); ok {
		d, err := time.ParseDuration(interval)
		if err != nil {
			return nil, fmt.Errorf("poll.interval: %w", err)
		}
		if d < 1*time.Second {
			return nil, fmt.Errorf("poll.interval must be at least 1s")
		}
		cfg.Interval = d
	}
	// Handle numeric seconds from JSON/YAML
	if intervalSec, ok := pollMap["interval"].(float64); ok && intervalSec > 0 {
		cfg.Interval = time.Duration(intervalSec) * time.Second
	}
	if intervalSec, ok := pollMap["interval"].(int); ok && intervalSec > 0 {
		cfg.Interval = time.Duration(intervalSec) * time.Second
	}

	if maxAttempts, ok := pollMap["maxAttempts"].(float64); ok && maxAttempts > 0 {
		cfg.MaxAttempts = int(maxAttempts)
	}
	if maxAttempts, ok := pollMap["maxAttempts"].(int); ok && maxAttempts > 0 {
		cfg.MaxAttempts = maxAttempts
	}

	return &cfg, nil
}

// executePoll wraps execute() in a polling loop, re-executing the HTTP request
// until a CEL condition is met or max attempts are exhausted.
func (p *HTTPProvider) executePoll(
	ctx context.Context,
	_ interface {
		Do(req interface{}) (interface{}, error)
	},
	_, _, _ string,
	_ map[string]any,
	pollCfg *pollConfig,
	_ bool,
	executeFunc func() (*provider.Output, error),
) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	var lastOutput *provider.Output
	var lastErr error

	for attempt := 1; attempt <= pollCfg.MaxAttempts; attempt++ {
		lgr.V(1).Info("poll attempt", "attempt", attempt, "maxAttempts", pollCfg.MaxAttempts)

		lastOutput, lastErr = executeFunc()
		if lastErr != nil {
			lgr.V(1).Info("poll attempt failed with error", "attempt", attempt, "error", lastErr)
			// On error, continue polling unless context is done
			if ctx.Err() != nil {
				return nil, fmt.Errorf("%s: poll cancelled: %w", ProviderName, ctx.Err())
			}
			if attempt < pollCfg.MaxAttempts {
				sleepWithContext(ctx, pollCfg.Interval)
				continue
			}
			return nil, fmt.Errorf("%s: poll exhausted after %d attempts, last error: %w", ProviderName, pollCfg.MaxAttempts, lastErr)
		}

		// Build evaluation data from the response output
		evalData := lastOutput.Data

		// Check failWhen first (early exit with error)
		if pollCfg.FailWhen != "" {
			result, err := celexp.EvaluateExpression(ctx, pollCfg.FailWhen, evalData, nil)
			if err != nil {
				lgr.V(1).Info("poll failWhen expression error (ignored)", "error", err)
			} else if boolResult, ok := result.(bool); ok && boolResult {
				return nil, fmt.Errorf("%s: poll failWhen condition met on attempt %d", ProviderName, attempt)
			}
		}

		// Check until condition (success — stop polling)
		result, err := celexp.EvaluateExpression(ctx, pollCfg.Until, evalData, nil)
		if err != nil {
			lgr.V(1).Info("poll until expression error (continuing)", "error", err, "attempt", attempt)
		} else if boolResult, ok := result.(bool); ok && boolResult {
			lgr.V(1).Info("poll until condition met", "attempt", attempt)
			return lastOutput, nil
		}

		// Not done yet — wait and retry
		if attempt < pollCfg.MaxAttempts {
			lgr.V(2).Info("poll condition not met, waiting", "interval", pollCfg.Interval)
			if cancelled := sleepWithContext(ctx, pollCfg.Interval); cancelled {
				return nil, fmt.Errorf("%s: poll cancelled: %w", ProviderName, ctx.Err())
			}
		}
	}

	// Max attempts exhausted — return the last output (not an error)
	lgr.V(1).Info("poll max attempts exhausted, returning last response", "maxAttempts", pollCfg.MaxAttempts)
	return lastOutput, nil
}

// sleepWithContext sleeps for the given duration but returns early if context is cancelled.
// Returns true if the context was cancelled.
func sleepWithContext(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return false
	case <-ctx.Done():
		return true
	}
}
