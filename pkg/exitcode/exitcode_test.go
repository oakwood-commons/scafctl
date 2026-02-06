package exitcode

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExitCodes(t *testing.T) {
	t.Parallel()

	// Verify exit codes have expected values (document contract)
	assert.Equal(t, 0, Success)
	assert.Equal(t, 1, GeneralError)
	assert.Equal(t, 2, ValidationFailed)
	assert.Equal(t, 3, InvalidInput)
	assert.Equal(t, 4, FileNotFound)
	assert.Equal(t, 5, RenderFailed)
	assert.Equal(t, 6, ActionFailed)
	assert.Equal(t, 7, ConfigError)
	assert.Equal(t, 8, CatalogError)
	assert.Equal(t, 9, TimeoutError)
	assert.Equal(t, 10, PermissionDenied)
}

func TestDescription(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code     int
		expected string
	}{
		{Success, "success"},
		{GeneralError, "general error"},
		{ValidationFailed, "validation failed"},
		{InvalidInput, "invalid input"},
		{FileNotFound, "file not found"},
		{RenderFailed, "render failed"},
		{ActionFailed, "action failed"},
		{ConfigError, "configuration error"},
		{CatalogError, "catalog error"},
		{TimeoutError, "timeout"},
		{PermissionDenied, "permission denied"},
		{999, "unknown error"},
		{-1, "unknown error"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, Description(tt.code))
	}
}
