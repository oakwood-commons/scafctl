// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecutionError(t *testing.T) {
	t.Run("error with provider", func(t *testing.T) {
		cause := errors.New("connection timeout")
		err := NewExecutionError("my-resolver", "resolve", "http", 0, cause)

		assert.Equal(t, "my-resolver", err.ResolverName)
		assert.Equal(t, "resolve", err.Phase)
		assert.Equal(t, "http", err.Provider)
		assert.Equal(t, 0, err.Step)
		assert.Equal(t, cause, err.Cause)

		expected := `resolver "my-resolver" failed in resolve phase (step 0, provider http): connection timeout`
		assert.Equal(t, expected, err.Error())

		// Test Unwrap
		assert.Equal(t, cause, errors.Unwrap(err))
	})

	t.Run("error without provider", func(t *testing.T) {
		cause := errors.New("invalid condition")
		err := &ExecutionError{
			ResolverName: "test-resolver",
			Phase:        "transform",
			Step:         2,
			Provider:     "", // empty provider
			Cause:        cause,
		}

		expected := `resolver "test-resolver" failed in transform phase (step 2): invalid condition`
		assert.Equal(t, expected, err.Error())
	})
}

func TestValidationFailure(t *testing.T) {
	t.Run("with message", func(t *testing.T) {
		f := ValidationFailure{
			Rule:     0,
			Provider: "validation",
			Message:  "value must be a valid email",
			Cause:    errors.New("regex mismatch"),
		}

		assert.Equal(t, "value must be a valid email", f.Error())
	})

	t.Run("without message but with cause", func(t *testing.T) {
		f := ValidationFailure{
			Rule:     1,
			Provider: "validation",
			Cause:    errors.New("value too short"),
		}

		assert.Equal(t, "value too short", f.Error())
	})

	t.Run("without message or cause", func(t *testing.T) {
		f := ValidationFailure{
			Rule:     2,
			Provider: "validation",
		}

		assert.Equal(t, "rule 2 failed", f.Error())
	})
}

func TestAggregatedValidationError(t *testing.T) {
	t.Run("single failure", func(t *testing.T) {
		err := &AggregatedValidationError{
			ResolverName: "email-resolver",
			Value:        "invalid-email",
			Failures: []ValidationFailure{
				{Rule: 0, Provider: "validation", Message: "must be a valid email"},
			},
		}

		expected := `resolver "email-resolver" validation failed: must be a valid email`
		assert.Equal(t, expected, err.Error())
		assert.True(t, err.HasFailures())
	})

	t.Run("multiple failures", func(t *testing.T) {
		err := &AggregatedValidationError{
			ResolverName: "user-data",
			Value:        map[string]any{"name": ""},
			Failures: []ValidationFailure{
				{Rule: 0, Provider: "validation", Message: "name is required"},
				{Rule: 1, Provider: "validation", Message: "email must be valid"},
				{Rule: 2, Provider: "cel", Message: "age must be >= 18"},
			},
		}

		assert.Contains(t, err.Error(), "validation failed with 3 errors")
		assert.Contains(t, err.Error(), "[rule 1] name is required")
		assert.Contains(t, err.Error(), "[rule 2] email must be valid")
		assert.Contains(t, err.Error(), "[rule 3] age must be >= 18")
		assert.True(t, err.HasFailures())
	})

	t.Run("no failures", func(t *testing.T) {
		err := &AggregatedValidationError{
			ResolverName: "empty",
			Failures:     []ValidationFailure{},
		}

		expected := `resolver "empty" validation failed (no details)`
		assert.Equal(t, expected, err.Error())
		assert.False(t, err.HasFailures())
	})

	t.Run("AddFailure", func(t *testing.T) {
		err := &AggregatedValidationError{
			ResolverName: "test",
			Failures:     make([]ValidationFailure, 0),
		}

		err.AddFailure(ValidationFailure{Rule: 0, Message: "first"})
		err.AddFailure(ValidationFailure{Rule: 1, Message: "second"})

		assert.Len(t, err.Failures, 2)
		assert.Equal(t, "first", err.Failures[0].Message)
		assert.Equal(t, "second", err.Failures[1].Message)
	})

	t.Run("Unwrap returns nil", func(t *testing.T) {
		err := &AggregatedValidationError{
			ResolverName: "test",
		}
		assert.Nil(t, err.Unwrap())
	})
}

func TestCircularDependencyError(t *testing.T) {
	t.Run("with cycle", func(t *testing.T) {
		err := NewCircularDependencyError([]string{"a", "b", "c", "a"})

		expected := "circular dependency detected: a → b → c → a"
		assert.Equal(t, expected, err.Error())
	})

	t.Run("empty cycle", func(t *testing.T) {
		err := &CircularDependencyError{}

		assert.Equal(t, "circular dependency detected", err.Error())
	})
}

