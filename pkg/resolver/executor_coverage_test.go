// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── RedactValue tests ─────────────────────────────────────────────────────────

func TestRedactValue_NotSensitive(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "visible", RedactValue("visible", false))
	assert.Equal(t, 42, RedactValue(42, false))
	assert.Nil(t, RedactValue(nil, false))
}

func TestRedactValue_Sensitive(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "[REDACTED]", RedactValue("secret-value", true))
	assert.Equal(t, "[REDACTED]", RedactValue(42, true))
	assert.Equal(t, "[REDACTED]", RedactValue(nil, true))
}

// ── RedactError tests ─────────────────────────────────────────────────────────

func TestRedactError_NilError(t *testing.T) {
	t.Parallel()
	assert.Nil(t, RedactError(nil, true))
	assert.Nil(t, RedactError(nil, false))
}

func TestRedactError_NotSensitive(t *testing.T) {
	t.Parallel()
	err := assert.AnError
	assert.Equal(t, err, RedactError(err, false))
}

func TestRedactError_Sensitive(t *testing.T) {
	t.Parallel()
	err := assert.AnError
	redacted := RedactError(err, true)
	require.Error(t, redacted)
	// The redacted error should be a RedactedError wrapper
	assert.NotEqual(t, err.Error(), redacted.Error())
}

// ── RedactMapValues tests ─────────────────────────────────────────────────────

func TestRedactMapValues_NotSensitive(t *testing.T) {
	t.Parallel()
	m := map[string]any{"key1": "value1", "key2": 42}
	result := RedactMapValues(m, false)
	assert.Equal(t, m, result)
}

func TestRedactMapValues_Sensitive(t *testing.T) {
	t.Parallel()
	m := map[string]any{"key1": "value1", "key2": 42}
	result := RedactMapValues(m, true)
	assert.Equal(t, "[REDACTED]", result["key1"])
	assert.Equal(t, "[REDACTED]", result["key2"])
	assert.Len(t, result, 2, "should preserve all keys")
}

func TestRedactMapValues_EmptyMap(t *testing.T) {
	t.Parallel()
	m := map[string]any{}
	result := RedactMapValues(m, true)
	assert.Empty(t, result)
}

// ── OptionsFromAppConfig tests ────────────────────────────────────────────────

func TestOptionsFromAppConfig_AllDefaults(t *testing.T) {
	t.Parallel()
	cfg := ConfigInput{}
	opts := OptionsFromAppConfig(cfg)
	assert.Empty(t, opts, "zero-value config should produce no options")
}

func TestOptionsFromAppConfig_AllSet(t *testing.T) {
	t.Parallel()
	cfg := ConfigInput{
		Timeout:        45 * time.Second,
		PhaseTimeout:   10 * time.Minute,
		MaxConcurrency: 4,
		WarnValueSize:  1024,
		MaxValueSize:   10240,
		ValidateAll:    true,
	}
	opts := OptionsFromAppConfig(cfg)
	assert.Len(t, opts, 6)

	// Apply to executor to verify
	exec := NewExecutor(nil, opts...)
	assert.Equal(t, 45*time.Second, exec.timeout)
	assert.Equal(t, 10*time.Minute, exec.phaseTimeout)
	assert.Equal(t, 4, exec.maxConcurrency)
	assert.Equal(t, int64(1024), exec.warnValueSize)
	assert.Equal(t, int64(10240), exec.maxValueSize)
	assert.True(t, exec.validateAll)
}

func TestOptionsFromAppConfig_PartialConfig(t *testing.T) {
	t.Parallel()
	cfg := ConfigInput{
		Timeout:     30 * time.Second,
		ValidateAll: true,
	}
	opts := OptionsFromAppConfig(cfg)
	assert.Len(t, opts, 2)
}

// ── Executor option tests ─────────────────────────────────────────────────────

func TestCoverage_WithSkipTransform(t *testing.T) {
	t.Parallel()
	exec := NewExecutor(nil, WithSkipTransform(true))
	assert.True(t, exec.skipTransform)
}

func TestCoverage_WithSkipTransform_False(t *testing.T) {
	t.Parallel()
	exec := NewExecutor(nil, WithSkipTransform(false))
	assert.False(t, exec.skipTransform)
}

func TestWithWarnValueSize(t *testing.T) {
	t.Parallel()
	exec := NewExecutor(nil, WithWarnValueSize(512))
	assert.Equal(t, int64(512), exec.warnValueSize)
}

func TestWithMaxValueSize(t *testing.T) {
	t.Parallel()
	exec := NewExecutor(nil, WithMaxValueSize(1024*1024))
	assert.Equal(t, int64(1024*1024), exec.maxValueSize)
}

func TestNewExecutor_Defaults(t *testing.T) {
	t.Parallel()
	exec := NewExecutor(nil)
	assert.Equal(t, 0, exec.maxConcurrency, "default max concurrency should be unlimited (0)")
	assert.Nil(t, exec.progressCallback)
	assert.False(t, exec.validateAll)
	assert.False(t, exec.skipValidation)
	assert.False(t, exec.skipTransform)
}

func TestNewExecutor_MultipleOptions(t *testing.T) {
	t.Parallel()
	exec := NewExecutor(nil,
		WithMaxConcurrency(5),
		WithPhaseTimeout(2*time.Minute),
		WithDefaultTimeout(15*time.Second),
		WithValidateAll(true),
		WithSkipValidation(true),
	)
	assert.Equal(t, 5, exec.maxConcurrency)
	assert.Equal(t, 2*time.Minute, exec.phaseTimeout)
	assert.Equal(t, 15*time.Second, exec.timeout)
	assert.True(t, exec.validateAll)
	assert.True(t, exec.skipValidation)
}

// ── Benchmark tests ───────────────────────────────────────────────────────────

func BenchmarkRedactValue(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		RedactValue("secret-value", true)
	}
}

func BenchmarkRedactMapValues(b *testing.B) {
	m := map[string]any{
		"key1": "value1",
		"key2": "value2",
		"key3": "value3",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		RedactMapValues(m, true)
	}
}

func BenchmarkOptionsFromAppConfig(b *testing.B) {
	cfg := ConfigInput{
		Timeout:        30 * time.Second,
		PhaseTimeout:   5 * time.Minute,
		MaxConcurrency: 4,
		WarnValueSize:  1024,
		MaxValueSize:   10240,
		ValidateAll:    true,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		OptionsFromAppConfig(cfg)
	}
}

// ── WithMockedResolvers tests ─────────────────────────────────────────────────

func TestWithMockedResolvers_SetsField(t *testing.T) {
	t.Parallel()
	mocks := map[string]any{
		"api-data":  []any{"item1", "item2"},
		"api-count": 42,
	}
	executor := NewExecutor(nil, WithMockedResolvers(mocks))
	require.NotNil(t, executor.mockedResolvers)
	assert.Equal(t, 42, executor.mockedResolvers["api-count"])
	assert.True(t, executor.isMocked("api-data"))
	assert.False(t, executor.isMocked("not-mocked"))
}

func TestIsMocked_NilMap(t *testing.T) {
	t.Parallel()
	executor := NewExecutor(nil)
	assert.False(t, executor.isMocked("anything"))
}
