package action

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewErrorContext(t *testing.T) {
	t.Run("HTTP error", func(t *testing.T) {
		err := &HTTPError{StatusCode: 429, Message: "rate limited"}
		ctx := NewErrorContext(err, 2, 5)

		assert.Equal(t, "rate limited", ctx.Message)
		assert.Equal(t, ErrorTypeHTTP, ctx.Type)
		assert.Equal(t, 429, ctx.StatusCode)
		assert.Equal(t, 0, ctx.ExitCode)
		assert.Equal(t, 2, ctx.Attempt)
		assert.Equal(t, 5, ctx.MaxAttempts)
	})

	t.Run("HTTP 500 error", func(t *testing.T) {
		err := &HTTPError{StatusCode: 500, Message: "internal server error"}
		ctx := NewErrorContext(err, 1, 3)

		assert.Equal(t, ErrorTypeHTTP, ctx.Type)
		assert.Equal(t, 500, ctx.StatusCode)
	})

	t.Run("timeout error from context.DeadlineExceeded", func(t *testing.T) {
		err := context.DeadlineExceeded
		ctx := NewErrorContext(err, 1, 3)

		assert.Equal(t, ErrorTypeTimeout, ctx.Type)
		assert.Equal(t, 0, ctx.StatusCode)
		assert.Equal(t, 0, ctx.ExitCode)
	})

	t.Run("timeout error from message", func(t *testing.T) {
		err := errors.New("connection timeout")
		ctx := NewErrorContext(err, 1, 3)

		assert.Equal(t, ErrorTypeTimeout, ctx.Type)
	})

	t.Run("validation error", func(t *testing.T) {
		err := errors.New("validation failed: field required")
		ctx := NewErrorContext(err, 1, 3)

		assert.Equal(t, ErrorTypeValidation, ctx.Type)
	})

	t.Run("invalid error", func(t *testing.T) {
		err := errors.New("invalid input")
		ctx := NewErrorContext(err, 1, 3)

		assert.Equal(t, ErrorTypeValidation, ctx.Type)
	})

	t.Run("unknown error", func(t *testing.T) {
		err := errors.New("something went wrong")
		ctx := NewErrorContext(err, 3, 5)

		assert.Equal(t, "something went wrong", ctx.Message)
		assert.Equal(t, ErrorTypeUnknown, ctx.Type)
		assert.Equal(t, 0, ctx.StatusCode)
		assert.Equal(t, 0, ctx.ExitCode)
		assert.Equal(t, 3, ctx.Attempt)
		assert.Equal(t, 5, ctx.MaxAttempts)
	})

	t.Run("wrapped HTTP error", func(t *testing.T) {
		httpErr := &HTTPError{StatusCode: 503, Message: "service unavailable"}
		wrappedErr := errors.Join(errors.New("request failed"), httpErr)
		ctx := NewErrorContext(wrappedErr, 1, 3)

		assert.Equal(t, ErrorTypeHTTP, ctx.Type)
		assert.Equal(t, 503, ctx.StatusCode)
	})
}

func TestErrorContext_ToMap(t *testing.T) {
	ctx := &ErrorContext{
		Message:     "test error",
		Type:        ErrorTypeHTTP,
		StatusCode:  429,
		ExitCode:    0,
		Attempt:     2,
		MaxAttempts: 5,
	}

	m := ctx.ToMap()

	assert.Equal(t, "test error", m["message"])
	assert.Equal(t, ErrorTypeHTTP, m["type"])
	assert.Equal(t, 429, m["statusCode"])
	assert.Equal(t, 0, m["exitCode"])
	assert.Equal(t, 2, m["attempt"])
	assert.Equal(t, 5, m["maxAttempts"])
}

func TestHTTPError(t *testing.T) {
	err := &HTTPError{StatusCode: 404, Message: "not found"}

	assert.Equal(t, "not found", err.Error())
	assert.Equal(t, 404, err.StatusCode)
}

func TestNewErrorContext_ExecError(t *testing.T) {
	// Create a real exec error by running a command that exits with non-zero
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "sh", "-c", "exit 42")
	err := cmd.Run()
	if err != nil {
		errCtx := NewErrorContext(err, 1, 3)

		assert.Equal(t, ErrorTypeExec, errCtx.Type)
		assert.Equal(t, 42, errCtx.ExitCode)
		assert.Equal(t, 0, errCtx.StatusCode)
	}
}
