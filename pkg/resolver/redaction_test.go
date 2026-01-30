package resolver

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactForLog(t *testing.T) {
	t.Run("sensitive true", func(t *testing.T) {
		result := redactForLog("secret password", true)
		assert.Equal(t, "[REDACTED]", result)
	})

	t.Run("sensitive false", func(t *testing.T) {
		result := redactForLog("public value", false)
		assert.Equal(t, "public value", result)
	})

	t.Run("empty string sensitive", func(t *testing.T) {
		result := redactForLog("", true)
		assert.Equal(t, "[REDACTED]", result)
	})
}

func TestRedactValue(t *testing.T) {
	t.Run("string sensitive", func(t *testing.T) {
		result := RedactValue("secret", true)
		assert.Equal(t, "[REDACTED]", result)
	})

	t.Run("string not sensitive", func(t *testing.T) {
		result := RedactValue("public", false)
		assert.Equal(t, "public", result)
	})

	t.Run("map sensitive", func(t *testing.T) {
		input := map[string]any{"key": "value"}
		result := RedactValue(input, true)
		assert.Equal(t, "[REDACTED]", result)
	})

	t.Run("map not sensitive", func(t *testing.T) {
		input := map[string]any{"key": "value"}
		result := RedactValue(input, false)
		assert.Equal(t, input, result)
	})

	t.Run("nil sensitive", func(t *testing.T) {
		result := RedactValue(nil, true)
		assert.Equal(t, "[REDACTED]", result)
	})

	t.Run("nil not sensitive", func(t *testing.T) {
		result := RedactValue(nil, false)
		assert.Nil(t, result)
	})

	t.Run("integer sensitive", func(t *testing.T) {
		result := RedactValue(12345, true)
		assert.Equal(t, "[REDACTED]", result)
	})
}

func TestRedactError(t *testing.T) {
	t.Run("sensitive error", func(t *testing.T) {
		original := errors.New("password is 'secret123'")
		result := RedactError(original, true)

		assert.Equal(t, "[REDACTED]", result.Error())

		// Verify original is accessible via Unwrap
		unwrapped := errors.Unwrap(result)
		assert.Equal(t, original, unwrapped)
	})

	t.Run("non-sensitive error", func(t *testing.T) {
		original := errors.New("connection refused")
		result := RedactError(original, false)

		assert.Equal(t, original, result)
	})

	t.Run("nil error", func(t *testing.T) {
		result := RedactError(nil, true)
		assert.Nil(t, result)

		result = RedactError(nil, false)
		assert.Nil(t, result)
	})
}

func TestRedactMapValues(t *testing.T) {
	t.Run("sensitive map", func(t *testing.T) {
		input := map[string]any{
			"username": "admin",
			"password": "secret123",
			"token":    "abc123xyz",
		}

		result := RedactMapValues(input, true)

		// Keys should be preserved
		assert.Contains(t, result, "username")
		assert.Contains(t, result, "password")
		assert.Contains(t, result, "token")

		// Values should be redacted
		assert.Equal(t, "[REDACTED]", result["username"])
		assert.Equal(t, "[REDACTED]", result["password"])
		assert.Equal(t, "[REDACTED]", result["token"])

		// Original map should be unchanged
		assert.Equal(t, "admin", input["username"])
	})

	t.Run("non-sensitive map", func(t *testing.T) {
		input := map[string]any{
			"name": "public",
			"id":   123,
		}

		result := RedactMapValues(input, false)

		// Should return original map
		assert.Equal(t, input, result)
	})

	t.Run("empty map sensitive", func(t *testing.T) {
		input := map[string]any{}

		result := RedactMapValues(input, true)

		assert.Empty(t, result)
	})

	t.Run("nil map", func(t *testing.T) {
		result := RedactMapValues(nil, true)
		assert.Empty(t, result)

		result = RedactMapValues(nil, false)
		assert.Nil(t, result)
	})
}