func TestPhaseTimeoutError(t *testing.T) {
	t.Run("with waiting resolvers", func(t *testing.T) {
		err := &PhaseTimeoutError{
			Phase:            2,
			ResolversWaiting: []string{"resolver-a", "resolver-b"},
		}

		assert.Contains(t, err.Error(), "phase 2 timed out")
		assert.Contains(t, err.Error(), "2 resolvers still waiting")
		assert.Contains(t, err.Error(), "resolver-a, resolver-b")
	})

	t.Run("no waiting resolvers", func(t *testing.T) {
		err := &PhaseTimeoutError{
			Phase: 1,
		}

		assert.Equal(t, "phase 1 timed out", err.Error())
	})
}

func TestValueSizeError(t *testing.T) {
	err := &ValueSizeError{
		ResolverName: "large-data",
		ActualSize:   10485760,
		MaxSize:      1048576,
	}

	assert.Contains(t, err.Error(), `resolver "large-data"`)
	assert.Contains(t, err.Error(), "10485760 bytes")
	assert.Contains(t, err.Error(), "1048576 bytes")
}

func TestTypeCoercionError(t *testing.T) {
	cause := errors.New("cannot convert string to int")
	err := &TypeCoercionError{
		ResolverName: "age",
		Phase:        "resolve",
		SourceType:   "string",
		TargetType:   TypeInt,
		Cause:        cause,
	}

	assert.Contains(t, err.Error(), `resolver "age"`)
	assert.Contains(t, err.Error(), "string to int")
	assert.Contains(t, err.Error(), "resolve phase")
	assert.Equal(t, cause, errors.Unwrap(err))
}

func TestRedactedError(t *testing.T) {
	original := errors.New("password is 'secret123'")
	err := NewRedactedError(original)

	assert.Equal(t, "[REDACTED]", err.Error())
	assert.Equal(t, original, errors.Unwrap(err))
}

func TestErrorTypeCheckers(t *testing.T) {
	t.Run("IsExecutionError", func(t *testing.T) {
		execErr := &ExecutionError{ResolverName: "test", Phase: "resolve", Cause: errors.New("failed")}
		wrappedErr := errors.New("wrapper: " + execErr.Error())

		assert.True(t, IsExecutionError(execErr))
		assert.False(t, IsExecutionError(wrappedErr))
		assert.False(t, IsExecutionError(nil))
	})

	t.Run("IsValidationError", func(t *testing.T) {
		valErr := &AggregatedValidationError{ResolverName: "test"}

		assert.True(t, IsValidationError(valErr))
		assert.False(t, IsValidationError(errors.New("not validation")))
	})

	t.Run("IsCircularDependencyError", func(t *testing.T) {
		cycleErr := &CircularDependencyError{Cycle: []string{"a", "b"}}

		assert.True(t, IsCircularDependencyError(cycleErr))
		assert.False(t, IsCircularDependencyError(errors.New("not cycle")))
	})

	t.Run("IsValueSizeError", func(t *testing.T) {
		sizeErr := &ValueSizeError{ResolverName: "test", ActualSize: 100, MaxSize: 50}

		assert.True(t, IsValueSizeError(sizeErr))
		assert.False(t, IsValueSizeError(errors.New("not size")))
	})

	t.Run("IsTypeCoercionError", func(t *testing.T) {
		coerceErr := &TypeCoercionError{ResolverName: "test", Cause: errors.New("failed")}

		assert.True(t, IsTypeCoercionError(coerceErr))
		assert.False(t, IsTypeCoercionError(errors.New("not coercion")))
	})
}

func TestErrorsAs(t *testing.T) {
	t.Run("ExecutionError", func(t *testing.T) {
		err := &ExecutionError{
			ResolverName: "my-resolver",
			Phase:        "resolve",
			Provider:     "http",
			Cause:        errors.New("connection refused"),
		}

		var execErr *ExecutionError
		require.True(t, errors.As(err, &execErr))
		assert.Equal(t, "my-resolver", execErr.ResolverName)
		assert.Equal(t, "resolve", execErr.Phase)
	})

	t.Run("AggregatedValidationError", func(t *testing.T) {
		err := &AggregatedValidationError{
			ResolverName: "email",
			Failures: []ValidationFailure{
				{Rule: 0, Message: "invalid format"},
			},
		}

		var valErr *AggregatedValidationError
		require.True(t, errors.As(err, &valErr))
		assert.Equal(t, "email", valErr.ResolverName)
		assert.Len(t, valErr.Failures, 1)
	})
}

func TestIsForEachTypeError(t *testing.T) {
	err := &ForEachTypeError{ResolverName: "myResolver", Step: 0, ActualType: "string"}
	assert.True(t, IsForEachTypeError(err))
	assert.False(t, IsForEachTypeError(errors.New("other error")))
}

func TestFailedResolver_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	fr := &FailedResolver{ResolverName: "test", Phase: 1, Err: cause}
	assert.Equal(t, cause, fr.Unwrap())
}

func TestIsAggregatedExecutionError(t *testing.T) {
	err := &AggregatedExecutionError{
		Errors: []*FailedResolver{{ResolverName: "r1", Phase: 1, Err: errors.New("failed")}},
	}
	assert.True(t, IsAggregatedExecutionError(err))
	assert.False(t, IsAggregatedExecutionError(errors.New("plain error")))
}
