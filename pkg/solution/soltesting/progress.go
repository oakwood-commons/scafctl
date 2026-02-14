// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting

// TestProgressCallback receives notifications as test execution progresses.
// Implementations must be safe for concurrent use when runner concurrency > 1.
type TestProgressCallback interface {
	// OnTestStart is called when an individual test begins execution.
	// It is not called for tests that fail before execution (validation errors,
	// dry runs, setup failures, etc.) — those only receive OnTestComplete.
	OnTestStart(solution, test string)

	// OnTestComplete is called when a test finishes with its result.
	// This is called for every test, including those that were skipped or
	// errored before execution began.
	OnTestComplete(result TestResult)

	// Wait blocks until all progress output has been flushed.
	// Must be called after the last OnTestComplete before reading stdout output.
	Wait()
}
