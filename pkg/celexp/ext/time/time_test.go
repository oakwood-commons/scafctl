// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celtime

import (
	"testing"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNowFunc_CELIntegration(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		validate   func(t *testing.T, result any)
	}{
		{
			name:       "time.now returns timestamp",
			expression: "time.now()",
			validate: func(t *testing.T, result any) {
				ts, ok := result.(time.Time)
				require.True(t, ok, "result should be a time.Time")
				// Check that the returned time is recent (within 1 second)
				assert.WithinDuration(t, time.Now(), ts, time.Second)
			},
		},
		{
			name:       "time.now can be compared",
			expression: `time.now() > timestamp("2020-01-01T00:00:00Z")`,
			validate: func(t *testing.T, result any) {
				assert.Equal(t, true, result)
			},
		},
		{
			name:       "time.now can be used in conditional",
			expression: `time.now() > timestamp("2030-01-01T00:00:00Z") ? "future" : "past"`,
			validate: func(t *testing.T, result any) {
				assert.Equal(t, "past", result)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			funcObj := NowFunc()
			env, err := cel.NewEnv(funcObj.EnvOptions...)
			require.NoError(t, err)

			ast, issues := env.Compile(tt.expression)
			require.Nil(t, issues, "compilation should succeed")

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]any{})
			require.NoError(t, err)

			tt.validate(t, result.Value())
		})
	}
}

func TestNowFmtFunc_CELIntegration(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		validate   func(t *testing.T, result any)
	}{
		{
			name:       "RFC3339 format",
			expression: `time.nowFmt("2006-01-02T15:04:05Z07:00")`,
			validate: func(t *testing.T, result any) {
				str, ok := result.(string)
				require.True(t, ok, "result should be a string")
				// Parse to verify it's a valid RFC3339 timestamp
				_, err := time.Parse("2006-01-02T15:04:05Z07:00", str)
				assert.NoError(t, err, "result should be valid RFC3339 format")
			},
		},
		{
			name:       "date only format",
			expression: `time.nowFmt("2006-01-02")`,
			validate: func(t *testing.T, result any) {
				str, ok := result.(string)
				require.True(t, ok, "result should be a string")
				// Parse to verify it's a valid date
				parsed, err := time.Parse("2006-01-02", str)
				assert.NoError(t, err, "result should be valid date format")
				// Should be today's date
				now := time.Now()
				assert.Equal(t, now.Year(), parsed.Year())
				assert.Equal(t, now.Month(), parsed.Month())
				assert.Equal(t, now.Day(), parsed.Day())
			},
		},
		{
			name:       "custom format",
			expression: `time.nowFmt("January 2, 2006 at 3:04 PM")`,
			validate: func(t *testing.T, result any) {
				str, ok := result.(string)
				require.True(t, ok, "result should be a string")
				// Parse to verify it's valid
				_, err := time.Parse("January 2, 2006 at 3:04 PM", str)
				assert.NoError(t, err, "result should be valid custom format")
			},
		},
		{
			name:       "Unix timestamp format",
			expression: `time.nowFmt("20060102150405")`,
			validate: func(t *testing.T, result any) {
				str, ok := result.(string)
				require.True(t, ok, "result should be a string")
				assert.Len(t, str, 14, "Unix timestamp should be 14 characters")
				// Parse to verify it's valid
				_, err := time.Parse("20060102150405", str)
				assert.NoError(t, err, "result should be valid Unix timestamp format")
			},
		},
		{
			name:       "time only format",
			expression: `time.nowFmt("15:04:05")`,
			validate: func(t *testing.T, result any) {
				str, ok := result.(string)
				require.True(t, ok, "result should be a string")
				// Parse to verify it's valid time
				_, err := time.Parse("15:04:05", str)
				assert.NoError(t, err, "result should be valid time format")
			},
		},
		{
			name:       "year only format",
			expression: `time.nowFmt("2006")`,
			validate: func(t *testing.T, result any) {
				str, ok := result.(string)
				require.True(t, ok, "result should be a string")
				assert.Equal(t, time.Now().Format("2006"), str)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			funcObj := NowFmtFunc()
			env, err := cel.NewEnv(funcObj.EnvOptions...)
			require.NoError(t, err)

			ast, issues := env.Compile(tt.expression)
			require.Nil(t, issues, "compilation should succeed")

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]any{})
			require.NoError(t, err)

			tt.validate(t, result.Value())
		})
	}
}

func TestNowFmtFunc_InvalidInput(t *testing.T) {
	tests := []struct {
		name          string
		expression    string
		wantCompError bool
	}{
		{
			name:          "non-string layout causes compile error",
			expression:    `time.nowFmt(123)`,
			wantCompError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			funcObj := NowFmtFunc()
			env, err := cel.NewEnv(funcObj.EnvOptions...)
			require.NoError(t, err)

			_, issues := env.Compile(tt.expression)
			if tt.wantCompError {
				require.NotNil(t, issues, "compilation should fail for invalid input")
			} else {
				require.Nil(t, issues, "compilation should succeed")
			}
		})
	}
}

func TestCombinedTimeFunctions(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		validate   func(t *testing.T, result any)
	}{
		{
			name:       "combine now and nowFmt",
			expression: `time.nowFmt("2006") == string(time.now().getFullYear())`,
			validate: func(t *testing.T, result any) {
				assert.Equal(t, true, result)
			},
		},
		{
			name:       "use nowFmt in string concatenation",
			expression: `"Today is " + time.nowFmt("Monday, January 2")`,
			validate: func(t *testing.T, result any) {
				str, ok := result.(string)
				require.True(t, ok)
				assert.Contains(t, str, "Today is ")
			},
		},
		{
			name: "use nowFmt in conditional",
			expression: func() string {
				currentYear := time.Now().Format("2006")
				return `time.nowFmt("2006") == "` + currentYear + `" ? "correct year" : "wrong year"`
			}(),
			validate: func(t *testing.T, result any) {
				assert.Equal(t, "correct year", result)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nowFunc := NowFunc()
			nowFmtFunc := NowFmtFunc()

			envOptions := append(nowFunc.EnvOptions, nowFmtFunc.EnvOptions...)
			env, err := cel.NewEnv(envOptions...)
			require.NoError(t, err)

			ast, issues := env.Compile(tt.expression)
			require.Nil(t, issues, "compilation should succeed")

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]any{})
			require.NoError(t, err)

			tt.validate(t, result.Value())
		})
	}
}

func BenchmarkNowFunc_CEL(b *testing.B) {
	funcObj := NowFunc()
	env, _ := cel.NewEnv(funcObj.EnvOptions...)
	ast, _ := env.Compile("time.now()")
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for b.Loop() {
		_, _, _ = prog.Eval(map[string]any{})
	}
}

func BenchmarkNowFmtFunc_CEL(b *testing.B) {
	funcObj := NowFmtFunc()
	env, _ := cel.NewEnv(funcObj.EnvOptions...)
	ast, _ := env.Compile(`time.nowFmt("2006-01-02T15:04:05Z07:00")`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for b.Loop() {
		_, _, _ = prog.Eval(map[string]any{})
	}
}
