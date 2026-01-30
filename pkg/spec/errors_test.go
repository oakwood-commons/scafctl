package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOnErrorBehavior_IsValid(t *testing.T) {
	tests := []struct {
		name     string
		behavior OnErrorBehavior
		expected bool
	}{
		{"fail", OnErrorFail, true},
		{"continue", OnErrorContinue, true},
		{"empty", OnErrorBehavior(""), true},
		{"invalid", OnErrorBehavior("invalid"), false},
		{"uppercase", OnErrorBehavior("FAIL"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.behavior.IsValid()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestOnErrorBehavior_OrDefault(t *testing.T) {
	tests := []struct {
		name     string
		behavior OnErrorBehavior
		expected OnErrorBehavior
	}{
		{"fail returns fail", OnErrorFail, OnErrorFail},
		{"continue returns continue", OnErrorContinue, OnErrorContinue},
		{"empty returns fail", OnErrorBehavior(""), OnErrorFail},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.behavior.OrDefault()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestOnErrorBehavior_Constants(t *testing.T) {
	assert.Equal(t, OnErrorBehavior("fail"), OnErrorFail)
	assert.Equal(t, OnErrorBehavior("continue"), OnErrorContinue)
}
