// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpc

import (
	"context"
	"net/http"
	"time"

	"github.com/hashicorp/go-retryablehttp"
)

// BuildStatusCodeCheckRetry returns a CheckRetry function that retries on the given HTTP status codes
// as well as on any connection/network error. Passing nil or an empty slice retries only on errors.
func BuildStatusCodeCheckRetry(statusCodes []int) retryablehttp.CheckRetry {
	return func(ctx context.Context, resp *http.Response, err error) (bool, error) {
		// Let the default policy handle network errors and context cancellation.
		if err != nil || resp == nil {
			return retryablehttp.DefaultRetryPolicy(ctx, resp, err)
		}
		for _, code := range statusCodes {
			if resp.StatusCode == code {
				return true, nil
			}
		}
		return false, nil
	}
}

// BuildNamedBackoff returns a Backoff function built from a named strategy.
//
// Supported strategy values: "none", "linear", "exponential" (default).
// The returned durations are clamped to [initialWait, maxWait].
func BuildNamedBackoff(strategy string, initialWait, maxWait time.Duration) retryablehttp.Backoff {
	return func(_, _ time.Duration, attemptNum int, _ *http.Response) time.Duration {
		var wait time.Duration
		switch strategy {
		case "none":
			wait = initialWait
		case "linear":
			wait = initialWait * time.Duration(attemptNum+1)
		case "exponential":
			// Cap exponent to avoid overflow (2^10 * initialWait is more than enough).
			exp := attemptNum
			if exp > 10 {
				exp = 10
			}
			wait = initialWait * time.Duration(1<<exp)
		default:
			wait = initialWait
		}
		if wait < initialWait {
			wait = initialWait
		}
		if wait > maxWait {
			wait = maxWait
		}
		return wait
	}
}
